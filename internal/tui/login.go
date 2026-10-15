package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/CCH-HQ/cchp-cli/internal/auth"
	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

type signedInMsg struct {
	profile *auth.Profile
	err     error
}

// loginScreen asks for two things, the server address and the token, then
// checks them with the server before saving anything.
type loginScreen struct {
	form *huh.Form
	// address shows addressFeedback as its description. The screen refreshes
	// it after every form update, on the program's own goroutine, because huh
	// evaluates a DescriptionFunc on a command goroutine while the form writes
	// the bound value.
	address *huh.Input
	server  string
	token   string
	working bool
	err     error
}

func newLoginScreen(server, current string) *loginScreen {
	s := &loginScreen{server: server}
	if s.server == "" {
		s.server = current
	}
	if s.server == "" {
		s.server = "https://"
	}
	s.address = huh.NewInput().
		Title("Server address").
		Value(&s.server).
		Description(addressFeedback(s.server)).
		Validate(func(v string) error {
			_, err := auth.NormalizeServer(v)
			return sentenceErr(err)
		})
	s.form = huh.NewForm(huh.NewGroup(
		s.address,
		huh.NewInput().
			Title("Management token").
			Description("Create one in the console: Account › Security › Management tokens.").
			EchoMode(huh.EchoModePassword).
			Value(&s.token).
			Validate(func(v string) error { return sentenceErr(auth.CheckTokenShape(v)) }),
	)).WithTheme(formTheme()).WithShowHelp(false).WithWidth(70)
	return s
}

func sentenceErr(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(ui.Sentence(err.Error()))
}

// addressFeedback checks the address as it is typed.
func addressFeedback(v string) string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" || trimmed == "https://" || trimmed == "http://" {
		return "For example https://hub.example.com"
	}
	norm, err := auth.NormalizeServer(trimmed)
	if err != nil {
		return ui.Warning.Render(ui.Sentence(err.Error()))
	}
	if norm != strings.TrimRight(trimmed, "/") {
		return ui.Success.Render("✓ ") + "cchp will use " + norm
	}
	return ui.Success.Render("✓ ") + "Looks right"
}

func (s *loginScreen) init(m *Model) tea.Cmd { return s.form.Init() }

func (s *loginScreen) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case signedInMsg:
		s.working = false
		if msg.err != nil {
			s.err = msg.err
			s.token = ""
			fresh := newLoginScreen(s.server, "")
			s.form, s.address = fresh.form, fresh.address
			return s.form.Init()
		}
		m.loadProfile()
		line := ui.Success.Render("✓ ") + "Signed in as " + ui.Key.Render(auth.DisplayName(msg.profile.Identity)) + " to " + msg.profile.Server
		if m.quitAfterLogin {
			m.done = line
			return tea.Quit
		}
		menu := newMenu()
		menu.note = line
		m.stack = []screen{menu}
		return m.verify()
	case tea.KeyPressMsg:
		if msg.String() == "esc" && !s.working {
			if m.quitAfterLogin {
				return tea.Quit
			}
			return pop
		}
	}
	if s.working {
		return nil
	}
	form, cmd := s.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		s.form = f
	}
	s.address.Description(addressFeedback(s.server))
	switch s.form.State {
	case huh.StateCompleted:
		s.working = true
		s.err = nil
		server, token := s.server, strings.TrimSpace(s.token)
		r := m.deps.Runner
		ctx := m.ctx
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			p, err := r.SignIn(ctx, server, token)
			return signedInMsg{profile: p, err: err}
		}
	case huh.StateAborted:
		if m.quitAfterLogin {
			return tea.Quit
		}
		return pop
	}
	return cmd
}

func (s *loginScreen) view(m *Model) string {
	var b strings.Builder
	b.WriteString("  " + ui.Title.Render("Sign in") + "\n\n")
	if s.working {
		b.WriteString("  " + m.spin.View() + " Checking your token with " + s.server + "…")
		return b.String()
	}
	for _, line := range strings.Split(s.form.View(), "\n") {
		b.WriteString("  " + line + "\n")
	}
	if s.err != nil {
		b.WriteString("\n  " + strings.ReplaceAll(plainError(s.err), "\n", "\n  "))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (s *loginScreen) hints() string {
	return hints("enter", "next", "shift+tab", "back", "esc", "cancel")
}
