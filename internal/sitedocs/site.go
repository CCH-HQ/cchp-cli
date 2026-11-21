package sitedocs

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

// DefaultSite is the documentation site cchp reads unless --docs-url or
// CCHP_DOCS_URL names another one, such as a preview build.
const DefaultSite = "https://cch-plus.com"

// SiteEnv names the environment variable that sets the site.
const SiteEnv = "CCHP_DOCS_URL"

// publicHost is the site's public host. Its llms.txt files link to it, so a
// URL on it names a page of whichever site cchp reads, preview builds included.
const publicHost = "cch-plus.com"

// IndexSlug is the slug of a language's docs home page, /<lang>/docs.
const IndexSlug = "index"

// Site is the documentation site's origin.
type Site struct {
	origin *url.URL
}

// ParseSite checks a site address: an origin over HTTPS, or plain HTTP on
// this computer for local previews.
func ParseSite(raw string) (*Site, error) {
	u, err := url.Parse(strings.TrimSuffix(strings.TrimSpace(raw), "/"))
	switch {
	case err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http"):
		return nil, siteFailure("%q is not a docs site address", raw)
	case u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "":
		return nil, siteFailure("the docs site address %q must be an origin, without a path, query or user", raw)
	case u.Scheme == "http" && !loopback(u.Hostname()):
		return nil, siteFailure("the docs site %q needs https://; plain http:// is accepted only for this computer", raw)
	}
	return &Site{origin: &url.URL{Scheme: u.Scheme, Host: strings.ToLower(u.Host)}}, nil
}

func siteFailure(format string, args ...any) *contract.Failure {
	return contract.Fail(contract.KindUsage, contract.ExitUsage, format, args...).
		WithHint("Use an origin such as %s, or http://127.0.0.1:<port> for a local preview.", DefaultSite)
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// String is the site's origin.
func (s *Site) String() string { return s.origin.String() }

func (s *Site) url(path string, query url.Values) string {
	u := *s.origin
	u.Path = path
	u.RawQuery = query.Encode()
	return u.String()
}

// LLMSTxtURL is a language's docs index in llms.txt format.
func (s *Site) LLMSTxtURL(lang string) string { return s.url("/"+lang+"/llms.txt", nil) }

// FullURL is every docs page of a language in one Markdown file.
func (s *Site) FullURL(lang string) string { return s.url("/"+lang+"/llms-full.txt", nil) }

// PageURL is a docs page as people read it.
func (s *Site) PageURL(lang, slug string) string {
	if slug == IndexSlug {
		return s.url("/"+lang+"/docs", nil)
	}
	return s.url("/"+lang+"/docs/"+slug, nil)
}

// MarkdownURL is a docs page as Markdown.
func (s *Site) MarkdownURL(lang, slug string) string {
	return s.url("/"+lang+"/docs/"+slug+".md", nil)
}

// SearchURL is the site's full-text search. A limit of 0 leaves the site's default.
func (s *Site) SearchURL(lang, query string, limit int) string {
	q := url.Values{"q": {query}, "lang": {lang}}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return s.url("/api/docs/search", q)
}

// PageRef names a docs page. Lang is empty when the page was named by its slug
// alone, so the language comes from --lang and the settings.
type PageRef struct {
	Lang string
	Slug string
}

// ParseRef reads a page reference: a slug such as installation/docker-compose,
// a docs page URL or its .md URL on the site or on cch-plus.com, or a path
// such as /zh-CN/docs/providers.
func (s *Site) ParseRef(arg string) (PageRef, error) {
	arg = strings.TrimSpace(arg)
	if strings.Contains(arg, "://") {
		u, err := url.Parse(arg)
		if err != nil {
			return PageRef{}, refFailure(arg, "it is not a valid URL")
		}
		if !s.serves(u) {
			return PageRef{}, refFailure(arg, "cchp reads docs pages of "+s.String()+" and "+publicHost+" only")
		}
		return pathRef(arg, u.Path)
	}
	if strings.HasPrefix(arg, "/") {
		return pathRef(arg, arg)
	}
	slug, ok := cleanSlug(strings.TrimSuffix(strings.Trim(arg, "/"), ".md"))
	if !ok {
		return PageRef{}, refFailure(arg, "a slug is a page path such as installation/docker-compose")
	}
	return PageRef{Slug: slug}, nil
}

// serves reports whether u is on the site or on its public host.
func (s *Site) serves(u *url.URL) bool {
	if (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	return host == s.origin.Host || host == publicHost || host == "www."+publicHost
}

// pathRef reads /<lang>/docs[/<slug>][.md]. An unprefixed /docs/... path is
// English, as the site itself redirects it.
func pathRef(arg, path string) (PageRef, error) {
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 3)
	lang := DefaultLanguage
	if len(parts) > 0 && parts[0] != "docs" {
		code, ok := Canonical(parts[0])
		if !ok {
			return PageRef{}, refFailure(arg, "its path does not start with a docs language ("+strings.Join(Codes(), ", ")+")")
		}
		lang, parts = code, parts[1:]
	}
	if len(parts) == 0 || parts[0] != "docs" {
		return PageRef{}, refFailure(arg, "it is not a docs page; docs pages live under /<lang>/docs/")
	}
	rest := ""
	if len(parts) > 1 {
		rest = strings.TrimSuffix(strings.Trim(strings.Join(parts[1:], "/"), "/"), ".md")
	}
	if rest == "" {
		return PageRef{Lang: lang, Slug: IndexSlug}, nil
	}
	slug, ok := cleanSlug(rest)
	if !ok {
		return PageRef{}, refFailure(arg, "its page path is not valid")
	}
	return PageRef{Lang: lang, Slug: slug}, nil
}

// cleanSlug accepts slash-separated path segments without empty, . or .. parts.
func cleanSlug(slug string) (string, bool) {
	if slug == "" {
		return "", false
	}
	for seg := range strings.SplitSeq(slug, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.ContainsAny(seg, "?#\\") {
			return "", false
		}
	}
	return slug, true
}

func refFailure(arg, why string) *contract.Failure {
	return contract.Fail(contract.KindUsage, contract.ExitUsage, "cannot read %q: %s", arg, why).
		WithHint("Name a page by its slug from cchp docs index, such as installation/docker-compose, or by its docs URL.")
}

// rebase moves a link on cch-plus.com to the site in use, keeping its path,
// query and fragment, so a preview's links lead to the preview. Other links
// stay as they are.
func (s *Site) rebase(link string) string {
	u, err := url.Parse(link)
	if err != nil || !s.serves(u) {
		return link
	}
	u.Scheme, u.Host = s.origin.Scheme, s.origin.Host
	return u.String()
}

// refFromLink reads the page behind a link of the site's llms.txt or search
// results. ok is false for links to anything else.
func (s *Site) refFromLink(link string) (PageRef, bool) {
	u, err := url.Parse(link)
	if err != nil || !s.serves(u) {
		return PageRef{}, false
	}
	ref, err := pathRef(link, u.Path)
	return ref, err == nil
}
