//go:build e2e

package e2e

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/CCH-HQ/cchp-cli/internal/auth"
	"github.com/CCH-HQ/cchp-cli/internal/runner"
	"github.com/CCH-HQ/cchp-cli/internal/tui"
)

func tuiDeps(t *testing.T) tui.Deps {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CCHP_CREDENTIAL_STORE", "file")
	t.Setenv("CCHP_TOKEN", "")
	t.Setenv("CCHP_TOKEN_FILE", "")
	t.Setenv("CCHP_BASE_URL", "")
	store, err := auth.OpenStore(auth.Dirs{Config: filepath.Join(dir, "config"), Cache: filepath.Join(dir, "cache")})
	if err != nil {
		t.Fatal(err)
	}
	return tui.Deps{Runner: runner.New(store, "e2e"), Store: store, Version: "e2e"}
}

func waitText(t *testing.T, tm *teatest.TestModel, text string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool { return strings.Contains(ansi.Strip(string(b)), text) },
		teatest.WithDuration(30*time.Second), teatest.WithCheckInterval(50*time.Millisecond))
}

func TestTUISignInAndBrowse(t *testing.T) {
	d := tuiDeps(t)
	m := tui.NewModel(context.Background(), d, true, baseURL)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 30))
	waitText(t, tm, "Looks right")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	time.Sleep(500 * time.Millisecond)
	tm.Type(adminToken)
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	tm.WaitFinished(t, teatest.WithFinalTimeout(30*time.Second))
	if !strings.Contains(ansi.Strip(tui.DoneLine(m)), "Signed in as admin@e2e.example.com to "+baseURL) {
		t.Fatalf("sign-in line %q", ansi.Strip(tui.DoneLine(m)))
	}
	p, err := d.Store.Current()
	if err != nil || p == nil || p.Server != baseURL {
		t.Fatalf("saved profile %+v %v", p, err)
	}

	home := tui.NewModel(context.Background(), d, false, "")
	tm = teatest.NewTestModel(t, home, teatest.WithInitialTermSize(100, 30))
	waitText(t, tm, "admin@e2e.example.com")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyDown})
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	waitText(t, tm, "items")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEscape})

	// Provider keys: pick the first provider and see its keys, read a page at a time.
	waitText(t, tm, "The upstream services your gateway calls")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyDown})
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	waitText(t, tm, "items")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		out := ansi.Strip(string(b))
		i := strings.LastIndex(out, "Keys of ")
		return i >= 0 && (strings.Contains(out[i:], "item") || strings.Contains(out[i:], "This provider has no keys yet."))
	}, teatest.WithDuration(30*time.Second), teatest.WithCheckInterval(50*time.Millisecond))
	tm.Send(tea.KeyPressMsg{Code: 'q', Text: "q"})
	tm.Send(tea.KeyPressMsg{Code: 'q', Text: "q"})
	tm.Send(tea.KeyPressMsg{Code: 'q', Text: "q"})
	tm.WaitFinished(t, teatest.WithFinalTimeout(10*time.Second))
}
