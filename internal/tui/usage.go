package tui

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"

	cchpcli "github.com/CCH-HQ/cchp-cli"
)

// table lays out columns: the first left-aligned, the rest right-aligned, with
// a bold header row.
func table(cols [][]string, firstColHeader bool) string {
	widths := make([]int, len(cols))
	rowsN := 0
	for i, c := range cols {
		for _, cell := range c {
			if w := lipgloss.Width(cell); w > widths[i] {
				widths[i] = w
			}
		}
		if len(c) > rowsN {
			rowsN = len(c)
		}
	}
	var b strings.Builder
	for r := 0; r < rowsN; r++ {
		b.WriteString("  ")
		for i, c := range cols {
			cell := ""
			if r < len(c) {
				cell = c[r]
			}
			style := lipgloss.NewStyle().Width(widths[i])
			if i > 0 {
				style = style.Align(lipgloss.Right)
			}
			switch {
			case r == 0:
				style = style.Bold(true)
			case i == 0 && firstColHeader:
				style = style.Faint(true)
			}
			b.WriteString(style.Render(cell))
			if i < len(cols)-1 {
				b.WriteString("   ")
			}
		}
		if r < rowsN-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func num(v any) string {
	n, ok := v.(json.Number)
	if !ok {
		return "—"
	}
	return groupDigits(n.String())
}

func percent(v any) string {
	n, ok := v.(json.Number)
	if !ok {
		return "—"
	}
	f, err := n.Float64()
	if err != nil {
		return "—"
	}
	if f <= 1 {
		f *= 100
	}
	return fmt.Sprintf("%.1f%%", f)
}

func cost(v any) string {
	n, ok := v.(json.Number)
	if !ok {
		return "—"
	}
	return credits(n)
}

func sum(a, b any) any {
	x, ok1 := a.(json.Number)
	y, ok2 := b.(json.Number)
	if !ok1 || !ok2 {
		return nil
	}
	xi, err1 := x.Int64()
	yi, err2 := y.Int64()
	if err1 != nil || err2 != nil {
		return nil
	}
	return json.Number(fmt.Sprint(xi + yi))
}

// guide topics shown in the TUI, the same files as "cchp guide".
var tuiGuides = []entry{
	{title: "Getting started", desc: "Sign in and run your first commands.", data: row{"file": "getting-started.md"}},
	{title: "Tokens and permissions", desc: "Why a call is refused and what to ask for.", data: row{"file": "tokens.md"}},
	{title: "Common tasks", desc: "Usage, providers, keys and recent requests.", data: row{"file": "tasks.md"}},
	{title: "Errors", desc: "What each error means and what to do next.", data: row{"file": "errors.md"}},
}

type guideList struct{ list list.Model }

func newGuideList() *guideList {
	items := make([]list.Item, len(tuiGuides))
	for i, g := range tuiGuides {
		items[i] = g
	}
	l := list.New(items, itemDelegate(), 80, 14)
	l.Title = "Guides"
	l.Styles = listStyles()
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()
	return &guideList{list: l}
}

func (s *guideList) init(m *Model) tea.Cmd {
	s.list.SetSize(m.width, maxInt(m.height-7, 8))
	return nil
}

func (s *guideList) update(m *Model, msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc", "q":
			return pop
		case "enter":
			if e, ok := s.list.SelectedItem().(entry); ok {
				return push(&guideView{title: e.title, file: str(e.data, "file")})
			}
		}
	}
	var cmd tea.Cmd
	s.list, cmd = s.list.Update(msg)
	return cmd
}

func (s *guideList) view(m *Model) string { return s.list.View() }
func (s *guideList) hints() string        { return hints("↑/↓", "move", "enter", "read", "esc", "back") }

type guideView struct {
	title string
	file  string
	vp    viewport.Model
	err   error
}

func (s *guideView) init(m *Model) tea.Cmd {
	s.vp = viewport.New(viewport.WithWidth(m.width), viewport.WithHeight(maxInt(m.height-6, 8)))
	text, err := fs.ReadFile(cchpcli.Docs, "guides/"+s.file)
	if err != nil {
		s.err = err
		return nil
	}
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("auto"), glamour.WithWordWrap(minInt(m.width-4, 88)))
	if err != nil {
		s.err = err
		return nil
	}
	out, err := r.Render(string(text))
	if err != nil {
		s.err = err
		return nil
	}
	s.vp.SetContent(out)
	return nil
}

func (s *guideView) update(m *Model, msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc", "q", "backspace":
			return pop
		}
	}
	var cmd tea.Cmd
	s.vp, cmd = s.vp.Update(msg)
	return cmd
}

func (s *guideView) view(m *Model) string {
	if s.err != nil {
		return "  " + plainError(s.err)
	}
	return s.vp.View()
}

func (s *guideView) hints() string { return hints("↑/↓", "scroll", "esc", "back") }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
