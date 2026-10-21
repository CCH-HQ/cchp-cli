//go:build e2e

// End-to-end tests against a real CCHP server started by scripts/e2e/up.sh and
// prepared by scripts/e2e/bootstrap.sh. They run the built cchp binary.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var (
	binary     string
	baseURL    string
	adminToken string
	limitedTok string
)

func TestMain(m *testing.M) {
	baseURL = mustEnv("CCHP_E2E_BASE_URL")
	adminToken = readSecret(mustEnv("CCHP_E2E_ADMIN_TOKEN_FILE"))
	limitedTok = readSecret(mustEnv("CCHP_E2E_LIMITED_TOKEN_FILE"))
	dir, err := os.MkdirTemp("", "cchp-e2e-bin-")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "cchp")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/cchp")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		panic(name + " is not set; run scripts/e2e/up.sh and scripts/e2e/bootstrap.sh")
	}
	return v
}

func readSecret(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(string(data))
}

type result struct {
	OK         bool            `json:"ok"`
	Operation  string          `json:"operation"`
	Status     int             `json:"status"`
	RequestID  string          `json:"request_id"`
	Data       json.RawMessage `json:"data"`
	Outcome    string          `json:"outcome"`
	Pagination *struct {
		Pages      int    `json:"pages"`
		Items      int    `json:"items"`
		HasMore    bool   `json:"has_more"`
		NextCursor string `json:"next_cursor"`
	} `json:"pagination"`
	Secret *struct {
		Path   string   `json:"path"`
		Fields []string `json:"fields"`
	} `json:"secret"`
	File *struct {
		Path  string `json:"path"`
		Bytes int64  `json:"bytes"`
	} `json:"file"`
	DryRun *struct {
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Body    json.RawMessage   `json:"body"`
	} `json:"dry_run"`
	Error *struct {
		Kind               string `json:"kind"`
		Message            string `json:"message"`
		RequiredPermission string `json:"required_permission"`
		ChallengeAction    string `json:"challenge_action"`
		Fields             []struct {
			Location string `json:"location"`
			Message  string `json:"message"`
		} `json:"fields"`
	} `json:"error"`
}

type run struct {
	exit   int
	stdout string
	stderr string
	res    result
}

// cchp runs the binary in agent mode with an isolated config directory.
func cchp(t *testing.T, env []string, stdin string, args ...string) run {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	r := run{stdout: stdout.String(), stderr: stderr.String()}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		r.exit = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running cchp %v: %v", args, err)
	}
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	if last := lines[len(lines)-1]; strings.HasPrefix(last, "{") {
		if err := json.Unmarshal([]byte(last), &r.res); err != nil {
			t.Fatalf("cchp %v printed invalid JSON: %v\n%s", args, err, r.stdout)
		}
	}
	return r
}

// session is an isolated cchp home signed in with the admin token.
type session struct {
	env []string
}

func newSession(t *testing.T) *session {
	t.Helper()
	dir := t.TempDir()
	env := []string{
		"CCHP_CONFIG_DIR=" + filepath.Join(dir, "config"),
		"CCHP_CACHE_DIR=" + filepath.Join(dir, "cache"),
		"CCHP_CREDENTIAL_STORE=file",
		"CCHP_TOKEN=", "CCHP_TOKEN_FILE=", "CCHP_BASE_URL=",
	}
	s := &session{env: env}
	r := cchp(t, env, adminToken, "login", "--server", baseURL, "--token-stdin", "--json")
	if r.exit != 0 || !r.res.OK {
		t.Fatalf("login failed: %d %s %s", r.exit, r.stdout, r.stderr)
	}
	return s
}

func (s *session) run(t *testing.T, stdin string, args ...string) run {
	t.Helper()
	return cchp(t, s.env, stdin, append(args, "--json")...)
}

func expectOK(t *testing.T, r run) {
	t.Helper()
	if r.exit != 0 || !r.res.OK {
		t.Fatalf("want success, got exit %d\nstdout: %s\nstderr: %s", r.exit, r.stdout, r.stderr)
	}
}

func expectFail(t *testing.T, r run, exit int, kind string) {
	t.Helper()
	if r.exit != exit || r.res.OK || r.res.Error == nil || r.res.Error.Kind != kind {
		t.Fatalf("want exit %d kind %s, got exit %d\nstdout: %s\nstderr: %s", exit, kind, r.exit, r.stdout, r.stderr)
	}
	if strings.TrimSpace(r.stderr) == "" {
		t.Fatal("failures must explain themselves on stderr")
	}
}

func TestLoginStatusWhoami(t *testing.T) {
	s := newSession(t)
	if strings.Contains(s.run(t, "", "status").stdout, adminToken) {
		t.Fatal("status printed the token")
	}
	r := s.run(t, "", "status")
	expectOK(t, r)
	var st struct {
		Server     string `json:"server"`
		TokenWorks bool   `json:"token_works"`
		Identity   struct {
			OrganizationID string `json:"organization_id"`
			TokenKind      string `json:"token_kind"`
		} `json:"identity"`
	}
	if err := json.Unmarshal(r.res.Data, &st); err != nil {
		t.Fatal(err)
	}
	if !st.TokenWorks || st.Identity.TokenKind != "management-api" || st.Identity.OrganizationID == "" {
		t.Fatalf("status %+v", st)
	}
	r = s.run(t, "", "whoami")
	expectOK(t, r)
	if r.res.Operation != "getWhoami" || r.res.Status != 200 || r.res.RequestID == "" {
		t.Fatalf("whoami %+v", r.res)
	}
	// A wrong token is rejected and nothing is saved.
	bad := cchp(t, s.env, "cch_auth_live_NOPE_nope", "login", "--server", baseURL, "--token-stdin", "--json")
	expectFail(t, bad, 3, "unauthorized")
	expectOK(t, s.run(t, "", "status"))
	r = s.run(t, "", "logout")
	expectOK(t, r)
	expectFail(t, s.run(t, "", "status"), 14, "not_signed_in")
}

func TestEnvironmentCredentials(t *testing.T) {
	dir := t.TempDir()
	env := []string{"CCHP_CONFIG_DIR=" + dir, "CCHP_CACHE_DIR=" + filepath.Join(dir, "cache"), "CCHP_BASE_URL=" + baseURL, "CCHP_TOKEN=" + adminToken, "CCHP_TOKEN_FILE="}
	expectOK(t, cchp(t, env, "", "whoami", "--json"))
}

func TestDiscoveryWorksOffline(t *testing.T) {
	dir := t.TempDir()
	env := []string{"CCHP_CONFIG_DIR=" + dir, "CCHP_CACHE_DIR=" + dir, "CCHP_BASE_URL=", "CCHP_TOKEN=", "CCHP_TOKEN_FILE="}
	r := cchp(t, env, "", "commands", "--search", "provider keys", "--json")
	expectOK(t, r)
	var list struct {
		Count    int `json:"count"`
		Commands []struct {
			Operation string `json:"operation"`
			Command   string `json:"command"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(r.res.Data, &list); err != nil {
		t.Fatal(err)
	}
	if list.Count == 0 || list.Commands[0].Operation != "listProviderKeys" || list.Commands[0].Command != "cchp keys list" {
		t.Fatalf("search %+v", list)
	}
	r = cchp(t, env, "", "schema", "createAPIKey", "--json")
	expectOK(t, r)
	if !strings.Contains(string(r.res.Data), `"body_schema"`) || !strings.Contains(string(r.res.Data), `"permissions"`) {
		t.Fatalf("schema %s", r.res.Data)
	}
	r = cchp(t, env, "", "commands", "show", "deleteOrgProvider", "--json")
	expectOK(t, r)
	if !strings.Contains(string(r.res.Data), "--confirm deleteOrgProvider") {
		t.Fatalf("show %s", r.res.Data)
	}
	r = cchp(t, env, "", "version", "--json")
	expectOK(t, r)
	if !strings.Contains(string(r.res.Data), `"tag":"v1.0.10"`) {
		t.Fatalf("version %s", r.res.Data)
	}
	expectFail(t, cchp(t, env, "", "api", "noSuchOperation", "--json"), 2, "usage")
}

func TestAPICallsAndHumanAliasesShareOnePath(t *testing.T) {
	s := newSession(t)
	a := s.run(t, "", "api", "listOrgProviders", "--limit", "1")
	b := s.run(t, "", "providers", "list", "--limit", "1")
	expectOK(t, a)
	expectOK(t, b)
	if a.res.Operation != "listOrgProviders" || b.res.Operation != "listOrgProviders" {
		t.Fatalf("operations %s %s", a.res.Operation, b.res.Operation)
	}
}

func createProvider(t *testing.T, s *session, name string) string {
	t.Helper()
	r := s.run(t, `{"name":"`+name+`"}`, "api", "createOrgProvider", "--body-file", "-", "--allow-write")
	expectOK(t, r)
	if r.res.Outcome != "applied" {
		t.Fatalf("outcome %q", r.res.Outcome)
	}
	var out struct {
		Provider struct {
			ID string `json:"id"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(r.res.Data, &out); err != nil {
		t.Fatal(err)
	}
	return out.Provider.ID
}

func TestMutationNeedsAllowWriteAndBodiesArriveExactly(t *testing.T) {
	s := newSession(t)
	body := `{"name":"Exact ` + fmt.Sprint(time.Now().UnixNano()) + `","notes":"","enabled":false,"cost_multiplier_ppm":0}`
	expectFail(t, s.run(t, body, "api", "createOrgProvider", "--body-file", "-"), 13, "policy")
	r := s.run(t, body, "api", "createOrgProvider", "--body-file", "-", "--allow-write")
	expectOK(t, r)
	var out struct {
		Provider struct {
			ID                string `json:"id"`
			Enabled           *bool  `json:"enabled"`
			CostMultiplierPPM *int64 `json:"cost_multiplier_ppm"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(r.res.Data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Provider.Enabled == nil || *out.Provider.Enabled || out.Provider.CostMultiplierPPM == nil || *out.Provider.CostMultiplierPPM != 0 {
		t.Fatalf("false and 0 did not arrive: %s", r.res.Data)
	}
	// Deleting needs --confirm naming the operation.
	expectFail(t, s.run(t, "", "api", "deleteOrgProvider", "--provider-id", out.Provider.ID, "--allow-write"), 13, "policy")
	expectFail(t, s.run(t, "", "api", "deleteOrgProvider", "--provider-id", out.Provider.ID, "--allow-write", "--confirm", "deleteModel"), 13, "policy")
	expectOK(t, s.run(t, "", "api", "deleteOrgProvider", "--provider-id", out.Provider.ID, "--allow-write", "--confirm", "deleteOrgProvider"))
}

func TestPagination(t *testing.T) {
	s := newSession(t)
	for i := 0; i < 3; i++ {
		createProvider(t, s, fmt.Sprintf("Page %d %d", i, time.Now().UnixNano()))
	}
	r := s.run(t, "", "api", "listOrgProviders", "--limit", "2")
	expectOK(t, r)
	p := r.res.Pagination
	if p == nil || p.Pages != 1 || p.Items != 2 || !p.HasMore || p.NextCursor == "" {
		t.Fatalf("first page %+v", p)
	}
	next := s.run(t, "", "api", "listOrgProviders", "--limit", "2", "--cursor", p.NextCursor)
	expectOK(t, next)
	if next.res.Pagination.Items == 0 || string(next.res.Data) == string(r.res.Data) {
		t.Fatalf("second page %+v", next.res.Pagination)
	}
	all := s.run(t, "", "api", "listOrgProviders", "--limit", "2", "--page-limit", "50")
	expectOK(t, all)
	if all.res.Pagination.HasMore || all.res.Pagination.Pages < 2 || all.res.Pagination.Items < 3 {
		t.Fatalf("all pages %+v", all.res.Pagination)
	}
	some := s.run(t, "", "api", "listOrgProviders", "--limit", "2", "--page-limit", "50", "--max-items", "3")
	expectOK(t, some)
	if some.res.Pagination.Items != 3 || !some.res.Pagination.HasMore {
		t.Fatalf("max items %+v", some.res.Pagination)
	}
}

func TestPermissionDenied(t *testing.T) {
	dir := t.TempDir()
	env := []string{"CCHP_CONFIG_DIR=" + dir, "CCHP_CACHE_DIR=" + dir, "CCHP_BASE_URL=" + baseURL, "CCHP_TOKEN=" + limitedTok, "CCHP_TOKEN_FILE="}
	r := cchp(t, env, "", "api", "listUsers", "--json")
	expectFail(t, r, 4, "permission_denied")
	if r.res.Status != 403 || r.res.RequestID == "" || r.res.Error.RequiredPermission != "member:read" ||
		r.res.Error.Message != "Your token can't do this. Ask an admin for the member:read permission." {
		t.Fatalf("403 %+v %+v", r.res, r.res.Error)
	}
}

func TestInvalidInput(t *testing.T) {
	s := newSession(t)
	// Caught locally against the schema, before anything is sent.
	r := s.run(t, `{"name":5}`, "api", "createAPIKey", "--body-file", "-", "--allow-write", "--confirm", "createAPIKey", "--secret-out", filepath.Join(t.TempDir(), "s.json"))
	expectFail(t, r, 7, "invalid_input")
	locations := map[string]bool{}
	for _, f := range r.res.Error.Fields {
		locations[f.Location] = true
	}
	if !locations["body.name"] || !locations["body"] {
		t.Fatalf("fields %+v", r.res.Error.Fields)
	}
	// Rejected by the server with 422.
	r = s.run(t, `{"name":"x","permissions":["no:such"]}`, "api", "createAPIKey", "--body-file", "-", "--allow-write", "--confirm", "createAPIKey", "--secret-out", filepath.Join(t.TempDir(), "s.json"))
	expectFail(t, r, 7, "validation")
	if r.res.Status != 422 || r.res.Outcome != "not_applied" {
		t.Fatalf("422 %+v", r.res)
	}
}

func setChallengeMode(t *testing.T, s *session, mode string) {
	t.Helper()
	body := `{"values":{"security.challenge.action.auth_token_create.mode":"` + mode + `"}}`
	r := s.run(t, body, "api", "saveSystemConfig", "--body-file", "-", "--allow-write", "--confirm", "saveSystemConfig")
	expectOK(t, r)
}

func TestHumanVerificationIsReportedPlainly(t *testing.T) {
	s := newSession(t)
	setChallengeMode(t, s, "enforce")
	defer setChallengeMode(t, s, "off")
	secret := filepath.Join(t.TempDir(), "s.json")
	r := s.run(t, `{"name":"challenge test","permissions":["usage:read"]}`, "api", "createAPIKey", "--body-file", "-", "--allow-write", "--confirm", "createAPIKey", "--secret-out", secret)
	expectFail(t, r, 8, "human_verification_required")
	if r.res.Status != 428 || r.res.Error.ChallengeAction != "auth_token.create" ||
		r.res.Error.Message != "This server needs you to complete a human check in the browser for this action." {
		t.Fatalf("428 %+v %+v", r.res, r.res.Error)
	}
	if _, err := os.Stat(secret); !os.IsNotExist(err) {
		t.Fatal("no secret file may exist after a refused call")
	}
}

func TestSecretGoesOnlyToTheSecretFile(t *testing.T) {
	s := newSession(t)
	secret := filepath.Join(t.TempDir(), "token.json")
	r := s.run(t, `{"name":"secret test","permissions":["usage:read"]}`, "api", "createAPIKey", "--body-file", "-", "--allow-write", "--confirm", "createAPIKey", "--secret-out", secret)
	expectOK(t, r)
	if strings.Contains(r.stdout, "cch_auth_live_") || strings.Contains(r.stderr, "cch_auth_live_") {
		t.Fatal("the new token reached stdout or stderr")
	}
	if r.res.Secret == nil || r.res.Secret.Path != secret || len(r.res.Secret.Fields) != 1 {
		t.Fatalf("secret %+v", r.res.Secret)
	}
	info, err := os.Stat(secret)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secret file %v %v", info, err)
	}
	data, _ := os.ReadFile(secret)
	if !strings.Contains(string(data), "cch_auth_live_") {
		t.Fatalf("secret file content %s", data)
	}
	// The new token works.
	var saved struct {
		Values struct {
			Plaintext string `json:"plaintext"`
		} `json:"values"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	env := []string{"CCHP_CONFIG_DIR=" + t.TempDir(), "CCHP_BASE_URL=" + baseURL, "CCHP_TOKEN=" + saved.Values.Plaintext, "CCHP_TOKEN_FILE="}
	expectOK(t, cchp(t, env, "", "whoami", "--json"))
}

// countingProxy is a real reverse proxy that counts what passes through it.
func countingProxy(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	target, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	var n atomic.Int64
	rp := httputil.NewSingleHostReverseProxy(target)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		rp.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func TestDryRunSendsZeroRequests(t *testing.T) {
	proxy, count := countingProxy(t)
	dir := t.TempDir()
	env := []string{"CCHP_CONFIG_DIR=" + filepath.Join(dir, "c"), "CCHP_CACHE_DIR=" + filepath.Join(dir, "k"), "CCHP_CREDENTIAL_STORE=file", "CCHP_TOKEN=", "CCHP_TOKEN_FILE=", "CCHP_BASE_URL="}
	login := cchp(t, env, adminToken, "login", "--server", proxy.URL, "--token-stdin", "--json")
	expectOK(t, login)
	before := count.Load()
	r := cchp(t, env, `{"name":"Dry","api_key":"sk-should-not-leak"}`, "api", "createOrgProvider", "--body-file", "-", "--dry-run", "--json")
	if r.exit != 7 {
		t.Fatalf("an invalid body is refused even in a dry run: %s", r.stdout)
	}
	r = cchp(t, env, `{"name":"Dry run","notes":"keep"}`, "api", "deleteOrgProvider", "--provider-id", "p1", "--dry-run", "--json")
	expectOK(t, r)
	r = cchp(t, env, `{"name":"Dry run","notes":"keep"}`, "api", "createOrgProvider", "--body-file", "-", "--dry-run", "--json")
	expectOK(t, r)
	if got := count.Load() - before; got != 0 {
		t.Fatalf("dry runs sent %d requests", got)
	}
	if r.res.DryRun == nil || r.res.DryRun.Method != "POST" || !strings.HasPrefix(r.res.DryRun.URL, proxy.URL+"/api/account/orgs/") {
		t.Fatalf("dry run %+v", r.res.DryRun)
	}
	if strings.Contains(r.stdout, adminToken) || r.res.DryRun.Headers["Authorization"] != "Bearer [redacted]" {
		t.Fatalf("dry run exposed the token: %s", r.stdout)
	}
	if string(r.res.DryRun.Body) != `{"name":"Dry run","notes":"keep"}` {
		t.Fatalf("dry run body %s", r.res.DryRun.Body)
	}
	expectOK(t, cchp(t, env, "", "whoami", "--json"))
	if got := count.Load() - before; got != 1 {
		t.Fatalf("a real call should send exactly one request, sent %d", got)
	}
}

func TestDownloadsAndStreams(t *testing.T) {
	s := newSession(t)
	out := filepath.Join(t.TempDir(), "events.csv")
	expectFail(t, s.run(t, "", "api", "exportFinanceEvents"), 2, "usage")
	r := s.run(t, "", "api", "exportFinanceEvents", "--output", out)
	expectOK(t, r)
	if r.res.File == nil || r.res.File.Path != out {
		t.Fatalf("file %+v", r.res.File)
	}
	info, err := os.Stat(out)
	if err != nil || info.Size() != r.res.File.Bytes {
		t.Fatalf("download %v %v", info, err)
	}
	expectFail(t, s.run(t, "", "api", "exportFinanceEvents", "--output", out), 2, "usage")

	r = s.run(t, "", "api", "streamSystemHealth", "--max-events", "2", "--timeout", "30s")
	expectOK(t, r)
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], `"type":"event"`) || !strings.Contains(lines[2], `"ended":"max_events"`) {
		t.Fatalf("stream output:\n%s", r.stdout)
	}
}

func TestSkillInstall(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills", "cchp")
	r := cchp(t, nil, "", "skill", "install", "--dir", dir, "--json")
	expectOK(t, r)
	for _, f := range []string{"SKILL.md", "references/auth.md", "references/output-contract.md", "references/pagination.md", "references/errors.md", "references/workflows.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	expectOK(t, cchp(t, nil, "", "skill", "install", "--dir", dir, "--json"))
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectFail(t, cchp(t, nil, "", "skill", "install", "--dir", dir, "--json"), 2, "usage")
	expectOK(t, cchp(t, nil, "", "skill", "install", "--dir", dir, "--force", "--json"))
	r = cchp(t, nil, "", "skill", "show", "--agents-md")
	if r.exit != 0 || !strings.Contains(r.stdout, "cchp commands --search") {
		t.Fatalf("agents-md %s", r.stdout)
	}
}

func TestBrowserOnlyOperationsAreExplained(t *testing.T) {
	s := newSession(t)
	r := s.run(t, "", "api", "getMFAStatus")
	expectFail(t, r, 8, "browser_session_required")
	if r.res.Error.Message != "This server needs you to sign in from the browser for this action." {
		t.Fatalf("message %q", r.res.Error.Message)
	}
}
