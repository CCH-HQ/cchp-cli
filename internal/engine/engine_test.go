package engine_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CCH-HQ/cchp-cli/internal/assets"
	"github.com/CCH-HQ/cchp-cli/internal/catalog"
	"github.com/CCH-HQ/cchp-cli/internal/engine"
)

const testToken = "cch_auth_live_TESTPREFIX_secretpart123"

// recorder is a real HTTP server that remembers what reached it.
type recorder struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   [][]byte
	handler  http.HandlerFunc
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.requests = append(r.requests, req.Clone(context.Background()))
	r.bodies = append(r.bodies, body)
	r.mu.Unlock()
	if r.handler != nil {
		r.handler(w, req)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-Id", "req-1")
	_, _ = w.Write([]byte(`{"ok":true,"big":12345678901234567890}`))
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func newEngine(t *testing.T, base string) *engine.Engine {
	t.Helper()
	return &engine.Engine{BaseURL: base, Token: testToken, StateDir: t.TempDir(), SpecGzip: assets.EngineSpecGzip, UserAgent: "cchp-test"}
}

func op(t *testing.T, id string) *catalog.Operation {
	t.Helper()
	o, ok := assets.Catalog().Get(id)
	if !ok {
		t.Fatalf("no operation %s", id)
	}
	return o
}

func TestJSONBodyIsSentByteForByte(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	body := `{"name":"x","nothing":null,"off":false,"zero":0,"empty":"","file":"@/etc/passwd","big":12345678901234567890,"dec":1.50,"exp":1e3,"esc":"line\nbreak é"}`
	res, err := newEngine(t, srv.URL).Do(context.Background(), engine.Request{
		Op: op(t, "createOrgProvider"), PathArgs: []string{"org-1"}, JSONBody: []byte(body),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 200 {
		t.Fatalf("status %d", res.Status)
	}
	if rec.count() != 1 {
		t.Fatalf("server saw %d requests", rec.count())
	}
	if got := string(rec.bodies[0]); got != body {
		t.Fatalf("body changed on the way:\n got %s\nwant %s", got, body)
	}
	if ct := rec.requests[0].Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type %q", ct)
	}
	if got := rec.requests[0].URL.Path; got != "/api/account/orgs/org-1/providers" {
		t.Fatalf("path %q", got)
	}
	if auth := rec.requests[0].Header.Get("Authorization"); auth != "Bearer "+testToken {
		t.Fatalf("authorization %q", auth)
	}
	// Responses keep large numbers exact.
	if !strings.Contains(string(res.Raw), "12345678901234567890") {
		t.Fatalf("raw response %s", res.Raw)
	}
}

func TestQueryAndArrayParameters(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	_, err := newEngine(t, srv.URL).Do(context.Background(), engine.Request{
		Op:    op(t, "listUsageLogs"),
		Flags: []engine.Flag{{Name: "limit", Value: "5"}, {Name: "models", Value: "a b"}, {Name: "models", Value: "c&d"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The spec declares these arrays with explode: false, so values are joined
	// with commas and each value is escaped.
	q := rec.requests[0].URL.Query()
	if q.Get("limit") != "5" || q.Get("models") != "a b,c&d" {
		t.Fatalf("query %v", rec.requests[0].URL.RawQuery)
	}
}

func TestDryRunSendsNothing(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	planned, err := newEngine(t, srv.URL).Plan(context.Background(), engine.Request{
		Op: op(t, "createOrgProvider"), PathArgs: []string{"org-1"}, JSONBody: []byte(`{"name":"x"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.count() != 0 {
		t.Fatalf("dry run reached the server %d times", rec.count())
	}
	if planned.Method != http.MethodPost || !strings.HasSuffix(planned.URL, "/api/account/orgs/org-1/providers") {
		t.Fatalf("planned %s %s", planned.Method, planned.URL)
	}
	if got := planned.Header.Get("Authorization"); got != "Bearer [redacted]" {
		t.Fatalf("dry run exposed the token: %q", got)
	}
	if string(planned.Body) != `{"name":"x"}` {
		t.Fatalf("planned body %s", planned.Body)
	}
}

func TestCrossOriginRedirectDropsCredentials(t *testing.T) {
	other := &recorder{}
	otherSrv := httptest.NewServer(other)
	defer otherSrv.Close()
	bound := &recorder{handler: func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, otherSrv.URL+"/elsewhere", http.StatusFound)
	}}
	boundSrv := httptest.NewServer(bound)
	defer boundSrv.Close()

	_, err := newEngine(t, boundSrv.URL).Do(context.Background(), engine.Request{Op: op(t, "getWhoami")})
	if err != nil {
		t.Fatal(err)
	}
	if bound.count() != 1 || bound.requests[0].Header.Get("Authorization") == "" {
		t.Fatalf("the bound server should get one credentialed request")
	}
	if other.count() != 1 {
		t.Fatalf("redirect target saw %d requests", other.count())
	}
	if got := other.requests[0].Header.Get("Authorization"); got != "" {
		t.Fatalf("credentials leaked across origins: %q", got)
	}
}

func TestSameOrigin(t *testing.T) {
	cases := []struct {
		a, b string
		same bool
	}{
		{"https://hub.example.com", "https://hub.example.com:443/api/x", true},
		{"https://hub.example.com", "https://HUB.example.com/", true},
		{"https://hub.example.com", "http://hub.example.com", false},
		{"https://hub.example.com", "https://hub.example.com:8443", false},
		{"https://hub.example.com", "https://evil.example.com", false},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080/x", true},
	}
	for _, c := range cases {
		if got := engine.SameOrigin(mustURL(t, c.a), mustURL(t, c.b)); got != c.same {
			t.Errorf("SameOrigin(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestUnreachableServerIsNotSent(t *testing.T) {
	srv := httptest.NewServer(&recorder{})
	base := srv.URL
	srv.Close()
	_, err := newEngine(t, base).Do(context.Background(), engine.Request{Op: op(t, "createOrgProvider"), PathArgs: []string{"o"}, JSONBody: []byte(`{"name":"x"}`)})
	var notSent *engine.NotSentError
	if !errors.As(err, &notSent) {
		t.Fatalf("want NotSentError, got %T %v", err, err)
	}
}

func TestMutationWithoutAnswerIsUnknownAndNotRetried(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := &recorder{handler: func(w http.ResponseWriter, r *http.Request) {
		// The request has arrived; the caller gives up before any answer.
		cancel()
		time.Sleep(2 * time.Second)
	}}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	_, err := newEngine(t, srv.URL).Do(ctx, engine.Request{Op: op(t, "createOrgProvider"), PathArgs: []string{"o"}, JSONBody: []byte(`{"name":"x"}`)})
	var unknown *engine.UnknownOutcomeError
	if !errors.As(err, &unknown) {
		t.Fatalf("want UnknownOutcomeError, got %T %v", err, err)
	}
	time.Sleep(2500 * time.Millisecond)
	if rec.count() != 1 {
		t.Fatalf("a mutation reached the server %d times", rec.count())
	}
}

func TestDeadlineBeforeSendingIsNotSentAndStaysUnsent(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newEngine(t, srv.URL).Do(ctx, engine.Request{Op: op(t, "createOrgProvider"), PathArgs: []string{"o"}, JSONBody: []byte(`{"name":"x"}`)})
	var notSent *engine.NotSentError
	if !errors.As(err, &notSent) {
		t.Fatalf("want NotSentError, got %T %v", err, err)
	}
	// The abandoned run finishes before the next run starts; it must not send.
	if _, err := newEngine(t, srv.URL).Plan(context.Background(), engine.Request{Op: op(t, "listOrgProviders"), PathArgs: []string{"o"}}); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 0 {
		t.Fatalf("an abandoned mutation reached the server %d times", rec.count())
	}
}

func TestServerErrorOnMutationIsNotRetried(t *testing.T) {
	rec := &recorder{handler: func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	res, err := newEngine(t, srv.URL).Do(context.Background(), engine.Request{Op: op(t, "createOrgProvider"), PathArgs: []string{"o"}, JSONBody: []byte(`{"name":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != http.StatusBadGateway || rec.count() != 1 {
		t.Fatalf("status %d after %d requests", res.Status, rec.count())
	}
}

func TestReadsAreRetriedOnServerErrors(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	rec := &recorder{handler: func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	res, err := newEngine(t, srv.URL).Do(context.Background(), engine.Request{Op: op(t, "getWhoami"), Retry: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 200 || rec.count() != 2 {
		t.Fatalf("status %d after %d requests", res.Status, rec.count())
	}
}
