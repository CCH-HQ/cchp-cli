package sitedocs

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// LLMSTxt is an llms.txt file as https://llmstxt.org defines it.
type LLMSTxt struct {
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
	// Details is the Markdown between the summary and the first section.
	Details  string        `json:"details,omitempty"`
	Sections []LLMSSection `json:"sections"`
}

// LLMSSection is an H2 section and its list of links.
type LLMSSection struct {
	Title string `json:"title"`
	// Optional marks the conventional "Optional" section: links an agent can
	// skip when it needs a shorter context.
	Optional bool       `json:"optional"`
	Links    []LLMSLink `json:"links"`
}

// LLMSLink is one list item: a link and the notes after its colon. Title and
// notes are plain text, without Markdown syntax.
type LLMSLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Notes string `json:"notes,omitempty"`
}

var markdown = goldmark.New()

var setextH1Underline = regexp.MustCompile(`^=+[ \t]*$`)

// ParseLLMSTxt reads an llms.txt file: an optional byte order mark, an H1 with
// the title, an optional blockquote with the summary, Markdown blocks without
// headings, then H2 sections. Each section holds one Markdown list whose items
// are a link, optionally followed by a colon and notes. Anything else is an
// error that says where the file departs from the format.
func ParseLLMSTxt(src []byte) (*LLMSTxt, error) {
	src = bytes.TrimPrefix(src, []byte("\xef\xbb\xbf"))
	doc := markdown.Parser().Parse(text.NewReader(src))
	h1, ok := doc.FirstChild().(*ast.Heading)
	if !ok || h1.Level != 1 {
		return nil, errors.New("it does not start with an H1 title")
	}
	f := &LLMSTxt{Title: plainText(h1, src), Sections: []LLMSSection{}}
	if f.Title == "" {
		return nil, errors.New("its H1 title is empty")
	}
	// The details start after the title line (and a setext underline) or after
	// the summary's last line.
	detailsFrom := lineEnd(src, lastStop(h1))
	if next := lineEnd(src, detailsFrom); setextH1Underline.Match(bytes.TrimSpace(src[detailsFrom:next])) {
		detailsFrom = next
	}
	node := h1.NextSibling()
	if quote, ok := node.(*ast.Blockquote); ok {
		f.Summary = blockText(quote, src)
		detailsFrom = lineEnd(src, lastStop(quote))
		node = node.NextSibling()
	}

	firstDetail := node
	for ; node != nil; node = node.NextSibling() {
		h, ok := node.(*ast.Heading)
		if !ok {
			continue
		}
		if h.Level != 2 {
			return nil, fmt.Errorf("it has an H%d heading %q before its first section; only H2 headings start sections", h.Level, plainText(h, src))
		}
		if h.Lines().Len() == 0 {
			return nil, errors.New("it has a section with an empty H2 title")
		}
		break
	}
	if firstDetail != node {
		detailsTo := len(src)
		if node != nil {
			detailsTo = lineStart(src, node.Lines().At(0).Start)
		}
		f.Details = trimQuoteMarkers(string(src[detailsFrom:detailsTo]))
	}

	for node != nil {
		h, ok := node.(*ast.Heading)
		if !ok || h.Level != 2 {
			return nil, fmt.Errorf("it has an H%d heading %q; sections start with H2 headings", headingLevel(node), plainText(node, src))
		}
		section := LLMSSection{Title: plainText(h, src), Links: []LLMSLink{}}
		if section.Title == "" {
			return nil, errors.New("it has a section with an empty H2 title")
		}
		section.Optional = section.Title == "Optional"
		for node = node.NextSibling(); node != nil; node = node.NextSibling() {
			if _, ok := node.(*ast.Heading); ok {
				break
			}
			list, ok := node.(*ast.List)
			if !ok {
				return nil, fmt.Errorf("its section %q holds a %s; sections hold lists of links", section.Title, blockName(node))
			}
			for item := list.FirstChild(); item != nil; item = item.NextSibling() {
				link, err := parseItem(item, src)
				if err != nil {
					return nil, fmt.Errorf("in its section %q, %w", section.Title, err)
				}
				section.Links = append(section.Links, link)
			}
		}
		f.Sections = append(f.Sections, section)
	}
	return f, nil
}

// parseItem reads "[title](url)" or "[title](url): notes".
func parseItem(item ast.Node, src []byte) (LLMSLink, error) {
	block := item.FirstChild()
	if block == nil || block.NextSibling() != nil || (block.Kind() != ast.KindTextBlock && block.Kind() != ast.KindParagraph) {
		return LLMSLink{}, fmt.Errorf("the list item %q is not a single line of text", plainText(item, src))
	}
	link, ok := block.FirstChild().(*ast.Link)
	if !ok {
		return LLMSLink{}, fmt.Errorf("the list item %q does not start with a link", plainText(item, src))
	}
	out := LLMSLink{Title: plainText(link, src), URL: string(link.Destination)}
	if out.Title == "" || out.URL == "" {
		return LLMSLink{}, fmt.Errorf("the list item %q has a link without a title or URL", plainText(item, src))
	}
	after := link.NextSibling()
	if after == nil {
		return out, nil
	}
	colon, ok := after.(*ast.Text)
	if !ok || !bytes.HasPrefix(colon.Value(src), []byte(":")) {
		return LLMSLink{}, fmt.Errorf("the list item %q does not separate its link from its notes with a colon", plainText(item, src))
	}
	var b strings.Builder
	for n := after; n != nil; n = n.NextSibling() {
		writePlain(&b, n, src)
	}
	out.Notes = strings.TrimSpace(strings.TrimPrefix(b.String(), ":"))
	return out, nil
}

// plainText is the text of a node's inline content without Markdown syntax.
func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	writePlain(&b, n, src)
	return strings.TrimSpace(b.String())
}

func writePlain(b *strings.Builder, n ast.Node, src []byte) {
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.Text:
			v := t.Value(src)
			if !t.IsRaw() {
				v = util.ResolveNumericReferences(util.ResolveEntityNames(util.UnescapePunctuations(v)))
			}
			b.Write(v)
			if t.SoftLineBreak() || t.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(t.Value)
		}
		return ast.WalkContinue, nil
	})
}

// blockText is the Markdown source of the text blocks inside n: lines of a
// paragraph joined by newlines, paragraphs by blank lines.
func blockText(n ast.Node, src []byte) string {
	var paras []string
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || c.Type() != ast.TypeBlock || c.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}
		var lines []string
		for i := 0; i < c.Lines().Len(); i++ {
			seg := c.Lines().At(i)
			lines = append(lines, strings.TrimSpace(string(seg.Value(src))))
		}
		paras = append(paras, strings.Join(lines, "\n"))
		return ast.WalkSkipChildren, nil
	})
	return strings.Join(paras, "\n\n")
}

// lastStop is the end offset of the last source line inside n.
func lastStop(n ast.Node) int {
	stop := 0
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && c.Type() == ast.TypeBlock && c.Lines().Len() > 0 {
			stop = max(stop, c.Lines().At(c.Lines().Len()-1).Stop)
		}
		return ast.WalkContinue, nil
	})
	return stop
}

// lineEnd is the offset just past the newline that ends the line holding offset i.
func lineEnd(src []byte, i int) int {
	if i >= len(src) {
		return len(src)
	}
	if j := bytes.IndexByte(src[i:], '\n'); j >= 0 {
		return i + j + 1
	}
	return len(src)
}

// lineStart is the offset where the line holding offset i starts.
func lineStart(src []byte, i int) int {
	return bytes.LastIndexByte(src[:i], '\n') + 1
}

// trimQuoteMarkers drops the summary blockquote's empty ">" lines that follow
// its text, then surrounding blank lines.
func trimQuoteMarkers(s string) string {
	lines := strings.Split(s, "\n")
	for len(lines) > 0 && (strings.TrimSpace(lines[0]) == "" || strings.TrimSpace(lines[0]) == ">") {
		lines = lines[1:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func headingLevel(n ast.Node) int {
	if h, ok := n.(*ast.Heading); ok {
		return h.Level
	}
	return 0
}

func blockName(n ast.Node) string {
	switch n.Kind() {
	case ast.KindParagraph:
		return "paragraph"
	case ast.KindBlockquote:
		return "blockquote"
	case ast.KindFencedCodeBlock, ast.KindCodeBlock:
		return "code block"
	case ast.KindThematicBreak:
		return "horizontal rule"
	case ast.KindHTMLBlock:
		return "HTML block"
	}
	return strings.ToLower(n.Kind().String())
}

// MarkdownTitle is the text of a Markdown document's first heading when that
// heading is its first block and an H1.
func MarkdownTitle(src []byte) string {
	doc := markdown.Parser().Parse(text.NewReader(src))
	if h, ok := doc.FirstChild().(*ast.Heading); ok && h.Level == 1 {
		return plainText(h, src)
	}
	return ""
}
