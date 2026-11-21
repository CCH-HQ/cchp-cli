package selfupdate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Method identifies how cchp was installed.
type Method string

const (
	MethodHomebrew  Method = "homebrew"
	MethodGoInstall Method = "go_install"
	MethodDirect    Method = "direct"
	MethodDev       Method = "dev"
)

// Detection describes the detected installation method and executable path.
type Detection struct {
	// Method is the effective installation method ("homebrew", "go_install",
	// "direct", or "dev").
	Method Method
	// UnderlyingMethod is the path-based installation method ("homebrew",
	// "go_install", or "direct"), used when --force overrides a dev build.
	UnderlyingMethod Method
	// ExecutablePath is the symlink-resolved path to the running binary.
	ExecutablePath string
	// OriginalPath is the path reported by os.Executable before symlink resolution.
	OriginalPath string
	// IsDev is true when the current binary's version is "dev" or non-SemVer.
	IsDev bool
}

// DetectEnv holds injectable environment hooks for Detect.
type DetectEnv struct {
	Getenv       func(string) string
	LookPath     func(string) (string, error)
	EvalSymlinks func(string) (string, error)
	UserHomeDir  func() (string, error)
	GOOS         string
}

var defaultHomebrewCellarDirs = []string{
	"/opt/homebrew/Cellar/cchp",
	"/usr/local/Cellar/cchp",
	"/home/linuxbrew/.linuxbrew/Cellar/cchp",
}

// Detect determines how cchp was installed from its executable path,
// environment variables, PATH lookup, and build version.
func Detect(execPath, currentVersion string, env DetectEnv) Detection {
	getenv := env.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	evalSymlinks := env.EvalSymlinks
	if evalSymlinks == nil {
		evalSymlinks = filepath.EvalSymlinks
	}
	userHomeDir := env.UserHomeDir
	if userHomeDir == nil {
		userHomeDir = os.UserHomeDir
	}
	goos := env.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}

	orig := filepath.Clean(execPath)
	resolved := resolvePath(orig, evalSymlinks)

	underlying := MethodDirect
	switch {
	case isHomebrewPath(resolved, orig, getenv, evalSymlinks, goos):
		underlying = MethodHomebrew
	case isGoInstallPath(resolved, orig, getenv, env.LookPath, evalSymlinks, userHomeDir, goos):
		underlying = MethodGoInstall
	}

	isDev := !IsReleaseVersion(currentVersion)
	method := underlying
	if isDev {
		method = MethodDev
	}

	return Detection{
		Method:           method,
		UnderlyingMethod: underlying,
		ExecutablePath:   resolved,
		OriginalPath:     orig,
		IsDev:            isDev,
	}
}

func resolvePath(p string, evalSymlinks func(string) (string, error)) string {
	if p == "" {
		return ""
	}
	clean := filepath.Clean(p)
	if evalSymlinks != nil {
		if r, err := evalSymlinks(clean); err == nil && r != "" {
			return filepath.Clean(r)
		}
	}
	return clean
}

func isHomebrewPath(resolved, orig string, getenv func(string) string, evalSymlinks func(string) (string, error), goos string) bool {
	var cellarDirs []string
	if cellar := strings.TrimSpace(getenv("HOMEBREW_CELLAR")); cellar != "" {
		cellarDirs = append(cellarDirs, filepath.Join(cellar, "cchp"))
	}
	if prefix := strings.TrimSpace(getenv("HOMEBREW_PREFIX")); prefix != "" {
		cellarDirs = append(cellarDirs, filepath.Join(prefix, "Cellar", "cchp"))
	}
	cellarDirs = append(cellarDirs, defaultHomebrewCellarDirs...)

	for _, dir := range cellarDirs {
		if pathWithinDir(resolved, dir, evalSymlinks, goos) || pathWithinDir(orig, dir, evalSymlinks, goos) {
			return true
		}
	}

	// Catch custom Homebrew prefixes even when HOMEBREW_PREFIX is not exported.
	for _, p := range []string{resolved, orig} {
		slashed := filepath.ToSlash(p)
		if strings.Contains(slashed, "/Cellar/cchp/") {
			return true
		}
	}
	return false
}

func isGoInstallPath(resolved, orig string, getenv func(string) string, lookPath func(string) (string, error), evalSymlinks func(string) (string, error), userHomeDir func() (string, error), goos string) bool {
	if lookPath == nil {
		return false
	}
	goBin, err := lookPath("go")
	if err != nil || goBin == "" {
		return false
	}

	var dirs []string
	if gobin := strings.TrimSpace(getenv("GOBIN")); gobin != "" {
		dirs = append(dirs, gobin)
	}
	if gopath := strings.TrimSpace(getenv("GOPATH")); gopath != "" {
		for _, entry := range filepath.SplitList(gopath) {
			if trimmed := strings.TrimSpace(entry); trimmed != "" {
				dirs = append(dirs, filepath.Join(trimmed, "bin"))
			}
		}
	} else if home, err := userHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, "go", "bin"))
	}

	for _, dir := range dirs {
		if pathWithinDir(resolved, dir, evalSymlinks, goos) || pathWithinDir(orig, dir, evalSymlinks, goos) {
			return true
		}
	}
	return false
}

// pathWithinDir reports whether target is strictly inside dir, comparing both
// raw cleaned paths and symlink-resolved paths.
func pathWithinDir(target, dir string, evalSymlinks func(string) (string, error), goos string) bool {
	if target == "" || dir == "" {
		return false
	}
	candidates := [][2]string{
		{filepath.Clean(target), filepath.Clean(dir)},
		{resolvePath(target, evalSymlinks), resolvePath(dir, evalSymlinks)},
	}
	for _, pair := range candidates {
		t, d := pair[0], pair[1]
		if goos == "windows" {
			t = strings.ToLower(t)
			d = strings.ToLower(d)
		}
		rel, err := filepath.Rel(d, t)
		if err != nil {
			continue
		}
		if rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}
