package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
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
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

type recordedCall struct {
	Name string
	Args []string
}

type fakeCommandRunner struct {
	mu      sync.Mutex
	calls   []recordedCall
	handler func(name string, args ...string) (string, string, error)
}

func (f *fakeCommandRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, recordedCall{Name: name, Args: slices.Clone(args)})
	h := f.handler
	f.mu.Unlock()
	if h != nil {
		return h(name, args...)
	}
	return "", "", nil
}

func (f *fakeCommandRunner) snapshot() []recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func mapEnv(values map[string]string) func(string) string {
	return func(k string) string {
		return values[k]
	}
}

func intPtr(v int) *int { return &v }

func buildTarGzArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header %s: %v", name, err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("write tar body %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func buildZipArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type fakeReleaseServer struct {
	server          *httptest.Server
	mu              sync.Mutex
	seenAuthHeaders []string
	seenUserAgents  []string
	requestPaths    []string
}

func startFakeReleaseServer(t *testing.T, tag string, assets map[string][]byte) *fakeReleaseServer {
	t.Helper()
	frs := &fakeReleaseServer{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		frs.mu.Lock()
		frs.requestPaths = append(frs.requestPaths, r.URL.Path)
		frs.seenUserAgents = append(frs.seenUserAgents, r.Header.Get("User-Agent"))
		if auth := r.Header.Get("Authorization"); auth != "" {
			frs.seenAuthHeaders = append(frs.seenAuthHeaders, auth)
		}
		if cookie := r.Header.Get("Cookie"); cookie != "" {
			frs.seenAuthHeaders = append(frs.seenAuthHeaders, "Cookie:"+cookie)
		}
		frs.mu.Unlock()

		switch r.URL.Path {
		case "/repos/CCH-HQ/cchp-cli/releases/latest",
			"/repos/CCH-HQ/cchp-cli/releases/tags/" + tag:
			var list []Asset
			for name, body := range assets {
				list = append(list, Asset{
					Name:               name,
					BrowserDownloadURL: srv.URL + "/download/" + name,
					Size:               int64(len(body)),
				})
			}
			rel := Release{
				TagName: tag,
				HTMLURL: "https://github.com/CCH-HQ/cchp-cli/releases/tag/" + tag,
				Assets:  list,
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rel)
		default:
			if name, ok := strings.CutPrefix(r.URL.Path, "/download/"); ok {
				if body, exists := assets[name]; exists {
					w.Header().Set("Content-Type", "application/octet-stream")
					_, _ = w.Write(body)
					return
				}
			}
			http.NotFound(w, r)
		}
	}))
	frs.server = srv
	t.Cleanup(srv.Close)
	return frs
}

func TestVersionNormalizationAndComparison(t *testing.T) {
	valid := []struct {
		in   string
		want string
	}{
		{"0.2.0", "v0.2.0"},
		{"v0.2.0", "v0.2.0"},
		{" 1.2.3-rc.1 ", "v1.2.3-rc.1"},
		{"0.10.0+meta", "v0.10.0+meta"},
	}
	for _, tc := range valid {
		got, ok := NormalizeVersion(tc.in)
		if !ok || got != tc.want {
			t.Errorf("NormalizeVersion(%q) = (%q, %v), want (%q, true)", tc.in, got, ok, tc.want)
		}
	}

	invalid := []string{
		"",
		"dev",
		"test",
		"none",
		"v1",
		"1.2",
		"../v0.2.0",
		"v0.0.0-20261121090000-abcdef123456",
	}
	for _, in := range invalid {
		if got, ok := NormalizeVersion(in); ok {
			t.Errorf("NormalizeVersion(%q) = (%q, true), want invalid", in, got)
		}
	}

	cmp, ok := CompareVersions("0.1.4", "v0.2.0")
	if !ok || cmp >= 0 {
		t.Fatalf("CompareVersions(0.1.4, v0.2.0) = (%d, %v), want < 0", cmp, ok)
	}
	cmp, ok = CompareVersions("v0.2.0", "0.2.0")
	if !ok || cmp != 0 {
		t.Fatalf("CompareVersions(v0.2.0, 0.2.0) = (%d, %v), want 0", cmp, ok)
	}
	cmp, ok = CompareVersions("0.3.0", "v0.2.0")
	if !ok || cmp <= 0 {
		t.Fatalf("CompareVersions(0.3.0, v0.2.0) = (%d, %v), want > 0", cmp, ok)
	}
}

func TestDocsURLLocaleResolution(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"default en", nil, "https://cch-plus.com/en/docs/cli#install"},
		{"CCHP_DOCS_LANG zh-CN", map[string]string{"CCHP_DOCS_LANG": "zh-CN"}, "https://cch-plus.com/zh-CN/docs/cli#install"},
		{"LANG zh_HK maps to zh-TW", map[string]string{"LANG": "zh_HK.UTF-8"}, "https://cch-plus.com/zh-TW/docs/cli#install"},
		{"LC_ALL ja", map[string]string{"LC_ALL": "ja_JP.UTF-8"}, "https://cch-plus.com/ja/docs/cli#install"},
		{"unsupported locale defaults to en", map[string]string{"LANG": "de_DE.UTF-8"}, "https://cch-plus.com/en/docs/cli#install"},
		{"invalid CCHP_DOCS_LANG falls back safely to en", map[string]string{"CCHP_DOCS_LANG": "invalid-lang"}, "https://cch-plus.com/en/docs/cli#install"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DocsURL(mapEnv(tc.env)); got != tc.want {
				t.Fatalf("DocsURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDetectInstallMethods(t *testing.T) {
	noSymlink := func(p string) (string, error) { return p, nil }
	hasGo := func(name string) (string, error) {
		if name == "go" {
			return "/usr/bin/go", nil
		}
		return "", errors.New("not found")
	}
	noGo := func(string) (string, error) { return "", errors.New("not found") }
	homeDir := func() (string, error) { return "/home/alice", nil }

	t.Run("homebrew standard paths", func(t *testing.T) {
		paths := []string{
			"/opt/homebrew/Cellar/cchp/0.2.0/bin/cchp",
			"/usr/local/Cellar/cchp/0.2.0/bin/cchp",
			"/home/linuxbrew/.linuxbrew/Cellar/cchp/0.2.0/bin/cchp",
		}
		for _, p := range paths {
			det := Detect(p, "0.2.0", DetectEnv{
				Getenv:       mapEnv(nil),
				LookPath:     hasGo,
				EvalSymlinks: noSymlink,
				UserHomeDir:  homeDir,
			})
			if det.Method != MethodHomebrew {
				t.Errorf("Detect(%q) = %q, want homebrew", p, det.Method)
			}
		}
	})

	t.Run("homebrew symlink into Cellar via HOMEBREW_PREFIX", func(t *testing.T) {
		root := t.TempDir()
		cellarBin := filepath.Join(root, "Cellar", "cchp", "0.2.0", "bin", "cchp")
		binLink := filepath.Join(root, "bin", "cchp")
		if err := os.MkdirAll(filepath.Dir(cellarBin), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(binLink), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cellarBin, []byte("bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(cellarBin, binLink); err != nil {
			t.Fatal(err)
		}

		det := Detect(binLink, "0.2.0", DetectEnv{
			Getenv:      mapEnv(map[string]string{"HOMEBREW_PREFIX": root}),
			LookPath:    hasGo,
			UserHomeDir: homeDir,
		})
		if det.Method != MethodHomebrew {
			t.Fatalf("Detect(symlink) = %q, want homebrew (resolved=%q)", det.Method, det.ExecutablePath)
		}
	})

	t.Run("direct /usr/local/bin is not homebrew", func(t *testing.T) {
		det := Detect("/usr/local/bin/cchp", "0.2.0", DetectEnv{
			Getenv:       mapEnv(nil),
			LookPath:     hasGo,
			EvalSymlinks: noSymlink,
			UserHomeDir:  homeDir,
		})
		if det.Method != MethodDirect {
			t.Fatalf("Detect(/usr/local/bin/cchp) = %q, want direct", det.Method)
		}
	})

	t.Run("go install in GOBIN and GOPATH/bin", func(t *testing.T) {
		det := Detect("/custom/gobin/cchp", "0.2.0", DetectEnv{
			Getenv:       mapEnv(map[string]string{"GOBIN": "/custom/gobin"}),
			LookPath:     hasGo,
			EvalSymlinks: noSymlink,
			UserHomeDir:  homeDir,
		})
		if det.Method != MethodGoInstall {
			t.Fatalf("Detect(GOBIN) = %q, want go_install", det.Method)
		}

		det = Detect("/ws/go2/bin/cchp", "0.2.0", DetectEnv{
			Getenv:       mapEnv(map[string]string{"GOPATH": "/ws/go1" + string(filepath.ListSeparator) + "/ws/go2"}),
			LookPath:     hasGo,
			EvalSymlinks: noSymlink,
			UserHomeDir:  homeDir,
		})
		if det.Method != MethodGoInstall {
			t.Fatalf("Detect(multi-GOPATH) = %q, want go_install", det.Method)
		}

		det = Detect("/home/alice/go/bin/cchp", "0.2.0", DetectEnv{
			Getenv:       mapEnv(nil),
			LookPath:     hasGo,
			EvalSymlinks: noSymlink,
			UserHomeDir:  homeDir,
		})
		if det.Method != MethodGoInstall {
			t.Fatalf("Detect(default GOPATH) = %q, want go_install", det.Method)
		}

		// Without go binary available, falls back to direct.
		det = Detect("/home/alice/go/bin/cchp", "0.2.0", DetectEnv{
			Getenv:       mapEnv(nil),
			LookPath:     noGo,
			EvalSymlinks: noSymlink,
			UserHomeDir:  homeDir,
		})
		if det.Method != MethodDirect {
			t.Fatalf("Detect(no go binary) = %q, want direct", det.Method)
		}
	})

	t.Run("dev build preserves underlying method", func(t *testing.T) {
		det := Detect("/home/alice/go/bin/cchp", "dev", DetectEnv{
			Getenv:       mapEnv(nil),
			LookPath:     hasGo,
			EvalSymlinks: noSymlink,
			UserHomeDir:  homeDir,
		})
		if !det.IsDev || det.Method != MethodDev || det.UnderlyingMethod != MethodGoInstall {
			t.Fatalf("Detect(dev in GOPATH) = %+v, want Method=dev Underlying=go_install", det)
		}
	})
}

func TestDirectUpdateTarGzAtomicReplacement(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "cchp")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	newBin := []byte("#!/bin/sh\necho new-v0.3.0\n")
	archiveName := "cchp_0.3.0_linux_amd64.tar.gz"
	archiveBytes := buildTarGzArchive(t, map[string][]byte{
		"cchp":      newBin,
		"README.md": []byte("readme"),
		"LICENSE":   []byte("MIT"),
	})
	sum := sha256Hex(archiveBytes)
	checksumsTxt := fmt.Sprintf("%s  %s\n", sum, archiveName)

	frs := startFakeReleaseServer(t, "v0.3.0", map[string][]byte{
		archiveName:     archiveBytes,
		"checksums.txt": []byte(checksumsTxt),
	})

	var progress []string
	u := New(Config{
		CurrentVersion: "0.2.0",
		UserAgent:      "cchp/0.2.0",
		APIBaseURL:     frs.server.URL,
		RetryMax:       intPtr(0),
		Executable:     func() (string, error) { return binPath, nil },
		Getenv:         mapEnv(map[string]string{"CCHP_TOKEN": "super-secret-token"}),
		LookPath:       func(string) (string, error) { return "", errors.New("none") },
		GOOS:           "linux",
		GOARCH:         "amd64",
		OnProgress:     func(msg string) { progress = append(progress, msg) },
	})

	res, err := u.Run(context.Background(), RunOptions{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !res.Updated || res.CurrentVersion != "v0.2.0" || res.TargetVersion != "v0.3.0" || res.InstallMethod != MethodDirect || res.SHA256 != sum {
		t.Fatalf("unexpected result: %+v", res)
	}
	if len(progress) == 0 {
		t.Errorf("expected progress updates")
	}

	gotBytes, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBytes, newBin) {
		t.Fatalf("binary content = %q, want %q", gotBytes, newBin)
	}
	info, err := os.Stat(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
		t.Fatalf("binary perm = %o, want 0755", info.Mode().Perm())
	}

	// Ensure zero credentials were sent and User-Agent was clean.
	if len(frs.seenAuthHeaders) > 0 {
		t.Fatalf("unexpected auth headers sent to GitHub: %v", frs.seenAuthHeaders)
	}
	for _, ua := range frs.seenUserAgents {
		if ua != "cchp/0.2.0" {
			t.Errorf("User-Agent = %q, want cchp/0.2.0", ua)
		}
	}
}

func TestDirectUpdateZipAndWindowsSwap(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "cchp.exe")
	if err := os.WriteFile(binPath, []byte("old-win-bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	newBin := []byte("new-win-v0.3.0")
	archiveName := "cchp_0.3.0_windows_amd64.zip"
	archiveBytes := buildZipArchive(t, map[string][]byte{
		"cchp.exe":  newBin,
		"README.md": []byte("readme"),
	})
	sum := sha256Hex(archiveBytes)
	frs := startFakeReleaseServer(t, "v0.3.0", map[string][]byte{
		archiveName:     archiveBytes,
		"checksums.txt": []byte(fmt.Sprintf("%s  %s\n", sum, archiveName)),
	})

	u := New(Config{
		CurrentVersion: "0.2.0",
		APIBaseURL:     frs.server.URL,
		RetryMax:       intPtr(0),
		Executable:     func() (string, error) { return binPath, nil },
		Getenv:         mapEnv(nil),
		LookPath:       func(string) (string, error) { return "", errors.New("none") },
		GOOS:           "windows",
		GOARCH:         "amd64",
	})

	res, err := u.Run(context.Background(), RunOptions{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !res.Updated || res.ArchiveName != archiveName || res.SHA256 != sum {
		t.Fatalf("unexpected result: %+v", res)
	}
	got, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newBin) {
		t.Fatalf("replaced binary = %q, want %q", got, newBin)
	}
}

func TestChecksumMismatchFailsClosed(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "cchp")
	origContent := []byte("original-intact-binary")
	if err := os.WriteFile(binPath, origContent, 0o755); err != nil {
		t.Fatal(err)
	}

	archiveName := "cchp_0.3.0_linux_amd64.tar.gz"
	archiveBytes := buildTarGzArchive(t, map[string][]byte{"cchp": []byte("tampered")})
	badSum := strings.Repeat("a", 64)
	frs := startFakeReleaseServer(t, "v0.3.0", map[string][]byte{
		archiveName:     archiveBytes,
		"checksums.txt": []byte(fmt.Sprintf("%s  %s\n", badSum, archiveName)),
	})

	u := New(Config{
		CurrentVersion: "0.2.0",
		APIBaseURL:     frs.server.URL,
		RetryMax:       intPtr(0),
		Executable:     func() (string, error) { return binPath, nil },
		Getenv:         mapEnv(nil),
		LookPath:       func(string) (string, error) { return "", errors.New("none") },
		GOOS:           "linux",
		GOARCH:         "amd64",
	})

	_, err := u.Run(context.Background(), RunOptions{})
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	f, ok := errors.AsType[*contract.Failure](err)
	if !ok {
		t.Fatalf("expected *contract.Failure, got %T: %v", err, err)
	}
	if !strings.Contains(strings.ToLower(f.Err.Message), "checksum") {
		t.Errorf("expected checksum in error message, got %q", f.Err.Message)
	}
	if !strings.Contains(f.Err.Hint, "https://cch-plus.com/en/docs/cli#install") {
		t.Errorf("expected official docs URL in hint, got %q", f.Err.Hint)
	}

	// Original binary MUST remain untouched.
	after, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, origContent) {
		t.Fatalf("original binary was modified on checksum mismatch!")
	}
}

func TestMissingChecksumsAssetFailsClosed(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "cchp")
	if err := os.WriteFile(binPath, []byte("orig"), 0o755); err != nil {
		t.Fatal(err)
	}

	archiveName := "cchp_0.3.0_linux_amd64.tar.gz"
	frs := startFakeReleaseServer(t, "v0.3.0", map[string][]byte{
		archiveName: buildTarGzArchive(t, map[string][]byte{"cchp": []byte("new")}),
	})

	u := New(Config{
		CurrentVersion: "0.2.0",
		APIBaseURL:     frs.server.URL,
		RetryMax:       intPtr(0),
		Executable:     func() (string, error) { return binPath, nil },
		Getenv:         mapEnv(nil),
		LookPath:       func(string) (string, error) { return "", errors.New("none") },
		GOOS:           "linux",
		GOARCH:         "amd64",
	})

	_, err := u.Run(context.Background(), RunOptions{})
	f, ok := errors.AsType[*contract.Failure](err)
	if !ok || !strings.Contains(f.Err.Message, "checksums.txt") || !strings.Contains(f.Err.Hint, "https://cch-plus.com/en/docs/cli#install") {
		t.Fatalf("expected missing checksums.txt failure with docs URL, got %v", err)
	}
}

func TestExtractRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()

	t.Run("tar.gz traversal", func(t *testing.T) {
		archivePath := filepath.Join(dir, "evil.tar.gz")
		data := buildTarGzArchive(t, map[string][]byte{
			"../../etc/evil": []byte("bad"),
			"cchp":           []byte("bin"),
		})
		if err := os.WriteFile(archivePath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		err := ExtractBinary(archivePath, "cchp", filepath.Join(dir, "out-tar"))
		if err == nil || !strings.Contains(err.Error(), "traversal") {
			t.Fatalf("expected traversal error, got %v", err)
		}
	})

	t.Run("zip traversal", func(t *testing.T) {
		archivePath := filepath.Join(dir, "evil.zip")
		data := buildZipArchive(t, map[string][]byte{
			"../escape.exe": []byte("bad"),
			"cchp.exe":      []byte("bin"),
		})
		if err := os.WriteFile(archivePath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		err := ExtractBinary(archivePath, "cchp.exe", filepath.Join(dir, "out-zip"))
		if err == nil || !strings.Contains(err.Error(), "traversal") {
			t.Fatalf("expected traversal error, got %v", err)
		}
	})
}

func TestHomebrewUpdater(t *testing.T) {
	frs := startFakeReleaseServer(t, "v0.3.0", nil)
	cellarBin := "/opt/homebrew/Cellar/cchp/0.2.0/bin/cchp"
	hasBrew := func(name string) (string, error) {
		if name == "brew" {
			return "/opt/homebrew/bin/brew", nil
		}
		return "", errors.New("not found")
	}

	t.Run("normal upgrade", func(t *testing.T) {
		runner := &fakeCommandRunner{
			handler: func(name string, args ...string) (string, string, error) {
				return "==> Upgrading cch-hq/tap/cchp 0.2.0 -> 0.3.0\n", "", nil
			},
		}
		u := New(Config{
			CurrentVersion: "0.2.0",
			APIBaseURL:     frs.server.URL,
			RetryMax:       intPtr(0),
			CmdRunner:      runner,
			Executable:     func() (string, error) { return cellarBin, nil },
			EvalSymlinks:   func(p string) (string, error) { return p, nil },
			Getenv:         mapEnv(nil),
			LookPath:       hasBrew,
		})
		res, err := u.Run(context.Background(), RunOptions{})
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
		if !res.Updated || res.InstallMethod != MethodHomebrew {
			t.Fatalf("unexpected result: %+v", res)
		}
		calls := runner.snapshot()
		if len(calls) != 1 || calls[0].Name != "brew" || !slices.Equal(calls[0].Args, []string{"upgrade", HomebrewFormula}) {
			t.Fatalf("unexpected brew calls: %+v", calls)
		}
	})

	t.Run("stale tap triggers brew update then retry upgrade", func(t *testing.T) {
		attempts := 0
		runner := &fakeCommandRunner{
			handler: func(name string, args ...string) (string, string, error) {
				if len(args) > 0 && args[0] == "upgrade" {
					attempts++
					if attempts == 1 {
						return "", "Warning: cch-hq/tap/cchp 0.2.0 already installed\n", nil
					}
					return "==> Upgrading cch-hq/tap/cchp 0.2.0 -> 0.3.0\n", "", nil
				}
				return "Updated 1 tap.\n", "", nil
			},
		}
		u := New(Config{
			CurrentVersion: "0.2.0",
			APIBaseURL:     frs.server.URL,
			RetryMax:       intPtr(0),
			CmdRunner:      runner,
			Executable:     func() (string, error) { return cellarBin, nil },
			EvalSymlinks:   func(p string) (string, error) { return p, nil },
			Getenv:         mapEnv(nil),
			LookPath:       hasBrew,
		})
		res, err := u.Run(context.Background(), RunOptions{})
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
		if !res.Updated {
			t.Fatalf("expected Updated=true, got %+v", res)
		}
		calls := runner.snapshot()
		if len(calls) != 3 || !slices.Equal(calls[1].Args, []string{"update"}) || !slices.Equal(calls[2].Args, []string{"upgrade", HomebrewFormula}) {
			t.Fatalf("expected upgrade -> update -> upgrade, got %+v", calls)
		}
	})

	t.Run("already installed handled gracefully", func(t *testing.T) {
		runner := &fakeCommandRunner{
			handler: func(name string, args ...string) (string, string, error) {
				if len(args) > 0 && args[0] == "upgrade" {
					return "", "Warning: cch-hq/tap/cchp 0.2.0 already installed\n", nil
				}
				return "Already up-to-date.\n", "", nil
			},
		}
		u := New(Config{
			CurrentVersion: "0.2.0",
			APIBaseURL:     frs.server.URL,
			RetryMax:       intPtr(0),
			CmdRunner:      runner,
			Executable:     func() (string, error) { return cellarBin, nil },
			EvalSymlinks:   func(p string) (string, error) { return p, nil },
			Getenv:         mapEnv(nil),
			LookPath:       hasBrew,
		})
		res, err := u.Run(context.Background(), RunOptions{})
		if err != nil {
			t.Fatalf("expected graceful handling, got error: %v", err)
		}
		if res.Updated || !res.UpToDate {
			t.Fatalf("expected Updated=false UpToDate=true, got %+v", res)
		}
	})

	t.Run("brew failure returns guidance with docs URL", func(t *testing.T) {
		runner := &fakeCommandRunner{
			handler: func(name string, args ...string) (string, string, error) {
				return "", "Error: Could not resolve HEAD", errors.New("exit status 1")
			},
		}
		u := New(Config{
			CurrentVersion: "0.2.0",
			APIBaseURL:     frs.server.URL,
			RetryMax:       intPtr(0),
			CmdRunner:      runner,
			Executable:     func() (string, error) { return cellarBin, nil },
			EvalSymlinks:   func(p string) (string, error) { return p, nil },
			Getenv:         mapEnv(map[string]string{"LANG": "zh_CN.UTF-8"}),
			LookPath:       hasBrew,
		})
		_, err := u.Run(context.Background(), RunOptions{})
		f, ok := errors.AsType[*contract.Failure](err)
		if !ok {
			t.Fatalf("expected *contract.Failure, got %v", err)
		}
		if !strings.Contains(f.Err.Hint, "https://cch-plus.com/zh-CN/docs/cli#install") || !strings.Contains(f.Err.Hint, "brew upgrade") {
			t.Fatalf("expected zh-CN docs URL and brew hint, got %q", f.Err.Hint)
		}
	})

	t.Run("homebrew rejects --version", func(t *testing.T) {
		u := New(Config{
			CurrentVersion: "0.2.0",
			APIBaseURL:     frs.server.URL,
			RetryMax:       intPtr(0),
			Executable:     func() (string, error) { return cellarBin, nil },
			EvalSymlinks:   func(p string) (string, error) { return p, nil },
			Getenv:         mapEnv(nil),
			LookPath:       hasBrew,
		})
		_, err := u.Run(context.Background(), RunOptions{Version: "v0.1.4"})
		f, ok := errors.AsType[*contract.Failure](err)
		if !ok || f.Exit != contract.ExitUsage || !strings.Contains(f.Err.Hint, "https://cch-plus.com/en/docs/cli#install") {
			t.Fatalf("expected ExitUsage with docs URL, got %v", err)
		}
	})
}

func TestGoInstallUpdater(t *testing.T) {
	frs := startFakeReleaseServer(t, "v0.3.0", nil)
	goBin := "/home/alice/go/bin/cchp"
	hasGo := func(name string) (string, error) {
		if name == "go" {
			return "/usr/local/go/bin/go", nil
		}
		return "", errors.New("not found")
	}

	t.Run("runs go install @v<latest>", func(t *testing.T) {
		runner := &fakeCommandRunner{}
		u := New(Config{
			CurrentVersion: "0.2.0",
			APIBaseURL:     frs.server.URL,
			RetryMax:       intPtr(0),
			CmdRunner:      runner,
			Executable:     func() (string, error) { return goBin, nil },
			EvalSymlinks:   func(p string) (string, error) { return p, nil },
			Getenv:         mapEnv(map[string]string{"GOPATH": "/home/alice/go"}),
			LookPath:       hasGo,
		})
		res, err := u.Run(context.Background(), RunOptions{})
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
		if !res.Updated || res.InstallMethod != MethodGoInstall {
			t.Fatalf("unexpected result: %+v", res)
		}
		calls := runner.snapshot()
		wantArgs := []string{"install", GoInstallPkg + "@v0.3.0"}
		if len(calls) != 1 || calls[0].Name != "go" || !slices.Equal(calls[0].Args, wantArgs) {
			t.Fatalf("got calls %+v, want go %v", calls, wantArgs)
		}
	})

	t.Run("go install error includes docs URL and manual command", func(t *testing.T) {
		runner := &fakeCommandRunner{
			handler: func(name string, args ...string) (string, string, error) {
				return "", "go: network unreachable", errors.New("exit status 1")
			},
		}
		u := New(Config{
			CurrentVersion: "0.2.0",
			APIBaseURL:     frs.server.URL,
			RetryMax:       intPtr(0),
			CmdRunner:      runner,
			Executable:     func() (string, error) { return goBin, nil },
			EvalSymlinks:   func(p string) (string, error) { return p, nil },
			Getenv:         mapEnv(map[string]string{"GOPATH": "/home/alice/go"}),
			LookPath:       hasGo,
		})
		_, err := u.Run(context.Background(), RunOptions{})
		f, ok := errors.AsType[*contract.Failure](err)
		if !ok {
			t.Fatalf("expected *contract.Failure, got %v", err)
		}
		if !strings.Contains(f.Err.Hint, "https://cch-plus.com/en/docs/cli#install") || !strings.Contains(f.Err.Hint, "go install "+GoInstallPkg+"@v0.3.0") {
			t.Fatalf("unexpected hint: %q", f.Err.Hint)
		}
	})
}

func TestDevBuildHandlingAndForce(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "cchp")
	orig := []byte("dev-binary")
	if err := os.WriteFile(binPath, orig, 0o755); err != nil {
		t.Fatal(err)
	}

	archiveName := "cchp_0.3.0_linux_amd64.tar.gz"
	newBin := []byte("release-0.3.0")
	archiveBytes := buildTarGzArchive(t, map[string][]byte{"cchp": newBin})
	frs := startFakeReleaseServer(t, "v0.3.0", map[string][]byte{
		archiveName:     archiveBytes,
		"checksums.txt": []byte(fmt.Sprintf("%s  %s\n", sha256Hex(archiveBytes), archiveName)),
	})

	newUpdater := func() *Updater {
		return New(Config{
			CurrentVersion: "dev",
			APIBaseURL:     frs.server.URL,
			RetryMax:       intPtr(0),
			Executable:     func() (string, error) { return binPath, nil },
			Getenv:         mapEnv(nil),
			LookPath:       func(string) (string, error) { return "", errors.New("none") },
			GOOS:           "linux",
			GOARCH:         "amd64",
		})
	}

	// Without --force: must fail and leave binary untouched.
	_, err := newUpdater().Run(context.Background(), RunOptions{})
	f, ok := errors.AsType[*contract.Failure](err)
	if !ok || f.Exit != contract.ExitPolicy {
		t.Fatalf("expected ExitPolicy for dev build, got %v", err)
	}
	if !strings.Contains(f.Err.Hint, "--force") || !strings.Contains(f.Err.Hint, "https://cch-plus.com/en/docs/cli#install") {
		t.Fatalf("expected --force and docs URL in hint, got %q", f.Err.Hint)
	}

	// --check succeeds on dev build.
	res, err := newUpdater().Run(context.Background(), RunOptions{Check: true})
	if err != nil {
		t.Fatalf("--check on dev build error: %v", err)
	}
	if !res.DevBuild || res.InstallMethod != MethodDev || res.LatestVersion != "v0.3.0" || !res.NeedsForce {
		t.Fatalf("unexpected --check result on dev build: %+v", res)
	}

	// --dry-run succeeds and leaves binary untouched.
	res, err = newUpdater().Run(context.Background(), RunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("--dry-run on dev build error: %v", err)
	}
	if !res.DryRun || !res.NeedsForce || res.Updated {
		t.Fatalf("unexpected --dry-run result on dev build: %+v", res)
	}
	if got, _ := os.ReadFile(binPath); !bytes.Equal(got, orig) {
		t.Fatalf("binary modified during --dry-run")
	}

	// --force replaces binary.
	res, err = newUpdater().Run(context.Background(), RunOptions{Force: true})
	if err != nil {
		t.Fatalf("--force on dev build error: %v", err)
	}
	if !res.Updated || res.EffectiveMethod != MethodDirect {
		t.Fatalf("unexpected --force result: %+v", res)
	}
	if got, _ := os.ReadFile(binPath); !bytes.Equal(got, newBin) {
		t.Fatalf("binary not updated with --force")
	}
}

func TestNoCredentialsTransportRejectsAuth(t *testing.T) {
	tr := noCredentials{next: http.DefaultTransport}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.github.com/repos/CCH-HQ/cchp-cli/releases/latest", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret-cchp-token")
	resp, err := tr.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "Authorization") {
		t.Fatalf("expected noCredentials rejection, got resp=%v err=%v", resp, err)
	}
}

func TestErrorPathsIncludeDocsURL(t *testing.T) {
	t.Run("HTTP 500", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"upstream down"}`))
		}))
		defer srv.Close()

		u := New(Config{
			CurrentVersion: "0.2.0",
			APIBaseURL:     srv.URL,
			RetryMax:       intPtr(0),
			Executable:     func() (string, error) { return "/usr/local/bin/cchp", nil },
			EvalSymlinks:   func(p string) (string, error) { return p, nil },
			Getenv:         mapEnv(nil),
			LookPath:       func(string) (string, error) { return "", errors.New("none") },
		})
		_, err := u.Run(context.Background(), RunOptions{})
		f, ok := errors.AsType[*contract.Failure](err)
		if !ok || f.Exit != contract.ExitServer || !strings.Contains(f.Err.Hint, "https://cch-plus.com/en/docs/cli#install") {
			t.Fatalf("expected ExitServer with docs URL, got %v", err)
		}
	})

	t.Run("HTTP 403 rate limit", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
		}))
		defer srv.Close()

		u := New(Config{
			CurrentVersion: "0.2.0",
			APIBaseURL:     srv.URL,
			RetryMax:       intPtr(0),
			Executable:     func() (string, error) { return "/usr/local/bin/cchp", nil },
			EvalSymlinks:   func(p string) (string, error) { return p, nil },
			Getenv:         mapEnv(nil),
			LookPath:       func(string) (string, error) { return "", errors.New("none") },
		})
		_, err := u.Run(context.Background(), RunOptions{})
		f, ok := errors.AsType[*contract.Failure](err)
		if !ok || f.Exit != contract.ExitRateLimited || !strings.Contains(f.Err.Hint, "https://cch-plus.com/en/docs/cli#install") {
			t.Fatalf("expected ExitRateLimited with docs URL, got %v", err)
		}
	})

	t.Run("unsupported platform", func(t *testing.T) {
		frs := startFakeReleaseServer(t, "v0.3.0", nil)
		u := New(Config{
			CurrentVersion: "0.2.0",
			APIBaseURL:     frs.server.URL,
			RetryMax:       intPtr(0),
			Executable:     func() (string, error) { return "/usr/local/bin/cchp", nil },
			EvalSymlinks:   func(p string) (string, error) { return p, nil },
			Getenv:         mapEnv(nil),
			LookPath:       func(string) (string, error) { return "", errors.New("none") },
			GOOS:           "freebsd",
			GOARCH:         "amd64",
		})
		_, err := u.Run(context.Background(), RunOptions{})
		f, ok := errors.AsType[*contract.Failure](err)
		if !ok || f.Exit != contract.ExitNotFound || !strings.Contains(f.Err.Hint, "https://cch-plus.com/en/docs/cli#install") {
			t.Fatalf("expected ExitNotFound with docs URL, got %v", err)
		}
	})

	t.Run("invalid --version", func(t *testing.T) {
		u := New(Config{
			CurrentVersion: "0.2.0",
			Executable:     func() (string, error) { return "/usr/local/bin/cchp", nil },
			EvalSymlinks:   func(p string) (string, error) { return p, nil },
			Getenv:         mapEnv(nil),
			LookPath:       func(string) (string, error) { return "", errors.New("none") },
		})
		_, err := u.Run(context.Background(), RunOptions{Version: "../evil"})
		f, ok := errors.AsType[*contract.Failure](err)
		if !ok || f.Exit != contract.ExitUsage || !strings.Contains(f.Err.Hint, "https://cch-plus.com/en/docs/cli#install") {
			t.Fatalf("expected ExitUsage with docs URL, got %v", err)
		}
	})

	t.Run("permission denied on target dir", func(t *testing.T) {
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

		archiveName := "cchp_0.3.0_linux_amd64.tar.gz"
		archiveBytes := buildTarGzArchive(t, map[string][]byte{"cchp": []byte("new")})
		frs := startFakeReleaseServer(t, "v0.3.0", map[string][]byte{
			archiveName:     archiveBytes,
			"checksums.txt": []byte(fmt.Sprintf("%s  %s\n", sha256Hex(archiveBytes), archiveName)),
		})

		u := New(Config{
			CurrentVersion: "0.2.0",
			APIBaseURL:     frs.server.URL,
			RetryMax:       intPtr(0),
			Executable:     func() (string, error) { return binPath, nil },
			Getenv:         mapEnv(nil),
			LookPath:       func(string) (string, error) { return "", errors.New("none") },
			GOOS:           "linux",
			GOARCH:         "amd64",
		})
		_, err := u.Run(context.Background(), RunOptions{})
		f, ok := errors.AsType[*contract.Failure](err)
		if !ok || f.Exit != contract.ExitPermission || f.Err.Kind != contract.KindPermission {
			t.Fatalf("expected ExitPermission, got %v", err)
		}
		if !strings.Contains(f.Err.Hint, "https://cch-plus.com/en/docs/cli#install") {
			t.Fatalf("expected docs URL in permission error hint, got %q", f.Err.Hint)
		}
	})
}

func TestUpToDateForceExplicitVersionAndDryRunPlans(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "cchp")
	if err := os.WriteFile(binPath, []byte("v0.2.0-orig"), 0o755); err != nil {
		t.Fatal(err)
	}

	archiveName := "cchp_0.2.0_linux_amd64.tar.gz"
	newBin := []byte("v0.2.0-reinstalled")
	archiveBytes := buildTarGzArchive(t, map[string][]byte{"cchp": newBin})
	frs := startFakeReleaseServer(t, "v0.2.0", map[string][]byte{
		archiveName:     archiveBytes,
		"checksums.txt": []byte(fmt.Sprintf("%s  %s\n", sha256Hex(archiveBytes), archiveName)),
	})

	newDirect := func(ver string) *Updater {
		return New(Config{
			CurrentVersion: ver,
			APIBaseURL:     frs.server.URL,
			RetryMax:       intPtr(0),
			Executable:     func() (string, error) { return binPath, nil },
			Getenv:         mapEnv(nil),
			LookPath:       func(string) (string, error) { return "", errors.New("none") },
			GOOS:           "linux",
			GOARCH:         "amd64",
		})
	}

	// Already up to date -> does not download or overwrite.
	res, err := newDirect("0.2.0").Run(context.Background(), RunOptions{})
	if err != nil {
		t.Fatalf("up-to-date Run() error: %v", err)
	}
	if res.Updated || !res.UpToDate || res.Action != "none" {
		t.Fatalf("expected up-to-date no-op, got %+v", res)
	}
	if got, _ := os.ReadFile(binPath); string(got) != "v0.2.0-orig" {
		t.Fatalf("binary changed when already up to date")
	}

	// --force reinstalls even when already up to date.
	res, err = newDirect("0.2.0").Run(context.Background(), RunOptions{Force: true})
	if err != nil {
		t.Fatalf("--force Run() error: %v", err)
	}
	if !res.Updated {
		t.Fatalf("expected Updated=true on --force, got %+v", res)
	}
	if got, _ := os.ReadFile(binPath); !bytes.Equal(got, newBin) {
		t.Fatalf("binary not reinstalled on --force")
	}

	// Explicit --version hits /releases/tags/<tag> even when current is newer.
	res, err = newDirect("0.3.0").Run(context.Background(), RunOptions{Version: "0.2.0"})
	if err != nil {
		t.Fatalf("--version Run() error: %v", err)
	}
	if !res.Updated || res.TargetVersion != "v0.2.0" {
		t.Fatalf("expected explicit --version update to v0.2.0, got %+v", res)
	}

	// Dry-run plans for Homebrew and Go install invoke zero commands.
	runner := &fakeCommandRunner{}
	brewUpdater := New(Config{
		CurrentVersion: "0.1.4",
		APIBaseURL:     frs.server.URL,
		RetryMax:       intPtr(0),
		CmdRunner:      runner,
		Executable:     func() (string, error) { return "/opt/homebrew/Cellar/cchp/0.1.4/bin/cchp", nil },
		EvalSymlinks:   func(p string) (string, error) { return p, nil },
		Getenv:         mapEnv(nil),
		LookPath:       func(name string) (string, error) { return "/opt/homebrew/bin/" + name, nil },
	})
	res, err = brewUpdater.Run(context.Background(), RunOptions{DryRun: true})
	if err != nil || !res.DryRun || !slices.Equal(res.PlannedCommand, []string{"brew", "upgrade", HomebrewFormula}) {
		t.Fatalf("unexpected brew dry-run: res=%+v err=%v", res, err)
	}

	goUpdater := New(Config{
		CurrentVersion: "0.1.4",
		APIBaseURL:     frs.server.URL,
		RetryMax:       intPtr(0),
		CmdRunner:      runner,
		Executable:     func() (string, error) { return "/home/alice/go/bin/cchp", nil },
		EvalSymlinks:   func(p string) (string, error) { return p, nil },
		Getenv:         mapEnv(map[string]string{"GOPATH": "/home/alice/go"}),
		LookPath:       func(name string) (string, error) { return "/usr/bin/" + name, nil },
	})
	res, err = goUpdater.Run(context.Background(), RunOptions{DryRun: true})
	if err != nil || !res.DryRun || !slices.Equal(res.PlannedCommand, []string{"go", "install", GoInstallPkg + "@v0.2.0"}) {
		t.Fatalf("unexpected go dry-run: res=%+v err=%v", res, err)
	}
	if calls := runner.snapshot(); len(calls) != 0 {
		t.Fatalf("dry-run invoked commands: %+v", calls)
	}
}
