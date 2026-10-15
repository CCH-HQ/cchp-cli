package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

type menuItem struct {
	label string
	desc  string
	open  func(m *Model) tea.Cmd
}

type menuScreen struct {
	cursor int
	note   string
}

func newMenu() *menuScreen { return &menuScreen{} }

func (s *menuScreen) items(m *Model) []menuItem {
	signIn := menuItem{"Sign in", "Connect cchp to your CCHP server", func(m *Model) tea.Cmd {
		return push(newLoginScreen("", m.server))
	}}
	guides := menuItem{"Guides", "Short how-tos for common jobs", func(m *Model) tea.Cmd { return push(newGuideList()) }}
	quit := menuItem{"Quit", "", func(m *Model) tea.Cmd { return tea.Quit }}
	if !m.signedIn {
		return []menuItem{signIn, guides, quit}
	}
	items := []menuItem{
		{"See usage", "Requests, tokens and cost for today, this week and this month", func(m *Model) tea.Cmd { return push(newUsageScreen()) }},
		{"Providers", "The upstream services your gateway calls", func(m *Model) tea.Cmd { return push(providersScreen()) }},
		{"Provider keys", "Pick a provider to see its keys", func(m *Model) tea.Cmd { return push(providerKeysPicker()) }},
		{"LLM tokens", "Keys your apps use to call models", func(m *Model) tea.Cmd { return push(llmTokensScreen()) }},
		{"Recent requests", "The latest calls through the gateway", func(m *Model) tea.Cmd { return push(requestsScreen()) }},
		guides,
	}
	if m.checkErr != nil {
		items = append(items, signIn)
	}
	items = append(items, menuItem{"Sign out", "Forget the saved token on this computer", func(m *Model) tea.Cmd {
		if err := m.deps.Store.Delete(m.server); err != nil {
			s.note = plainError(err)
			return nil
		}
		m.loadProfile()
		s.cursor = 0
		s.note = ui.Success.Render("✓ ") + "Signed out. The token still works until you revoke it in the console."
		return nil
	}}, quit)
	return items
}

func (s *menuScreen) init(m *Model) tea.Cmd { return nil }

func (s *menuScreen) update(m *Model, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	items := s.items(m)
	switch key.String() {
	case "up", "k", "shift+tab":
		s.cursor = (s.cursor - 1 + len(items)) % len(items)
	case "down", "j", "tab":
		s.cursor = (s.cursor + 1) % len(items)
	case "enter", "space":
		s.note = ""
		return items[s.cursor].open(m)
	case "q", "esc":
		return tea.Quit
	}
	return nil
}

func (s *menuScreen) view(m *Model) string {
	var b strings.Builder
	if !m.signedIn {
		b.WriteString("  " + ui.Title.Render("Welcome!") + " You're not signed in yet.\n")
		b.WriteString("  " + ui.Muted.Render("You'll need your server address and a management token from the console.") + "\n\n")
	} else {
		b.WriteString("  " + ui.Title.Render("What would you like to do?") + "\n\n")
	}
	items := s.items(m)
	if s.cursor >= len(items) {
		s.cursor = 0
	}
	for i, it := range items {
		if i == s.cursor {
			b.WriteString("  " + ui.Selected.Render("› "+it.label))
			if it.desc != "" {
				b.WriteString("  " + ui.Muted.Render(it.desc))
			}
		} else {
			b.WriteString("    " + it.label)
		}
		b.WriteString("\n")
	}
	if s.note != "" {
		b.WriteString("\n  " + s.note + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (s *menuScreen) hints() string {
	return hints("↑/↓", "move", "enter", "open", "q", "quit")
}
