package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/selfupdate"
)

func makeTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     0o755,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func startCLIFakeGitHub(t *testing.T, tag string, archiveName string, archiveBytes []byte, checksumsOverride string, seenAuth *[]string) *httptest.Server {
	t.Helper()
	sumBytes := sha256.Sum256(archiveBytes)
	checksums := checksumsOverride
	if checksums == "" {
		checksums = fmt.Sprintf("%s  %s\n", hex.EncodeToString(sumBytes[:]), archiveName)
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seenAuth != nil {
			if auth := r.Header.Get("Authorization"); auth != "" {
				*seenAuth = append(*seenAuth, auth)
			}
		}
		switch r.URL.Path {
		case "/repos/CCH-HQ/cchp-cli/releases/latest",
			"/repos/CCH-HQ/cchp-cli/releases/tags/" + tag:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(selfupdate.Release{
				TagName: tag,
				HTMLURL: "https://github.com/CCH-HQ/cchp-cli/releases/tag/" + tag,
				Assets: []selfupdate.Asset{
					{Name: archiveName, BrowserDownloadURL: srv.URL + "/dl/" + archiveName, Size: int64(len(archiveBytes))},
					{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/dl/checksums.txt", Size: int64(len(checksums))},
				},
			})
		case "/dl/" + archiveName:
			_, _ = w.Write(archiveBytes)
		case "/dl/checksums.txt":
			_, _ = w.Write([]byte(checksums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runUpdateCLI(t *testing.T, version string, humanTTY bool, env map[string]string, cfgFn func(*selfupdate.Config), args ...string) (int, string, string) {
	t.Helper()
	dir := t.TempDir()
	for k, v := range map[string]string{
		"CCHP_CONFIG_DIR":       dir + "/config",
		"CCHP_CACHE_DIR":        dir + "/cache",
		"CCHP_CREDENTIAL_STORE": "file",
		"CCHP_AGENT":            "",
		"CCHP_DOCS_LANG":        "",
		"LC_ALL":                "",
		"LC_MESSAGES":           "",
		"LANG":                  "",
		"NO_COLOR":              "1",
	} {
		t.Setenv(k, v)
	}
	for k, v := range env {
		t.Setenv(k, v)
	}

	var out, errOut bytes.Buffer
	code := runWith(args, IO{
		In:     strings.NewReader(""),
		Out:    &out,
		Err:    &errOut,
		OutTTY: humanTTY,
		InTTY:  humanTTY,
	}, BuildInfo{Version: version}, func(a *app) {
		a.configureUpdater = func(cfg *selfupdate.Config) {
			zero := 0
			cfg.RetryMax = &zero
			if cfgFn != nil {
				cfgFn(cfg)
			}
		}
	})
	return code, out.String(), errOut.String()
}

func parseJSONResult(t *testing.T, raw string) map[string]any {
	t.Helper()
	var res map[string]any
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("expected valid JSON result, got %q: %v", raw, err)
	}
	if res["schema_version"] != float64(contract.SchemaVersion) {
		t.Fatalf("schema_version = %v, want %d", res["schema_version"], contract.SchemaVersion)
	}
	return res
}

func TestCLIUpdateCheckDryRunAndDirectSuccess(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "cchp")
	origBin := []byte("old-cchp-v0.1.4")
	newBin := []byte("new-cchp-v0.2.0")
	if err := os.WriteFile(binPath, origBin, 0o755); err != nil {
		t.Fatal(err)
	}

	archiveName := "cchp_0.2.0_linux_amd64.tar.gz"
	archiveBytes := makeTarGz(t, "cchp", newBin)
	var seenAuth []string
	srv := startCLIFakeGitHub(t, "v0.2.0", archiveName, archiveBytes, "", &seenAuth)

	cfgFn := func(cfg *selfupdate.Config) {
		cfg.APIBaseURL = srv.URL
		cfg.Executable = func() (string, error) { return binPath, nil }
		cfg.LookPath = func(string) (string, error) { return "", errors.New("none") }
		cfg.GOOS = "linux"
		cfg.GOARCH = "amd64"
	}

	// Ensure zero auth is required and CCHP_TOKEN is never sent to GitHub.
	env := map[string]string{
		"CCHP_BASE_URL": "https://hub.example.com",
		"CCHP_TOKEN":    "cchp_secret_mgmt_token_12345",
	}

	// 1. --check in JSON mode
	code, stdout, _ := runUpdateCLI(t, "0.1.4", false, env, cfgFn, "update", "--check", "--json")
	if code != contract.ExitOK {
		t.Fatalf("--check --json exit = %d, stdout=%q", code, stdout)
	}
	res := parseJSONResult(t, stdout)
	data, _ := res["data"].(map[string]any)
	if res["ok"] != true || data["update_available"] != true || data["current_version"] != "v0.1.4" || data["latest_version"] != "v0.2.0" || data["check_only"] != true {
		t.Fatalf("unexpected --check --json data: %v", res)
	}

	// 2. --check in human mode
	code, stdout, _ = runUpdateCLI(t, "0.1.4", true, env, cfgFn, "update", "--check")
	if code != contract.ExitOK || !strings.Contains(stdout, "v0.1.4") || !strings.Contains(stdout, "v0.2.0") {
		t.Fatalf("--check human exit=%d stdout=%q", code, stdout)
	}

	// 3. --dry-run in JSON and human modes leaves binary untouched
	code, stdout, _ = runUpdateCLI(t, "0.1.4", false, env, cfgFn, "update", "--dry-run", "--json")
	if code != contract.ExitOK {
		t.Fatalf("--dry-run --json exit=%d stdout=%q", code, stdout)
	}
	res = parseJSONResult(t, stdout)
	data, _ = res["data"].(map[string]any)
	if res["ok"] != true || data["dry_run"] != true || data["updated"] != false || data["archive_name"] != archiveName {
		t.Fatalf("unexpected --dry-run --json data: %v", res)
	}
	if got, _ := os.ReadFile(binPath); !bytes.Equal(got, origBin) {
		t.Fatalf("--dry-run modified binary!")
	}

	code, stdout, _ = runUpdateCLI(t, "0.1.4", true, env, cfgFn, "update", "--dry-run")
	if code != contract.ExitOK || !strings.Contains(stdout, "Dry run") || !strings.Contains(stdout, archiveName) {
		t.Fatalf("--dry-run human exit=%d stdout=%q", code, stdout)
	}

	// 4. Actual update in human mode replaces the binary
	code, stdout, _ = runUpdateCLI(t, "0.1.4", true, env, cfgFn, "update")
	if code != contract.ExitOK || !strings.Contains(stdout, "v0.1.4 -> v0.2.0") {
		t.Fatalf("update human exit=%d stdout=%q", code, stdout)
	}
	if got, _ := os.ReadFile(binPath); !bytes.Equal(got, newBin) {
		t.Fatalf("binary after update = %q, want %q", got, newBin)
	}
	if len(seenAuth) > 0 {
		t.Fatalf("CCHP auth leaked to GitHub: %v", seenAuth)
	}
}

func TestCLIUpdateFailureOutputsDocsURLInHumanAndJSON(t *testing.T) {
	t.Run("HTTP 500 with default en and zh-CN locale", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"internal error"}`))
		}))
		defer srv.Close()

		cfgFn := func(cfg *selfupdate.Config) {
			cfg.APIBaseURL = srv.URL
			cfg.Executable = func() (string, error) { return "/usr/local/bin/cchp", nil }
			cfg.EvalSymlinks = func(p string) (string, error) { return p, nil }
			cfg.LookPath = func(string) (string, error) { return "", errors.New("none") }
			cfg.GOOS = "linux"
			cfg.GOARCH = "amd64"
		}

		// Default English docs link
		wantEN := "https://cch-plus.com/en/docs/cli#install"
		code, stdout, stderr := runUpdateCLI(t, "0.1.4", true, nil, cfgFn, "update")
		if code != contract.ExitServer {
			t.Fatalf("human HTTP 500 exit=%d, want %d", code, contract.ExitServer)
		}
		if !strings.Contains(stderr, wantEN) && !strings.Contains(stdout, wantEN) {
			t.Fatalf("expected docs URL %q in human output, stdout=%q stderr=%q", wantEN, stdout, stderr)
		}

		code, stdout, _ = runUpdateCLI(t, "0.1.4", false, nil, cfgFn, "update", "--json")
		if code != contract.ExitServer {
			t.Fatalf("json HTTP 500 exit=%d, want %d", code, contract.ExitServer)
		}
		res := parseJSONResult(t, stdout)
		errObj, _ := res["error"].(map[string]any)
		hint, _ := errObj["hint"].(string)
		if res["ok"] != false || !strings.Contains(hint, wantEN) {
			t.Fatalf("expected docs URL %q in json error.hint, got %v", wantEN, res)
		}

		// Locale-aware zh-CN docs link
		wantZH := "https://cch-plus.com/zh-CN/docs/cli#install"
		zhEnv := map[string]string{"LANG": "zh_CN.UTF-8"}
		code, stdout, stderr = runUpdateCLI(t, "0.1.4", true, zhEnv, cfgFn, "update")
		if code != contract.ExitServer || !strings.Contains(stderr, wantZH) {
			t.Fatalf("expected zh-CN docs URL %q in human stderr, exit=%d stdout=%q stderr=%q", wantZH, code, stdout, stderr)
		}
		code, stdout, _ = runUpdateCLI(t, "0.1.4", false, zhEnv, cfgFn, "update", "--json")
		res = parseJSONResult(t, stdout)
		errObj, _ = res["error"].(map[string]any)
		hint, _ = errObj["hint"].(string)
		if code != contract.ExitServer || !strings.Contains(hint, wantZH) {
			t.Fatalf("expected zh-CN docs URL %q in json error.hint, got %v", wantZH, res)
		}
	})

	t.Run("checksum mismatch prints docs URL in human and JSON", func(t *testing.T) {
		dir := t.TempDir()
		binPath := filepath.Join(dir, "cchp")
		if err := os.WriteFile(binPath, []byte("orig"), 0o755); err != nil {
			t.Fatal(err)
		}
		archiveName := "cchp_0.2.0_linux_amd64.tar.gz"
		archiveBytes := makeTarGz(t, "cchp", []byte("tampered"))
		badChecksums := fmt.Sprintf("%s  %s\n", strings.Repeat("0", 64), archiveName)
		srv := startCLIFakeGitHub(t, "v0.2.0", archiveName, archiveBytes, badChecksums, nil)

		cfgFn := func(cfg *selfupdate.Config) {
			cfg.APIBaseURL = srv.URL
			cfg.Executable = func() (string, error) { return binPath, nil }
			cfg.LookPath = func(string) (string, error) { return "", errors.New("none") }
			cfg.GOOS = "linux"
			cfg.GOARCH = "amd64"
		}

		wantURL := "https://cch-plus.com/en/docs/cli#install"
		code, _, stderr := runUpdateCLI(t, "0.1.4", true, nil, cfgFn, "update")
		if code == contract.ExitOK || !strings.Contains(stderr, wantURL) {
			t.Fatalf("human checksum mismatch exit=%d stderr=%q", code, stderr)
		}
		code, stdout, _ := runUpdateCLI(t, "0.1.4", false, nil, cfgFn, "update", "--json")
		res := parseJSONResult(t, stdout)
		errObj, _ := res["error"].(map[string]any)
		hint, _ := errObj["hint"].(string)
		if code == contract.ExitOK || !strings.Contains(hint, wantURL) {
			t.Fatalf("json checksum mismatch exit=%d res=%v", code, res)
		}
	})

	t.Run("permission denied prints docs URL in human and JSON", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("POSIX read-only dir test requires non-root Unix")
		}
		roDir := t.TempDir()
		binPath := filepath.Join(roDir, "cchp")
		if err := os.WriteFile(binPath, []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(roDir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })

		archiveName := "cchp_0.2.0_linux_amd64.tar.gz"
		archiveBytes := makeTarGz(t, "cchp", []byte("new"))
		srv := startCLIFakeGitHub(t, "v0.2.0", archiveName, archiveBytes, "", nil)

		cfgFn := func(cfg *selfupdate.Config) {
			cfg.APIBaseURL = srv.URL
			cfg.Executable = func() (string, error) { return binPath, nil }
			cfg.LookPath = func(string) (string, error) { return "", errors.New("none") }
			cfg.GOOS = "linux"
			cfg.GOARCH = "amd64"
		}

		wantURL := "https://cch-plus.com/en/docs/cli#install"
		code, _, stderr := runUpdateCLI(t, "0.1.4", true, nil, cfgFn, "update")
		if code != contract.ExitPermission || !strings.Contains(stderr, wantURL) {
			t.Fatalf("human permission denied exit=%d stderr=%q", code, stderr)
		}
		code, stdout, _ := runUpdateCLI(t, "0.1.4", false, nil, cfgFn, "update", "--json")
		res := parseJSONResult(t, stdout)
		errObj, _ := res["error"].(map[string]any)
		hint, _ := errObj["hint"].(string)
		if code != contract.ExitPermission || errObj["kind"] != contract.KindPermission || !strings.Contains(hint, wantURL) {
			t.Fatalf("json permission denied exit=%d res=%v", code, res)
		}
	})

	t.Run("dev build without --force prints docs URL in human and JSON", func(t *testing.T) {
		cfgFn := func(cfg *selfupdate.Config) {
			cfg.Executable = func() (string, error) { return "/tmp/cchp", nil }
			cfg.EvalSymlinks = func(p string) (string, error) { return p, nil }
			cfg.LookPath = func(string) (string, error) { return "", errors.New("none") }
		}
		wantURL := "https://cch-plus.com/en/docs/cli#install"
		code, _, stderr := runUpdateCLI(t, "dev", true, nil, cfgFn, "update")
		if code != contract.ExitPolicy || !strings.Contains(stderr, wantURL) || !strings.Contains(stderr, "--force") {
			t.Fatalf("human dev build exit=%d stderr=%q", code, stderr)
		}
		code, stdout, _ := runUpdateCLI(t, "dev", false, nil, cfgFn, "update", "--json")
		res := parseJSONResult(t, stdout)
		errObj, _ := res["error"].(map[string]any)
		hint, _ := errObj["hint"].(string)
		if code != contract.ExitPolicy || !strings.Contains(hint, wantURL) || !strings.Contains(hint, "--force") {
			t.Fatalf("json dev build exit=%d res=%v", code, res)
		}
	})
}
