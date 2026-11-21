//go:build e2e

// End-to-end tests of cchp docs in every language against the documentation
// site: https://cch-plus.com, or the preview build CCHP_DOCS_URL names. They
// run the built cchp binary.
package docs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	binary string
	site   string
)

var languages = []string{"zh-CN", "zh-TW", "en", "ja", "ru", "ko"}

func TestMain(m *testing.M) {
	site = strings.TrimSuffix(os.Getenv("CCHP_DOCS_URL"), "/")
	if site == "" {
		site = "https://cch-plus.com"
	}
	dir, err := os.MkdirTemp("", "cchp-docs-e2e-bin-")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "cchp")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/cchp")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type failure struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

type result struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *failure        `json:"error"`
}

type run struct {
	exit   int
	stdout string
	stderr string
	res    result
}

// home is an isolated cchp home: its own config and cache, no language or
// docs site from the environment.
func home(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	return []string{
		"CCHP_CONFIG_DIR=" + filepath.Join(dir, "config"),
		"CCHP_CACHE_DIR=" + filepath.Join(dir, "cache"),
		"CCHP_DOCS_URL=" + site,
		"CCHP_DOCS_LANG=", "LC_ALL=", "LC_MESSAGES=", "LANG=",
		"CCHP_TOKEN=", "CCHP_TOKEN_FILE=", "CCHP_BASE_URL=",
	}
}

func cchp(t *testing.T, env []string, args ...string) run {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	r := run{stdout: stdout.String(), stderr: stderr.String()}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		r.exit = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running cchp %v: %v", args, err)
	}
	if strings.HasPrefix(r.stdout, "{") {
		if err := json.Unmarshal([]byte(r.stdout), &r.res); err != nil {
			t.Fatalf("cchp %v printed invalid JSON: %v\n%s", args, err, r.stdout)
		}
	}
	return r
}

func data[T any](t *testing.T, r run) T {
	t.Helper()
	if r.exit != 0 || !r.res.OK {
		t.Fatalf("want success, got exit %d\nstdout: %.2000s\nstderr: %s", r.exit, r.stdout, r.stderr)
	}
	var v T
	if err := json.Unmarshal(r.res.Data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func expectFail(t *testing.T, r run, exit int, kind string) *failure {
	t.Helper()
	if r.exit != exit || r.res.OK || r.res.Error == nil || r.res.Error.Kind != kind {
		t.Fatalf("want exit %d kind %s, got exit %d\nstdout: %s\nstderr: %s", exit, kind, r.exit, r.stdout, r.stderr)
	}
	return r.res.Error
}

type page struct {
	Title       string `json:"title"`
	Slug        string `json:"slug"`
	URL         string `json:"url"`
	MarkdownURL string `json:"md_url"`
	Description string `json:"description"`
}

type index struct {
	Lang       string `json:"lang"`
	LangSource string `json:"lang_source"`
	Title      string `json:"title"`
	Sections   []struct {
		Title string `json:"title"`
		Pages []page `json:"pages"`
	} `json:"sections"`
	Count int `json:"count"`
}

type doc struct {
	Lang        string `json:"lang"`
	LangSource  string `json:"lang_source"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	MarkdownURL string `json:"md_url"`
	Markdown    string `json:"markdown"`
}

type full struct {
	Lang     string `json:"lang"`
	Bytes    int    `json:"bytes"`
	Markdown string `json:"markdown"`
}

type search struct {
	Lang    string `json:"lang"`
	Query   string `json:"query"`
	Count   int    `json:"count"`
	Results []struct {
		Title       string  `json:"title"`
		Section     string  `json:"section"`
		Slug        string  `json:"slug"`
		URL         string  `json:"url"`
		MarkdownURL string  `json:"md_url"`
		Snippet     string  `json:"snippet"`
		Score       float64 `json:"score"`
		Markdown    string  `json:"markdown"`
	} `json:"results"`
}

// The topics every language is searched for. Chinese, Japanese and Korean
// queries have no spaces. A topic with a page of its own must find one of its
// pages in the top three; the others must find something. Ranking quality is
// the site's; these check that cchp carries the ranking through unchanged.
var (
	searchQueries = map[string][]string{
		"zh-CN": {"安装", "Codex", "网络代理", "通行密钥", "消费限额"},
		"zh-TW": {"安裝", "Codex", "網路代理", "通行密鑰", "消費限額"},
		"en":    {"install", "Codex", "network proxy", "passkey", "spending limit"},
		"ja":    {"インストール", "Codex", "ネットワークプロキシ", "パスキー", "利用上限"},
		"ru":    {"установка", "Codex", "прокси", "ключ доступа", "лимит расходов"},
		"ko":    {"설치", "Codex", "네트워크 프록시", "패스키", "사용 한도"},
	}
	searchTopics = [][]string{
		{"installation", "installation/docker-compose", "installation/binary", "installation/kubernetes"},
		{"guide/codex"},
		{"providers/network-proxies"},
		nil,
		nil,
	}
)

func TestEveryLanguage(t *testing.T) {
	for _, lang := range languages {
		t.Run(lang, func(t *testing.T) {
			env := home(t)
			idx := data[index](t, cchp(t, env, "docs", "index", "--lang", lang, "--json"))
			if idx.Lang != lang || idx.LangSource != "flag" || idx.Title == "" || len(idx.Sections) == 0 || idx.Count == 0 {
				t.Fatalf("index %+v", idx)
			}
			var pages []page
			for _, s := range idx.Sections {
				if s.Title == "" || len(s.Pages) == 0 {
					t.Fatalf("section %+v", s)
				}
				pages = append(pages, s.Pages...)
			}
			if len(pages) != idx.Count {
				t.Fatalf("count %d, pages %d", idx.Count, len(pages))
			}
			for _, p := range pages {
				if p.Title == "" || p.Slug == "" || p.MarkdownURL != site+"/"+lang+"/docs/"+p.Slug+".md" || !strings.HasPrefix(p.URL, site+"/"+lang+"/docs") {
					t.Fatalf("page %+v", p)
				}
				got := data[doc](t, cchp(t, env, "docs", "read", p.Slug, "--lang", lang, "--json"))
				if got.Lang != lang || got.Slug != p.Slug || got.MarkdownURL != p.MarkdownURL || got.URL != p.URL || strings.TrimSpace(got.Markdown) == "" || got.Title == "" {
					t.Fatalf("read %s: %+v", p.Slug, got)
				}
			}

			first := pages[0]
			byURL := data[doc](t, cchp(t, env, "docs", "read", first.URL, "--json"))
			byMD := data[doc](t, cchp(t, env, "docs", "read", first.MarkdownURL, "--json"))
			if byURL.Slug != first.Slug || byMD.Slug != first.Slug || byURL.LangSource != "url" || byURL.Lang != lang || byURL.Markdown != byMD.Markdown {
				t.Fatalf("read by URL %+v / %+v", byURL, byMD)
			}
			raw := cchp(t, env, "docs", "read", first.Slug, "--lang", lang)
			if raw.exit != 0 || raw.stdout != byURL.Markdown {
				t.Fatalf("raw read printed %.300q, want the page's Markdown", raw.stdout)
			}

			all := data[full](t, cchp(t, env, "docs", "full", "--lang", lang, "--json"))
			if all.Lang != lang || all.Bytes != len(all.Markdown) || all.Bytes == 0 {
				t.Fatalf("full %d bytes", all.Bytes)
			}
			for _, p := range pages {
				if !strings.Contains(all.Markdown, p.Title) {
					t.Fatalf("llms-full.txt lacks the page %q", p.Title)
				}
			}
			if rawFull := cchp(t, env, "docs", "full", "--lang", lang); rawFull.exit != 0 || rawFull.stdout != all.Markdown {
				t.Fatal("raw full is not the llms-full.txt Markdown")
			}

			for i, query := range searchQueries[lang] {
				res := data[search](t, cchp(t, env, "docs", "search", query, "--lang", lang, "--limit", "5", "--read-top", "1", "--json"))
				if res.Lang != lang || res.Query != query || res.Count == 0 || res.Count > 5 || res.Count != len(res.Results) {
					t.Fatalf("search %q: %+v", query, res)
				}
				var top []string
				for j, r := range res.Results {
					if r.Title == "" || r.Slug == "" || r.MarkdownURL != site+"/"+lang+"/docs/"+r.Slug+".md" || r.Score <= 0 ||
						!strings.HasPrefix(r.URL, site+"/"+lang+"/docs") {
						t.Fatalf("search %q result %+v", query, r)
					}
					if j > 0 && r.Score > res.Results[j-1].Score {
						t.Fatalf("search %q: results are not ranked", query)
					}
					if (j == 0) != (r.Markdown != "") {
						t.Fatalf("search %q: --read-top 1 reads only the first result", query)
					}
					if j < 3 {
						top = append(top, r.Slug)
					}
				}
				if pages := searchTopics[i]; pages != nil && !slices.ContainsFunc(top, func(slug string) bool { return slices.Contains(pages, slug) }) {
					t.Errorf("search %q in %s: top results %v hold none of %v", query, lang, top, pages)
				}
				t.Logf("search %q: %v", query, top)
				read := data[doc](t, cchp(t, env, "docs", "read", res.Results[0].Slug, "--lang", lang, "--json"))
				if read.Markdown != res.Results[0].Markdown {
					t.Fatalf("search %q: --read-top Markdown differs from cchp docs read", query)
				}
			}
		})
	}
}

func TestLanguageSelection(t *testing.T) {
	env := home(t)
	cases := []struct {
		env        []string
		lang, from string
	}{
		{[]string{"LANG=zh_HK.UTF-8"}, "zh-TW", "LANG"},
		{[]string{"LANG=ko_KR.UTF-8", "LC_ALL=ru_RU.UTF-8"}, "ru", "LC_ALL"},
		{[]string{"LANG=zh_CN.UTF-8", "CCHP_DOCS_LANG=ja"}, "ja", "CCHP_DOCS_LANG"},
		{[]string{"LANG=C.UTF-8"}, "en", "default"},
	}
	for _, c := range cases {
		idx := data[index](t, cchp(t, append(slices.Clone(env), c.env...), "docs", "index", "--json"))
		if idx.Lang != c.lang || idx.LangSource != c.from {
			t.Errorf("%v: lang %s from %s; want %s from %s", c.env, idx.Lang, idx.LangSource, c.lang, c.from)
		}
	}
	f := expectFail(t, cchp(t, env, "docs", "index", "--lang", "de", "--json"), 2, "usage")
	if !strings.Contains(f.Hint, "zh-CN, zh-TW, en, ja, ru, ko") {
		t.Fatalf("hint %q", f.Hint)
	}
	expectFail(t, cchp(t, append(slices.Clone(env), "CCHP_DOCS_LANG=xx"), "docs", "search", "x", "--json"), 2, "usage")
}

func TestMissingPageAndBadReferences(t *testing.T) {
	env := home(t)
	f := expectFail(t, cchp(t, env, "docs", "read", "no/such-page", "--lang", "en", "--json"), 5, "not_found")
	if f.Message != `There is no docs page "no/such-page".` {
		t.Fatalf("message %q", f.Message)
	}
	expectFail(t, cchp(t, env, "docs", "read", "https://example.com/en/docs/installation", "--json"), 2, "usage")
	expectFail(t, cchp(t, env, "docs", "read", site+"/en/docs/installation", "--lang", "ja", "--json"), 2, "usage")
	expectFail(t, cchp(t, env, "docs", "search", "x", "--limit", "51", "--json"), 2, "usage")
}

// recordingProxy forwards to the docs site and records each request's headers.
func recordingProxy(t *testing.T) (*httptest.Server, func() []http.Header) {
	t.Helper()
	target, err := url.Parse(site)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var seen []http.Header
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		mu.Lock()
		seen = append(seen, r.In.Header.Clone())
		mu.Unlock()
		r.SetURL(target)
		r.Out.Host = target.Host
	}}
	srv := httptest.NewServer(proxy)
	t.Cleanup(srv.Close)
	return srv, func() []http.Header {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(seen)
	}
}

// The docs commands never send credentials, even with a token in the
// environment and a saved sign-in.
func TestNoCredentialsReachTheSite(t *testing.T) {
	proxy, seen := recordingProxy(t)
	env := append(home(t), "CCHP_DOCS_URL="+proxy.URL, "CCHP_BASE_URL="+proxy.URL, "CCHP_TOKEN=cch_auth_live_E2E_docs-secret-never-sent", "CCHP_CREDENTIAL_STORE=file")
	idx := data[index](t, cchp(t, env, "docs", "index", "--lang", "en", "--json"))
	slug := idx.Sections[0].Pages[0].Slug
	data[doc](t, cchp(t, env, "docs", "read", slug, "--lang", "en", "--no-cache", "--json"))
	// A cch-plus.com URL is read from the site in use.
	data[doc](t, cchp(t, env, "docs", "read", "https://cch-plus.com/en/docs/"+slug+".md", "--json"))
	data[search](t, cchp(t, env, "docs", "search", "install", "--lang", "en", "--read-top", "1", "--json"))
	data[full](t, cchp(t, env, "docs", "full", "--lang", "en", "--json"))
	headers := seen()
	if len(headers) < 4 {
		t.Fatalf("the site saw %d requests", len(headers))
	}
	for _, h := range headers {
		for _, name := range []string{"Authorization", "Cookie", "Proxy-Authorization", "X-Cch-Organization"} {
			if h.Get(name) != "" {
				t.Fatalf("a docs request carried %s", name)
			}
		}
		if ua := h.Get("User-Agent"); !strings.HasPrefix(ua, "cchp/") {
			t.Fatalf("user agent %q", ua)
		}
	}
}

// A second read within 5 minutes comes from the cache; --no-cache asks the site.
func TestDocumentsAreCached(t *testing.T) {
	proxy, seen := recordingProxy(t)
	env := append(home(t), "CCHP_DOCS_URL="+proxy.URL)
	first := cchp(t, env, "docs", "index", "--lang", "ko", "--json", "--debug")
	data[index](t, first)
	if !strings.Contains(first.stderr, "/ko/llms.txt -> 200 (site)") {
		t.Fatalf("first read: %s", first.stderr)
	}
	before := len(seen())
	second := cchp(t, env, "docs", "index", "--lang", "ko", "--json", "--debug")
	data[index](t, second)
	if !strings.Contains(second.stderr, "/ko/llms.txt -> 200 (cache)") || len(seen()) != before {
		t.Fatalf("second read: %s", second.stderr)
	}
	third := cchp(t, env, "docs", "index", "--lang", "ko", "--no-cache", "--json", "--debug")
	data[index](t, third)
	if !strings.Contains(third.stderr, "/ko/llms.txt -> 200 (site)") || len(seen()) != before+1 {
		t.Fatalf("--no-cache read: %s", third.stderr)
	}
	dry := cchp(t, env, "docs", "full", "--lang", "ko", "--dry-run", "--json")
	if dry.exit != 0 || !strings.Contains(dry.stdout, `"url":"`+proxy.URL+`/ko/llms-full.txt"`) || len(seen()) != before+1 {
		t.Fatalf("dry run: %s", dry.stdout)
	}
}
