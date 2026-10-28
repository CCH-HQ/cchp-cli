package sitedocs

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

// recordedSite serves the answers scripts/record-docs-fixtures.sh recorded
// from the documentation site, by request URI, and logs each request.
type recordedSite struct {
	t         *testing.T
	responses map[string]recording
	// aliases serve a recorded answer under another request URI.
	aliases map[string]string

	mu   sync.Mutex
	seen []*http.Request
}

type recording struct {
	Status  int               `json:"status"`
	File    string            `json:"file"`
	Headers map[string]string `json:"headers"`
}

func newRecordedSite(t *testing.T, aliases map[string]string) (*recordedSite, *httptest.Server) {
	t.Helper()
	data, err := os.ReadFile("testdata/site/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Responses map[string]recording `json:"responses"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	rs := &recordedSite{t: t, responses: manifest.Responses, aliases: aliases}
	srv := httptest.NewServer(rs)
	t.Cleanup(srv.Close)
	return rs, srv
}

func (rs *recordedSite) body(uri string) []byte {
	rs.t.Helper()
	f, err := os.Open(filepath.Join("testdata/site", rs.responses[uri].File))
	if err != nil {
		rs.t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		rs.t.Fatal(err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		rs.t.Fatal(err)
	}
	return data
}

func (rs *recordedSite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rs.mu.Lock()
	rs.seen = append(rs.seen, r.Clone(context.Background()))
	rs.mu.Unlock()
	uri := r.URL.RequestURI()
	if alias, ok := rs.aliases[uri]; ok {
		uri = alias
	}
	rec, ok := rs.responses[uri]
	if !ok {
		rs.t.Errorf("no recorded answer for %s", uri)
		w.WriteHeader(http.StatusTeapot)
		return
	}
	for k, v := range rec.Headers {
		w.Header().Set(k, v)
	}
	body := rs.body(uri)
	if rec.Status == http.StatusOK && rec.Headers["ETag"] != "" {
		// Answers conditional requests the way the site does.
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
		return
	}
	w.WriteHeader(rec.Status)
	_, _ = w.Write(body)
}

func (rs *recordedSite) requests() []*http.Request {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]*http.Request(nil), rs.seen...)
}

func newClient(t *testing.T, siteURL string) *Client {
	t.Helper()
	site, err := ParseSite(siteURL)
	if err != nil {
		t.Fatal(err)
	}
	return New(Options{Site: site, CacheDir: t.TempDir(), UserAgent: "cchp/test"})
}

func failure(t *testing.T, err error, exit int, kind string) *contract.Failure {
	t.Helper()
	f, ok := errors.AsType[*contract.Failure](err)
	if !ok || f.Exit != exit || f.Err.Kind != kind {
		t.Fatalf("want exit %d kind %s, got %#v", exit, kind, err)
	}
	return f
}

func TestIndexReadsEveryLanguage(t *testing.T) {
	_, srv := newRecordedSite(t, nil)
	c := newClient(t, srv.URL)
	ctx := context.Background()
	firstSections := map[string]string{"zh-CN": "介绍", "zh-TW": "簡介", "en": "Introduction", "ja": "はじめに", "ru": "Введение", "ko": "소개"}
	for _, lang := range Codes() {
		idx, err := c.Index(ctx, lang)
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		if idx.Lang != lang || idx.URL != srv.URL+"/"+lang+"/llms.txt" || idx.Title == "" || idx.Summary == "" || idx.Details == "" || idx.Count != 46 {
			t.Fatalf("%s index %+v", lang, idx)
		}
		if idx.Sections[0].Title != firstSections[lang] {
			t.Errorf("%s first section %q", lang, idx.Sections[0].Title)
		}
		pages := 0
		for _, s := range idx.Sections {
			for _, p := range s.Pages {
				pages++
				if p.Slug == "" || p.Title == "" || p.Description == "" ||
					p.MarkdownURL != srv.URL+"/"+lang+"/docs/"+p.Slug+".md" || p.URL != c.site.PageURL(lang, p.Slug) {
					t.Fatalf("%s page %+v", lang, p)
				}
			}
		}
		if pages != idx.Count {
			t.Fatalf("%s: %d pages, count %d", lang, pages, idx.Count)
		}
	}
	idx, err := c.Index(ctx, "en")
	if err != nil {
		t.Fatal(err)
	}
	install := idx.Sections[1]
	if install.Title != "Installation" || install.Pages[0].Slug != "installation" || install.Pages[1].Slug != "installation/docker-compose" {
		t.Fatalf("installation section %+v", install)
	}
	for _, p := range install.Pages {
		if p.Slug == "installation/lan-testing" && !strings.HasPrefix(p.Description, "Turn on CCH_INSECURE_DEPLOYMENT to test") {
			t.Fatalf("escaped Markdown in a description: %q", p.Description)
		}
	}
	if idx.Sections[0].Pages[0].Slug != IndexSlug || idx.Sections[0].Pages[0].URL != srv.URL+"/en/docs" {
		t.Fatalf("docs home %+v", idx.Sections[0].Pages[0])
	}
}

func TestReadPage(t *testing.T) {
	rs, srv := newRecordedSite(t, nil)
	c := newClient(t, srv.URL)
	page, err := c.Read(context.Background(), "zh-CN", "providers/network-proxies")
	if err != nil {
		t.Fatal(err)
	}
	want := rs.body("/zh-CN/docs/providers/network-proxies.md")
	if page.Markdown != string(want) || page.Title != "网络代理" || page.Lang != "zh-CN" || page.Slug != "providers/network-proxies" ||
		page.URL != srv.URL+"/zh-CN/docs/providers/network-proxies" || page.MarkdownURL != srv.URL+"/zh-CN/docs/providers/network-proxies.md" {
		t.Fatalf("page %+v", page)
	}
	home, err := c.Read(context.Background(), "en", IndexSlug)
	if err != nil || home.URL != srv.URL+"/en/docs" || home.Title == "" {
		t.Fatalf("home %+v %v", home, err)
	}
}

func TestReadMissingPage(t *testing.T) {
	// The Korean site answers guide/codex the way it answers any missing page.
	_, srv := newRecordedSite(t, map[string]string{"/ko/docs/guide/codex.md": "/en/docs/no-such-page.md"})
	c := newClient(t, srv.URL)
	_, err := c.Read(context.Background(), "ko", "guide/codex")
	f := failure(t, err, contract.ExitNotFound, contract.KindNotFound)
	if f.Status != 404 || f.Err.Message != `The docs page "guide/codex" is not available in ko. It is available in zh-CN, zh-TW, en, ja, ru.` ||
		f.Err.Hint != "Read it with --lang zh-CN." {
		t.Fatalf("%+v", f)
	}
	_, err = c.Read(context.Background(), "en", "no-such-page")
	f = failure(t, err, contract.ExitNotFound, contract.KindNotFound)
	if f.Err.Message != `There is no docs page "no-such-page".` || f.Err.Detail != "" {
		t.Fatalf("%+v", f)
	}
}

func TestSearch(t *testing.T) {
	rs, srv := newRecordedSite(t, nil)
	c := newClient(t, srv.URL)
	ctx := context.Background()

	res, err := c.Search(ctx, "zh-CN", "网络代理", 5, 2)
	if err != nil {
		t.Fatal(err)
	}
	if res.Lang != "zh-CN" || res.Query != "网络代理" || res.Count != 5 || len(res.Results) != 5 ||
		res.URL != srv.URL+"/api/docs/search?lang=zh-CN&limit=5&q=%E7%BD%91%E7%BB%9C%E4%BB%A3%E7%90%86" {
		t.Fatalf("search %+v", res)
	}
	proxies := string(rs.body("/zh-CN/docs/providers/network-proxies.md"))
	keys := string(rs.body("/zh-CN/docs/providers/keys.md"))
	wantSlugs := []string{"providers/network-proxies", "providers/network-proxies", "providers/network-proxies", "providers/keys", "providers/create-provider"}
	wantMarkdown := []string{proxies, "", "", keys, ""}
	for i, r := range res.Results {
		if r.Slug != wantSlugs[i] || r.Markdown != wantMarkdown[i] || r.Title == "" || r.Section == "" || r.Snippet == "" || r.Score <= 0 ||
			r.MarkdownURL != srv.URL+"/zh-CN/docs/"+r.Slug+".md" || !strings.HasPrefix(r.URL, srv.URL+"/zh-CN/docs/"+r.Slug+"#") {
			t.Fatalf("result %d: %+v", i, r)
		}
		if i > 0 && r.Score > res.Results[i-1].Score {
			t.Fatalf("results are not ranked: %+v", res.Results)
		}
	}

	res, err = c.Search(ctx, "en", "docker compose", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 10 || res.Results[0].Slug != "installation/lan-testing" || res.Results[1].Slug != "installation/docker-compose" ||
		res.Results[0].Markdown == "" || res.Results[1].Markdown == "" || res.Results[2].Markdown != "" {
		t.Fatalf("default limit %+v", res)
	}

	res, err = c.Search(ctx, "ko", "패스키", 3, 0)
	if err != nil || res.Count != 3 || res.Results[0].Markdown != "" {
		t.Fatalf("ko %+v %v", res, err)
	}
	res, err = c.Search(ctx, "en", "zzzqqqxxyy", 5, 3)
	if err != nil || res.Count != 0 || res.Results == nil {
		t.Fatalf("no results %+v %v", res, err)
	}

	_, err = c.Search(ctx, "en", strings.Repeat("a", 201), 0, 0)
	f := failure(t, err, contract.ExitInvalid, contract.KindValidation)
	if f.Status != 400 || f.Err.Message != `The docs site rejected the request: "q" is longer than 200 characters` {
		t.Fatalf("%+v", f)
	}
}

func TestFull(t *testing.T) {
	rs, srv := newRecordedSite(t, nil)
	c := newClient(t, srv.URL)
	full, err := c.Full(context.Background(), "en")
	if err != nil {
		t.Fatal(err)
	}
	want := rs.body("/en/llms-full.txt")
	if full.Markdown != string(want) || full.Bytes != len(want) || full.URL != srv.URL+"/en/llms-full.txt" || !strings.HasPrefix(full.Markdown, "# CCHP Documentation") {
		t.Fatalf("full %d bytes", full.Bytes)
	}
}

// The site's own llms.txt links to https://cch-plus.com; reading another site,
// such as a preview, keeps every link on that site.
func TestLinksStayOnThePreview(t *testing.T) {
	rs, srv := newRecordedSite(t, nil)
	if !strings.Contains(string(rs.body("/en/llms.txt")), "(https://cch-plus.com/en/docs/installation.md)") {
		t.Fatal("the recorded llms.txt links to cch-plus.com")
	}
	c := newClient(t, srv.URL)
	idx, err := c.Index(context.Background(), "en")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range idx.Sections {
		for _, p := range s.Pages {
			if !strings.HasPrefix(p.URL, srv.URL) || !strings.HasPrefix(p.MarkdownURL, srv.URL) {
				t.Fatalf("page %+v left the site", p)
			}
		}
	}
	for _, r := range rs.requests() {
		if r.Host != strings.TrimPrefix(srv.URL, "http://") {
			t.Fatalf("request to %s", r.Host)
		}
	}
}

func TestRequestsCarryNoCredentials(t *testing.T) {
	rs, srv := newRecordedSite(t, nil)
	c := newClient(t, srv.URL)
	ctx := context.Background()
	if _, err := c.Index(ctx, "ja"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(ctx, "ja", "guide/codex"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Search(ctx, "ko", "패스키", 3, 0); err != nil {
		t.Fatal(err)
	}
	for _, r := range rs.requests() {
		for _, h := range []string{"Authorization", "Cookie", "Proxy-Authorization", "X-Cch-Organization"} {
			if r.Header.Get(h) != "" {
				t.Fatalf("%s carried %s", r.URL, h)
			}
		}
		if r.Header.Get("User-Agent") != "cchp/test" || r.Header.Get("Accept") == "" {
			t.Fatalf("%s headers %v", r.URL, r.Header)
		}
	}

	// The transport refuses a credential even if code ever adds one.
	before := len(rs.requests())
	for _, h := range []string{"Authorization", "Cookie", "Proxy-Authorization"} {
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/en/llms.txt", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(h, "Bearer cch_auth_live_X_secret")
		resp, err := retryTransport(nil).RoundTrip(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "never send credentials") {
			t.Fatalf("%s: %v", h, err)
		}
	}
	req, err := http.NewRequest(http.MethodGet, strings.Replace(srv.URL, "http://", "http://u:p@", 1)+"/en/llms.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := retryTransport(nil).RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("a user in the URL was sent")
	}
	if len(rs.requests()) != before {
		t.Fatal("a request with credentials reached the site")
	}
}

func TestDocumentsAreCachedAndRevalidated(t *testing.T) {
	rs, srv := newRecordedSite(t, nil)
	dir := t.TempDir()
	site, err := ParseSite(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := func(noCache bool) *Client {
		return New(Options{Site: site, CacheDir: dir, NoCache: noCache, UserAgent: "cchp/test"})
	}
	ctx := context.Background()
	if _, err := client(false).Index(ctx, "ru"); err != nil {
		t.Fatal(err)
	}
	// A new process reads the disk cache: no request within max-age.
	if _, err := client(false).Index(ctx, "ru"); err != nil {
		t.Fatal(err)
	}
	if n := len(rs.requests()); n != 1 {
		t.Fatalf("%d requests; the second read should come from the cache", n)
	}
	// --no-cache asks the site again.
	if _, err := client(true).Index(ctx, "ru"); err != nil {
		t.Fatal(err)
	}
	if n := len(rs.requests()); n != 2 {
		t.Fatalf("%d requests; --no-cache should ask the site", n)
	}
	// Search answers are never cached.
	for range 2 {
		if _, err := client(false).Search(ctx, "en", "zzzqqqxxyy", 5, 0); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(rs.requests()); n != 4 {
		t.Fatalf("%d requests; every search should ask the site", n)
	}
}

// caching serves one document with the given Cache-Control, answers
// If-None-Match with 304, and counts full and conditional answers.
type caching struct {
	cacheControl string
	mu           sync.Mutex
	full, notMod int
}

func (s *caching) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("ETag", `"v1"`)
	if s.cacheControl != "" {
		w.Header().Set("Cache-Control", s.cacheControl)
	}
	if r.Header.Get("If-None-Match") == `"v1"` {
		s.notMod++
		w.WriteHeader(http.StatusNotModified)
		return
	}
	s.full++
	_, _ = io.WriteString(w, "# Page\n\nBody.\n")
}

func TestCachePolicy(t *testing.T) {
	cases := []struct {
		cacheControl  string
		full, notMod  int
		storedControl string
	}{
		// Fresh for the site's lifetime: the second read needs no request.
		{"public, max-age=300", 1, 0, "max-age=300"},
		// A longer lifetime is capped at 5 minutes.
		{"public, max-age=86400", 1, 0, "max-age=300"},
		{"max-age=60", 1, 0, "max-age=60"},
		// No lifetime or no-cache: revalidated every time with ETag.
		{"", 1, 1, "no-cache"},
		{"no-cache", 1, 1, "no-cache"},
		{"public, max-age=0", 1, 1, "no-cache"},
		// no-store: never kept.
		{"no-store", 2, 0, "no-store"},
	}
	for _, c := range cases {
		s := &caching{cacheControl: c.cacheControl}
		srv := httptest.NewServer(s)
		cl := newClient(t, srv.URL)
		for range 2 {
			page, err := cl.Read(context.Background(), "en", "page")
			if err != nil || page.Markdown != "# Page\n\nBody.\n" {
				t.Fatalf("%q: %+v %v", c.cacheControl, page, err)
			}
		}
		if s.full != c.full || s.notMod != c.notMod {
			t.Errorf("Cache-Control %q: %d full and %d conditional answers; want %d and %d", c.cacheControl, s.full, s.notMod, c.full, c.notMod)
		}
		resp, err := (&http.Client{Transport: freshness{next: http.DefaultTransport}}).Get(srv.URL + "/en/docs/page.md")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if got := resp.Header.Get("Cache-Control"); got != c.storedControl {
			t.Errorf("Cache-Control %q is kept as %q; want %q", c.cacheControl, got, c.storedControl)
		}
		srv.Close()
	}
}

// flaky fails a number of times with a status, then serves a page.
type flaky struct {
	status   int
	failures int
	mu       sync.Mutex
	calls    int
}

func (s *flaky) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.calls <= s.failures {
		w.Header().Set("Retry-After", "0")
		http.Error(w, "upstream trouble", s.status)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = io.WriteString(w, "# Page\n")
}

func TestReadsAreRetriedLikeOtherReads(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests, http.StatusBadGateway} {
		s := &flaky{status: status, failures: 2}
		srv := httptest.NewServer(s)
		if _, err := newClient(t, srv.URL).Read(context.Background(), "en", "page"); err != nil || s.calls != 3 {
			t.Fatalf("%d: %v after %d calls", status, err, s.calls)
		}
		srv.Close()
	}
	s := &flaky{status: http.StatusServiceUnavailable, failures: 3}
	srv := httptest.NewServer(s)
	defer srv.Close()
	_, err := newClient(t, srv.URL).Read(context.Background(), "en", "page")
	f := failure(t, err, contract.ExitServer, contract.KindServer)
	if s.calls != 3 || f.Status != 503 || !f.Err.Retryable || f.Err.Detail != "upstream trouble" {
		t.Fatalf("%+v after %d calls", f, s.calls)
	}
	gone := &flaky{status: http.StatusForbidden, failures: 1}
	srv2 := httptest.NewServer(gone)
	defer srv2.Close()
	_, err = newClient(t, srv2.URL).Read(context.Background(), "en", "page")
	_ = failure(t, err, contract.ExitInternal, contract.KindHTTP)
	if gone.calls != 1 {
		t.Fatalf("a 403 was retried: %d calls", gone.calls)
	}
}

func TestInvalidAnswers(t *testing.T) {
	cases := []struct {
		path, contentType, body, want string
	}{
		{"/en/llms.txt", "text/plain", "no title here\n", "The docs site sent an invalid llms.txt at %s/en/llms.txt: it does not start with an H1 title"},
		{"/en/llms.txt", "text/html; charset=utf-8", "<!doctype html><title>Login</title>", "The docs site sent an invalid document at %s/en/llms.txt: its Content-Type is \"text/html; charset=utf-8\", not Markdown or plain text; check the docs site address"},
		{"/api/docs/search", "application/json", `{"lang":"en","query":"x"}`, "The docs site sent an invalid search answer at %s/api/docs/search?lang=en&q=x: it has no results list"},
		{"/api/docs/search", "application/json", `{"lang":"ja","query":"x","results":[]}`, "The docs site sent an invalid search answer at %s/api/docs/search?lang=en&q=x: it answers for language \"ja\", not \"en\""},
		{"/api/docs/search", "application/json", `{"lang":"en","query":"x","results":[{"title":"T","url":"u","md_url":"m"}]}`, "The docs site sent an invalid search answer at %s/api/docs/search?lang=en&q=x: result 1 lacks a title, url, md_url or score"},
		{"/api/docs/search", "application/json", `not json`, "The docs site sent an invalid search answer at %s/api/docs/search?lang=en&q=x: invalid character 'o' in literal null (expecting 'u')"},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != c.path {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", c.contentType)
			_, _ = io.WriteString(w, c.body)
		}))
		cl := newClient(t, srv.URL)
		var err error
		if strings.HasPrefix(c.path, "/api") {
			_, err = cl.Search(context.Background(), "en", "x", 0, 0)
		} else {
			_, err = cl.Index(context.Background(), "en")
		}
		f := failure(t, err, contract.ExitServer, contract.KindServer)
		if want := strings.ReplaceAll(c.want, "%s", srv.URL); f.Err.Message != want {
			t.Errorf("got  %q\nwant %q", f.Err.Message, want)
		}
		srv.Close()
	}
}

func TestUnreachableSiteAndTimeout(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close()
	_, err := newClient(t, addr).Index(context.Background(), "en")
	f := failure(t, err, contract.ExitNotSent, contract.KindNotSent)
	if !f.Err.Retryable || !strings.Contains(f.Err.Hint, "CCHP_DOCS_URL") {
		t.Fatalf("%+v", f)
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer slow.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = newClient(t, slow.URL).Index(ctx, "en")
	f = failure(t, err, contract.ExitUnknown, contract.KindUnknownOutcome)
	if !strings.Contains(f.Err.Message, "before --timeout ran out") {
		t.Fatalf("%+v", f)
	}
}
