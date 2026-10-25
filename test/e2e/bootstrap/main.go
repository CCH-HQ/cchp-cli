// Command bootstrap prepares a fresh CCHP server for the cchp end-to-end tests.
//
// It redeems the installation setup token, creates the first platform
// administrator through the real setup API, signs in from a browser-style
// session, and creates management API tokens through POST /api/account/api-keys.
// Tokens are written to mode-600 files under CCHP_E2E_DIR and never printed.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type session struct {
	base    string
	origin  string
	client  *http.Client
	cookies map[string]string
	csrf    string
	org     string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap:", err)
		os.Exit(1)
	}
}

func run() error {
	base := mustEnv("CCHP_E2E_BASE_URL")
	origin := mustEnv("CCHP_E2E_PUBLIC_ORIGIN")
	dir := mustEnv("CCHP_E2E_DIR")

	s := &session{base: base, origin: origin, client: &http.Client{Timeout: 30 * time.Second}, cookies: map[string]string{}}

	var status struct {
		NeedsBootstrap    bool `json:"needsBootstrap"`
		NeedsOrganization bool `json:"needsOrganization"`
		Completed         bool `json:"completed"`
	}
	if err := s.call(http.MethodGet, "/api/setup/status", nil, nil, &status); err != nil {
		return err
	}
	password := filepath.Join(dir, "admin-password")
	if status.NeedsBootstrap {
		if err := s.install(password); err != nil {
			return err
		}
	} else if err := s.signIn(password); err != nil {
		return err
	}
	if err := s.finishSetup(); err != nil {
		return err
	}

	if err := s.selectOrganization(); err != nil {
		return err
	}
	var catalog struct {
		Modules []struct {
			Actions []struct {
				Key       string `json:"key"`
				Grantable bool   `json:"grantable"`
			} `json:"actions"`
		} `json:"modules"`
	}
	if err := s.call(http.MethodGet, "/api/account/api-keys/permission-catalog", nil, nil, &catalog); err != nil {
		return err
	}
	var all []string
	for _, m := range catalog.Modules {
		for _, a := range m.Actions {
			if a.Grantable {
				all = append(all, a.Key)
			}
		}
	}
	if len(all) == 0 {
		return errors.New("permission catalog returned no grantable permissions")
	}
	if err := s.createToken(filepath.Join(dir, "admin.token"), "cchp-cli e2e admin", all); err != nil {
		return err
	}
	// A token that can read its own identity and nothing else, for permission-denied tests.
	if err := s.createToken(filepath.Join(dir, "limited.token"), "cchp-cli e2e limited", []string{"announcement:read"}); err != nil {
		return err
	}
	fmt.Println("bootstrap: tokens written to", dir)
	return nil
}

func (s *session) install(passwordFile string) error {
	setupToken, err := gatewaySetupToken()
	if err != nil {
		return err
	}
	if err := s.csrfToken(); err != nil {
		return err
	}
	if err := s.call(http.MethodPost, "/api/setup/token", map[string]any{"token": setupToken}, nil, nil); err != nil {
		return err
	}
	pw := randomHex(16) + "Aa1!"
	if err := os.WriteFile(passwordFile, []byte(pw), 0o600); err != nil {
		return err
	}
	if err := s.call(http.MethodPost, "/api/setup/site", map[string]any{"siteName": "CCHP e2e"}, nil, nil); err != nil {
		return err
	}
	body := map[string]any{"display_name": "E2E Admin", "email": "admin@e2e.example.com", "password": pw}
	if err := s.callWithChallenge(http.MethodPost, "/api/setup/administrator", body, nil); err != nil {
		return err
	}
	return s.refreshSessionCSRF()
}

// selectOrganization waits until the first organization exists (it is created
// asynchronously) and sends its ID as X-CCH-Organization, as the console does.
func (s *session) selectOrganization() error {
	for range 60 {
		var me struct {
			Organizations []struct {
				ID string `json:"id"`
			} `json:"organizations"`
		}
		if err := s.call(http.MethodGet, "/api/account/me", nil, nil, &me); err != nil {
			return err
		}
		for _, o := range me.Organizations {
			if o.ID != "" && o.ID != "00000000-0000-0000-0000-000000000000" {
				s.org = o.ID
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("the first organization did not appear")
}

// finishSetup walks the remaining installation steps: optional steps are
// skipped, and creating the first organization completes the installation once
// the server has created it.
func (s *session) finishSetup() error {
	orgRequested := false
	for range 120 {
		var status struct {
			Step      string `json:"step"`
			Completed bool   `json:"completed"`
		}
		if err := s.call(http.MethodGet, "/api/setup/status", nil, nil, &status); err != nil {
			return err
		}
		if status.Completed {
			return nil
		}
		var err error
		switch status.Step {
		case "mfa", "email", "database":
			err = s.call(http.MethodPost, "/api/setup/progress", map[string]any{"step": status.Step, "skipped": true}, idempotency(), nil)
		case "login_methods":
			err = s.call(http.MethodPost, "/api/setup/progress", map[string]any{"step": status.Step, "skipped": false}, idempotency(), nil)
		case "organization":
			if orgRequested {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			err = s.call(http.MethodPost, "/api/setup/create-org", map[string]any{"orgName": "E2E Org", "teamName": "E2E Team"}, idempotency(), nil)
			if err == nil {
				orgRequested = true
				err = s.refreshSessionCSRF()
			}
		default:
			err = fmt.Errorf("unexpected setup step %q", status.Step)
		}
		if err != nil {
			return err
		}
	}
	return errors.New("setup did not complete")
}

func (s *session) signIn(passwordFile string) error {
	pw, err := os.ReadFile(passwordFile)
	if err != nil {
		return err
	}
	if err := s.csrfToken(); err != nil {
		return err
	}
	body := map[string]any{"email": "admin@e2e.example.com", "password": string(pw)}
	if err := s.callWithChallenge(http.MethodPost, "/api/auth/sign-in/password", body, nil); err != nil {
		return err
	}
	return s.refreshSessionCSRF()
}

func (s *session) createToken(path, name string, permissions []string) error {
	var out struct {
		Plaintext string `json:"plaintext"`
	}
	body := map[string]any{"name": name + " " + time.Now().UTC().Format("20060102T150405"), "permissions": permissions}
	if err := s.call(http.MethodPost, "/api/account/api-keys", body, idempotency(), &out); err != nil {
		return err
	}
	if out.Plaintext == "" {
		return errors.New("createAPIKey returned no plaintext")
	}
	return os.WriteFile(path, []byte(out.Plaintext), 0o600)
}

// csrfToken fetches the anonymous pre-login CSRF token the way a browser does.
func (s *session) csrfToken() error {
	var out struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := s.call(http.MethodGet, "/api/auth/csrf", nil, nil, &out); err != nil {
		return err
	}
	s.csrf = out.CSRFToken
	return nil
}

// refreshSessionCSRF reads the CSRF token bound to the signed-in user session.
func (s *session) refreshSessionCSRF() error {
	var out struct {
		CSRFToken *string `json:"csrf_token"`
	}
	if err := s.call(http.MethodGet, "/api/account/me", nil, nil, &out); err != nil {
		return err
	}
	if out.CSRFToken == nil || *out.CSRFToken == "" {
		return errors.New("session has no csrf token")
	}
	s.csrf = *out.CSRFToken
	return nil
}

func (s *session) call(method, path string, body any, headers map[string]string, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, s.base+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", s.origin)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	if s.csrf != "" {
		req.Header.Set("X-CSRF-Token", s.csrf)
	}
	if s.org != "" {
		req.Header.Set("X-CCH-Organization", s.org)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	var parts []string
	for k, v := range s.cookies {
		parts = append(parts, k+"="+v)
	}
	if len(parts) > 0 {
		req.Header.Set("Cookie", strings.Join(parts, "; "))
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(resp.Body)
	if cerr := resp.Body.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	// Browsers keep __Host- cookies for http://localhost; net/http's jar would not.
	for _, c := range resp.Cookies() {
		if c.MaxAge < 0 || c.Value == "" {
			delete(s.cookies, c.Name)
			continue
		}
		s.cookies[c.Name] = c.Value
	}
	if resp.StatusCode >= 300 {
		return &httpError{method: method, path: path, status: resp.StatusCode, body: data}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func gatewaySetupToken() (string, error) {
	cmd := exec.Command(mustEnv("CCHP_E2E_GATEWAY_BIN"), "identity", "setup-token")
	cmd.Dir = mustEnv("CCHP_E2E_SRC")
	cmd.Env = append(os.Environ(),
		"DATABASE_URL="+mustEnv("CCHP_E2E_DATABASE_URL"),
		"CCHP_DATABASE_RUNTIME_ROLE=cchp_runtime",
		"APP_ENV=development",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gateway identity setup-token: %w: %s", err, stderr.String())
	}
	return strings.TrimSpace(string(out)), nil
}

func idempotency() map[string]string {
	return map[string]string{"Idempotency-Key": randomHex(16)}
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func mustEnv(name string) string {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		fmt.Fprintf(os.Stderr, "bootstrap: %s is not set\n", name)
		os.Exit(2)
	}
	return v
}
