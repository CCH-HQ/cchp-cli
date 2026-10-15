package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/CCH-HQ/cchp-cli/internal/catalog"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/engine"
	"github.com/CCH-HQ/cchp-cli/internal/redact"
)

// problem is CCHP's problem+json error body.
type problem struct {
	Title     string         `json:"title"`
	Status    int            `json:"status"`
	Detail    string         `json:"detail"`
	Code      int64          `json:"code"`
	RequestID string         `json:"request_id"`
	Hint      string         `json:"hint"`
	Name      string         `json:"name"`
	Params    map[string]any `json:"params"`
	Details   map[string]any `json:"details"`
	Errors    []struct {
		Message  string `json:"message"`
		Location string `json:"location"`
	} `json:"errors"`
	RetryAfter int64 `json:"retry_after"`
}

// httpFailure turns a non-2xx response into a plain-language failure.
func httpFailure(op *catalog.Operation, res *engine.Result) *contract.Failure {
	var p problem
	if len(res.Raw) > 0 && isJSON(res.Header) {
		if err := json.Unmarshal(res.Raw, &p); err != nil {
			p.Detail = strings.TrimSpace(string(res.Raw))
		}
	} else if len(res.Raw) > 0 {
		p.Detail = strings.TrimSpace(string(res.Raw))
	}
	f := &contract.Failure{Status: res.Status, RequestID: requestID(res)}
	e := &f.Err
	e.Code = p.Code
	e.Detail = redact.String(p.Detail)
	e.Hint = redact.String(p.Hint)
	if len(res.Raw) > 0 && isJSON(res.Header) {
		if masked, err := redact.JSON(res.Raw); err == nil {
			e.Problem = masked
		}
	}
	for _, fe := range p.Errors {
		e.Fields = append(e.Fields, contract.FieldError{Location: fe.Location, Message: fe.Message})
	}
	if field, ok := p.Params["field"].(string); ok && len(e.Fields) == 0 {
		e.Fields = append(e.Fields, contract.FieldError{Location: "body." + field, Message: p.Detail})
	}

	switch status := res.Status; {
	case status == http.StatusUnauthorized:
		e.Kind, f.Exit = contract.KindUnauthorized, contract.ExitAuth
		e.Message = "The server didn't accept your token. It may be revoked, expired or mistyped."
		e.Hint = "Run cchp login with a valid management token."
	case status == http.StatusForbidden && strings.Contains(p.Detail, "browser"):
		e.Kind, f.Exit = contract.KindBrowserOnly, contract.ExitBrowser
		e.Message = "This server needs you to sign in from the browser for this action."
	case status == http.StatusForbidden:
		e.Kind, f.Exit = contract.KindPermission, contract.ExitPermission
		e.RequiredPermission = op.Policy.Permission
		if op.Policy.Permission != "" {
			e.Message = fmt.Sprintf("Your token can't do this. Ask an admin for the %s permission.", op.Policy.Permission)
		} else {
			e.Message = "Your token can't do this. Ask an admin for a token with permission for this action."
		}
		e.Hint = "Effective permissions are what both you and the token are allowed. Run cchp api getMyPermissionSummary to see yours."
	case status == http.StatusNotFound:
		e.Kind, f.Exit = contract.KindNotFound, contract.ExitNotFound
		e.Message = "Nothing was found there. Check the ID."
	case status == http.StatusConflict || status == http.StatusPreconditionFailed:
		e.Kind, f.Exit = contract.KindConflict, contract.ExitConflict
		e.Message = "The server refused because the current state doesn't allow this: " + nonEmpty(p.Detail, http.StatusText(status))
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		e.Kind, f.Exit = contract.KindValidation, contract.ExitInvalid
		e.Message = "The server rejected the input: " + nonEmpty(p.Detail, http.StatusText(status))
	case status == http.StatusPreconditionRequired:
		e.Kind, f.Exit = contract.KindHumanCheck, contract.ExitBrowser
		e.Message = "This server needs you to complete a human check in the browser for this action."
		e.Hint = "Do this action in the CCHP console, or ask an admin to change its human verification setting."
		if ch, ok := p.Details["challenge"].(map[string]any); ok {
			e.ChallengeAction, _ = ch["action"].(string)
		}
	case status == http.StatusTooManyRequests:
		e.Kind, f.Exit = contract.KindRateLimited, contract.ExitRateLimited
		e.Retryable = true
		e.Message = "The server is limiting requests right now."
		if p.RetryAfter > 0 {
			e.Hint = fmt.Sprintf("Try again in %d seconds.", p.RetryAfter)
		} else if ra := res.Header.Get("Retry-After"); ra != "" {
			e.Hint = "Try again after " + ra + " seconds."
		}
	case status >= 500:
		e.Kind, f.Exit = contract.KindServer, contract.ExitServer
		e.Retryable = op.Policy.Retry
		e.Message = "The server had a problem handling this: " + nonEmpty(p.Detail, http.StatusText(status))
	default:
		e.Kind, f.Exit = contract.KindHTTP, contract.ExitInternal
		e.Message = fmt.Sprintf("The server answered %d %s.", status, http.StatusText(status))
	}
	return f
}

func nonEmpty(s, alt string) string {
	if strings.TrimSpace(s) == "" {
		return alt
	}
	return s
}

// excludedFailure explains why cchp does not run an operation. Operations that
// need a signed-in browser get the plain browser message.
func excludedFailure(op *catalog.Operation) *contract.Failure {
	if strings.Contains(op.Reason, "browser") {
		f := contract.Fail(contract.KindBrowserOnly, contract.ExitBrowser, "This server needs you to sign in from the browser for this action.")
		f.Err.Hint = op.Reason + " Use the CCHP console."
		return f
	}
	return contract.Fail(contract.KindUsage, contract.ExitUsage, "cchp does not run %s: %s", op.ID, op.Reason)
}

// transportFailure classifies an engine error by whether the request may have
// reached the server. Mutations are never retried automatically.
func transportFailure(op *catalog.Operation, err error, idemKey string) error {
	var f *contract.Failure
	if errors.As(err, &f) {
		return f
	}
	msg := redact.String(err.Error())
	var notSent *engine.NotSentError
	if errors.As(err, &notSent) {
		out := contract.Fail(contract.KindNotSent, contract.ExitNotSent, "The request was not sent: %s", msg)
		out.Err.Retryable = true
		if op.Method != http.MethodGet {
			out.Outcome = contract.OutcomeNotApplied
		}
		return out
	}
	out := contract.Fail(contract.KindUnknownOutcome, contract.ExitUnknown, "No answer from the server: %s", msg)
	if op.Method == http.MethodGet {
		out.Err.Retryable = true
		return out
	}
	out.Outcome = contract.OutcomeUnknown
	out.Err.Hint = idempotencyHint(idemKey, contract.OutcomeUnknown)
	return out
}
