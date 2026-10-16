// Package engine runs CCHP operations through embedded Restish v2.
//
// cchp owns the command tree, policy and output contract; Restish builds and
// sends the HTTP request from the pinned engine spec. The two meet only through
// Restish's supported embedding interfaces: a branded CLI with bundled config,
// a custom auth handler, a JSON content type that sends bodies byte for byte, a
// capture formatter, and the standard library transport Restish clones.
package engine

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	restish "github.com/rest-sh/restish/v2"
	"github.com/rest-sh/restish/v2/config"

	"github.com/CCH-HQ/cchp-cli/internal/catalog"
)

const apiName = "cchp"

// Engine executes operations against one bound server.
type Engine struct {
	// BaseURL is the bound server, for example https://hub.example.com.
	BaseURL string
	// Token is the management token; empty for public operations and dry runs.
	Token string
	// StateDir holds the materialised engine spec and Restish's private config and cache.
	StateDir string
	// SpecGzip is the embedded engine spec.
	SpecGzip []byte
	// UserAgent is sent with every request.
	UserAgent string
}

// Request is one operation call.
type Request struct {
	Op *catalog.Operation
	// PathArgs holds path parameter values in path template order.
	PathArgs []string
	// Flags holds non-path parameters as flag/value pairs; array values repeat.
	Flags []Flag
	// Headers are extra "Name: value" request headers managed by cchp.
	Headers []string
	// JSONBody is sent byte for byte as the application/json body.
	JSONBody []byte
	// BinaryFile is sent as the raw request body.
	BinaryFile string
	// FormFiles maps multipart field names to local file paths.
	FormFiles map[string]string
	// Retry allows Restish's automatic retry of transient failures (reads only).
	Retry bool
	// HeaderTimeout bounds the wait for response headers.
	HeaderTimeout time.Duration
	// MaxBodyMiB raises the response size limit for downloads.
	MaxBodyMiB int
	// MaxEvents stops a stream after this many events.
	MaxEvents int
	// OnEvent receives stream events; returning an error stops the stream.
	OnEvent func(Event) error
	// DryRun builds the request without sending it.
	DryRun bool
}

// Flag is one parameter flag value.
type Flag struct {
	Name  string
	Value string
}

// Event is one server-sent event as Restish decoded it.
type Event struct {
	Payload any
}

// Result is the final HTTP response.
type Result struct {
	Status      int
	Header      http.Header
	Raw         []byte
	Body        any
	Diagnostics string
}

// PlannedRequest is what a dry run would have sent.
type PlannedRequest struct {
	Method string      `json:"method"`
	URL    string      `json:"url"`
	Header http.Header `json:"headers"`
	Body   []byte      `json:"-"`
}

// NotSentError reports a failure before any byte reached the server.
type NotSentError struct{ Err error }

func (e *NotSentError) Error() string { return e.Err.Error() }
func (e *NotSentError) Unwrap() error { return e.Err }

// UnknownOutcomeError reports a failure after the request may have reached the server.
type UnknownOutcomeError struct{ Err error }

func (e *UnknownOutcomeError) Error() string { return e.Err.Error() }
func (e *UnknownOutcomeError) Unwrap() error { return e.Err }

var (
	errDryRun   = errors.New("dry run: request not sent")
	errNoEngine = errors.New("the engine produced no response")
)

// One Restish run at a time: the transport guard below is process wide.
var runMu sync.Mutex

var guard = &transportGuard{}

func init() {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = guard.proxy
	http.DefaultTransport = base
}

// transportGuard sees every outgoing request, including redirect hops, through
// the Proxy hook of the transport Restish clones. During an engine run it
// refuses to carry credentials to any origin other than the bound one, captures
// dry runs, and stops a run whose caller has given up from sending anything more.
type transportGuard struct {
	mu      sync.Mutex
	origin  *url.URL
	dryRun  bool
	planned *PlannedRequest
	run     *runState
}

// runState belongs to one engine run. Fields are guarded by transportGuard.mu.
type runState struct {
	// sent is set once a request of this run has been let through to the network.
	sent bool
	// abandoned is set when the caller's deadline passed before anything was
	// sent; the guard then refuses every request of this run.
	abandoned bool
}

func (g *transportGuard) arm(origin *url.URL, dryRun bool, run *runState) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.origin, g.dryRun, g.planned, g.run = origin, dryRun, nil, run
}

// abandon marks the run as given up. It returns false when a request of the
// run was already let through, so its outcome is unknown.
func (g *transportGuard) abandon(run *runState) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if run.sent {
		return false
	}
	run.abandoned = true
	return true
}

func (g *transportGuard) proxy(req *http.Request) (*url.URL, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Outside an engine run the process's other HTTP traffic passes unchanged.
	if g.run == nil {
		return http.ProxyFromEnvironment(req)
	}
	if g.run.abandoned {
		return nil, &NotSentError{Err: errors.New("the deadline passed before the request was sent")}
	}
	if g.dryRun {
		planned := &PlannedRequest{Method: req.Method, URL: req.URL.String(), Header: req.Header.Clone()}
		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(body)
			if err != nil {
				return nil, err
			}
			planned.Body = data
		}
		g.planned = planned
		return nil, errDryRun
	}
	if req.Header.Get("Authorization") != "" && (g.origin == nil || !SameOrigin(g.origin, req.URL)) {
		return nil, &NotSentError{Err: fmt.Errorf("refusing to send credentials to %s; cchp only sends them to the signed-in server", originString(req.URL))}
	}
	g.run.sent = true
	return http.ProxyFromEnvironment(req)
}

// SameOrigin compares scheme, host and effective port.
func SameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

func originString(u *url.URL) string { return u.Scheme + "://" + u.Host }

// authHandler attaches the management token, and only for the bound origin.
type authHandler struct {
	token  string
	origin *url.URL
	dryRun bool
}

func (a *authHandler) Parameters() []restish.AuthParam { return nil }

func (a *authHandler) Authenticate(_ context.Context, req *http.Request, _ restish.AuthContext) error {
	if !SameOrigin(a.origin, req.URL) {
		return &NotSentError{Err: fmt.Errorf("refusing to send credentials to %s; cchp only sends them to the signed-in server", originString(req.URL))}
	}
	if a.dryRun {
		req.Header.Set("Authorization", "Bearer [redacted]")
		return nil
	}
	if a.token == "" {
		return &NotSentError{Err: errors.New("not signed in")}
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	return nil
}

// captureFormatter receives the response from Restish instead of printing it.
// Bounded responses arrive through Format, stream events through FormatValue.
type captureFormatter struct {
	resp    *restish.Response
	onEvent func(Event) error
	events  int
}

func (f *captureFormatter) Format(_ io.Writer, resp *restish.Response, _ bool) error {
	f.resp = resp
	return nil
}

func (f *captureFormatter) FormatValue(_ io.Writer, value any, _ bool) error {
	f.events++
	if f.onEvent == nil {
		return nil
	}
	return f.onEvent(Event{Payload: value})
}

// rawJSON is the application/json content type: request bodies arrive as the
// exact JSON text cchp validated, so null, false, 0, "" and number spelling are
// sent unchanged, and responses decode numbers as json.Number.
func rawJSON() *restish.ContentType {
	return &restish.ContentType{
		Name:      "json",
		MIMETypes: []string{"application/json"},
		Suffixes:  []string{"+json"},
		Quality:   0.9,
		Marshal: func(v any) ([]byte, error) {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("engine: JSON bodies must arrive pre-encoded, got %T", v)
			}
			return []byte(s), nil
		},
		Unmarshal: DecodeJSON,
	}
}

// DecodeJSON decodes one JSON document keeping numbers as json.Number.
func DecodeJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("unexpected data after the JSON document")
	}
	return v, nil
}

// Plan returns the request a dry run would send.
func (e *Engine) Plan(ctx context.Context, req Request) (*PlannedRequest, error) {
	req.DryRun = true
	_, planned, err := e.run(ctx, req)
	return planned, err
}

// Do sends the request and returns the response.
func (e *Engine) Do(ctx context.Context, req Request) (*Result, error) {
	res, _, err := e.run(ctx, req)
	return res, err
}

func (e *Engine) run(ctx context.Context, req Request) (*Result, *PlannedRequest, error) {
	origin, err := url.Parse(e.BaseURL)
	if err != nil {
		return nil, nil, err
	}
	specPath, err := e.materialiseSpec()
	if err != nil {
		return nil, nil, err
	}
	args, stdin, cleanup, err := e.argv(req)
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()

	type outcome struct {
		res     *Result
		planned *PlannedRequest
		sent    bool
		err     error
	}
	done := make(chan outcome, 1)
	state := &runState{}
	go func() {
		runMu.Lock()
		defer runMu.Unlock()
		guard.arm(origin, req.DryRun, state)
		capture := &captureFormatter{onEvent: req.OnEvent}
		cli := restish.New()
		cli.SetCommandName("cchp")
		cli.SetSignalHandling(false)
		cli.Paths = config.NewPathsWithConfigFile(filepath.Join(e.StateDir, "restish.json"))
		cli.AddAuthHandler("cchp", &authHandler{token: e.Token, origin: origin, dryRun: req.DryRun})
		cli.AddContentType(rawJSON())
		cli.AddFormatter("cchp", capture)
		auth := &restish.AuthConfig{Type: "cchp"}
		cli.SetDefaultConfig(&restish.Config{APIs: map[string]*restish.APIConfig{apiName: {
			BaseURL:   e.BaseURL,
			SpecFiles: []string{specPath},
			Profiles: map[string]*restish.ProfileConfig{"default": {
				Headers:     []string{"User-Agent: " + e.UserAgent},
				Auth:        auth,
				Credentials: map[string]*config.CredentialConfig{"managementBearer": {Auth: auth}},
			}},
		}}})
		cli.SetCommandSurface(restish.CommandSurface{PromotedAPI: apiName, HideSupportCommands: true})
		var stdout, stderr bytes.Buffer
		cli.Stdout, cli.Stderr, cli.Stdin = &stdout, &stderr, stdin
		runErr := cli.Run(args)

		guard.mu.Lock()
		planned, sent := guard.planned, state.sent
		guard.origin, guard.dryRun, guard.planned, guard.run = nil, false, nil, nil
		guard.mu.Unlock()
		if req.DryRun {
			if planned == nil {
				if runErr == nil {
					runErr = errNoEngine
				}
				done <- outcome{err: runErr}
				return
			}
			done <- outcome{planned: planned}
			return
		}
		if runErr != nil {
			done <- outcome{sent: sent, err: classify(runErr, sent, stderr.String())}
			return
		}
		res, err := result(&stdout, capture)
		if res != nil {
			res.Diagnostics = strings.TrimSpace(stderr.String())
		}
		done <- outcome{res: res, err: err}
	}()

	select {
	case out := <-done:
		return out.res, out.planned, out.err
	case <-ctx.Done():
		if guard.abandon(state) {
			return nil, nil, &NotSentError{Err: fmt.Errorf("deadline reached before the request was sent: %w", ctx.Err())}
		}
		return nil, nil, &UnknownOutcomeError{Err: fmt.Errorf("deadline reached before the server answered: %w", ctx.Err())}
	}
}

func classify(err error, sent bool, stderr string) error {
	if notSent, ok := errors.AsType[*NotSentError](err); ok {
		return notSent
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return &NotSentError{Err: err}
	}
	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return &NotSentError{Err: err}
	}
	if !sent {
		msg := err.Error()
		if s := strings.TrimSpace(stderr); s != "" {
			msg = msg + ": " + s
		}
		return &NotSentError{Err: errors.New(msg)}
	}
	return &UnknownOutcomeError{Err: err}
}

func result(stdout *bytes.Buffer, capture *captureFormatter) (*Result, error) {
	if stdout.Len() == 0 {
		return nil, errNoEngine
	}
	// Restish printed the response head (--rsh-print h); read it with net/http.
	head, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(stdout.Bytes())), nil)
	if err != nil {
		return nil, fmt.Errorf("engine: reading the response head: %w", err)
	}
	_ = head.Body.Close()
	res := &Result{Status: head.StatusCode, Header: head.Header}
	if capture.resp != nil {
		res.Status = capture.resp.Status
		res.Header = http.Header(capture.resp.Headers)
		res.Raw = capture.resp.Raw
		res.Body = capture.resp.Body
	}
	return res, nil
}

// materialiseSpec writes the embedded engine spec once per content hash so
// Restish can load and cache it like any local spec file. It also creates the
// engine's private, empty Restish config file: cchp passes it explicitly, which
// keeps user and project Restish configuration out of every request.
func (e *Engine) materialiseSpec() (string, error) {
	if err := os.MkdirAll(e.StateDir, 0o700); err != nil {
		return "", err
	}
	cfgPath := filepath.Join(e.StateDir, "restish.json")
	if _, err := os.Stat(cfgPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	sum := sha256.Sum256(e.SpecGzip)
	path := filepath.Join(e.StateDir, "engine-spec-"+hex.EncodeToString(sum[:8])+".json")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(e.SpecGzip))
	if err != nil {
		return "", err
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(e.StateDir, ".engine-spec-*")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return path, os.Rename(tmp.Name(), path)
}

func (e *Engine) argv(req Request) ([]string, io.Reader, func(), error) {
	cleanup := func() {}
	args := []string{"cchp", "--rsh-config", filepath.Join(e.StateDir, "restish.json"), req.Op.ID,
		"-o", "cchp", "--rsh-print", "hb", "--rsh-ignore-status-code", "--rsh-no-paginate", "--rsh-no-cache"}
	if req.Retry && !req.DryRun {
		args = append(args, "--rsh-retry", "2")
	} else {
		args = append(args, "--rsh-retry", "0")
	}
	if req.HeaderTimeout > 0 {
		args = append(args, "--rsh-timeout", req.HeaderTimeout.String())
	}
	if req.MaxBodyMiB > 0 {
		args = append(args, "--rsh-max-body-size", strconv.Itoa(req.MaxBodyMiB))
	}
	if req.MaxEvents > 0 {
		args = append(args, "--rsh-max-items", strconv.Itoa(req.MaxEvents))
	}
	for _, h := range req.Headers {
		args = append(args, "--rsh-header", h)
	}
	for _, f := range req.Flags {
		args = append(args, "--"+f.Name+"="+f.Value)
	}
	var stdin io.Reader = strings.NewReader("")
	var bodyArgs []string
	switch {
	case req.JSONBody != nil:
		literal, err := json.Marshal(string(req.JSONBody))
		if err != nil {
			return nil, nil, cleanup, err
		}
		stdin = bytes.NewReader(literal)
	case req.BinaryFile != "":
		// Restish reads @file bodies itself; a private link with a neutral name keeps
		// the file's bytes opaque whatever its name or location.
		dir, err := os.MkdirTemp(e.StateDir, "upload-")
		if err != nil {
			return nil, nil, cleanup, err
		}
		cleanup = func() { _ = os.RemoveAll(dir) }
		abs, err := filepath.Abs(req.BinaryFile)
		if err != nil {
			return nil, nil, cleanup, err
		}
		link := filepath.Join(dir, "body.bin")
		if err := os.Symlink(abs, link); err != nil {
			return nil, nil, cleanup, err
		}
		bodyArgs = append(bodyArgs, "@"+link)
	case len(req.FormFiles) > 0:
		names := make([]string, 0, len(req.FormFiles))
		for name := range req.FormFiles {
			names = append(names, name)
		}
		sort.Strings(names)
		var parts []string
		for _, name := range names {
			abs, err := filepath.Abs(req.FormFiles[name])
			if err != nil {
				return nil, nil, cleanup, err
			}
			quoted, err := json.Marshal("@" + abs)
			if err != nil {
				return nil, nil, cleanup, err
			}
			parts = append(parts, name+": "+string(quoted))
		}
		bodyArgs = append(bodyArgs, strings.Join(parts, ", "))
	}
	args = append(args, "--")
	args = append(args, req.PathArgs...)
	args = append(args, bodyArgs...)
	return args, stdin, cleanup, nil
}

// Commands lists the operation commands Restish builds from the engine spec,
// with any warnings it printed. cchp-gen uses it to run the whole spec through
// the engine and report operations it cannot build as unsupported.
func Commands(stateDir string, specGzip []byte) ([]string, string, error) {
	e := &Engine{BaseURL: "https://cchp.invalid", StateDir: stateDir, SpecGzip: specGzip, UserAgent: "cchp-gen"}
	specPath, err := e.materialiseSpec()
	if err != nil {
		return nil, "", err
	}
	runMu.Lock()
	defer runMu.Unlock()
	cli := restish.New()
	cli.SetCommandName("cchp")
	cli.SetSignalHandling(false)
	cli.Paths = config.NewPathsWithConfigFile(filepath.Join(stateDir, "restish.json"))
	cli.SetDefaultConfig(&restish.Config{APIs: map[string]*restish.APIConfig{apiName: {BaseURL: e.BaseURL, SpecFiles: []string{specPath}}}})
	cli.SetCommandSurface(restish.CommandSurface{PromotedAPI: apiName, HideSupportCommands: true})
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr, cli.Stdin = &stdout, &stderr, strings.NewReader("")
	if err := cli.Run([]string{"cchp", "--rsh-config", filepath.Join(stateDir, "restish.json"), "__complete", ""}); err != nil {
		return nil, stderr.String(), err
	}
	var names []string
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		name, _, _ := strings.Cut(line, "\t")
		names = append(names, name)
	}
	return names, stderr.String(), nil
}
