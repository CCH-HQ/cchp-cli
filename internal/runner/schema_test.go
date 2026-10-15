package runner

import (
	"errors"
	"strings"
	"testing"

	"github.com/CCH-HQ/cchp-cli/internal/assets"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

func validate(t *testing.T, opID, body string) []contract.FieldError {
	t.Helper()
	op, ok := assets.Catalog().Get(opID)
	if !ok {
		t.Fatalf("no operation %s", opID)
	}
	err := ValidateBody(op, []byte(body))
	if err == nil {
		return nil
	}
	var f *contract.Failure
	if !errors.As(err, &f) {
		t.Fatalf("unexpected error type %T: %v", err, err)
	}
	if f.Err.Kind != contract.KindInvalidInput || f.Exit != contract.ExitInvalid {
		t.Fatalf("kind %s exit %d", f.Err.Kind, f.Exit)
	}
	return f.Err.Fields
}

func TestValidateBodyReportsFieldPaths(t *testing.T) {
	fields := validate(t, "createAPIKey", `{"name": 5}`)
	want := map[string]string{
		"body":      "missing property 'permissions'",
		"body.name": "got number, want string",
	}
	if len(fields) != len(want) {
		t.Fatalf("got %d field errors, want %d: %+v", len(fields), len(want), fields)
	}
	for _, f := range fields {
		if !strings.Contains(f.Message, want[f.Location]) {
			t.Errorf("%s: got %q, want it to contain %q", f.Location, f.Message, want[f.Location])
		}
	}
}

func TestValidateBodyRejectsUnknownFields(t *testing.T) {
	fields := validate(t, "createOrgProvider", `{"name":"Acme","api_key":"x"}`)
	if len(fields) != 1 || fields[0].Location != "body" || !strings.Contains(fields[0].Message, "api_key") {
		t.Fatalf("got %+v", fields)
	}
}

func TestValidateBodyNestedPath(t *testing.T) {
	fields := validate(t, "createAPIKey", `{"name":"n","permissions":["a", 3]}`)
	if len(fields) != 1 || fields[0].Location != "body.permissions[1]" {
		t.Fatalf("got %+v", fields)
	}
}

func TestValidateBodyRejectsInvalidJSON(t *testing.T) {
	fields := validate(t, "createAPIKey", `{"name": "x",}`)
	if len(fields) != 1 || fields[0].Location != "body" {
		t.Fatalf("got %+v", fields)
	}
	fields = validate(t, "createAPIKey", `{"name":"x","permissions":["a"]} {}`)
	if len(fields) != 1 {
		t.Fatalf("trailing data accepted: %+v", fields)
	}
}

func TestValidateBodyAcceptsValid(t *testing.T) {
	if fields := validate(t, "createAPIKey", `{"name":"n","permissions":["usage:read"],"expiresAt":null}`); fields != nil {
		t.Fatalf("got %+v", fields)
	}
}
