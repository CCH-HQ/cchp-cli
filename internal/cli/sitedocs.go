package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/sitedocs"
	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

const docsLong = `Read the CCHP documentation at cch-plus.com through its llms.txt files:
list the pages, read one as Markdown, search them, or get every page at once.
Agents use these commands to answer questions about CCHP and to find how to
configure something before they run the matching cchp command.

  cchp docs search "network proxy" --read-top 1 --json
  cchp docs index --lang zh-CN
  cchp docs read providers/network-proxies

The language is --lang, then CCHP_DOCS_LANG, then the system locale (LC_ALL,
LC_MESSAGES or LANG), then en. The site is --docs-url, then CCHP_DOCS_URL, then
https://cch-plus.com.

These commands read public pages only; they need no sign-in and never send
your token. Documents are cached for at most 5 minutes, then checked with the
site again.`

// docsFlags are the settings every docs command shares.
type docsFlags struct {
	lang    string
	site    string
	noCache bool
}

// docsRun is one docs command's client and language.
type docsRun struct {
	client     *sitedocs.Client
	site       *sitedocs.Site
	lang       string
	langSource string
}

func (a *app) docsCommand() *cobra.Command {
	var f docsFlags
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Read and search the CCHP documentation (no sign-in needed)",
		Long:  docsLong,
		Args:  suggestSubcommand,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	pf := cmd.PersistentFlags()
	pf.StringVar(&f.lang, "lang", "", "Docs language: "+strings.Join(sitedocs.Codes(), ", ")+".")
	pf.StringVar(&f.site, "docs-url", "", "Docs site to read, such as a local preview (default "+sitedocs.DefaultSite+" or "+sitedocs.SiteEnv+").")
	pf.BoolVar(&f.noCache, "no-cache", false, "Fetch every document from the site again instead of using the cache.")
	cmd.AddCommand(a.docsIndexCommand(&f), a.docsReadCommand(&f), a.docsSearchCommand(&f), a.docsFullCommand(&f))
	return cmd
}

func (a *app) docsSetup(f *docsFlags) (*docsRun, error) {
	raw := f.site
	if raw == "" {
		raw = os.Getenv(sitedocs.SiteEnv)
	}
	if raw == "" {
		raw = sitedocs.DefaultSite
	}
	site, err := sitedocs.ParseSite(raw)
	if err != nil {
		return nil, err
	}
	r := &docsRun{site: site}
	r.lang, r.langSource, err = sitedocs.Resolve(f.lang, os.Getenv)
	if err != nil {
		return nil, err
	}
	var debug io.Writer
	if a.debug {
		debug = a.io.Err
	}
	r.client = sitedocs.New(sitedocs.Options{
		Site:      site,
		CacheDir:  filepath.Join(a.store.Dirs.Cache, "docs"),
		NoCache:   f.noCache,
		UserAgent: a.runner.UserAgent,
		Debug:     debug,
	})
	return r, nil
}

func (a *app) docsContext(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	return context.WithTimeout(cmd.Context(), a.timeout)
}

// docsDryRun shows the first request a docs command would send, and sends nothing.
func (a *app) docsDryRun(url, accept string) error {
	d := &contract.DryRun{
		Method:  "GET",
		URL:     url,
		Headers: map[string]string{"Accept": accept, "User-Agent": a.runner.UserAgent},
		Policy:  contract.PolicyView{Risk: "read", Retry: true, Classified: true},
	}
	if a.agent {
		return contract.Write(a.io.Out, &contract.Result{OK: true, DryRun: d})
	}
	return a.humanDryRun(ui.Writer(a.io.Out), d)
}

// The docs results as agents get them, with where the language came from:
// flag, url, CCHP_DOCS_LANG, LC_ALL, LC_MESSAGES, LANG or default.
type (
	docsIndexData struct {
		*sitedocs.Index
		LangSource string `json:"lang_source"`
	}
	docsPageData struct {
		*sitedocs.Page
		LangSource string `json:"lang_source"`
	}
	docsSearchData struct {
		*sitedocs.Search
		LangSource string `json:"lang_source"`
	}
	docsFullData struct {
		*sitedocs.Full
		LangSource string `json:"lang_source"`
	}
)

// writeDocsMarkdown prints a document: JSON with --json, the Markdown as it is
// when stdout is not a terminal (so it can be redirected into a file), and
// rendered for people on a terminal.
func (a *app) writeDocsMarkdown(data any, markdown, source string) error {
	switch {
	case a.jsonOut:
		return a.writeData(data)
	case a.agent:
		_, err := io.WriteString(a.io.Out, markdown)
		return err
	}
	if err := a.renderMarkdown(markdown); err != nil {
		return err
	}
	fmt.Fprintln(ui.Writer(a.io.Out), ui.Muted.Render(source))
	return nil
}

func (a *app) docsIndexCommand(f *docsFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "index",
		Short: "List the docs pages by section, from the language's llms.txt",
		Long: `List the docs pages of a language by section, with each page's slug, URL and
description, from https://cch-plus.com/<lang>/llms.txt. Read a page with
cchp docs read <slug>.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := a.docsSetup(f)
			if err != nil {
				return err
			}
			if a.dryRun {
				return a.docsDryRun(r.site.LLMSTxtURL(r.lang), sitedocs.AcceptDocument)
			}
			ctx, cancel := a.docsContext(cmd)
			defer cancel()
			idx, err := r.client.Index(ctx, r.lang)
			if err != nil {
				return err
			}
			if a.agent {
				return a.writeData(docsIndexData{Index: idx, LangSource: r.langSource})
			}
			w := ui.Writer(a.io.Out)
			fmt.Fprintln(w, ui.Title.Render(idx.Title)+ui.Muted.Render(" · "+idx.Lang))
			if idx.Summary != "" {
				fmt.Fprintln(w, ui.Wrap(idx.Summary, a.io.Width, 0))
			}
			for _, s := range idx.Sections {
				fmt.Fprintln(w)
				fmt.Fprintln(w, ui.Title.Render(s.Title))
				for _, p := range s.Pages {
					name := p.Slug
					if name == "" {
						name = p.URL
					}
					fmt.Fprintln(w, "  "+ui.Key.Render(name)+"  "+p.Title)
					if p.Description != "" {
						fmt.Fprintln(w, "    "+ui.Muted.Render(ui.Wrap(p.Description, a.io.Width-4, 4)))
					}
				}
			}
			fmt.Fprintln(w)
			fmt.Fprintln(w, ui.Muted.Render(fmt.Sprintf("%d pages. Read one with: cchp docs read <slug> --lang %s", idx.Count, idx.Lang)))
			return nil
		},
	}
}

func (a *app) docsReadCommand(f *docsFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "read <slug|url>",
		Short: "Print one docs page as Markdown",
		Long: `Print one docs page as Markdown, from https://cch-plus.com/<lang>/docs/<slug>.md.
Name the page by its slug from cchp docs index or cchp docs search, such as
installation/docker-compose, or by its docs URL or .md URL. A URL brings its
own language.

Without --json the Markdown is printed as it is, so it can be redirected into
a file; on a terminal it is rendered for reading.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := a.docsSetup(f)
			if err != nil {
				return err
			}
			ref, err := r.site.ParseRef(args[0])
			if err != nil {
				return err
			}
			if ref.Lang != "" {
				if r.langSource == sitedocs.SourceFlag && ref.Lang != r.lang {
					return contract.Fail(contract.KindUsage, contract.ExitUsage, "the URL is a page in %s, but --lang asks for %s", ref.Lang, r.lang).
						WithHint("Leave out --lang, or name the page by its slug: cchp docs read %s --lang %s", ref.Slug, r.lang)
				}
				r.lang, r.langSource = ref.Lang, sitedocs.SourceURL
			}
			if a.dryRun {
				return a.docsDryRun(r.site.MarkdownURL(r.lang, ref.Slug), sitedocs.AcceptDocument)
			}
			ctx, cancel := a.docsContext(cmd)
			defer cancel()
			page, err := r.client.Read(ctx, r.lang, ref.Slug)
			if err != nil {
				return err
			}
			return a.writeDocsMarkdown(docsPageData{Page: page, LangSource: r.langSource}, page.Markdown, page.URL)
		},
	}
}

func (a *app) docsSearchCommand(f *docsFlags) *cobra.Command {
	var limit, readTop int
	cmd := &cobra.Command{
		Use:   "search <words>...",
		Short: "Search the docs with the site's full-text search",
		Long: `Search the docs of a language with the full-text search of cch-plus.com and list
the results by relevance, with each page's title, section, URL, slug, a
snippet that shows the match, and the score. Chinese, Japanese and Korean
words need no spaces.

Results are sections of pages, so a page can appear more than once; its url
leads to the section. --read-top N adds the Markdown of the N best pages, on
each page's first result, so an agent can answer in one call:

  cchp docs search 网络代理 --lang zh-CN --read-top 1 --json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(strings.Join(args, " "))
			if query == "" {
				return contract.Fail(contract.KindUsage, contract.ExitUsage, "search needs words to look for")
			}
			if cmd.Flags().Changed("limit") && (limit < 1 || limit > sitedocs.MaxSearchLimit) {
				return contract.Fail(contract.KindUsage, contract.ExitUsage, "--limit must be between 1 and %d", sitedocs.MaxSearchLimit)
			}
			if readTop < 0 || readTop > sitedocs.MaxSearchLimit {
				return contract.Fail(contract.KindUsage, contract.ExitUsage, "--read-top must be between 0 and %d", sitedocs.MaxSearchLimit)
			}
			r, err := a.docsSetup(f)
			if err != nil {
				return err
			}
			if a.dryRun {
				return a.docsDryRun(r.site.SearchURL(r.lang, query, limit), sitedocs.AcceptJSON)
			}
			ctx, cancel := a.docsContext(cmd)
			defer cancel()
			res, err := r.client.Search(ctx, r.lang, query, limit, readTop)
			if err != nil {
				return err
			}
			if a.agent {
				return a.writeData(docsSearchData{Search: res, LangSource: r.langSource})
			}
			w := ui.Writer(a.io.Out)
			if res.Count == 0 {
				fmt.Fprintf(w, "Nothing matches %q in %s. Try other words, or another language with --lang.\n", res.Query, res.Lang)
				return nil
			}
			for i, h := range res.Results {
				head := fmt.Sprintf("%d. %s", i+1, ui.Key.Render(h.Title))
				if h.Section != "" {
					head += ui.Muted.Render(" · " + h.Section)
				}
				fmt.Fprintln(w, head)
				if h.Snippet != "" {
					fmt.Fprintln(w, "   "+ui.Wrap(h.Snippet, a.io.Width-3, 3))
				}
				next := h.URL
				if h.Slug != "" {
					next = "cchp docs read " + h.Slug + " --lang " + res.Lang
				}
				fmt.Fprintln(w, "   "+ui.Muted.Render(next))
			}
			fmt.Fprintln(w, ui.Muted.Render(fmt.Sprintf("%d results for %q in %s.", res.Count, res.Query, res.Lang)))
			for _, h := range res.Results {
				if h.Markdown == "" {
					continue
				}
				fmt.Fprintln(w)
				fmt.Fprintln(w, ui.Title.Render("── "+h.Title+" ──"))
				if err := a.renderMarkdown(h.Markdown); err != nil {
					return err
				}
				fmt.Fprintln(w, ui.Muted.Render(h.URL))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, fmt.Sprintf("Most results to return, 1 to %d (default 10).", sitedocs.MaxSearchLimit))
	cmd.Flags().IntVar(&readTop, "read-top", 0, "Also read the Markdown of this many best pages.")
	return cmd
}

func (a *app) docsFullCommand(f *docsFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "full",
		Short: "Print every docs page of a language as one Markdown file (llms-full.txt)",
		Long: `Print every docs page of a language as one Markdown document, in sidebar order,
from https://cch-plus.com/<lang>/llms-full.txt. Each page starts with its title
and URL.

Without --json the Markdown is printed as it is, so it can be redirected into
a file; on a terminal it is rendered for reading.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := a.docsSetup(f)
			if err != nil {
				return err
			}
			if a.dryRun {
				return a.docsDryRun(r.site.FullURL(r.lang), sitedocs.AcceptDocument)
			}
			ctx, cancel := a.docsContext(cmd)
			defer cancel()
			full, err := r.client.Full(ctx, r.lang)
			if err != nil {
				return err
			}
			return a.writeDocsMarkdown(docsFullData{Full: full, LangSource: r.langSource}, full.Markdown, full.URL)
		},
	}
}
