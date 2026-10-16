package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/CCH-HQ/cchp-cli/internal/auth"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/runner"
	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

// screen is one page of the TUI.
type screen interface {
	init(m *Model) tea.Cmd
	update(m *Model, msg tea.Msg) tea.Cmd
	view(m *Model) string
	hints() string
}

type (
	pushMsg     struct{ s screen }
	popMsg      struct{}
	identityMsg struct {
		id  *auth.Identity
		err error
	}
	fetchedMsg struct {
		owner screen
		key   string
		res   *contract.Result
		doc   any
		err   error
	}
)

func push(s screen) tea.Cmd { return func() tea.Msg { return pushMsg{s} } }
func pop() tea.Msg          { return popMsg{} }

// Model is the whole TUI.
type Model struct {
	deps    Deps
	ctx     context.Context
	stack   []screen
	width   int
	height  int
	profile *auth.Profile
	// signedIn is true when a credential resolves (saved or from the environment).
	signedIn bool
	server   string
	identity *auth.Identity
	checkErr error
	spin     spinner.Model
	// done holds the line printed after the program ends, for example after sign-in.
	done string
	// quitAfterLogin ends the program once sign-in succeeds (cchp login).
	quitAfterLogin bool
}

// NewModel builds the home screen. With login set it starts on sign-in and
// quits once it succeeds.
func NewModel(ctx context.Context, d Deps, login bool, server string) *Model {
	m := &Model{deps: d, ctx: ctx, width: 80, height: 24}
	m.spin = spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(lipgloss.NewStyle().Foreground(ui.Accent)))
	m.loadProfile()
	if login {
		m.quitAfterLogin = true
		m.stack = []screen{newLoginScreen(server, m.server)}
	} else {
		m.stack = []screen{newMenu()}
	}
	return m
}

func (m *Model) loadProfile() {
	m.profile, m.signedIn, m.server, m.identity, m.checkErr = nil, false, "", nil, nil
	p, err := m.deps.Store.Current()
	if err != nil {
		m.checkErr = err
		return
	}
	if p != nil {
		m.profile = p
		m.server = p.Server
		id := p.Identity
		m.identity = &id
	}
	if cred, err := m.deps.Runner.Credential(); err == nil {
		m.signedIn = true
		m.server = cred.Server
		if cred.Identity != nil {
			m.identity = cred.Identity
		}
	} else if !errors.Is(err, auth.ErrNotSignedIn) {
		m.checkErr = err
	}
}

func (m *Model) top() screen { return m.stack[len(m.stack)-1] }

// Init starts the first screen and checks the token in the background.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.top().init(m), m.spin.Tick}
	if m.signedIn && !m.quitAfterLogin {
		cmds = append(cmds, m.verify())
	}
	return tea.Batch(cmds...)
}

func (m *Model) verify() tea.Cmd {
	r := m.deps.Runner
	ctx := m.ctx
	return func() tea.Msg {
		cred, err := r.Credential()
		if err != nil {
			return identityMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		id, err := r.Whoami(ctx, cred)
		return identityMsg{id: id, err: err}
	}
}

// Update routes messages to the current screen.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case identityMsg:
		if msg.err != nil {
			m.checkErr = msg.err
		} else {
			m.identity, m.checkErr = msg.id, nil
		}
		return m, nil
	case fetchedMsg:
		for _, target := range m.stack {
			if target == msg.owner {
				return m, target.update(m, msg)
			}
		}
		return m, nil
	case pushMsg:
		m.stack = append(m.stack, msg.s)
		return m, msg.s.init(m)
	case popMsg:
		if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
			return m, m.top().update(m, tea.WindowSizeMsg{Width: m.width, Height: m.height})
		}
		return m, tea.Quit
	}
	return m, m.top().update(m, msg)
}

// View draws the header, the current screen and its key hints.
func (m *Model) View() tea.View {
	if m.quitAfterLogin && m.done != "" {
		// Sign-in finished: clear the form; the result line is printed after.
		return tea.NewView("")
	}
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	b.WriteString(m.top().view(m))
	b.WriteString("\n\n")
	b.WriteString("  " + ui.Wrap(m.top().hints(), maxInt(m.width-4, 12), 0))
	v := tea.NewView(b.String())
	v.AltScreen = !m.quitAfterLogin
	v.WindowTitle = "cchp"
	return v
}

func (m *Model) header() string {
	brand := "  " + ui.Brand.Render("cchp") + ui.Muted.Render("  CCHP from your terminal")
	var status string
	switch {
	case !m.signedIn:
		status = ui.Muted.Render("Not signed in")
	case m.checkErr != nil:
		status = ui.Warning.Render("Signed in to "+m.server+", but the token didn't work") + ui.Muted.Render(" — sign in again")
	case m.identity != nil:
		where := " on " + m.server
		if m.identity.OrganizationName != "" {
			where = " · " + m.identity.OrganizationName + where
		}
		status = ui.Success.Render("●") + " " + auth.DisplayName(*m.identity) + ui.Muted.Render(where)
	default:
		status = ui.Muted.Render("Signed in to " + m.server)
	}
	return ui.Wrap(brand, maxInt(m.width-2, 12), 2) + "\n  " +
		strings.ReplaceAll(ui.Wrap(status, maxInt(m.width-4, 12), 0), "\n", "\n  ")
}

// fetch runs one read operation through the runner.
func (m *Model) fetch(owner screen, key, opID string, params map[string][]string) tea.Cmd {
	r := m.deps.Runner
	ctx := m.ctx
	return func() tea.Msg {
		op, ok := r.Catalog.Get(opID)
		if !ok {
			return fetchedMsg{owner: owner, key: key, err: errors.New("unknown operation " + opID)}
		}
		res, err := r.Run(ctx, runner.Options{Op: op, Params: params, Timeout: 30 * time.Second})
		if err != nil {
			return fetchedMsg{owner: owner, key: key, err: err}
		}
		var doc any
		if len(res.Data) > 0 {
			dec := json.NewDecoder(strings.NewReader(string(res.Data)))
			dec.UseNumber()
			if err := dec.Decode(&doc); err != nil {
				return fetchedMsg{owner: owner, key: key, err: err}
			}
		}
		return fetchedMsg{owner: owner, key: key, res: res, doc: doc}
	}
}

// DoneLine is the line a finished sign-in prints.
func DoneLine(m *Model) string { return m.done }

// Run opens the home screen.
func Run(ctx context.Context, d Deps) error {
	m := NewModel(ctx, d, false, "")
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	return err
}

// Login runs the guided sign-in and prints the outcome.
func Login(ctx context.Context, d Deps, server string) error {
	m := NewModel(ctx, d, true, server)
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
		return err
	}
	if m.done == "" {
		return contract.Fail(contract.KindUsage, contract.ExitUsage, "Sign-in was cancelled.")
	}
	_, err = ui.Writer(os.Stdout).Write([]byte(m.done + "\n"))
	return err
}
