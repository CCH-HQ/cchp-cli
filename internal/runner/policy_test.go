package runner

import (
	"errors"
	"testing"

	"github.com/CCH-HQ/cchp-cli/internal/assets"
	"github.com/CCH-HQ/cchp-cli/internal/catalog"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

func mustOp(t *testing.T, id string) *catalog.Operation {
	t.Helper()
	op, ok := assets.Catalog().Get(id)
	if !ok {
		t.Fatalf("no operation %s", id)
	}
	return op
}

func policyErr(t *testing.T, o Options) *contract.Failure {
	t.Helper()
	err := checkPolicy(o)
	if err == nil {
		return nil
	}
	var f *contract.Failure
	if !errors.As(err, &f) {
		t.Fatalf("unexpected error %T", err)
	}
	if f.Err.Kind != contract.KindPolicy || f.Exit != contract.ExitPolicy {
		t.Fatalf("kind %s exit %d", f.Err.Kind, f.Exit)
	}
	return f
}

func TestAgentModeIsReadOnlyByDefault(t *testing.T) {
	read := mustOp(t, "listOrgProviders")
	write := mustOp(t, "createOrgProvider")
	if f := policyErr(t, Options{Op: read, Agent: true}); f != nil {
		t.Fatalf("reads must run: %v", f)
	}
	if f := policyErr(t, Options{Op: write, Agent: true}); f == nil {
		t.Fatal("writes must need --allow-write")
	}
	if f := policyErr(t, Options{Op: write, Agent: true, AllowWrite: true}); f != nil {
		t.Fatalf("allowed write refused: %v", f)
	}
}

func TestDestructiveNeedsConfirmNamingTheOperation(t *testing.T) {
	del := mustOp(t, "deleteOrgProvider")
	if del.Policy.Risk != catalog.RiskDestructive {
		t.Fatalf("deleteOrgProvider risk %s", del.Policy.Risk)
	}
	if f := policyErr(t, Options{Op: del, Agent: true, AllowWrite: true}); f == nil {
		t.Fatal("destructive without --confirm must be refused")
	}
	if f := policyErr(t, Options{Op: del, Agent: true, AllowWrite: true, Confirm: "deleteModel"}); f == nil {
		t.Fatal("--confirm must name this operation")
	}
	if f := policyErr(t, Options{Op: del, Agent: true, AllowWrite: true, Confirm: del.ID}); f != nil {
		t.Fatalf("confirmed delete refused: %v", f)
	}
}

func TestSecretOperationsNeedADestination(t *testing.T) {
	create := mustOp(t, "createAPIKey")
	if !create.Policy.SecretOutput() {
		t.Fatal("createAPIKey returns a one-time secret")
	}
	base := Options{Op: create, Agent: true, AllowWrite: true, Confirm: create.ID}
	if f := policyErr(t, base); f == nil {
		t.Fatal("secret output without --secret-out must be refused")
	}
	base.SecretOut = "secret.json"
	if f := policyErr(t, base); f != nil {
		t.Fatalf("refused: %v", f)
	}
}

func TestHumanModeConfirmsDestructiveCalls(t *testing.T) {
	del := mustOp(t, "deleteOrgProvider")
	if f := policyErr(t, Options{Op: del}); f == nil {
		t.Fatal("human destructive call without confirmation must be refused")
	}
	if f := policyErr(t, Options{Op: del, HumanConfirmed: true}); f != nil {
		t.Fatalf("confirmed: %v", f)
	}
	if f := policyErr(t, Options{Op: mustOp(t, "createOrgProvider")}); f != nil {
		t.Fatalf("human writes run without flags: %v", f)
	}
}

func TestDryRunNeedsNoFlags(t *testing.T) {
	if f := policyErr(t, Options{Op: mustOp(t, "deleteOrgProvider"), Agent: true, DryRun: true}); f != nil {
		t.Fatalf("dry run refused: %v", f)
	}
}

func TestUnclassifiedOperationsAreWrites(t *testing.T) {
	op := &catalog.Operation{ID: "newOperation", Method: "GET", Policy: catalog.Policy{Risk: catalog.RiskWrite}}
	f := policyErr(t, Options{Op: op, Agent: true})
	if f == nil {
		t.Fatal("an unclassified operation must be treated as a write")
	}
}

func TestEveryReadIsSafeToRetryAndOnlyReads(t *testing.T) {
	for _, op := range assets.Catalog().Operations {
		if op.Policy.Retry && op.Policy.Risk != catalog.RiskRead {
			t.Errorf("%s retries but is %s", op.ID, op.Policy.Risk)
		}
		// A DELETE that loses nothing may be a plain write, with a note saying why.
		if op.Method == "DELETE" && op.Policy.Risk != catalog.RiskDestructive && (op.Policy.Risk != catalog.RiskWrite || op.Policy.Note == "") {
			t.Errorf("%s deletes but is %s", op.ID, op.Policy.Risk)
		}
	}
}

// Client recordings in CCHP v1.0.17 hand out a temporary recording key once,
// on create and on rotate; it must go to --secret-out, never to stdout.
func TestRecordingKeysAreOneTimeSecrets(t *testing.T) {
	for _, id := range []string{"createClientRecording", "rotateClientRecordingKey"} {
		op := mustOp(t, id)
		if !op.Policy.SecretOutput() || len(op.Policy.SecretFields) != 1 || op.Policy.SecretFields[0] != "apiKey" {
			t.Fatalf("%s secret fields %v", id, op.Policy.SecretFields)
		}
		o := Options{Op: op, Agent: true, AllowWrite: true, Confirm: op.ID}
		if f := policyErr(t, o); f == nil {
			t.Fatalf("%s without --secret-out must be refused", id)
		}
		o.SecretOut = "recording-key.json"
		if f := policyErr(t, o); f != nil {
			t.Fatalf("%s refused: %v", id, f)
		}
	}
	if mustOp(t, "rotateClientRecordingKey").Policy.Risk != catalog.RiskDestructive {
		t.Fatal("rotating revokes the old recording key at once")
	}
}
