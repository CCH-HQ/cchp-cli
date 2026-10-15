package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

const keyringService = "cchp"

// Identity is what the server said about the token at sign-in.
type Identity struct {
	UserID           string `json:"user_id,omitempty"`
	Email            string `json:"email,omitempty"`
	Name             string `json:"name,omitempty"`
	OrganizationID   string `json:"organization_id,omitempty"`
	OrganizationName string `json:"organization_name,omitempty"`
	OrgRole          string `json:"org_role,omitempty"`
	TokenKind        string `json:"token_kind,omitempty"`
	IsPlatformAdmin  bool   `json:"is_platform_admin"`
}

// Profile is the non-secret record of a sign-in, kept in config.json.
type Profile struct {
	Server      string    `json:"server"`
	TokenPrefix string    `json:"token_prefix"`
	Store       string    `json:"store"`
	Identity    Identity  `json:"identity"`
	SignedInAt  time.Time `json:"signed_in_at"`
}

// Config is cchp's config.json.
type Config struct {
	Current  string              `json:"current,omitempty"`
	Profiles map[string]*Profile `json:"profiles,omitempty"`
}

// Credential is the token cchp uses for one run.
type Credential struct {
	Server   string
	Token    string
	Source   string // env, token-file, keychain or file
	Identity *Identity
}

// Dirs locates cchp's files.
type Dirs struct {
	Config string
	Cache  string
}

// DefaultDirs honours CCHP_CONFIG_DIR and CCHP_CACHE_DIR.
func DefaultDirs() (Dirs, error) {
	var d Dirs
	if v := os.Getenv("CCHP_CONFIG_DIR"); v != "" {
		d.Config = v
	} else {
		base, err := os.UserConfigDir()
		if err != nil {
			return d, fmt.Errorf("cannot find a config directory; set CCHP_CONFIG_DIR: %w", err)
		}
		d.Config = filepath.Join(base, "cchp")
	}
	if v := os.Getenv("CCHP_CACHE_DIR"); v != "" {
		d.Cache = v
	} else {
		base, err := os.UserCacheDir()
		if err != nil {
			return d, fmt.Errorf("cannot find a cache directory; set CCHP_CACHE_DIR: %w", err)
		}
		d.Cache = filepath.Join(base, "cchp")
	}
	return d, nil
}

// Store reads and writes credentials and config.json.
type Store struct {
	Dirs Dirs
	// Backend is "keychain" or "file".
	Backend string
}

// OpenStore picks the credential backend: the OS keychain when one is
// reachable, otherwise a mode-600 file. CCHP_CREDENTIAL_STORE=keychain|file
// chooses explicitly.
func OpenStore(d Dirs) (*Store, error) {
	s := &Store{Dirs: d}
	switch v := os.Getenv("CCHP_CREDENTIAL_STORE"); v {
	case "keychain", "file":
		s.Backend = v
	case "":
		if keychainAvailable() {
			s.Backend = "keychain"
		} else {
			s.Backend = "file"
		}
	default:
		return nil, fmt.Errorf("CCHP_CREDENTIAL_STORE must be keychain or file, not %q", v)
	}
	return s, nil
}

// keychainAvailable probes the OS keychain with a read of an entry that never
// exists: a reachable keychain answers "not found".
func keychainAvailable() bool {
	_, err := keyring.Get(keyringService, "cchp-availability-probe")
	return errors.Is(err, keyring.ErrNotFound)
}

func (s *Store) configPath() string      { return filepath.Join(s.Dirs.Config, "config.json") }
func (s *Store) credentialsPath() string { return filepath.Join(s.Dirs.Config, "credentials.json") }

// Load reads config.json; a missing file is an empty config.
func (s *Store) Load() (*Config, error) {
	cfg := &Config{Profiles: map[string]*Profile{}}
	data, err := os.ReadFile(s.configPath())
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("%s is damaged: %w", s.configPath(), err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]*Profile{}
	}
	return cfg, nil
}

func (s *Store) saveConfig(cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.configPath(), append(data, '\n'), 0o600)
}

// Save stores the token for server and makes it the current server.
func (s *Store) Save(server, token string, id Identity) (*Profile, error) {
	origin := Origin(server)
	switch s.Backend {
	case "keychain":
		if err := keyring.Set(keyringService, origin, token); err != nil {
			return nil, fmt.Errorf("saving to the system keychain: %w", err)
		}
	case "file":
		creds, err := s.readCredentialFile()
		if err != nil {
			return nil, err
		}
		creds[origin] = token
		data, err := json.MarshalIndent(creds, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := writeFileAtomic(s.credentialsPath(), append(data, '\n'), 0o600); err != nil {
			return nil, err
		}
	}
	cfg, err := s.Load()
	if err != nil {
		return nil, err
	}
	p := &Profile{Server: server, TokenPrefix: TokenPrefix(token), Store: s.Backend, Identity: id, SignedInAt: time.Now().UTC().Truncate(time.Second)}
	cfg.Profiles[origin] = p
	cfg.Current = origin
	return p, s.saveConfig(cfg)
}

// Delete removes the stored token and profile for server.
func (s *Store) Delete(server string) error {
	origin := Origin(server)
	cfg, err := s.Load()
	if err != nil {
		return err
	}
	p := cfg.Profiles[origin]
	if p == nil {
		return ErrNotSignedIn
	}
	switch p.Store {
	case "keychain":
		if err := keyring.Delete(keyringService, origin); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("removing from the system keychain: %w", err)
		}
	case "file":
		creds, err := s.readCredentialFile()
		if err != nil {
			return err
		}
		delete(creds, origin)
		data, err := json.MarshalIndent(creds, "", "  ")
		if err != nil {
			return err
		}
		if err := writeFileAtomic(s.credentialsPath(), append(data, '\n'), 0o600); err != nil {
			return err
		}
	}
	delete(cfg.Profiles, origin)
	if cfg.Current == origin {
		cfg.Current = ""
	}
	return s.saveConfig(cfg)
}

func (s *Store) readCredentialFile() (map[string]string, error) {
	creds := map[string]string{}
	path := s.credentialsPath()
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return creds, nil
	}
	if err != nil {
		return nil, err
	}
	if err := checkPrivate(path, info); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("%s is damaged: %w", path, err)
	}
	return creds, nil
}

func (s *Store) storedToken(p *Profile) (string, error) {
	origin := Origin(p.Server)
	switch p.Store {
	case "keychain":
		tok, err := keyring.Get(keyringService, origin)
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrNotSignedIn
		}
		return tok, err
	case "file":
		creds, err := s.readCredentialFile()
		if err != nil {
			return "", err
		}
		tok, ok := creds[origin]
		if !ok {
			return "", ErrNotSignedIn
		}
		return tok, nil
	}
	return "", fmt.Errorf("unknown credential store %q in config.json", p.Store)
}

// Resolve finds the credential for this run. Environment variables come first
// (CCHP_TOKEN or CCHP_TOKEN_FILE, always with CCHP_BASE_URL), then the stored
// credential of CCHP_BASE_URL or of the current server.
func (s *Store) Resolve() (*Credential, error) {
	envServer := os.Getenv("CCHP_BASE_URL")
	envToken := os.Getenv("CCHP_TOKEN")
	tokenFile := os.Getenv("CCHP_TOKEN_FILE")
	if envToken != "" && tokenFile != "" {
		return nil, errors.New("set either CCHP_TOKEN or CCHP_TOKEN_FILE, not both")
	}
	if envToken != "" || tokenFile != "" {
		if envServer == "" {
			return nil, errors.New("CCHP_BASE_URL must name the server the token belongs to")
		}
		server, err := NormalizeServer(envServer)
		if err != nil {
			return nil, fmt.Errorf("CCHP_BASE_URL: %w", err)
		}
		source, token := "env", envToken
		if tokenFile != "" {
			source = "token-file"
			token, err = readTokenFile(tokenFile)
			if err != nil {
				return nil, err
			}
		}
		if err := CheckTokenShape(token); err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		return &Credential{Server: server, Token: strings.TrimSpace(token), Source: source}, nil
	}
	cfg, err := s.Load()
	if err != nil {
		return nil, err
	}
	origin := cfg.Current
	if envServer != "" {
		server, err := NormalizeServer(envServer)
		if err != nil {
			return nil, fmt.Errorf("CCHP_BASE_URL: %w", err)
		}
		origin = Origin(server)
	}
	p := cfg.Profiles[origin]
	if origin == "" || p == nil {
		return nil, ErrNotSignedIn
	}
	tok, err := s.storedToken(p)
	if err != nil {
		return nil, err
	}
	id := p.Identity
	return &Credential{Server: p.Server, Token: tok, Source: p.Store, Identity: &id}, nil
}

// Server returns the server cchp would talk to without needing a token:
// CCHP_BASE_URL or the current server.
func (s *Store) Server() (string, error) {
	if v := os.Getenv("CCHP_BASE_URL"); v != "" {
		return NormalizeServer(v)
	}
	cfg, err := s.Load()
	if err != nil {
		return "", err
	}
	if p := cfg.Profiles[cfg.Current]; p != nil {
		return p.Server, nil
	}
	return "", ErrNotSignedIn
}

// Current returns the current profile, if any.
func (s *Store) Current() (*Profile, error) {
	cfg, err := s.Load()
	if err != nil {
		return nil, err
	}
	return cfg.Profiles[cfg.Current], nil
}

func readTokenFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("CCHP_TOKEN_FILE: %w", err)
	}
	if err := checkPrivate(path, info); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func checkPrivate(path string, info os.FileInfo) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s can be read by other users; run chmod 600 %s", path, path)
	}
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
