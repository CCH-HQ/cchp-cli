package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/CCH-HQ/cchp-cli/internal/auth"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

func TestDryRunOrgIDMessageMatchesAvailableFlags(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CCHP_BASE_URL", "https://hub.example.com")
	t.Setenv("CCHP_TOKEN", "")
	t.Setenv("CCHP_TOKEN_FILE", "")
	t.Setenv("CCHP_CREDENTIAL_STORE", "file")

	store, err := auth.OpenStore(auth.Dirs{Config: dir + "/config", Cache: dir + "/cache"})
	if err != nil {
		t.Fatal(err)
	}
	r := New(store, "test")

	// Operation with an --org-id parameter should suggest passing --org-id.
	_, err = r.Run(context.Background(), Options{
		Op:     mustOp(t, "listOrgProviders"),
		DryRun: true,
		Agent:  true,
	})
	var f *contract.Failure
	if !errors.As(err, &f) || f.Err.Kind != contract.KindUsage || !strings.Contains(f.Err.Message, "--org-id") {
		t.Fatalf("listOrgProviders dry run: got %v, want usage failure mentioning --org-id", err)
	}

	// Operation with X-CCH-Organization header and no --org-id flag must not
	// tell the user to pass a nonexistent --org-id flag.
	_, err = r.Run(context.Background(), Options{
		Op:     mustOp(t, "retryCCHubImport"),
		Params: map[string][]string{"job-id": {"job_123"}},
		DryRun: true,
		Agent:  true,
	})
	f = nil
	if !errors.As(err, &f) || f.Err.Kind != contract.KindUsage {
		t.Fatalf("retryCCHubImport dry run: got %v, want usage failure", err)
	}
	if strings.Contains(f.Err.Message, "--org-id") {
		t.Fatalf("retryCCHubImport has no --org-id flag, but error said: %q", f.Err.Message)
	}
	if !strings.Contains(f.Err.Hint, "cchp login") {
		t.Fatalf("retryCCHubImport dry run hint %q should mention cchp login", f.Err.Hint)
	}
}
