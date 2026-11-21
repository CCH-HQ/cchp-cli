// Package sitedocs reads the CCHP documentation site through the llms.txt
// protocol: each language's llms.txt index, its pages as Markdown, the whole
// language in one llms-full.txt, and the site's full-text search. Everything
// it reads is public; it never sends credentials.
package sitedocs

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/peterbourgon/diskv"
	"github.com/sandrolain/httpcache"
	"github.com/sandrolain/httpcache/diskcache"
	"golang.org/x/sync/errgroup"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/redact"
)

// maxBody bounds one response; a language's llms-full.txt is far smaller.
const maxBody = 64 << 20

// The Accept headers of document and search requests.
const (
	AcceptDocument = "text/markdown, text/plain;q=0.9"
	AcceptJSON     = "application/json"
)

// Options configures a Client.
type Options struct {
	Site *Site
	// CacheDir holds cached documents; they are revalidated with ETag and
	// Last-Modified after at most MaxAge.
	CacheDir string
	// NoCache fetches every document from the site again.
	NoCache   bool
	UserAgent string
	// Debug receives a trace of each request.
	Debug io.Writer
}

// Client reads one documentation site.
type Client struct {
	site      *Site
	userAgent string
	noCache   bool
	// documents fetches llms.txt, llms-full.txt and pages through the cache;
	// search answers are never cached.
	documents *http.Client
	search    *http.Client
}

// New builds a client. Reads are retried twice on network failures and on
// 408, 429, 500, 502, 503 and 504 answers, like cchp's other reads.
func New(o Options) *Client {
	// The cache library reports cache files it could not write or remove; the
	// request itself still succeeds. Those notes belong in the --debug trace.
	logOut := io.Discard
	if o.Debug != nil {
		logOut = o.Debug
	}
	httpcache.SetLogger(slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: slog.LevelWarn})))
	retrying := retryTransport(o.Debug)
	cache := diskcache.NewWithDiskv(diskv.New(diskv.Options{
		BasePath:     o.CacheDir,
		TempDir:      filepath.Join(o.CacheDir, "tmp"),
		CacheSizeMax: 16 << 20,
		PathPerm:     0o700,
		FilePerm:     0o600,
	}))
	cached := &httpcache.Transport{
		Transport:            freshness{next: retrying},
		Cache:                cache,
		MarkCachedResponses:  true,
		DisableWarningHeader: true,
	}
	return &Client{
		site:      o.Site,
		userAgent: o.UserAgent,
		noCache:   o.NoCache,
		documents: &http.Client{Transport: trace{next: cached, w: o.Debug}},
		search:    &http.Client{Transport: trace{next: retrying, w: o.Debug}},
	}
}

func retryTransport(debug io.Writer) http.RoundTripper {
	base := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	client := &retryablehttp.Client{
		// The retrying client returns redirects as they are; the outer client follows them.
		HTTPClient: &http.Client{
			Transport:     noCredentials{next: base},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		RetryWaitMin: time.Second,
		RetryWaitMax: 30 * time.Second,
		RetryMax:     2,
		CheckRetry:   retryRead,
		Backoff:      retryablehttp.DefaultBackoff,
		ErrorHandler: retryablehttp.PassthroughErrorHandler,
	}
	if debug != nil {
		client.RequestLogHook = func(_ retryablehttp.Logger, req *http.Request, attempt int) {
			if attempt > 0 {
				fmt.Fprintf(debug, "debug: retry %d of %s %s\n", attempt, req.Method, req.URL)
			}
		}
	}
	return &retryablehttp.RoundTripper{Client: client}
}

// retryRead retries network failures and the answers cchp retries for every read.
func retryRead(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if err != nil {
		return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
	}
	switch resp.StatusCode {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true, nil
	}
	return false, nil
}

// noCredentials refuses any request that would carry a credential. The docs
// commands never add one; this keeps it that way.
type noCredentials struct{ next http.RoundTripper }

func (t noCredentials) RoundTrip(req *http.Request) (*http.Response, error) {
	for _, h := range []string{"Authorization", "Proxy-Authorization", "Cookie"} {
		if req.Header.Get(h) != "" {
			return nil, fmt.Errorf("refusing to send a %s header to the docs site; the docs commands never send credentials", h)
		}
	}
	if req.URL.User != nil {
		return nil, errors.New("refusing to send a user name or password to the docs site; the docs commands never send credentials")
	}
	return t.next.RoundTrip(req)
}

// trace prints each request and where its answer came from to --debug.
type trace struct {
	next http.RoundTripper
	w    io.Writer
}

func (t trace) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if t.w == nil {
		return resp, err
	}
	if err != nil {
		fmt.Fprintf(t.w, "debug: %s %s -> %s\n", req.Method, req.URL, redact.String(err.Error()))
		return resp, err
	}
	from := "site"
	switch {
	case resp.Header.Get(httpcache.XRevalidated) == "1":
		from = "cache, revalidated with the site"
	case resp.Header.Get(httpcache.XFromCache) == "1":
		from = "cache"
	}
	fmt.Fprintf(t.w, "debug: %s %s -> %d (%s)\n", req.Method, req.URL, resp.StatusCode, from)
	return resp, nil
}

// Index is a language's docs index from its llms.txt.
type Index struct {
	Lang     string         `json:"lang"`
	URL      string         `json:"url"`
	Title    string         `json:"title"`
	Summary  string         `json:"summary,omitempty"`
	Details  string         `json:"details,omitempty"`
	Sections []IndexSection `json:"sections"`
	Count    int            `json:"count"`
}

// IndexSection is one sidebar group of the docs.
type IndexSection struct {
	Title    string     `json:"title"`
	Optional bool       `json:"optional"`
	Pages    []PageInfo `json:"pages"`
}

// PageInfo is one page of the index.
type PageInfo struct {
	Title string `json:"title"`
	// Slug names the page for cchp docs read; empty for links that are not docs pages of the site.
	Slug        string `json:"slug,omitempty"`
	URL         string `json:"url"`
	MarkdownURL string `json:"md_url,omitempty"`
	Description string `json:"description,omitempty"`
}

// Index reads /<lang>/llms.txt.
func (c *Client) Index(ctx context.Context, lang string) (*Index, error) {
	f, src, err := c.llmsTxt(ctx, lang)
	if err != nil {
		return nil, err
	}
	out := &Index{Lang: lang, URL: src, Title: f.Title, Summary: f.Summary, Details: f.Details, Sections: []IndexSection{}}
	base, err := url.Parse(src)
	if err != nil {
		panic(err)
	}
	for _, s := range f.Sections {
		section := IndexSection{Title: s.Title, Optional: s.Optional, Pages: []PageInfo{}}
		for _, l := range s.Links {
			target, err := base.Parse(l.URL)
			if err != nil {
				return nil, invalidDocument(src, "llms.txt", fmt.Errorf("the link %q of %q is not a valid URL", l.URL, l.Title))
			}
			page := PageInfo{Title: l.Title, URL: target.String(), Description: l.Notes}
			if ref, ok := c.site.refFromLink(target.String()); ok {
				page.Slug = ref.Slug
				page.URL = c.site.PageURL(ref.Lang, ref.Slug)
				page.MarkdownURL = c.site.MarkdownURL(ref.Lang, ref.Slug)
			} else if strings.HasSuffix(target.Path, ".md") {
				page.MarkdownURL = target.String()
			}
			section.Pages = append(section.Pages, page)
			out.Count++
		}
		out.Sections = append(out.Sections, section)
	}
	return out, nil
}

func (c *Client) llmsTxt(ctx context.Context, lang string) (*LLMSTxt, string, error) {
	src := c.site.LLMSTxtURL(lang)
	body, err := c.document(ctx, src)
	if err != nil {
		return nil, src, err
	}
	f, err := ParseLLMSTxt(body)
	if err != nil {
		return nil, src, invalidDocument(src, "llms.txt", err)
	}
	return f, src, nil
}

// Page is one docs page as Markdown.
type Page struct {
	Lang        string `json:"lang"`
	Slug        string `json:"slug"`
	Title       string `json:"title,omitempty"`
	URL         string `json:"url"`
	MarkdownURL string `json:"md_url"`
	Markdown    string `json:"markdown"`
}

// Read fetches /<lang>/docs/<slug>.md. When the language has no such page,
// the failure names the languages whose llms.txt lists it.
func (c *Client) Read(ctx context.Context, lang, slug string) (*Page, error) {
	page := &Page{Lang: lang, Slug: slug, URL: c.site.PageURL(lang, slug), MarkdownURL: c.site.MarkdownURL(lang, slug)}
	body, err := c.document(ctx, page.MarkdownURL)
	if f, ok := errors.AsType[*contract.Failure](err); ok && f.Status == http.StatusNotFound {
		return nil, c.missingPage(ctx, lang, slug, f)
	}
	if err != nil {
		return nil, err
	}
	page.Markdown = string(body)
	page.Title = MarkdownTitle(body)
	return page, nil
}

// missingPage explains a page the language does not have, naming the
// languages whose llms.txt lists it.
func (c *Client) missingPage(ctx context.Context, lang, slug string, notFound *contract.Failure) error {
	others := slices.DeleteFunc(Codes(), func(code string) bool { return code == lang })
	has := make([]bool, len(others))
	g, gctx := errgroup.WithContext(ctx)
	for i, other := range others {
		g.Go(func() error {
			idx, err := c.Index(gctx, other)
			if err != nil {
				return err
			}
			for _, s := range idx.Sections {
				for _, p := range s.Pages {
					if p.Slug == slug {
						has[i] = true
					}
				}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	var in []string
	for i, ok := range has {
		if ok {
			in = append(in, others[i])
		}
	}
	f := &contract.Failure{Exit: contract.ExitNotFound, Status: notFound.Status, RequestID: notFound.RequestID}
	f.Err.Kind = contract.KindNotFound
	if len(in) == 0 {
		f.Err.Message = fmt.Sprintf("There is no docs page %q.", slug)
		f.Err.Hint = "List the pages with cchp docs index, or find one with cchp docs search <words>."
		return f
	}
	f.Err.Message = fmt.Sprintf("The docs page %q is not available in %s. It is available in %s.", slug, lang, strings.Join(in, ", "))
	f.Err.Hint = fmt.Sprintf("Read it with --lang %s.", in[0])
	return f
}

// Full is every docs page of a language from its llms-full.txt.
type Full struct {
	Lang     string `json:"lang"`
	URL      string `json:"url"`
	Bytes    int    `json:"bytes"`
	Markdown string `json:"markdown"`
}

// Full fetches /<lang>/llms-full.txt.
func (c *Client) Full(ctx context.Context, lang string) (*Full, error) {
	src := c.site.FullURL(lang)
	body, err := c.document(ctx, src)
	if err != nil {
		return nil, err
	}
	return &Full{Lang: lang, URL: src, Bytes: len(body), Markdown: string(body)}, nil
}

// MaxSearchLimit is the most results one search returns.
const MaxSearchLimit = 50

// Search is the answer of the site's full-text search.
type Search struct {
	Lang    string `json:"lang"`
	Query   string `json:"query"`
	URL     string `json:"url"`
	Count   int    `json:"count"`
	Results []Hit  `json:"results"`
}

// Hit is one search result: a section of a page. URL leads to the section.
// Markdown holds the whole page on the first result of each page read with
// --read-top.
type Hit struct {
	Title       string  `json:"title"`
	Section     string  `json:"section"`
	Slug        string  `json:"slug,omitempty"`
	URL         string  `json:"url"`
	MarkdownURL string  `json:"md_url"`
	Snippet     string  `json:"snippet"`
	Score       float64 `json:"score"`
	Markdown    string  `json:"markdown,omitempty"`
}

// Search asks /api/docs/search. A limit of 0 leaves the site's default (10).
// readTop reads the Markdown of that many top results in the same call.
func (c *Client) Search(ctx context.Context, lang, query string, limit, readTop int) (*Search, error) {
	src := c.site.SearchURL(lang, query, limit)
	header, body, err := c.get(ctx, c.search, src, AcceptJSON)
	if err != nil {
		return nil, err
	}
	if !isMediaType(header, AcceptJSON) {
		return nil, invalidDocument(src, "search answer", fmt.Errorf("its Content-Type is %q, not application/json", header.Get("Content-Type")))
	}
	var answer struct {
		Lang    string `json:"lang"`
		Query   string `json:"query"`
		Results []struct {
			Title       string   `json:"title"`
			Section     string   `json:"section"`
			URL         string   `json:"url"`
			MarkdownURL string   `json:"md_url"`
			Snippet     string   `json:"snippet"`
			Score       *float64 `json:"score"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, invalidDocument(src, "search answer", err)
	}
	if answer.Results == nil {
		return nil, invalidDocument(src, "search answer", errors.New("it has no results list"))
	}
	if answer.Lang != lang {
		return nil, invalidDocument(src, "search answer", fmt.Errorf("it answers for language %q, not %q", answer.Lang, lang))
	}
	out := &Search{Lang: lang, Query: query, URL: src, Results: []Hit{}}
	for i, r := range answer.Results {
		if r.Title == "" || r.URL == "" || r.MarkdownURL == "" || r.Score == nil {
			return nil, invalidDocument(src, "search answer", fmt.Errorf("result %d lacks a title, url, md_url or score", i+1))
		}
		hit := Hit{Title: r.Title, Section: r.Section, URL: c.site.rebase(r.URL), MarkdownURL: c.site.rebase(r.MarkdownURL), Snippet: r.Snippet, Score: *r.Score}
		if ref, ok := c.site.refFromLink(r.MarkdownURL); ok {
			hit.Slug = ref.Slug
		}
		out.Results = append(out.Results, hit)
	}
	out.Count = len(out.Results)
	if err := c.readTop(ctx, src, out.Results, readTop); err != nil {
		return nil, err
	}
	return out, nil
}

// readTop reads the Markdown of the n best pages among the results. The search
// answers per section, so a page can rank several times; its Markdown goes on
// its first result only.
func (c *Client) readTop(ctx context.Context, src string, results []Hit, n int) error {
	var first []int
	seen := map[string]bool{}
	for i, r := range results {
		if len(first) == n {
			break
		}
		if !seen[r.MarkdownURL] {
			seen[r.MarkdownURL] = true
			first = append(first, i)
		}
	}
	g, gctx := errgroup.WithContext(ctx)
	for _, i := range first {
		g.Go(func() error {
			ref, ok := c.site.refFromLink(results[i].MarkdownURL)
			if !ok {
				return invalidDocument(src, "search answer", fmt.Errorf("result %d links to %s, which is not a docs page of the site", i+1, results[i].MarkdownURL))
			}
			body, err := c.document(gctx, c.site.MarkdownURL(ref.Lang, ref.Slug))
			if err != nil {
				return err
			}
			results[i].Markdown = string(body)
			return nil
		})
	}
	return g.Wait()
}

// document fetches a Markdown or text document through the cache.
func (c *Client) document(ctx context.Context, src string) ([]byte, error) {
	header, body, err := c.get(ctx, c.documents, src, AcceptDocument)
	if err != nil {
		return nil, err
	}
	if !isMediaType(header, "text/markdown", "text/plain", "text/x-markdown") {
		return nil, invalidDocument(src, "document", fmt.Errorf("its Content-Type is %q, not Markdown or plain text; check the docs site address", header.Get("Content-Type")))
	}
	if !utf8.Valid(body) {
		return nil, invalidDocument(src, "document", errors.New("it is not UTF-8 text"))
	}
	return body, nil
}

// get sends one GET with only Accept and User-Agent headers and returns the
// headers and body of a 2xx answer.
func (c *Client) get(ctx context.Context, client *http.Client, src, accept string) (http.Header, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", c.userAgent)
	if c.noCache {
		req.Header.Set("Cache-Control", "no-cache")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, transportFailure(src, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, nil, transportFailure(src, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, nil, httpFailure(src, resp.StatusCode, resp.Header, body)
	}
	if len(body) > maxBody {
		return nil, nil, invalidDocument(src, "document", fmt.Errorf("it is larger than %d MiB", maxBody>>20))
	}
	return resp.Header, body, nil
}

func isMediaType(h http.Header, want ...string) bool {
	mt, _, err := mime.ParseMediaType(h.Get("Content-Type"))
	return err == nil && slices.Contains(want, mt)
}

// transportFailure reports a request that got no answer, the way cchp reports
// it for every read: nothing sent when the connection never opened.
func transportFailure(src string, err error) *contract.Failure {
	host := src
	if u, perr := url.Parse(src); perr == nil {
		host = u.Host
	}
	msg := redact.String(err.Error())
	var dnsErr *net.DNSError
	var opErr *net.OpError
	var certErr *tls.CertificateVerificationError
	switch {
	case errors.As(err, &certErr):
		return contract.Fail(contract.KindNotSent, contract.ExitNotSent, "The request to %s was not sent: the site's TLS certificate is not trusted: %s", host, msg)
	case errors.As(err, &dnsErr), errors.As(err, &opErr) && opErr.Op == "dial":
		f := contract.Fail(contract.KindNotSent, contract.ExitNotSent, "The request to %s was not sent: %s", host, msg)
		f.Err.Retryable = true
		return f.WithHint("Check the network and the docs site address (--docs-url or %s).", SiteEnv)
	case errors.Is(err, context.DeadlineExceeded):
		f := contract.Fail(contract.KindUnknownOutcome, contract.ExitUnknown, "No answer from %s before --timeout ran out", host)
		f.Err.Retryable = true
		return f.WithHint("Run it again, or allow more time with --timeout.")
	}
	f := contract.Fail(contract.KindUnknownOutcome, contract.ExitUnknown, "No answer from %s: %s", host, msg)
	f.Err.Retryable = true
	return f
}

// httpFailure maps an error answer of the site to the output contract.
func httpFailure(src string, status int, header http.Header, body []byte) *contract.Failure {
	f := &contract.Failure{Status: status, RequestID: header.Get("X-Request-Id")}
	e := &f.Err
	detail := strings.TrimSpace(string(body))
	switch {
	case isMediaType(header, "application/json"):
		var p struct {
			Error   string `json:"error"`
			Message string `json:"message"`
			Detail  string `json:"detail"`
		}
		if json.Unmarshal(body, &p) == nil {
			detail = strings.TrimSpace(p.Error + " " + p.Message + " " + p.Detail)
		}
	case isMediaType(header, "text/html"):
		detail = ""
	}
	if r := []rune(detail); len(r) > 300 {
		detail = string(r[:300]) + "…"
	}
	e.Detail = redact.String(detail)
	switch {
	case status == http.StatusNotFound:
		e.Kind, f.Exit = contract.KindNotFound, contract.ExitNotFound
		e.Message = fmt.Sprintf("%s was not found on the docs site.", src)
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		e.Kind, f.Exit = contract.KindValidation, contract.ExitInvalid
		e.Message = "The docs site rejected the request"
		if detail != "" {
			e.Message += ": " + e.Detail
		}
	case status == http.StatusTooManyRequests:
		e.Kind, f.Exit, e.Retryable = contract.KindRateLimited, contract.ExitRateLimited, true
		e.Message = "The docs site is limiting requests; try again shortly."
	case status >= 500:
		e.Kind, f.Exit, e.Retryable = contract.KindServer, contract.ExitServer, true
		e.Message = fmt.Sprintf("The docs site failed with HTTP %d for %s.", status, src)
	default:
		e.Kind, f.Exit = contract.KindHTTP, contract.ExitInternal
		e.Message = fmt.Sprintf("The docs site answered HTTP %d for %s.", status, src)
	}
	return f
}

// invalidDocument reports an answer that does not follow the site's formats.
func invalidDocument(src, what string, err error) *contract.Failure {
	return contract.Fail(contract.KindServer, contract.ExitServer, "The docs site sent an invalid %s at %s: %v", what, src, err).
		WithHint("Report it to the CCHP docs maintainers; check the docs site address if you set --docs-url or %s.", SiteEnv)
}
