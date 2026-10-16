package tui

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
)

func deps(t *testing.T) Deps {
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
	return Deps{Runner: runner.New(store, "test"), Store: store, Version: "test"}
}

// plainView renders the model's current view as plain text for the golden files.
func plainView(t *testing.T, m tea.Model) []byte {
	t.Helper()
	text := ansi.Strip(m.(*Model).View().Content)
	var lines []string
	for l := range strings.SplitSeq(text, "\n") {
		lines = append(lines, strings.TrimRight(l, " "))
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func start(t *testing.T, m *Model) *teatest.TestModel {
	t.Helper()
	return teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 30))
}

func waitFor(t *testing.T, tm *teatest.TestModel, text string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool { return strings.Contains(ansi.Strip(string(b)), text) },
		// Reads are retried twice on network errors, so a server that is down
		// takes several seconds to report.
		teatest.WithDuration(20*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestHomeSignedOut(t *testing.T) {
	tm := start(t, NewModel(context.Background(), deps(t), false, ""))
	waitFor(t, tm, "Sign in")
	tm.Send(tea.KeyPressMsg{Code: 'q', Text: "q"})
	teatest.RequireEqualOutput(t, plainView(t, tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second))))
}

func TestHomeWithATokenThatNoLongerWorks(t *testing.T) {
	d := deps(t)
	// A saved sign-in for a server that is not running: the header says so plainly.
	if _, err := d.Store.Save("http://127.0.0.1:1", "cch_auth_live_GOLDEN_secret", auth.Identity{Email: "dana@example.com", OrganizationName: "Acme"}); err != nil {
		t.Fatal(err)
	}
	tm := start(t, NewModel(context.Background(), d, false, ""))
	waitFor(t, tm, "didn't work")
	tm.Send(tea.KeyPressMsg{Code: 'q', Text: "q"})
	teatest.RequireEqualOutput(t, plainView(t, tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second))))
}

func TestSignInChecksTheAddressAsYouType(t *testing.T) {
	tm := start(t, NewModel(context.Background(), deps(t), true, ""))
	waitFor(t, tm, "Server address")
	for range len("https://") {
		tm.Send(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	tm.Type("http://hub.example.com")
	waitFor(t, tm, "plain http is only")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEscape})
	teatest.RequireEqualOutput(t, plainView(t, tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second))))
}

func TestSignInRejectsAnLLMToken(t *testing.T) {
	tm := start(t, NewModel(context.Background(), deps(t), true, "https://hub.example.com"))
	waitFor(t, tm, "Looks right")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Let the form move focus to the token field before typing into it.
	time.Sleep(500 * time.Millisecond)
	tm.Type("cch_llm_live_ABC_secret")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	waitFor(t, tm, "LLM token")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEscape})
	out := plainView(t, tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second)))
	if strings.Contains(string(out), "cch_llm_live_ABC_secret") {
		t.Fatal("the token was shown on screen")
	}
	teatest.RequireEqualOutput(t, out)
}

func TestLabelsAndValues(t *testing.T) {
	cases := map[string]string{
		"created_at": "Created", "lastUsedAt": "Last used at", "website_url": "Website",
		"apiKeyId": "API key ID", "durationMs": "Duration", "cost_multiplier_ppm": "Cost multiplier",
	}
	for in, want := range cases {
		if got := label(in); got != want {
			t.Errorf("label(%q) = %q, want %q", in, got, want)
		}
	}
	if got := groupDigits("1234567"); got != "1,234,567" {
		t.Errorf("groupDigits = %q", got)
	}
}
