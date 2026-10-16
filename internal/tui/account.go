package tui

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/CCH-HQ/cchp-cli/internal/auth"
	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

type accountScreen struct {
	wallet     dashboardData
	generation int
	id         string
	vp         viewport.Model
}

func newAccountScreen() *accountScreen { return &accountScreen{id: rand.Text(), vp: viewport.New()} }

func (s *accountScreen) key() string { return fmt.Sprintf("account/%s/%d", s.id, s.generation) }

func (s *accountScreen) init(m *Model) tea.Cmd {
	s.generation++
	s.wallet = dashboardData{loading: true}
	s.resize(m)
	return m.fetch(s, s.key(), "getWalletOverview", nil)
}

func (s *accountScreen) resize(m *Model) {
	s.vp.SetWidth(maxInt(m.width-4, 12))
	s.vp.SetHeight(dashboardHeight(m, s.hints()))
}

func (s *accountScreen) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.resize(m)
	case fetchedMsg:
		if msg.key == s.key() {
			s.wallet.acceptWallet(msg)
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q", "backspace":
			return pop
		case "r":
			return s.init(m)
		}
	}
	s.vp.SetContent(s.content(m))
	var cmd tea.Cmd
	s.vp, cmd = s.vp.Update(msg)
	return cmd
}

func (s *accountScreen) view(m *Model) string {
	s.vp.SetContent(s.content(m))
	return "  " + strings.ReplaceAll(s.vp.View(), "\n", "\n  ")
}

func (s *accountScreen) content(m *Model) string {
	var b strings.Builder
	b.WriteString(ui.Title.Render("My account") + "\n")
	if m.identity != nil {
		b.WriteString(auth.DisplayName(*m.identity) + "\n")
	}
	if status := s.wallet.status(m); status != "" {
		return b.String() + "\n" + ui.Wrap(status, s.vp.Width(), 0)
	}
	b.WriteString("\n" + ui.Muted.Render("Wallet balance") + "\n" + ui.Title.Render(cost(s.wallet.doc["balanceUnits"])) + "\n\n")
	b.WriteString(ui.Title.Render("My packages") + "\n")
	packages, _ := s.wallet.doc["packages"].([]any)
	if len(packages) == 0 {
		b.WriteString(ui.Muted.Render("No packages. Your wallet balance is separate from package allowances."))
	}
	for _, item := range packages {
		p, ok := item.(map[string]any)
		if !ok {
			continue
		}
		b.WriteString("\n" + ui.Title.Render(str(p, "name")) + ui.Muted.Render(" · "+str(p, "status")) + "\n")
		switch str(p, "kind") {
		case "usage":
			b.WriteString("Usage package · " + cost(p["remainingUnits"]) + " remaining")
			if _, ok := p["totalUnits"]; ok {
				b.WriteString(" / " + cost(p["totalUnits"]) + " total")
			}
		case "time":
			period := map[string]string{"day": "Daily", "week": "Weekly", "month": "Monthly"}[str(p, "period")]
			if period == "" {
				period = str(p, "period")
			}
			b.WriteString(period + " package · " + cost(p["periodQuotaUnits"]) + " per period")
			if current, ok := p["currentPeriod"].(map[string]any); ok {
				b.WriteString("\n" + cost(current["usedUnits"]) + " used · " + cost(current["remainingUnits"]) + " remaining")
				b.WriteString("\n" + allowanceBar(current["usedUnits"], current["remainingUnits"], minInt(s.vp.Width(), 30)))
				b.WriteString("\nCurrent period: " + accountTime(str(current, "startsAt")) + " → " + accountTime(str(current, "endsAt")))
			} else if str(p, "status") == "active" {
				b.WriteString("\n" + ui.Muted.Render("Current period usage unavailable."))
			}
		default:
			b.WriteString("Package type: " + str(p, "kind"))
		}
		if expiry := str(p, "expiresAt"); expiry != "" {
			b.WriteString("\nExpires: " + accountTime(expiry))
		}
		b.WriteByte('\n')
	}
	return ui.Wrap(b.String(), s.vp.Width(), 0)
}

func (s *accountScreen) hints() string {
	return hints("↑/↓", "scroll", "r", "refresh", "esc", "back")
}

func packageSummary(doc map[string]any) string {
	items, _ := doc["packages"].([]any)
	var names []string
	for _, item := range items {
		p, _ := item.(map[string]any)
		if str(p, "status") == "active" {
			names = append(names, str(p, "name"))
		}
	}
	if len(names) == 0 {
		return "No active packages"
	}
	return strings.Join(names, ", ")
}

func accountTime(value string) string {
	if stamp, err := time.Parse(time.RFC3339, value); err == nil {
		return stamp.UTC().Format("2006-01-02 15:04 UTC")
	}
	return value
}

func allowanceBar(used, remaining any, width int) string {
	u, ok1 := used.(json.Number)
	r, ok2 := remaining.(json.Number)
	uf, err1 := u.Float64()
	rf, err2 := r.Float64()
	if !ok1 || !ok2 || err1 != nil || err2 != nil || uf < 0 || rf < 0 || uf+rf == 0 {
		return ""
	}
	filled := minInt(width, maxInt(0, int(uf/(uf+rf)*float64(width))))
	return lipgloss.NewStyle().Foreground(ui.Accent).Render(strings.Repeat("█", filled)) + ui.Muted.Render(strings.Repeat("░", width-filled))
}
