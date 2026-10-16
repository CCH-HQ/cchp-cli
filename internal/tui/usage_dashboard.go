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

	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

// dashboardData keeps independently loaded widgets useful when another read fails.
type dashboardData struct {
	doc     map[string]any
	loading bool
	err     error
}

func (d *dashboardData) accept(msg fetchedMsg) {
	d.loading, d.err = false, msg.err
	if msg.err == nil {
		var ok bool
		d.doc, ok = msg.doc.(map[string]any)
		if !ok {
			d.err = fmt.Errorf("the server returned an invalid response")
		}
	}
}

func (d *dashboardData) acceptWallet(msg fetchedMsg) {
	d.accept(msg)
	if d.err != nil {
		return
	}
	balance, validBalance := d.doc["balanceUnits"].(json.Number)
	_, balanceErr := balance.Int64()
	_, validPackages := d.doc["packages"].([]any)
	if !validBalance || balanceErr != nil || !validPackages {
		d.err = fmt.Errorf("the server returned an invalid wallet response")
	}
}

func (d dashboardData) status(m *Model) string {
	if d.loading {
		return m.spin.View() + " Loading…"
	}
	if d.err != nil {
		return plainError(d.err)
	}
	return ""
}

type usageScreen struct {
	summary, models, activity, wallet dashboardData
	days                              []usageDay
	metric                            int
	period                            int
	cursor, firstWeek                 int
	generation                        int
	id                                string
	start                             time.Time
	now                               func() time.Time
	vp                                viewport.Model
}

func newUsageScreen() *usageScreen {
	return &usageScreen{id: rand.Text(), now: time.Now, vp: viewport.New()}
}

func (s *usageScreen) key(part string) string {
	return fmt.Sprintf("usage/%s/%d/%s", s.id, s.generation, part)
}

func (s *usageScreen) init(m *Model) tea.Cmd {
	s.generation++
	s.summary, s.models = dashboardData{loading: true}, dashboardData{loading: true}
	s.activity, s.wallet = dashboardData{loading: true}, dashboardData{loading: true}
	now := s.now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	s.start = today.AddDate(0, 0, -364)
	s.days, s.cursor, s.firstWeek = nil, 364, 0
	s.resize(m)
	s.vp.GotoTop()
	return tea.Batch(
		m.fetch(s, s.key("summary"), "getUsageSummary", map[string][]string{"timezone": {"UTC"}}),
		m.fetch(s, s.key("models"), "listUsageByModel", map[string][]string{
			"start": {today.AddDate(0, 0, -6).Format(time.RFC3339)}, "end": {now.Format(time.RFC3339)}, "limit": {"8"},
		}),
		m.fetch(s, s.key("activity"), "getUsageTimeseries", map[string][]string{
			"start": {s.start.Format(time.RFC3339)}, "end": {now.Format(time.RFC3339)},
			"granularity": {"day"}, "groupBy": {"none"}, "timezone": {"UTC"},
		}),
		m.fetch(s, s.key("wallet"), "getWalletOverview", nil),
	)
}

func (s *usageScreen) resize(m *Model) {
	s.vp.SetWidth(maxInt(m.width-4, 12))
	s.vp.SetHeight(dashboardHeight(m, s.hints()))
}

func (s *usageScreen) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.resize(m)
	case fetchedMsg:
		switch msg.key {
		case s.key("summary"):
			s.summary.accept(msg)
		case s.key("models"):
			s.models.accept(msg)
		case s.key("wallet"):
			s.wallet.acceptWallet(msg)
		case s.key("activity"):
			s.activity.accept(msg)
			if s.activity.err == nil {
				s.days, s.activity.err = usageDays(s.activity.doc, s.start, 365)
			}
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q", "backspace":
			return pop
		case "r":
			return s.init(m)
		case "a":
			return push(newAccountScreen())
		case "1", "2", "3":
			s.period = int(msg.String()[0] - '1')
			s.vp.GotoTop()
		case "tab", "shift+tab":
			step := 1
			if msg.String() == "shift+tab" {
				step = 2
			}
			s.metric = (s.metric + step) % 3
			s.vp.GotoTop()
		case "left", "right", "up", "down", "home", "end":
			if len(s.days) > 0 {
				s.vp.GotoTop()
				delta := map[string]int{"left": -7, "right": 7, "up": -1, "down": 1}[msg.String()]
				s.cursor = maxInt(0, minInt(s.cursor+delta, len(s.days)-1))
				if msg.String() == "home" {
					s.cursor = 0
				} else if msg.String() == "end" {
					s.cursor = len(s.days) - 1
				}
			}
		default:
			s.vp.SetContent(s.content(m))
			var cmd tea.Cmd
			s.vp, cmd = s.vp.Update(msg)
			return cmd
		}
	default:
		s.vp.SetContent(s.content(m))
		var cmd tea.Cmd
		s.vp, cmd = s.vp.Update(msg)
		return cmd
	}
	return nil
}

func (s *usageScreen) view(m *Model) string {
	s.vp.SetContent(s.content(m))
	return "  " + strings.ReplaceAll(s.vp.View(), "\n", "\n  ")
}

func (s *usageScreen) content(m *Model) string {
	width := s.vp.Width()
	var b strings.Builder
	periods := []string{"Today", "This week", "This month"}
	b.WriteString(ui.Title.Render("Usage dashboard") + ui.Muted.Render(" · "+periods[s.period]+" · UTC") + "\n")
	if status := s.wallet.status(m); status != "" {
		b.WriteString(ui.Muted.Render("My wallet: ") + status)
	} else {
		b.WriteString("My balance " + ui.Title.Render(cost(s.wallet.doc["balanceUnits"])) + ui.Muted.Render(" · "+packageSummary(s.wallet.doc)+" · a account"))
	}
	b.WriteString("\n\n")
	window, _ := s.summary.doc[[]string{"today", "week", "month"}[s.period]].(map[string]any)
	values := []string{
		compactCount(window["requests"]),
		compactCount(window["inputTokens"]) + " / " + compactCount(window["outputTokens"]),
		cardCredits(window["costUnits"]), percent(window["successRate"]),
	}
	titles := []string{"Requests", "Tokens in / out", "Cost (credits)", "Success rate"}
	columns := 4
	if width < 72 {
		columns = 2
	}
	if width < 36 {
		columns = 1
	}
	cardWidth := (width - columns + 1) / columns
	for i := 0; i < 4; i += columns {
		var cards []string
		for j := i; j < i+columns; j++ {
			card := ui.Muted.Render(titles[j]) + "\n" + ui.Title.Render(values[j])
			cards = append(cards, lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(ui.Accent).PaddingLeft(1).Width(cardWidth-1).Render(card))
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, intersperse(cards, " ")...))
		b.WriteString("\n")
	}
	if status := s.summary.status(m); status != "" {
		b.WriteString(status + "\n")
	}
	b.WriteString("\n" + ui.Title.Render("Activity") + ui.Muted.Render(" · "+usageMetrics[s.metric]+" · 365 days · UTC") + "\n")
	if status := s.activity.status(m); status != "" {
		b.WriteString(status)
	} else if len(s.days) > 0 {
		b.WriteString(s.heatmap(width))
	}
	b.WriteString("\n\n" + ui.Title.Render("Busiest models · last 7 days") + "\n")
	if status := s.models.status(m); status != "" {
		b.WriteString(status)
	} else {
		items, _ := s.models.doc["items"].([]any)
		if len(items) == 0 {
			b.WriteString(ui.Muted.Render("No requests in the last 7 days."))
		} else {
			cols := [][]string{{"Model"}, {"Requests"}, {"Tokens"}, {"Cost"}}
			for _, item := range items {
				r, _ := item.(map[string]any)
				model := str(r, "requestedModel")
				if model == "" {
					model = str(r, "finalModel")
				} else if str(r, "finalModel") == "" {
					model += " (not sent to a provider)"
				} else if final := str(r, "finalModel"); final != "" && final != model {
					model += " → " + final
				}
				cols[0] = append(cols[0], model)
				cols[1] = append(cols[1], num(r["requests"]))
				cols[2] = append(cols[2], compactCount(sum(r["inputTokens"], r["outputTokens"])))
				cols[3] = append(cols[3], cost(r["costUnits"]))
			}
			b.WriteString(strings.ReplaceAll(strings.TrimPrefix(table(cols, false), "  "), "\n  ", "\n"))
		}
	}
	return ui.Wrap(b.String(), width, 0)
}

func (s *usageScreen) hints() string {
	return "←/→ week  ↑/↓ day  tab metric  1–3 period  pgup/dn scroll  a account  r reload  esc back"
}

func intersperse(parts []string, separator string) []string {
	var result []string
	for i, part := range parts {
		if i > 0 {
			result = append(result, separator)
		}
		result = append(result, part)
	}
	return result
}

func dashboardHeight(m *Model, keyHints string) int {
	footer := ui.Wrap(keyHints, maxInt(m.width-4, 12), 0)
	return maxInt(m.height-lipgloss.Height(m.header())-lipgloss.Height(footer)-4, 1)
}

func compactCount(value any) string {
	n, ok := value.(json.Number)
	if !ok {
		return "—"
	}
	f, err := n.Float64()
	if err != nil {
		return "—"
	}
	for _, unit := range []struct {
		size float64
		name string
	}{{1e12, "T"}, {1e9, "B"}, {1e6, "M"}, {1e3, "K"}} {
		if f >= unit.size || f <= -unit.size {
			return fmt.Sprintf("%.1f%s", f/unit.size, unit.name)
		}
	}
	return num(n)
}

func cardCredits(value any) string {
	n, ok := value.(json.Number)
	if !ok {
		return "—"
	}
	f, err := n.Float64()
	if err != nil {
		return "—"
	}
	f /= 1e10
	if f > 0 && f < 0.0001 {
		return "<0.0001"
	}
	return fmt.Sprintf("%.4f", f)
}
