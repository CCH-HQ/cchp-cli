package tui

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"

	cchpcli "github.com/CCH-HQ/cchp-cli"
	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

// usageScreen shows key figures for today, this week and this month, and the
// busiest models of the last seven days.
type usageScreen struct {
	summary map[string]any
	models  []any
	pending int
	err     error
}

func newUsageScreen() *usageScreen { return &usageScreen{} }

func (s *usageScreen) init(m *Model) tea.Cmd {
	s.pending = 2
	start := time.Now().UTC().Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	return tea.Batch(
		m.fetch("usage-summary", "getUsageSummary", nil),
		m.fetch("usage-models", "listUsageByModel", map[string][]string{"start": {start}, "limit": {"8"}}),
	)
}

func (s *usageScreen) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case fetchedMsg:
		switch msg.key {
		case "usage-summary":
			s.pending--
			if msg.err != nil {
				s.err = msg.err
				return nil
			}
			s.summary, _ = msg.doc.(map[string]any)
		case "usage-models":
			s.pending--
			if msg.err != nil {
				s.err = msg.err
				return nil
			}
			obj, _ := msg.doc.(map[string]any)
			s.models, _ = obj["items"].([]any)
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q", "backspace":
			return pop
		case "r":
			s.summary, s.models, s.err = nil, nil, nil
			return s.init(m)
		}
	}
	return nil
}

func (s *usageScreen) view(m *Model) string {
	head := "  " + ui.Title.Render("Usage") + "\n\n"
	if s.err != nil {
		return head + "  " + strings.ReplaceAll(plainError(s.err), "\n", "\n  ")
	}
	if s.pending > 0 {
		return head + "  " + m.spin.View() + " Loading…"
	}
	windows := []struct{ key, title string }{{"today", "Today"}, {"week", "This week"}, {"month", "This month"}}
	rows := []struct {
		label string
		get   func(map[string]any) string
	}{
		{"Requests", func(w map[string]any) string { return num(w["requests"]) }},
		{"Success rate", func(w map[string]any) string { return percent(w["successRate"]) }},
		{"Tokens in", func(w map[string]any) string { return num(w["inputTokens"]) }},
		{"Tokens out", func(w map[string]any) string { return num(w["outputTokens"]) }},
		{"Cost", func(w map[string]any) string { return cost(w["costUnits"]) }},
	}
	cols := [][]string{{""}}
	for _, r := range rows {
		cols[0] = append(cols[0], r.label)
	}
	for _, w := range windows {
		win, _ := s.summary[w.key].(map[string]any)
		col := []string{w.title}
		for _, r := range rows {
			col = append(col, r.get(win))
		}
		cols = append(cols, col)
	}
	var b strings.Builder
	b.WriteString(head)
	b.WriteString(table(cols, true))
	b.WriteString("\n\n  " + ui.Title.Render("Busiest models, last 7 days") + "\n\n")
	if len(s.models) == 0 {
		b.WriteString("  " + ui.Muted.Render("No requests in the last 7 days."))
		return b.String()
	}
	mcols := [][]string{{"Model"}, {"Requests"}, {"Tokens"}, {"Cost"}}
	for _, it := range s.models {
		r, _ := it.(map[string]any)
		requested, final := str(r, "requestedModel"), str(r, "finalModel")
		model := requested
		switch {
		case requested == "":
			model = final
		case final == "":
			model = requested + ui.Muted.Render(" (not sent to a provider)")
		case final != requested:
			model = requested + ui.Muted.Render(" → "+final)
		}
		mcols[0] = append(mcols[0], model)
		mcols[1] = append(mcols[1], num(r["requests"]))
		mcols[2] = append(mcols[2], num(sum(r["inputTokens"], r["outputTokens"])))
		mcols[3] = append(mcols[3], cost(r["costUnits"]))
	}
	b.WriteString(table(mcols, false))
	return b.String()
}

func (s *usageScreen) hints() string { return hints("r", "refresh", "esc", "back") }

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
