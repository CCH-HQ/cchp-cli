package sitedocs

import (
	"errors"
	"testing"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

func mustSite(t *testing.T, raw string) *Site {
	t.Helper()
	s, err := ParseSite(raw)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParseSite(t *testing.T) {
	for _, c := range []struct{ raw, want string }{
		{"https://cch-plus.com", "https://cch-plus.com"},
		{" https://CCH-Plus.com/ ", "https://cch-plus.com"},
		{"http://127.0.0.1:8787", "http://127.0.0.1:8787"},
		{"http://localhost:3000/", "http://localhost:3000"},
		{"http://[::1]:8787", "http://[::1]:8787"},
		{"https://preview.pages.dev", "https://preview.pages.dev"},
	} {
		if got := mustSite(t, c.raw).String(); got != c.want {
			t.Errorf("ParseSite(%q) = %q; want %q", c.raw, got, c.want)
		}
	}
	for _, raw := range []string{"cch-plus.com", "ftp://cch-plus.com", "http://cch-plus.com", "https://cch-plus.com/docs", "https://u:p@cch-plus.com", "https://cch-plus.com/?x=1", "https://"} {
		_, err := ParseSite(raw)
		f, ok := errors.AsType[*contract.Failure](err)
		if !ok || f.Exit != contract.ExitUsage {
			t.Errorf("ParseSite(%q) error %v; want a usage failure", raw, err)
		}
	}
}

func TestSiteURLs(t *testing.T) {
	s := mustSite(t, "https://cch-plus.com")
	for _, c := range []struct{ got, want string }{
		{s.LLMSTxtURL("zh-CN"), "https://cch-plus.com/zh-CN/llms.txt"},
		{s.FullURL("ja"), "https://cch-plus.com/ja/llms-full.txt"},
		{s.PageURL("en", IndexSlug), "https://cch-plus.com/en/docs"},
		{s.PageURL("ko", "installation/docker-compose"), "https://cch-plus.com/ko/docs/installation/docker-compose"},
		{s.MarkdownURL("en", IndexSlug), "https://cch-plus.com/en/docs/index.md"},
		{s.MarkdownURL("zh-TW", "providers/network-proxies"), "https://cch-plus.com/zh-TW/docs/providers/network-proxies.md"},
		{s.MarkdownURL("ru", "guide/a b"), "https://cch-plus.com/ru/docs/guide/a%20b.md"},
		{s.SearchURL("zh-CN", "网络 代理", 0), "https://cch-plus.com/api/docs/search?lang=zh-CN&q=%E7%BD%91%E7%BB%9C+%E4%BB%A3%E7%90%86"},
		{s.SearchURL("en", "passkeys & MFA", 50), "https://cch-plus.com/api/docs/search?lang=en&limit=50&q=passkeys+%26+MFA"},
		{mustSite(t, "http://127.0.0.1:8787").LLMSTxtURL("ru"), "http://127.0.0.1:8787/ru/llms.txt"},
	} {
		if c.got != c.want {
			t.Errorf("got %s; want %s", c.got, c.want)
		}
	}
}

func TestParseRef(t *testing.T) {
	s := mustSite(t, "http://127.0.0.1:8787")
	cases := []struct {
		arg  string
		want PageRef
	}{
		{"installation/docker-compose", PageRef{Slug: "installation/docker-compose"}},
		{"installation/docker-compose.md/", PageRef{Slug: "installation/docker-compose"}},
		{"index", PageRef{Slug: "index"}},
		{"https://cch-plus.com/zh-CN/docs/providers/network-proxies", PageRef{Lang: "zh-CN", Slug: "providers/network-proxies"}},
		{"https://cch-plus.com/zh-CN/docs/providers/network-proxies.md", PageRef{Lang: "zh-CN", Slug: "providers/network-proxies"}},
		{"https://www.cch-plus.com/ja/docs", PageRef{Lang: "ja", Slug: "index"}},
		{"https://cch-plus.com/ko/docs/index.md", PageRef{Lang: "ko", Slug: "index"}},
		{"https://cch-plus.com/docs/installation", PageRef{Lang: "en", Slug: "installation"}},
		{"http://127.0.0.1:8787/ru/docs/guide/codex.md", PageRef{Lang: "ru", Slug: "guide/codex"}},
		{"/zh-TW/docs/organizations/teams", PageRef{Lang: "zh-TW", Slug: "organizations/teams"}},
		{"https://cch-plus.com/zh-cn/docs/installation/?utm_source=x#install", PageRef{Lang: "zh-CN", Slug: "installation"}},
	}
	for _, c := range cases {
		got, err := s.ParseRef(c.arg)
		if err != nil || got != c.want {
			t.Errorf("ParseRef(%q) = %+v, %v; want %+v", c.arg, got, err, c.want)
		}
	}
	for _, arg := range []string{"", "/", "a//b", "../secret", "a/./b", "https://example.com/en/docs/x", "https://cch-plus.com/de/docs/x", "https://cch-plus.com/en/llms.txt", "https://cch-plus.com/en/docs/a/../b", "http://[bad"} {
		_, err := s.ParseRef(arg)
		f, ok := errors.AsType[*contract.Failure](err)
		if !ok || f.Exit != contract.ExitUsage {
			t.Errorf("ParseRef(%q) error %v; want a usage failure", arg, err)
		}
	}
}
