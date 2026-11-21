package selfupdate

import (
	"fmt"
	"os"
	"strings"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/sitedocs"
)

const (
	DefaultRepo       = "CCH-HQ/cchp-cli"
	HomebrewFormula   = "cch-hq/tap/cchp"
	GoInstallPkg      = "github.com/CCH-HQ/cchp-cli/cmd/cchp"
	InstallScriptURL  = "https://raw.githubusercontent.com/CCH-HQ/cchp-cli/main/install.sh"
	ReleasesPageURL   = "https://github.com/CCH-HQ/cchp-cli/releases/latest"
	cliDocsPathAnchor = "/docs/cli#install"
)

// DocsURL resolves the locale-appropriate official CLI install/update docs URL
// via sitedocs.Resolve("", getenv), defaulting to
// https://cch-plus.com/en/docs/cli#install.
func DocsURL(getenv func(string) string) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	lang, _, err := sitedocs.Resolve("", getenv)
	if err != nil || lang == "" {
		lang = sitedocs.DefaultLanguage
	}
	return sitedocs.DefaultSite + "/" + lang + cliDocsPathAnchor
}

// ManualUpgradeInstructions returns concrete manual upgrade steps tailored to
// the install method and operating system.
func ManualUpgradeInstructions(method Method, targetVersion, goos string) string {
	switch method {
	case MethodHomebrew:
		return "brew update && brew upgrade " + HomebrewFormula
	case MethodGoInstall:
		ref := "latest"
		if norm, ok := NormalizeVersion(targetVersion); ok {
			ref = norm
		}
		return "go install " + GoInstallPkg + "@" + ref
	case MethodDev:
		if goos == "windows" {
			return "re-run with --force, or download a release from " + ReleasesPageURL
		}
		return "re-run with --force, or install a release with: curl -fsSL " + InstallScriptURL + " | sh"
	default:
		if goos == "windows" {
			return "download the latest release from " + ReleasesPageURL
		}
		if norm, ok := NormalizeVersion(targetVersion); ok {
			return fmt.Sprintf("curl -fsSL %s | CCHP_VERSION=%s sh", InstallScriptURL, norm)
		}
		return "curl -fsSL " + InstallScriptURL + " | sh"
	}
}

// withGuidance ensures a Failure always carries concrete manual upgrade
// instructions and the official docs URL in its Hint.
func withGuidance(f *contract.Failure, method Method, targetVersion, docsURL, goos, extra string) *contract.Failure {
	if f == nil {
		return nil
	}
	var parts []string
	if existing := strings.TrimSpace(f.Err.Hint); existing != "" {
		parts = append(parts, existing)
	}
	if extra = strings.TrimSpace(extra); extra != "" {
		parts = append(parts, extra)
	}
	manual := ManualUpgradeInstructions(method, targetVersion, goos)
	if method == MethodDev {
		parts = append(parts, "To update manually, "+manual+".")
	} else {
		parts = append(parts, "Update manually:\n  "+manual)
	}
	parts = append(parts, "Update guide: "+docsURL)
	f.Err.Hint = strings.Join(parts, "\n")
	return f
}
