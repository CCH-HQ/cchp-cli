package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeServer(t *testing.T) {
	ok := map[string]string{
		"hub.example.com":               "https://hub.example.com",
		"https://Hub.Example.com/":      "https://hub.example.com",
		"https://hub.example.com/api":   "https://hub.example.com",
		"https://hub.example.com/cchp/": "https://hub.example.com/cchp",
		"http://127.0.0.1:18731":        "http://127.0.0.1:18731",
		"http://localhost:8080":         "http://localhost:8080",
		"https://hub.example.com:8443":  "https://hub.example.com:8443",
	}
	if got, err := NormalizeServer("  https://hub.example.com\n"); err != nil || got != "https://hub.example.com" {
		t.Errorf("surrounding whitespace: %q %v", got, err)
	}
	for in, want := range ok {
		got, err := NormalizeServer(in)
		if err != nil || got != want {
			t.Errorf("NormalizeServer(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "http://hub.example.com", "ftp://hub", "https://user:pw@hub", "https://hub?x=1", "https://"} {
		if _, err := NormalizeServer(bad); err == nil {
			t.Errorf("NormalizeServer(%q) should fail", bad)
		}
	}
}

func TestCheckTokenShape(t *testing.T) {
	if err := CheckTokenShape("cch_auth_live_ABC_secret"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "cch_llm_live_A_b", "sk-123", "cch_auth_live_"} {
		if CheckTokenShape(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if TokenPrefix("cch_auth_live_ABC_secret") != "cch_auth_live_ABC" {
		t.Fatal(TokenPrefix("cch_auth_live_ABC_secret"))
	}
}

func fileStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CCHP_CREDENTIAL_STORE", "file")
	t.Setenv("CCHP_TOKEN", "")
	t.Setenv("CCHP_TOKEN_FILE", "")
	t.Setenv("CCHP_BASE_URL", "")
	s, err := OpenStore(Dirs{Config: filepath.Join(dir, "config"), Cache: filepath.Join(dir, "cache")})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFileStoreIsPrivateAndBoundToTheServer(t *testing.T) {
	s := fileStore(t)
	if _, err := s.Resolve(); !errors.Is(err, ErrNotSignedIn) {
		t.Fatalf("empty store: %v", err)
	}
	if _, err := s.Save("https://a.example.com", "cch_auth_live_A_one", Identity{Email: "a@x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("https://b.example.com", "cch_auth_live_B_two", Identity{Email: "b@x"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "credentials.json"} {
		info, err := os.Stat(filepath.Join(s.Dirs.Config, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode %v", name, info.Mode().Perm())
		}
	}
	c, err := s.Resolve()
	if err != nil || c.Server != "https://b.example.com" || c.Token != "cch_auth_live_B_two" {
		t.Fatalf("current server: %+v %v", c, err)
	}
	t.Setenv("CCHP_BASE_URL", "https://a.example.com")
	c, err = s.Resolve()
	if err != nil || c.Token != "cch_auth_live_A_one" {
		t.Fatalf("CCHP_BASE_URL picks that server's token: %+v %v", c, err)
	}
	t.Setenv("CCHP_BASE_URL", "https://c.example.com")
	if _, err := s.Resolve(); !errors.Is(err, ErrNotSignedIn) {
		t.Fatalf("a server without a token must not borrow another's: %v", err)
	}
	t.Setenv("CCHP_BASE_URL", "")
	if err := s.Delete("https://b.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(); !errors.Is(err, ErrNotSignedIn) {
		t.Fatalf("after logout: %v", err)
	}
}

func TestEnvironmentCredentials(t *testing.T) {
	s := fileStore(t)
	t.Setenv("CCHP_TOKEN", "cch_auth_live_E_env")
	if _, err := s.Resolve(); err == nil {
		t.Fatal("CCHP_TOKEN without CCHP_BASE_URL must fail")
	}
	t.Setenv("CCHP_BASE_URL", "https://env.example.com")
	c, err := s.Resolve()
	if err != nil || c.Source != "env" || c.Server != "https://env.example.com" {
		t.Fatalf("%+v %v", c, err)
	}
	t.Setenv("CCHP_TOKEN", "")
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("cch_auth_live_F_file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CCHP_TOKEN_FILE", path)
	if _, err := s.Resolve(); err == nil {
		t.Fatal("a token file other users can read must be refused")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err = s.Resolve()
	if err != nil || c.Token != "cch_auth_live_F_file" || c.Source != "token-file" {
		t.Fatalf("%+v %v", c, err)
	}
}
