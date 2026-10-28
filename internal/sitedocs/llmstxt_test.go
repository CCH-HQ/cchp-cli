package sitedocs

import (
	"reflect"
	"strings"
	"testing"
)

// The example from https://llmstxt.org, cut down from the FastHTML llms.txt.
const fastHTML = "\xef\xbb\xbf# FastHTML\n\n" +
	"> FastHTML is a python library which brings together Starlette, Uvicorn, HTMX, and fastcore's `FT` \"FastTags\" into a library for creating server-rendered hypermedia applications.\n\n" +
	"Important notes:\n\n" +
	"- Although parts of its API are inspired by FastAPI, it is *not* compatible with FastAPI syntax and is not targeted at creating API services\n" +
	"- FastHTML is compatible with JS-native web components and any vanilla JS library, but not with React, Vue, or Svelte.\n\n" +
	"## Docs\n\n" +
	"- [FastHTML quick start](https://fastht.ml/docs/tutorials/quickstart_for_web_devs.html.md): A brief overview of many FastHTML features\n" +
	"- [HTMX reference](https://github.com/bigskysoftware/htmx/blob/master/www/content/reference.md): Brief description of all HTMX attributes, CSS classes, headers, events, extensions, js lib methods, and config options\n\n" +
	"## Examples\n\n" +
	"- [Todo list application](https://github.com/AnswerDotAI/fasthtml/blob/main/examples/adv_app.py): Detailed walk-thru of a complete CRUD app in FastHTML showing idiomatic use of FastHTML and HTMX patterns.\n\n" +
	"## Optional\n\n" +
	"- [Starlette full documentation](https://gist.githubusercontent.com/jph00/809e4a4808d4510be0e3dc9565e9cbd3/raw/9b717589ca44cedc8aaf00b2b8cacef922964c0f/starlette-sml.md): A subset of the Starlette documentation useful for FastHTML development. \n"

func TestParseLLMSTxtReadsTheSpecExample(t *testing.T) {
	f, err := ParseLLMSTxt([]byte(fastHTML))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "FastHTML" {
		t.Fatalf("title %q", f.Title)
	}
	if !strings.HasPrefix(f.Summary, "FastHTML is a python library") || !strings.Contains(f.Summary, "`FT` \"FastTags\"") {
		t.Fatalf("summary %q", f.Summary)
	}
	wantDetails := "Important notes:\n\n- Although parts of its API are inspired by FastAPI, it is *not* compatible with FastAPI syntax and is not targeted at creating API services\n- FastHTML is compatible with JS-native web components and any vanilla JS library, but not with React, Vue, or Svelte."
	if f.Details != wantDetails {
		t.Fatalf("details %q", f.Details)
	}
	want := []LLMSSection{
		{Title: "Docs", Links: []LLMSLink{
			{Title: "FastHTML quick start", URL: "https://fastht.ml/docs/tutorials/quickstart_for_web_devs.html.md", Notes: "A brief overview of many FastHTML features"},
			{Title: "HTMX reference", URL: "https://github.com/bigskysoftware/htmx/blob/master/www/content/reference.md", Notes: "Brief description of all HTMX attributes, CSS classes, headers, events, extensions, js lib methods, and config options"},
		}},
		{Title: "Examples", Links: []LLMSLink{
			{Title: "Todo list application", URL: "https://github.com/AnswerDotAI/fasthtml/blob/main/examples/adv_app.py", Notes: "Detailed walk-thru of a complete CRUD app in FastHTML showing idiomatic use of FastHTML and HTMX patterns."},
		}},
		{Title: "Optional", Optional: true, Links: []LLMSLink{
			{Title: "Starlette full documentation", URL: "https://gist.githubusercontent.com/jph00/809e4a4808d4510be0e3dc9565e9cbd3/raw/9b717589ca44cedc8aaf00b2b8cacef922964c0f/starlette-sml.md", Notes: "A subset of the Starlette documentation useful for FastHTML development."},
		}},
	}
	if !reflect.DeepEqual(f.Sections, want) {
		t.Fatalf("sections\n got %+v\nwant %+v", f.Sections, want)
	}
}

func TestParseLLMSTxtMinimalAndOptionalParts(t *testing.T) {
	f, err := ParseLLMSTxt([]byte("# Only a title\n"))
	if err != nil || f.Title != "Only a title" || f.Summary != "" || f.Details != "" || len(f.Sections) != 0 {
		t.Fatalf("%+v %v", f, err)
	}
	src := "Setext title\n============\n\nSome details without a summary.\n\n" +
		"## 安装\n\n* [Docker Compose 部署](/zh-CN/docs/installation/docker-compose.md)\n" +
		"* [`cchp` 命令行](https://cch-plus.com/zh-CN/docs/cli.md): 用 **cchp** 管理 CCHP,\n  支持 `--json`\n\n" +
		"## Empty section\n"
	f, err = ParseLLMSTxt([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Setext title" || f.Summary != "" || f.Details != "Some details without a summary." {
		t.Fatalf("%+v", f)
	}
	want := []LLMSSection{
		{Title: "安装", Links: []LLMSLink{
			{Title: "Docker Compose 部署", URL: "/zh-CN/docs/installation/docker-compose.md"},
			{Title: "cchp 命令行", URL: "https://cch-plus.com/zh-CN/docs/cli.md", Notes: "用 cchp 管理 CCHP, 支持 --json"},
		}},
		{Title: "Empty section", Links: []LLMSLink{}},
	}
	if !reflect.DeepEqual(f.Sections, want) {
		t.Fatalf("sections\n got %+v\nwant %+v", f.Sections, want)
	}
}

// remark-stringify, which renders the site's llms.txt, escapes Markdown
// characters in the notes; they come back as written.
func TestParseLLMSTxtUnescapesNotes(t *testing.T) {
	f, err := ParseLLMSTxt([]byte("# T\n\n## S\n\n- [A \\*star\\*](https://cch-plus.com/en/docs/a.md): Uses \\[brackets], \\_underscores\\_ &amp; `raw \\*code`\n- [Empty](https://cch-plus.com/en/docs/b.md): \n"))
	if err != nil {
		t.Fatal(err)
	}
	links := f.Sections[0].Links
	if links[0].Title != "A *star*" || links[0].Notes != "Uses [brackets], _underscores_ & raw \\*code" || links[1].Notes != "" {
		t.Fatalf("%+v", links)
	}
}

func TestParseLLMSTxtSummaryWithBlankQuoteLine(t *testing.T) {
	f, err := ParseLLMSTxt([]byte("# T\n> First line\n> second line\n>\n\nDetails here.\n\n## S\n\n- [a](https://a.example/a.md)\n"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Summary != "First line\nsecond line" || f.Details != "Details here." {
		t.Fatalf("summary %q details %q", f.Summary, f.Details)
	}
}

func TestParseLLMSTxtRejectsOtherShapes(t *testing.T) {
	cases := []struct{ src, want string }{
		{"", "it does not start with an H1 title"},
		{"Just text\n", "it does not start with an H1 title"},
		{"## Section first\n", "it does not start with an H1 title"},
		{"# \n", "its H1 title is empty"},
		{"# T\n\n### Deep\n", `it has an H3 heading "Deep" before its first section; only H2 headings start sections`},
		{"# T\n\n## S\n\n# Another title\n", `it has an H1 heading "Another title"; sections start with H2 headings`},
		{"# T\n\n## S\n\nA paragraph.\n", `its section "S" holds a paragraph; sections hold lists of links`},
		{"# T\n\n## S\n\n- plain text\n", `in its section "S", the list item "plain text" does not start with a link`},
		{"# T\n\n## S\n\n- [a](u) - notes\n", `in its section "S", the list item "a - notes" does not separate its link from its notes with a colon`},
		{"# T\n\n## S\n\n- [a](u)\n  - nested\n", `in its section "S", the list item "anested" is not a single line of text`},
		{"# T\n\n## S\n\n- [](u)\n", `in its section "S", the list item "" has a link without a title or URL`},
	}
	for _, c := range cases {
		_, err := ParseLLMSTxt([]byte(c.src))
		if err == nil || err.Error() != c.want {
			t.Errorf("ParseLLMSTxt(%q) error %v; want %q", c.src, err, c.want)
		}
	}
}

func TestMarkdownTitle(t *testing.T) {
	for src, want := range map[string]string{
		"# 网络代理\n\nSource: https://cch-plus.com/zh-CN/docs/providers/network-proxies\n": "网络代理",
		"\n\n# Spaced *title*\n": "Spaced title",
		"Intro\n\n# Later\n":     "",
		"## Not an H1\n":         "",
	} {
		if got := MarkdownTitle([]byte(src)); got != want {
			t.Errorf("MarkdownTitle(%q) = %q; want %q", src, got, want)
		}
	}
}
