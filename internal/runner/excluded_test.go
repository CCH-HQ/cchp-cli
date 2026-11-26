package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/CCH-HQ/cchp-cli/internal/auth"
	"github.com/CCH-HQ/cchp-cli/internal/catalog"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

func runOffline(t *testing.T, id string) *contract.Failure {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CCHP_BASE_URL", "https://hub.example.com")
	t.Setenv("CCHP_TOKEN", "")
	t.Setenv("CCHP_TOKEN_FILE", "")
	t.Setenv("CCHP_CREDENTIAL_STORE", "file")
	store, err := auth.OpenStore(auth.Dirs{Config: dir + "/config", Cache: dir + "/cache"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(store, "test").Run(context.Background(), Options{Op: mustOp(t, id), Agent: true})
	var f *contract.Failure
	if !errors.As(err, &f) {
		t.Fatalf("%s: want a failure before any request, got %v", id, err)
	}
	return f
}

// CCHP v1.0.17 rejects management tokens for plugin approvals: they need a
// recent browser sign-in and human verification, so cchp says so up front.
func TestPluginApprovalsNeedTheBrowser(t *testing.T) {
	for _, id := range []string{
		"approveOrgPluginInstall", "approveOrgPluginUpgrade", "reapproveOrgPluginInstall", "approveOrgPluginPairing",
		"approvePlatformPluginInstall", "approvePlatformPluginUpgrade", "reapprovePlatformPluginInstall", "approvePlatformPluginPairing",
	} {
		if op := mustOp(t, id); op.Status != catalog.StatusExcluded || op.Group != "plugins" {
			t.Errorf("%s: status %s group %s", id, op.Status, op.Group)
			continue
		}
		f := runOffline(t, id)
		if f.Err.Kind != contract.KindBrowserOnly || f.Exit != contract.ExitBrowser || !strings.Contains(f.Err.Hint, "human verification") {
			t.Errorf("%s: kind %s exit %d hint %q", id, f.Err.Kind, f.Exit, f.Err.Hint)
		}
	}
	// Narrowing a grant and rotating a plugin token do accept a management token.
	for _, id := range []string{"narrowOrgPluginGrant", "rotateOrgPluginCredential", "lookupOrgPluginPairing"} {
		if op := mustOp(t, id); op.Status != catalog.StatusAvailable {
			t.Errorf("%s: status %s", id, op.Status)
		}
	}
}

func TestImagePreprocessTestIsNotSentYet(t *testing.T) {
	f := runOffline(t, "testImagePreprocess")
	if f.Err.Kind != contract.KindUsage || f.Exit != contract.ExitUsage || !strings.Contains(f.Err.Message, "multipart form with text fields") {
		t.Fatalf("kind %s exit %d message %q", f.Err.Kind, f.Exit, f.Err.Message)
	}
}
