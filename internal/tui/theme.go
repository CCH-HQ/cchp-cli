// Package tui is cchp's interactive home screen for people: a short task list,
// guided sign-in, searchable lists and plain-language errors.
package tui

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/CCH-HQ/cchp-cli/internal/auth"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/runner"
	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

// Deps is what the TUI needs from the rest of cchp.
type Deps struct {
	Runner  *runner.Runner
	Store   *auth.Store
	Version string
}

// formTheme follows the terminal's own colors with cchp's accent.
func formTheme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		t := huh.ThemeBase(isDark)
		t.Focused.Base = t.Focused.Base.BorderForeground(ui.Accent)
		t.Focused.Card = t.Focused.Base
		t.Focused.Title = lipgloss.NewStyle().Bold(true)
		t.Focused.Description = lipgloss.NewStyle().Faint(true)
		t.Focused.ErrorIndicator = lipgloss.NewStyle().Foreground(ui.Bad).SetString(" *")
		t.Focused.ErrorMessage = lipgloss.NewStyle().Foreground(ui.Bad)
		t.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(ui.Accent)
		t.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(ui.Accent)
		t.Focused.FocusedButton = lipgloss.NewStyle().Padding(0, 2).MarginRight(1).Bold(true).Reverse(true)
		t.Focused.BlurredButton = lipgloss.NewStyle().Padding(0, 2).MarginRight(1)
		t.Blurred = t.Focused
		t.Blurred.Base = t.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
		t.Blurred.Card = t.Blurred.Base
		return t
	})
}

func listStyles() list.Styles {
	s := list.DefaultStyles(true)
	s.Title = lipgloss.NewStyle().Bold(true)
	s.TitleBar = lipgloss.NewStyle().Padding(0, 0, 1, 2)
	s.Filter.Focused.Prompt = lipgloss.NewStyle().Foreground(ui.Accent)
	s.Filter.Blurred.Prompt = lipgloss.NewStyle().Foreground(ui.Accent)
	s.Filter.Cursor.Color = ui.Accent
	s.StatusBar = lipgloss.NewStyle().Faint(true).Padding(0, 0, 1, 2)
	s.StatusEmpty = lipgloss.NewStyle().Faint(true)
	s.NoItems = lipgloss.NewStyle().Faint(true).Padding(0, 2)
	s.PaginationStyle = lipgloss.NewStyle().Faint(true).PaddingLeft(2)
	s.HelpStyle = lipgloss.NewStyle().Faint(true).Padding(1, 0, 0, 2)
	s.ActivePaginationDot = lipgloss.NewStyle().SetString("•")
	s.InactivePaginationDot = lipgloss.NewStyle().Faint(true).SetString("•")
	s.DividerDot = lipgloss.NewStyle().Faint(true).SetString(" • ")
	return s
}

func itemDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	s := list.NewDefaultItemStyles(true)
	s.NormalTitle = lipgloss.NewStyle().Padding(0, 0, 0, 2)
	s.NormalDesc = s.NormalTitle.Faint(true)
	s.SelectedTitle = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(ui.Accent).Foreground(ui.Accent).Bold(true).Padding(0, 0, 0, 1)
	s.SelectedDesc = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(ui.Accent).Faint(true).Padding(0, 0, 0, 1)
	s.DimmedTitle = s.NormalTitle.Faint(true)
	s.DimmedDesc = s.NormalDesc
	s.FilterMatch = lipgloss.NewStyle().Underline(true)
	d.Styles = s
	return d
}

// plainError explains an error in plain words, with the request ID in small print.
func plainError(err error) string {
	if f, ok := errors.AsType[*contract.Failure](err); ok {
		var b strings.Builder
		b.WriteString(ui.Failure.Render("✗ ") + ui.Sentence(f.Err.Message))
		if f.Err.Hint != "" {
			b.WriteString("\n  " + f.Err.Hint)
		}
		if f.RequestID != "" {
			b.WriteString("\n  " + ui.Muted.Render("request "+f.RequestID))
		}
		return b.String()
	}
	return ui.Failure.Render("✗ ") + fmt.Sprintf("Something went wrong: %v", err)
}

// hints renders key help: each key bold, its meaning and the separators faint.
func hints(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, ui.Key.Render(pairs[i])+ui.Muted.Render(" "+pairs[i+1]))
	}
	return strings.Join(parts, ui.Muted.Render("  ·  "))
}
