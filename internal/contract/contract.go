// Package contract defines cchp's machine output: one JSON result object on
// stdout per run (or NDJSON lines for streams), stable error kinds and exit codes.
package contract

import (
	"encoding/json"
	"fmt"
	"io"
)

// SchemaVersion changes only when an existing field changes meaning.
const SchemaVersion = 1

// Exit codes. A run that fails always exits nonzero.
const (
	ExitOK          = 0
	ExitInternal    = 1
	ExitUsage       = 2
	ExitAuth        = 3
	ExitPermission  = 4
	ExitNotFound    = 5
	ExitConflict    = 6
	ExitInvalid     = 7
	ExitBrowser     = 8
	ExitRateLimited = 9
	ExitServer      = 10
	ExitNotSent     = 11
	ExitUnknown     = 12
	ExitPolicy      = 13
	ExitNotSignedIn = 14
)

// Error kinds.
const (
	KindUsage          = "usage"
	KindInvalidInput   = "invalid_input"
	KindPolicy         = "policy"
	KindNotSignedIn    = "not_signed_in"
	KindUnauthorized   = "unauthorized"
	KindPermission     = "permission_denied"
	KindNotFound       = "not_found"
	KindConflict       = "conflict"
	KindValidation     = "validation"
	KindHumanCheck     = "human_verification_required"
	KindBrowserOnly    = "browser_session_required"
	KindRateLimited    = "rate_limited"
	KindServer         = "server_error"
	KindHTTP           = "http_error"
	KindNotSent        = "not_sent"
	KindUnknownOutcome = "unknown_outcome"
	KindInternal       = "internal"
)

// Outcomes of a mutation.
const (
	OutcomeApplied    = "applied"
	OutcomeNotApplied = "not_applied"
	OutcomeUnknown    = "unknown"
)

// Result is the JSON object cchp prints on stdout.
type Result struct {
	SchemaVersion  int             `json:"schema_version"`
	OK             bool            `json:"ok"`
	Operation      string          `json:"operation,omitempty"`
	Status         int             `json:"status,omitempty"`
	RequestID      string          `json:"request_id,omitempty"`
	Data           json.RawMessage `json:"data,omitempty"`
	Pagination     *Pagination     `json:"pagination,omitempty"`
	Outcome        string          `json:"outcome,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Secret         *SecretOutput   `json:"secret,omitempty"`
	File           *FileOutput     `json:"file,omitempty"`
	Stream         *StreamSummary  `json:"stream,omitempty"`
	DryRun         *DryRun         `json:"dry_run,omitempty"`
	Error          *Error          `json:"error,omitempty"`
	Warnings       []string        `json:"warnings,omitempty"`
}

// Pagination describes where a list stopped and how to continue.
type Pagination struct {
	Pages      int    `json:"pages"`
	Items      int    `json:"items"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor,omitempty"`
	NextPage   int    `json:"next_page,omitempty"`
	Total      *int64 `json:"total,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

// SecretOutput says where one-time secrets went.
type SecretOutput struct {
	Path   string   `json:"path"`
	Fields []string `json:"fields"`
}

// FileOutput describes a downloaded file.
type FileOutput struct {
	Path        string `json:"path"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
	ContentType string `json:"content_type,omitempty"`
}

// StreamSummary closes an NDJSON stream.
type StreamSummary struct {
	Events int    `json:"events"`
	Ended  string `json:"ended"` // max_events, deadline or server_closed
}

// DryRun is the request cchp would have sent.
type DryRun struct {
	Method   string            `json:"method"`
	URL      string            `json:"url"`
	Headers  map[string]string `json:"headers"`
	Body     json.RawMessage   `json:"body,omitempty"`
	BodyText string            `json:"body_text,omitempty"`
	Policy   PolicyView        `json:"policy"`
}

// PolicyView explains what a real run would need.
type PolicyView struct {
	Risk         string `json:"risk"`
	NeedsWrite   bool   `json:"needs_allow_write"`
	NeedsConfirm bool   `json:"needs_confirm"`
	NeedsSecret  bool   `json:"needs_secret_out"`
	Retry        bool   `json:"retry"`
	Classified   bool   `json:"classified"`
}

// Error is the failure description.
type Error struct {
	Kind               string          `json:"kind"`
	Message            string          `json:"message"`
	Hint               string          `json:"hint,omitempty"`
	Code               int64           `json:"code,omitempty"`
	Detail             string          `json:"detail,omitempty"`
	RequiredPermission string          `json:"required_permission,omitempty"`
	ChallengeAction    string          `json:"challenge_action,omitempty"`
	Fields             []FieldError    `json:"fields,omitempty"`
	Retryable          bool            `json:"retryable"`
	Problem            json.RawMessage `json:"problem,omitempty"`
}

// FieldError points at one bad input value.
type FieldError struct {
	Location string `json:"location"`
	Message  string `json:"message"`
}

// Failure is an error that carries its contract kind and exit code.
type Failure struct {
	Err  Error
	Exit int
	// Status and RequestID are set when the server answered.
	Status    int
	RequestID string
	Outcome   string
}

func (f *Failure) Error() string { return f.Err.Message }

// Fail builds a Failure.
func Fail(kind string, exit int, format string, args ...any) *Failure {
	return &Failure{Err: Error{Kind: kind, Message: fmt.Sprintf(format, args...)}, Exit: exit}
}

// WithHint adds a hint.
func (f *Failure) WithHint(format string, args ...any) *Failure {
	f.Err.Hint = fmt.Sprintf(format, args...)
	return f
}

// Write prints one result object followed by a newline.
func Write(w io.Writer, r *Result) error {
	r.SchemaVersion = SchemaVersion
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// Event is one NDJSON stream line.
type Event struct {
	Type      string          `json:"type"` // event
	Operation string          `json:"operation"`
	Seq       int             `json:"seq"`
	Event     string          `json:"event,omitempty"`
	ID        string          `json:"id,omitempty"`
	Data      json.RawMessage `json:"data"`
}

// WriteEvent prints one NDJSON stream line.
func WriteEvent(w io.Writer, e *Event) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}
