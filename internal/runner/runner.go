// Package runner executes one catalog operation end to end: input checks,
// policy, credentials, the Restish engine, pagination, secrets and downloads.
// The CLI and the TUI both call it, so humans and agents get the same behaviour.
package runner

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CCH-HQ/cchp-cli/internal/assets"
	"github.com/CCH-HQ/cchp-cli/internal/auth"
	"github.com/CCH-HQ/cchp-cli/internal/catalog"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/engine"
	"github.com/CCH-HQ/cchp-cli/internal/redact"
)

// Options is one operation call.
type Options struct {
	Op *catalog.Operation
	// Params holds parameter values by flag name; arrays repeat.
	Params map[string][]string
	// BodyFile is a JSON or binary body file; "-" reads Stdin.
	BodyFile string
	Stdin    io.Reader
	// Body is a JSON body given directly by cchp's own workflows.
	Body []byte
	// FormFiles maps multipart file fields to local paths.
	FormFiles map[string]string
	// SecretOut receives one-time secrets.
	SecretOut string
	// Output receives a binary download; "-" means Stdout.
	Output string
	Stdout io.Writer
	// PageLimit is the number of pages to fetch (default 1).
	PageLimit int
	// MaxItems stops paging once this many items arrived.
	MaxItems int
	// MaxEvents stops a stream after this many events.
	MaxEvents int
	// Timeout bounds the whole run.
	Timeout time.Duration
	// Agent turns on the agent policy: read-only unless allowed.
	Agent      bool
	AllowWrite bool
	Confirm    string
	// HumanConfirmed records an interactive confirmation in human mode.
	HumanConfirmed bool
	DryRun         bool
	// OnEvent receives stream events as they arrive.
	OnEvent func(*contract.Event) error
}

// Runner holds what every call needs.
type Runner struct {
	Store     *auth.Store
	Catalog   *catalog.Catalog
	UserAgent string
	// Debug, when set, receives a redacted trace of each request.
	Debug io.Writer

	cred     *auth.Credential
	identity *auth.Identity
}

// New builds a runner from the embedded catalog.
func New(store *auth.Store, version string) *Runner {
	return &Runner{Store: store, Catalog: assets.Catalog(), UserAgent: "cchp/" + version}
}

// Credential resolves and remembers the credential for this process.
func (r *Runner) Credential() (*auth.Credential, error) {
	if r.cred != nil {
		return r.cred, nil
	}
	c, err := r.Store.Resolve()
	if err != nil {
		return nil, err
	}
	r.cred = c
	return c, nil
}

// UseCredential sets the credential explicitly (login verifies a new token).
func (r *Runner) UseCredential(c *auth.Credential) {
	r.cred = c
	r.identity = c.Identity
}

func (r *Runner) engineFor(server, token string) *engine.Engine {
	return &engine.Engine{
		BaseURL:   server,
		Token:     token,
		StateDir:  filepath.Join(r.Store.Dirs.Cache, "engine"),
		SpecGzip:  assets.EngineSpecGzip,
		UserAgent: r.UserAgent,
	}
}

// Identity returns the token's identity, asking the server once if needed.
func (r *Runner) Identity(ctx context.Context) (*auth.Identity, error) {
	if r.identity != nil {
		return r.identity, nil
	}
	cred, err := r.Credential()
	if err != nil {
		return nil, err
	}
	if cred.Identity != nil && cred.Identity.OrganizationID != "" {
		r.identity = cred.Identity
		return r.identity, nil
	}
	id, err := r.Whoami(ctx, cred)
	if err != nil {
		return nil, err
	}
	r.identity = id
	return id, nil
}

// Whoami asks the server who the credential belongs to: getWhoami for the
// token and its organization, getMe for the person's email and name.
func (r *Runner) Whoami(ctx context.Context, cred *auth.Credential) (*auth.Identity, error) {
	var id auth.Identity
	if err := r.readJSON(ctx, cred, "getWhoami", &id); err != nil {
		return nil, err
	}
	var me struct {
		User struct {
			Email       string `json:"email"`
			DisplayName string `json:"displayName"`
		} `json:"user"`
		Organizations []struct {
			ID          string  `json:"id"`
			Name        string  `json:"name"`
			DisplayName *string `json:"displayName"`
		} `json:"organizations"`
	}
	if err := r.readJSON(ctx, cred, "getMe", &me); err != nil {
		return nil, err
	}
	if id.Email == "" {
		id.Email = me.User.Email
	}
	id.Name = me.User.DisplayName
	for _, o := range me.Organizations {
		if o.ID == id.OrganizationID {
			id.OrganizationName = o.Name
			if o.DisplayName != nil && *o.DisplayName != "" {
				id.OrganizationName = *o.DisplayName
			}
		}
	}
	return &id, nil
}

func (r *Runner) readJSON(ctx context.Context, cred *auth.Credential, opID string, out any) error {
	op, ok := r.Catalog.Get(opID)
	if !ok {
		return fmt.Errorf("catalog has no %s", opID)
	}
	res, err := r.engineFor(cred.Server, cred.Token).Do(ctx, engine.Request{Op: op, Retry: true, HeaderTimeout: 30 * time.Second})
	if err != nil {
		return transportFailure(op, err, "")
	}
	if res.Status != http.StatusOK {
		return httpFailure(op, res)
	}
	if err := json.Unmarshal(res.Raw, out); err != nil {
		return fmt.Errorf("reading %s: %w", opID, err)
	}
	return nil
}

// Run executes one operation and returns the result object.
func (r *Runner) Run(ctx context.Context, o Options) (*contract.Result, error) {
	op := o.Op
	switch op.Status {
	case catalog.StatusExcluded:
		return nil, excludedFailure(op)
	case catalog.StatusUnsupported:
		return nil, contract.Fail(contract.KindUsage, contract.ExitUsage, "cchp cannot run %s yet: %s", op.ID, op.Reason)
	}
	if o.Timeout <= 0 {
		o.Timeout = 60 * time.Second
	}
	if o.PageLimit <= 0 {
		o.PageLimit = 1
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	// A dry run needs only the server address; public operations may run
	// without a credential; everything else needs one.
	var server, token string
	cred, err := r.Credential()
	switch {
	case err == nil && !o.DryRun:
		server, token = cred.Server, cred.Token
	case err == nil || (o.DryRun || op.Public) && errors.Is(err, auth.ErrNotSignedIn):
		s, serr := r.Store.Server()
		if serr != nil {
			return nil, notSignedIn(serr)
		}
		server = s
	default:
		return nil, notSignedIn(err)
	}

	params, err := r.collectParams(ctx, o)
	if err != nil {
		return nil, err
	}
	req := engine.Request{Op: op, Retry: op.Policy.Retry, HeaderTimeout: o.Timeout, MaxEvents: o.MaxEvents, DryRun: o.DryRun}
	if err := r.prepareBody(&req, o); err != nil {
		return nil, err
	}
	if err := checkPolicy(o); err != nil {
		return nil, err
	}
	if op.Response.Kind == catalog.ResponseBinary && o.Output == "" && !o.DryRun {
		return nil, contract.Fail(contract.KindUsage, contract.ExitUsage, "%s returns a file; name where to save it with --output <path>", op.ID).WithHint("Use --output - to write it to standard output.")
	}

	idemKey := ""
	if op.Idempotency {
		idemKey = first(params["idempotency-key"])
		if idemKey == "" {
			idemKey = newKey()
			params["idempotency-key"] = []string{idemKey}
		}
	}
	if op.OrgHeader {
		orgID, err := r.orgID(ctx, o)
		if err != nil {
			return nil, err
		}
		req.Headers = append(req.Headers, "X-CCH-Organization: "+orgID)
	}

	eng := r.engineFor(server, token)
	result := &contract.Result{Operation: op.ID, IdempotencyKey: idemKey}
	pathArgs, flags := splitParams(op, params)
	req.PathArgs, req.Flags = pathArgs, flags

	if o.DryRun {
		planned, err := eng.Plan(ctx, req)
		if err != nil {
			return nil, transportFailure(op, err, idemKey)
		}
		view, err := dryRunView(op, planned, req)
		if err != nil {
			return nil, err
		}
		result.OK = true
		result.DryRun = view
		return result, nil
	}

	if op.Response.Kind == catalog.ResponseStream {
		return r.stream(ctx, eng, req, o, result)
	}
	if op.Policy.Pagination != nil && op.Response.Kind == catalog.ResponseJSON {
		return r.paged(ctx, eng, req, o, result, params)
	}
	res, err := eng.Do(ctx, req)
	if err != nil {
		return nil, transportFailure(op, err, idemKey)
	}
	r.trace(op, res)
	return r.finish(op, res, o, result)
}

func (r *Runner) trace(op *catalog.Operation, res *engine.Result) {
	if r.Debug == nil {
		return
	}
	fmt.Fprintf(r.Debug, "debug: %s %s -> %d\n", op.Method, op.Path, res.Status)
	for k, v := range redact.HeaderMap(res.Header) {
		fmt.Fprintf(r.Debug, "debug:   %s: %s\n", k, v)
	}
	if res.Diagnostics != "" {
		fmt.Fprintf(r.Debug, "debug: engine: %s\n", redact.String(res.Diagnostics))
	}
}

func notSignedIn(err error) error {
	if errors.Is(err, auth.ErrNotSignedIn) {
		return contract.Fail(contract.KindNotSignedIn, contract.ExitNotSignedIn, "You're not signed in.").
			WithHint("Run cchp login, or set CCHP_BASE_URL with CCHP_TOKEN or CCHP_TOKEN_FILE.")
	}
	return contract.Fail(contract.KindNotSignedIn, contract.ExitNotSignedIn, "%s", redact.String(err.Error()))
}

// orgID is the token's organization, used for orgId and X-CCH-Organization so
// cchp never names a different organization than the token's own.
func (r *Runner) orgID(ctx context.Context, o Options) (string, error) {
	if o.DryRun {
		p, err := r.Store.Current()
		if err != nil {
			return "", err
		}
		if p == nil || p.Identity.OrganizationID == "" {
			return "", contract.Fail(contract.KindUsage, contract.ExitUsage, "a dry run without a saved sign-in cannot know your organization; pass --org-id")
		}
		return p.Identity.OrganizationID, nil
	}
	id, err := r.Identity(ctx)
	if err != nil {
		if f, ok := errors.AsType[*contract.Failure](err); ok {
			return "", f
		}
		return "", notSignedIn(err)
	}
	if id.OrganizationID == "" {
		return "", contract.Fail(contract.KindUsage, contract.ExitUsage, "your token has no organization; pass --org-id")
	}
	return id.OrganizationID, nil
}

func (r *Runner) collectParams(ctx context.Context, o Options) (map[string][]string, error) {
	op := o.Op
	params := map[string][]string{}
	maps.Copy(params, o.Params)
	for _, p := range op.Params {
		vals := params[p.Flag]
		if len(vals) == 0 && p.In == "path" && p.Name == "orgId" {
			orgID, err := r.orgID(ctx, o)
			if err != nil {
				return nil, err
			}
			params[p.Flag] = []string{orgID}
			continue
		}
		if len(vals) == 0 {
			generated := p.In == "header" && strings.EqualFold(p.Name, "Idempotency-Key")
			if p.Required && !generated {
				hint := p.Description + " Pass --" + p.Flag + " <value>."
				if p.In == "path" {
					hint = p.Description + " Pass --" + p.Flag + " <value>, or give it as an argument."
				}
				return nil, contract.Fail(contract.KindUsage, contract.ExitUsage, "%s needs --%s", op.ID, p.Flag).WithHint("%s", hint)
			}
			continue
		}
		if p.Type != "array" && len(vals) > 1 {
			return nil, contract.Fail(contract.KindUsage, contract.ExitUsage, "--%s takes one value", p.Flag)
		}
		for _, v := range vals {
			if err := checkValue(p, v); err != nil {
				return nil, &contract.Failure{Err: contract.Error{Kind: contract.KindInvalidInput, Message: err.Error(),
					Fields: []contract.FieldError{{Location: p.In + "." + p.Name, Message: err.Error()}}}, Exit: contract.ExitInvalid}
			}
		}
	}
	return params, nil
}

func checkValue(p catalog.Param, v string) error {
	typ := p.Type
	if typ == "array" {
		typ = p.ItemType
	}
	switch typ {
	case "integer":
		if _, err := strconv.ParseInt(v, 10, 64); err != nil {
			return fmt.Errorf("--%s must be a whole number, got %q", p.Flag, v)
		}
	case "number":
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return fmt.Errorf("--%s must be a number, got %q", p.Flag, v)
		}
	case "boolean":
		if v != "true" && v != "false" {
			return fmt.Errorf("--%s must be true or false, got %q", p.Flag, v)
		}
	}
	if p.In == "path" && v == "" {
		return fmt.Errorf("--%s cannot be empty", p.Flag)
	}
	if len(p.Enum) > 0 {
		if slices.Contains(p.Enum, v) {
			return nil
		}
		return fmt.Errorf("--%s must be one of %s, got %q", p.Flag, strings.Join(p.Enum, ", "), v)
	}
	return nil
}

func splitParams(op *catalog.Operation, params map[string][]string) ([]string, []engine.Flag) {
	var path []string
	var flags []engine.Flag
	for _, p := range op.Params {
		vals := params[p.Flag]
		if len(vals) == 0 {
			continue
		}
		if p.In == "path" {
			path = append(path, vals[0])
			continue
		}
		for _, v := range vals {
			flags = append(flags, engine.Flag{Name: p.Flag, Value: v})
		}
	}
	return path, flags
}

func (r *Runner) prepareBody(req *engine.Request, o Options) error {
	op := o.Op
	if op.Body == nil {
		if o.BodyFile != "" || o.Body != nil || len(o.FormFiles) > 0 {
			return contract.Fail(contract.KindUsage, contract.ExitUsage, "%s takes no request body", op.ID)
		}
		return nil
	}
	switch op.Body.Kind {
	case "json":
		if len(o.FormFiles) > 0 {
			return contract.Fail(contract.KindUsage, contract.ExitUsage, "%s takes a JSON body; use --body-file", op.ID)
		}
		raw := o.Body
		if raw == nil && o.BodyFile != "" {
			var err error
			raw, err = readInput(o.BodyFile, o.Stdin)
			if err != nil {
				return contract.Fail(contract.KindUsage, contract.ExitUsage, "reading the body: %v", err)
			}
		}
		if raw == nil {
			if op.Body.Required {
				return contract.Fail(contract.KindUsage, contract.ExitUsage, "%s needs a JSON body: --body-file <path> or --body-file - for standard input", op.ID).
					WithHint("Run cchp schema %s to see the body's fields.", op.ID)
			}
			return nil
		}
		if err := ValidateBody(op, raw); err != nil {
			return err
		}
		req.JSONBody = bytes.TrimSpace(raw)
	case "multipart":
		if o.BodyFile != "" {
			return contract.Fail(contract.KindUsage, contract.ExitUsage, "%s uploads files; use --form-file %s=<path>", op.ID, strings.Join(op.Body.FileFields, "|"))
		}
		for _, f := range op.Body.FileFields {
			path, ok := o.FormFiles[f]
			if !ok {
				return contract.Fail(contract.KindUsage, contract.ExitUsage, "%s needs --form-file %s=<path>", op.ID, f)
			}
			if _, err := os.Stat(path); err != nil {
				return contract.Fail(contract.KindUsage, contract.ExitUsage, "--form-file %s: %v", f, err)
			}
		}
		for name := range o.FormFiles {
			if !contains(op.Body.FileFields, name) {
				return contract.Fail(contract.KindUsage, contract.ExitUsage, "%s has no file field %q; it takes %s", op.ID, name, strings.Join(op.Body.FileFields, ", "))
			}
		}
		req.FormFiles = o.FormFiles
	case "binary":
		if o.BodyFile == "" || o.BodyFile == "-" {
			return contract.Fail(contract.KindUsage, contract.ExitUsage, "%s uploads a file; pass --body-file <path>", op.ID)
		}
		if _, err := os.Stat(o.BodyFile); err != nil {
			return contract.Fail(contract.KindUsage, contract.ExitUsage, "--body-file: %v", err)
		}
		req.BinaryFile = o.BodyFile
	default:
		return contract.Fail(contract.KindUsage, contract.ExitUsage, "cchp cannot send %s bodies", op.Body.ContentType)
	}
	return nil
}

func readInput(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" {
		if stdin == nil {
			return nil, errors.New("no standard input")
		}
		return io.ReadAll(io.LimitReader(stdin, 64<<20))
	}
	return os.ReadFile(path)
}

// checkPolicy applies the operation policy. Agent mode is read-only unless
// --allow-write is given; destructive and secret-producing operations also need
// --confirm <operationId> (or an interactive confirmation in human mode).
func checkPolicy(o Options) error {
	op := o.Op
	p := op.Policy
	if o.DryRun {
		return nil
	}
	needsConfirm := p.Risk == catalog.RiskDestructive || p.SecretOutput()
	if o.Agent {
		if p.Risk != catalog.RiskRead && !o.AllowWrite {
			reason := "it changes data"
			if !p.Classified {
				reason = "it is not classified yet, so cchp treats it as a change"
			}
			return contract.Fail(contract.KindPolicy, contract.ExitPolicy, "%s needs --allow-write because %s", op.ID, reason).
				WithHint("Agent mode is read-only by default. Re-run with --allow-write if the change is intended; try --dry-run first.")
		}
		if needsConfirm && o.Confirm != op.ID {
			return contract.Fail(contract.KindPolicy, contract.ExitPolicy, "%s needs --confirm %s", op.ID, op.ID).
				WithHint("%s", confirmReason(op))
		}
		if p.SecretOutput() && o.SecretOut == "" {
			return contract.Fail(contract.KindPolicy, contract.ExitPolicy, "%s returns a secret that is shown only once; name a file for it with --secret-out <path>", op.ID)
		}
		return nil
	}
	if needsConfirm && !o.HumanConfirmed && o.Confirm != op.ID {
		return contract.Fail(contract.KindPolicy, contract.ExitPolicy, "%s needs confirmation; re-run it in a terminal or add --confirm %s", op.ID, op.ID).
			WithHint("%s", confirmReason(op))
	}
	return nil
}

func confirmReason(op *catalog.Operation) string {
	if op.Policy.SecretOutput() {
		return "It returns a secret that is shown only once."
	}
	return "It deletes or replaces data and may not be undone."
}

// ValidateBody checks that raw is one JSON document matching the operation's
// request schema and reports every problem with its field path.
func ValidateBody(op *catalog.Operation, raw []byte) error {
	doc, err := engine.DecodeJSON(raw)
	if err != nil {
		return &contract.Failure{Err: contract.Error{Kind: contract.KindInvalidInput, Message: "the body is not valid JSON: " + err.Error(),
			Fields: []contract.FieldError{{Location: "body", Message: err.Error()}}}, Exit: contract.ExitInvalid}
	}
	fields, err := schemaErrors(op.ID, doc)
	if err != nil {
		return err
	}
	if len(fields) > 0 {
		return &contract.Failure{Err: contract.Error{Kind: contract.KindInvalidInput,
			Message: fmt.Sprintf("the body does not match %s's schema (%d problem%s)", op.ID, len(fields), plural(len(fields))),
			Hint:    "Run cchp schema " + op.ID + " to see the expected fields.",
			Fields:  fields}, Exit: contract.ExitInvalid}
	}
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func contains(list []string, s string) bool {
	return slices.Contains(list, s)
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

func newKey() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "cchp-" + hex.EncodeToString(b)
}

func dryRunView(op *catalog.Operation, planned *engine.PlannedRequest, req engine.Request) (*contract.DryRun, error) {
	view := &contract.DryRun{
		Method:  planned.Method,
		URL:     redact.String(planned.URL),
		Headers: redact.HeaderMap(planned.Header),
		Policy: contract.PolicyView{
			Risk:         op.Policy.Risk,
			NeedsWrite:   op.Policy.Risk != catalog.RiskRead,
			NeedsConfirm: op.Policy.Risk == catalog.RiskDestructive || op.Policy.SecretOutput(),
			NeedsSecret:  op.Policy.SecretOutput(),
			Retry:        op.Policy.Retry,
			Classified:   op.Policy.Classified,
		},
	}
	switch {
	case req.JSONBody != nil:
		masked, err := redact.JSON(planned.Body)
		if err != nil {
			return nil, fmt.Errorf("the engine built a body that is not JSON: %w", err)
		}
		view.Body = masked
	case req.BinaryFile != "":
		view.BodyText = fmt.Sprintf("(contents of %s, %d bytes)", req.BinaryFile, len(planned.Body))
	case len(req.FormFiles) > 0:
		var parts []string
		for k, v := range req.FormFiles {
			parts = append(parts, k+"="+v)
		}
		sort.Strings(parts)
		view.BodyText = "(multipart upload: " + strings.Join(parts, ", ") + ")"
	}
	return view, nil
}

func (r *Runner) finish(op *catalog.Operation, res *engine.Result, o Options, result *contract.Result) (*contract.Result, error) {
	result.Status = res.Status
	result.RequestID = requestID(res)
	if res.Status < 200 || res.Status > 299 {
		f := httpFailure(op, res)
		f.Outcome = mutationOutcome(op, res.Status)
		f.Err.Hint = joinHint(f.Err.Hint, idempotencyHint(result.IdempotencyKey, f.Outcome))
		return nil, f
	}
	result.OK = true
	if op.Method != http.MethodGet {
		result.Outcome = contract.OutcomeApplied
	}
	if op.Response.Kind == catalog.ResponseBinary {
		file, err := saveDownload(o, res)
		if err != nil {
			return nil, err
		}
		result.File = file
		return result, nil
	}
	if len(bytes.TrimSpace(res.Raw)) == 0 {
		return result, nil
	}
	if !isJSON(res.Header) {
		text, err := json.Marshal(string(res.Raw))
		if err != nil {
			return nil, err
		}
		result.Data = text
		return result, nil
	}
	data := res.Raw
	if op.Policy.SecretOutput() {
		moved, secret, err := moveSecrets(op, data, o)
		if err != nil {
			return nil, err
		}
		data, result.Secret = moved, secret
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, data); err != nil {
		return nil, fmt.Errorf("the server sent invalid JSON: %w", err)
	}
	result.Data = buf.Bytes()
	return result, nil
}

func isJSON(h http.Header) bool {
	ct := strings.ToLower(h.Get("Content-Type"))
	return strings.HasPrefix(ct, "application/json") || strings.Contains(ct, "+json")
}

func requestID(res *engine.Result) string {
	if id := res.Header.Get("X-Request-Id"); id != "" {
		return id
	}
	if m, ok := res.Body.(map[string]any); ok {
		if id, ok := m["request_id"].(string); ok {
			return id
		}
	}
	return ""
}

func mutationOutcome(op *catalog.Operation, status int) string {
	if op.Method == http.MethodGet {
		return ""
	}
	if status >= 500 {
		return contract.OutcomeUnknown
	}
	return contract.OutcomeNotApplied
}

func idempotencyHint(key, outcome string) string {
	if outcome != contract.OutcomeUnknown {
		return ""
	}
	if key != "" {
		return fmt.Sprintf("The change may or may not have happened. Check before retrying; to retry safely, reuse --idempotency-key %s.", key)
	}
	return "The change may or may not have happened. Check the current state before running it again."
}

func joinHint(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " " + b
}

// moveSecrets writes the policy's secret fields to the --secret-out file and
// replaces them in the payload. In human mode without --secret-out the payload
// keeps them so the screen can show them once.
func moveSecrets(op *catalog.Operation, raw []byte, o Options) ([]byte, *contract.SecretOutput, error) {
	if o.SecretOut == "" {
		return raw, nil, nil
	}
	doc, err := engine.DecodeJSON(raw)
	if err != nil {
		return nil, nil, err
	}
	m, ok := doc.(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("%s returned a non-object; cannot extract its secrets", op.ID)
	}
	values := map[string]any{}
	var fields []string
	for _, f := range op.Policy.SecretFields {
		if v, ok := m[f]; ok && v != nil && v != "" {
			values[f] = v
			fields = append(fields, f)
			m[f] = "[saved to secret file]"
		}
	}
	if len(fields) == 0 {
		return raw, nil, nil
	}
	file := map[string]any{"operation": op.ID, "saved_at": time.Now().UTC().Format(time.RFC3339), "values": values}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	if err := writeNew(o.SecretOut, append(data, '\n')); err != nil {
		return nil, nil, contract.Fail(contract.KindUnknownOutcome, contract.ExitUnknown,
			"the change was made but its secret could not be saved: %v", err).
			WithHint("The secret is lost; revoke what was created and try again with a writable --secret-out path.")
	}
	out, err := json.Marshal(m)
	if err != nil {
		return nil, nil, err
	}
	return out, &contract.SecretOutput{Path: o.SecretOut, Fields: fields}, nil
}

func saveDownload(o Options, res *engine.Result) (*contract.FileOutput, error) {
	sum := sha256.Sum256(res.Raw)
	out := &contract.FileOutput{Path: o.Output, Bytes: int64(len(res.Raw)), SHA256: hex.EncodeToString(sum[:]), ContentType: res.Header.Get("Content-Type")}
	if o.Output == "-" {
		if _, err := o.Stdout.Write(res.Raw); err != nil {
			return nil, err
		}
		return out, nil
	}
	if err := writeNew(o.Output, res.Raw); err != nil {
		return nil, contract.Fail(contract.KindUsage, contract.ExitUsage, "saving the download: %v", err)
	}
	return out, nil
}

// writeNew creates path with mode 600 and refuses to replace an existing file.
func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
