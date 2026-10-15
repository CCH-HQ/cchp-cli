package runner

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/engine"
)

func problemResult(status int, body string) *engine.Result {
	h := http.Header{}
	h.Set("Content-Type", "application/problem+json")
	h.Set("X-Request-Id", "rid-1")
	return &engine.Result{Status: status, Header: h, Raw: []byte(body)}
}

func TestHTTPFailureKindsAndExitCodes(t *testing.T) {
	op := mustOp(t, "listUsers")
	cases := []struct {
		status int
		body   string
		kind   string
		exit   int
		text   string
	}{
		{401, `{"code":10003,"detail":"unauthorized","request_id":"rid-1"}`, contract.KindUnauthorized, contract.ExitAuth, "didn't accept your token"},
		{403, `{"code":10004,"detail":"forbidden"}`, contract.KindPermission, contract.ExitPermission, "Ask an admin for the member:read permission."},
		{403, `{"code":10004,"detail":"browser user session required"}`, contract.KindBrowserOnly, contract.ExitBrowser, "sign in from the browser"},
		{404, `{"code":10005,"detail":"not found"}`, contract.KindNotFound, contract.ExitNotFound, "Nothing was found"},
		{409, `{"code":10007,"detail":"step out of order"}`, contract.KindConflict, contract.ExitConflict, "step out of order"},
		{422, `{"code":30120,"detail":"validation failed","errors":[{"message":"expected string","location":"body.name"}]}`, contract.KindValidation, contract.ExitInvalid, "rejected the input"},
		{428, `{"code":30005,"detail":"additional verification required","details":{"challenge":{"action":"auth_token.create","presentation":"pow"}}}`, contract.KindHumanCheck, contract.ExitBrowser, "human check in the browser"},
		{429, `{"code":10010,"detail":"slow down","retry_after":7}`, contract.KindRateLimited, contract.ExitRateLimited, "limiting requests"},
		{502, `{"code":10002,"detail":"bad gateway"}`, contract.KindServer, contract.ExitServer, "had a problem"},
	}
	for _, c := range cases {
		f := httpFailure(op, problemResult(c.status, c.body))
		if f.Err.Kind != c.kind || f.Exit != c.exit || !strings.Contains(f.Err.Message, c.text) {
			t.Errorf("%d: got kind=%s exit=%d message=%q", c.status, f.Err.Kind, f.Exit, f.Err.Message)
		}
		if f.RequestID != "rid-1" {
			t.Errorf("%d: request id %q", c.status, f.RequestID)
		}
	}
	f := httpFailure(op, problemResult(422, `{"errors":[{"message":"expected string","location":"body.name"}]}`))
	if len(f.Err.Fields) != 1 || f.Err.Fields[0].Location != "body.name" {
		t.Errorf("fields %+v", f.Err.Fields)
	}
	f = httpFailure(op, problemResult(428, `{"details":{"challenge":{"action":"auth_token.create"}}}`))
	if f.Err.ChallengeAction != "auth_token.create" {
		t.Errorf("challenge action %q", f.Err.ChallengeAction)
	}
	f = httpFailure(op, problemResult(429, `{"retry_after":7}`))
	if !f.Err.Retryable || !strings.Contains(f.Err.Hint, "7 seconds") {
		t.Errorf("rate limit %+v", f.Err)
	}
}

func TestProblemBodiesAreRedacted(t *testing.T) {
	f := httpFailure(mustOp(t, "listUsers"), problemResult(400, `{"detail":"bad token cch_auth_live_AB_secretvalue","params":{"api_key":"sk-live"}}`))
	if strings.Contains(f.Err.Detail, "secretvalue") || strings.Contains(string(f.Err.Problem), "sk-live") || strings.Contains(string(f.Err.Problem), "secretvalue") {
		t.Fatalf("secrets in error: %+v %s", f.Err, f.Err.Problem)
	}
}

func TestMutationOutcomes(t *testing.T) {
	create := mustOp(t, "createOrgProvider")
	if got := mutationOutcome(create, 422); got != contract.OutcomeNotApplied {
		t.Errorf("4xx: %s", got)
	}
	if got := mutationOutcome(create, 503); got != contract.OutcomeUnknown {
		t.Errorf("5xx: %s", got)
	}
	if got := mutationOutcome(mustOp(t, "listUsers"), 503); got != "" {
		t.Errorf("reads have no outcome: %s", got)
	}
}

func TestTransportFailures(t *testing.T) {
	create := mustOp(t, "createOrgProvider")
	dial := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	err := transportFailure(create, &engine.NotSentError{Err: dial}, "k1")
	var f *contract.Failure
	if !errors.As(err, &f) || f.Err.Kind != contract.KindNotSent || f.Exit != contract.ExitNotSent || f.Outcome != contract.OutcomeNotApplied {
		t.Fatalf("not sent: %+v", err)
	}
	err = transportFailure(create, &engine.UnknownOutcomeError{Err: errors.New("EOF")}, "k1")
	if !errors.As(err, &f) || f.Err.Kind != contract.KindUnknownOutcome || f.Exit != contract.ExitUnknown || f.Outcome != contract.OutcomeUnknown {
		t.Fatalf("unknown: %+v", err)
	}
	if f.Err.Retryable || !strings.Contains(f.Err.Hint, "--idempotency-key k1") {
		t.Fatalf("a mutation with an unknown outcome is not retryable and names the key: %+v", f.Err)
	}
}
