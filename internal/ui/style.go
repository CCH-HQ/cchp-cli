// Package ui holds cchp's one restrained palette. It uses the terminal's own
// ANSI colors, so it follows light and dark themes, and output written through
// Writer drops color for NO_COLOR, dumb terminals and pipes.
package ui

import (
	"io"
	"os"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

var (
	Accent  = lipgloss.Color("4")
	Good    = lipgloss.Color("2")
	Bad     = lipgloss.Color("1")
	Caution = lipgloss.Color("3")
)

var (
	Title    = lipgloss.NewStyle().Bold(true)
	Brand    = lipgloss.NewStyle().Bold(true).Foreground(Accent)
	Muted    = lipgloss.NewStyle().Faint(true)
	Key      = lipgloss.NewStyle().Bold(true)
	Success  = lipgloss.NewStyle().Foreground(Good)
	Failure  = lipgloss.NewStyle().Foreground(Bad).Bold(true)
	Warning  = lipgloss.NewStyle().Foreground(Caution)
	Selected = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	Box      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Accent).Padding(0, 1)
	AlertBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Caution).Padding(0, 1)
)

// Writer wraps w so styled text is downgraded to what the terminal supports,
// with no color at all for NO_COLOR or when w is not a terminal.
func Writer(w io.Writer) io.Writer {
	return colorprofile.NewWriter(w, os.Environ())
}

// Wrap breaks text at word boundaries to fit width columns and indents every
// line after the first by indent spaces.
func Wrap(text string, width, indent int) string {
	if width <= indent+10 {
		return text
	}
	wrapped := ansi.Wrap(text, width-indent, "")
	return strings.ReplaceAll(wrapped, "\n", "\n"+strings.Repeat(" ", indent))
}

// Sentence starts text with a capital letter for people to read. The program
// name keeps its lower case.
func Sentence(text string) string {
	if text == "" || strings.HasPrefix(text, "cchp ") {
		return text
	}
	r := []rune(text)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// TerminalWidth is the width of the terminal behind f, or 0 when f is not one.
func TerminalWidth(f *os.File) int {
	w, _, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0
	}
	return w
}
