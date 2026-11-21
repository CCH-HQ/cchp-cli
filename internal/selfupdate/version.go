// Package selfupdate implements automatic install-method detection and safe
// self-updating for the cchp CLI.
package selfupdate

import (
	"regexp"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// releaseSemverPattern requires a full MAJOR.MINOR.PATCH version (with optional
// leading "v", prerelease, and build metadata) so shorthand strings like "1"
// or "1.2" are not mistaken for release tags.
var releaseSemverPattern = regexp.MustCompile(`^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// NormalizeVersion validates a release SemVer string (accepting with or without
// a leading "v") and returns its canonical "v"-prefixed form. Go pseudo-versions
// and non-SemVer strings such as "dev" return ("", false).
func NormalizeVersion(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if !releaseSemverPattern.MatchString(s) {
		return "", false
	}
	if !strings.HasPrefix(s, "v") {
		s = "v" + s
	}
	if !semver.IsValid(s) {
		return "", false
	}
	if module.IsPseudoVersion(s) {
		return "", false
	}
	return s, true
}

// IsReleaseVersion reports whether raw is a valid SemVer release version
// (as opposed to "dev", "test", empty, or a Go pseudo-version).
func IsReleaseVersion(raw string) bool {
	_, ok := NormalizeVersion(raw)
	return ok
}

// CompareVersions compares two SemVer release versions.
// It returns -1 if current < target, 0 if equal, +1 if current > target.
// If either version is not a valid SemVer release version, ok is false.
func CompareVersions(current, target string) (cmp int, ok bool) {
	c, ok1 := NormalizeVersion(current)
	t, ok2 := NormalizeVersion(target)
	if !ok1 || !ok2 {
		return 0, false
	}
	return semver.Compare(c, t), true
}

// DisplayVersion formats a version string cleanly: release versions carry a
// leading "v" (e.g. "0.2.0" -> "v0.2.0"), while non-release strings like "dev"
// are returned as-is (defaulting to "dev" if empty).
func DisplayVersion(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "dev"
	}
	if norm, ok := NormalizeVersion(s); ok {
		return norm
	}
	return s
}
