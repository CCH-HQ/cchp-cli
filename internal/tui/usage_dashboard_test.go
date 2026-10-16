package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/CCH-HQ/cchp-cli/internal/auth"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

const walletFixture = `{"balanceUnits":1255000000000,"packages":[
 {"id":"monthly","name":"Pro monthly","kind":"time","status":"active","period":"month","periodQuotaUnits":1000000000000,"expiresAt":"2026-11-01T00:00:00Z","currentPeriod":{"startsAt":"2026-10-01T00:00:00Z","endsAt":"2026-11-01T00:00:00Z","usedUnits":350000000000,"remainingUnits":650000000000}},
 {"id":"extra","name":"Extra credits","kind":"usage","status":"active","totalUnits":500000000000,"remainingUnits":200000000000},
 {"id":"old","name":"Old starter","kind":"usage","status":"expired","totalUnits":100000000000,"remainingUnits":0,"expiresAt":"2026-09-01T00:00:00Z"}]}`

const summaryFixture = `{"today":{"requests":1280,"inputTokens":2560000,"outputTokens":184000,"costUnits":125000000000,"successRate":0.996},"week":{"requests":8240,"inputTokens":16200000,"outputTokens":1240000,"costUnits":790000000000,"successRate":0.994},"month":{"requests":32910,"inputTokens":62000000,"outputTokens":5810000,"costUnits":3010000000000,"successRate":0.992}}`

const modelsFixture = `{"items":[{"requestedModel":"claude-sonnet-4","finalModel":"claude-sonnet-4","requests":6120,"inputTokens":12000000,"outputTokens":800000,"costUnits":550000000000},{"requestedModel":"fast-model","finalModel":"gpt-5-mini","requests":2120,"inputTokens":4200000,"outputTokens":440000,"costUnits":240000000000}]}`

func dashboardDoc(t *testing.T, value string) map[string]any {
	t.Helper()
	var doc map[string]any
	dec := json.NewDecoder(strings.NewReader(value))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func dashboardFixture(t *testing.T, width, height int) (*Model, *usageScreen) {
	t.Helper()
	m := NewModel(context.Background(), deps(t), false, "")
	m.width, m.height, m.signedIn, m.server = width, height, true, "https://hub.example.com"
	m.identity = &auth.Identity{Name: "Dana", OrganizationName: "Acme"}
	s := newUsageScreen()
	s.now = func() time.Time { return time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC) }
	m.stack = []screen{s}
	s.init(m)
	for part, fixture := range map[string]string{"summary": summaryFixture, "wallet": walletFixture, "models": modelsFixture} {
		s.update(m, fetchedMsg{key: s.key(part), doc: dashboardDoc(t, fixture)})
	}
	var buckets []any
	for i := range 365 {
		if i%7 == 0 || i%11 == 0 {
			continue
		}
		buckets = append(buckets, usageBucket(s.start.AddDate(0, 0, i), int64(i%19+1)*30))
	}
	today := usageBucket(s.start.AddDate(0, 0, 364), 1280)
	today["inputTokens"], today["outputTokens"], today["costUnits"] = json.Number("2560000"), json.Number("184000"), json.Number("125000000000")
	buckets = append(buckets, today)
	s.update(m, fetchedMsg{key: s.key("activity"), doc: map[string]any{"buckets": buckets}})
	return m, s
}

func usageBucket(stamp time.Time, requests int64) map[string]any {
	return map[string]any{
		"bucket": stamp.Format(time.RFC3339), "requests": json.Number(fmt.Sprint(requests)),
		"inputTokens": json.Number("2000"), "outputTokens": json.Number("1000"), "costUnits": json.Number("10000000000"),
	}
}

func TestUsageDashboard(t *testing.T) {
	m, _ := dashboardFixture(t, 100, 34)
	teatest.RequireEqualOutput(t, plainView(t, m))
}

func TestMyAccount(t *testing.T) {
	m, _ := dashboardFixture(t, 90, 40)
	s := newAccountScreen()
	m.stack = []screen{s}
	s.init(m)
	s.update(m, fetchedMsg{key: s.key(), doc: dashboardDoc(t, walletFixture)})
	teatest.RequireEqualOutput(t, plainView(t, m))
}

func TestUsageCalendarFillsGapsAndUsesUTC(t *testing.T) {
	start := time.Date(2024, 2, 28, 0, 0, 0, 0, time.UTC)
	buckets := []any{
		usageBucket(start.Add(-24*time.Hour), 999),
		usageBucket(start.AddDate(0, 0, 1), 10),
		usageBucket(time.Date(2024, 3, 1, 2, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)), 20),
		usageBucket(start.AddDate(0, 0, 4), 999),
	}
	days, err := usageDays(map[string]any{"buckets": buckets}, start, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 3 || days[0].requests != 0 || days[1].date.Format("2006-01-02") != "2024-02-29" || days[1].requests != 30 || days[1].inputTokens != 4000 || days[2].requests != 0 {
		t.Fatalf("incorrect sparse calendar: %+v", days)
	}
}

func TestUsageCalendarRejectsMalformedAndOverflowingBuckets(t *testing.T) {
	start := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	for _, field := range []string{"bucket", "requests", "inputTokens", "outputTokens", "costUnits"} {
		bucket := usageBucket(start, 1)
		delete(bucket, field)
		if _, err := usageDays(map[string]any{"buckets": []any{bucket}}, start, 1); err == nil {
			t.Errorf("missing %s accepted", field)
		}
	}
	for _, invalid := range []any{json.Number("-1"), json.Number("1.5"), json.Number("9223372036854775808"), "100"} {
		bucket := usageBucket(start, 1)
		bucket["requests"] = invalid
		if _, err := usageDays(map[string]any{"buckets": []any{bucket}}, start, 1); err == nil {
			t.Errorf("invalid request count %v accepted", invalid)
		}
	}
	bucket := usageBucket(start, 1)
	bucket["inputTokens"] = json.Number("9223372036854775807")
	if _, err := usageDays(map[string]any{"buckets": []any{bucket}}, start, 1); err == nil {
		t.Fatal("overflowing token sum accepted")
	}
	if _, err := usageDays(map[string]any{}, start, 1); err == nil {
		t.Fatal("missing data became an empty calendar")
	}
}

func TestHeatmapQuartilesKeepOutliersDistinct(t *testing.T) {
	days := []usageDay{{requests: 0}, {requests: 1}, {requests: 2}, {requests: 3}, {requests: 4}, {requests: 1000000}}
	if got := fmt.Sprint(intensityLevels(days, 0)); got != "[0 1 1 2 3 4]" {
		t.Fatalf("quartiles = %s", got)
	}
	if got := fmt.Sprint(intensityLevels(days, 2)); got != "[0 0 0 0 0 0]" {
		t.Fatalf("empty metric = %s", got)
	}
}

func TestDashboardNavigationAndResize(t *testing.T) {
	m, s := dashboardFixture(t, 80, 24)
	s.view(m)
	initialWeek := s.firstWeek
	s.update(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if s.cursor != 357 {
		t.Fatalf("left moved to %d", s.cursor)
	}
	s.update(m, tea.KeyPressMsg{Code: tea.KeyHome})
	s.view(m)
	if s.cursor != 0 || s.firstWeek != 0 || initialWeek == 0 {
		t.Fatalf("calendar did not follow selection: cursor %d, first week %d", s.cursor, s.firstWeek)
	}
	s.update(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if s.cursor != 0 {
		t.Fatal("cursor left calendar range")
	}
	s.update(m, tea.KeyPressMsg{Code: tea.KeyEnd})
	s.update(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if s.cursor != 364 {
		t.Fatal("cursor moved into a future day")
	}
	s.update(m, tea.KeyPressMsg{Code: tea.KeyTab})
	s.update(m, tea.KeyPressMsg{Code: '3', Text: "3"})
	if s.metric != 1 || s.period != 2 || !strings.Contains(s.content(m), "This month") {
		t.Fatal("metric or period switching failed")
	}
	for _, width := range []int{24, 40, 80, 120, 160} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		view := ansi.Strip(m.View().Content)
		for line := range strings.SplitSeq(view, "\n") {
			if lipgloss.Width(line) > width {
				t.Errorf("width %d overflowed with %q", width, line)
			}
		}
		if lipgloss.Height(view) > 30 {
			t.Errorf("width %d: view height %d exceeds terminal", width, lipgloss.Height(view))
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	s.update(m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	if s.vp.YOffset() == 0 {
		t.Fatal("dashboard cannot scroll to model rankings")
	}
}

func TestDashboardRefreshIgnoresOldAndOtherScreenResponses(t *testing.T) {
	m, s := dashboardFixture(t, 100, 30)
	oldKey := s.key("wallet")
	s.init(m)
	s.update(m, fetchedMsg{key: oldKey, doc: dashboardDoc(t, walletFixture)})
	if !s.wallet.loading || s.wallet.doc != nil {
		t.Fatal("old response overwrote a refresh")
	}
	other := newUsageScreen()
	other.init(m)
	if s.key("wallet") == other.key("wallet") {
		t.Fatal("two screen instances share response keys")
	}
	oldAccount := newAccountScreen()
	newAccount := newAccountScreen()
	oldAccount.init(m)
	newAccount.init(m)
	newAccount.update(m, fetchedMsg{key: oldAccount.key(), doc: dashboardDoc(t, walletFixture)})
	if !newAccount.wallet.loading {
		t.Fatal("a previous account screen populated the new screen")
	}
}

func TestDashboardPermissionFailuresDoNotEraseOtherWidgets(t *testing.T) {
	m, s := dashboardFixture(t, 100, 40)
	s.update(m, fetchedMsg{key: s.key("wallet"), err: contract.Fail(contract.KindPermission, contract.ExitPermission, "Wallet access is unavailable.")})
	view := ansi.Strip(s.content(m))
	for _, text := range []string{"Wallet access is unavailable", "1.3K", "Activity", "claude-sonnet-4"} {
		if !strings.Contains(view, text) {
			t.Errorf("missing %q after independent failure", text)
		}
	}
	s.update(m, fetchedMsg{key: s.key("activity"), err: fmt.Errorf("daily service unavailable")})
	view = ansi.Strip(s.content(m))
	if strings.Contains(view, "active days") || !strings.Contains(view, "daily service unavailable") {
		t.Fatal("failed activity read became a zero-activity calendar")
	}
	s.update(m, fetchedMsg{key: s.key("wallet"), doc: map[string]any{}})
	if s.wallet.err == nil {
		t.Fatal("malformed wallet became an empty balance and no packages")
	}
}

func TestDashboardReceivesBackgroundReadsWhileAccountIsOpen(t *testing.T) {
	m, usage := dashboardFixture(t, 100, 30)
	usage.init(m)
	account := newAccountScreen()
	m.Update(pushMsg{s: account})
	m.Update(fetchedMsg{owner: usage, key: usage.key("wallet"), doc: dashboardDoc(t, walletFixture)})
	if usage.wallet.loading || usage.wallet.doc == nil || !account.wallet.loading {
		t.Fatal("background read was lost or delivered to the wrong screen")
	}
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	m.Update(popMsg{})
	if m.top() != usage || !strings.Contains(usage.content(m), "125.5000 credits") {
		t.Fatal("returning to usage lost the completed wallet read")
	}
	if usage.vp.Width() != 36 || lipgloss.Width(ansi.Strip(m.View().Content)) > 40 || lipgloss.Height(ansi.Strip(m.View().Content)) > 20 {
		t.Fatal("returning to usage lost a terminal resize while the account page was open")
	}
	// A read completing after its originating screen was closed is ignored.
	m.Update(fetchedMsg{owner: account, key: account.key(), doc: dashboardDoc(t, walletFixture)})
	if !account.wallet.loading {
		t.Fatal("a detached screen consumed a late read")
	}
}

func TestAccountPackageBalancesAndEmptyState(t *testing.T) {
	m, _ := dashboardFixture(t, 100, 40)
	s := newAccountScreen()
	s.init(m)
	s.update(m, fetchedMsg{key: s.key(), doc: dashboardDoc(t, walletFixture)})
	content := ansi.Strip(s.content(m))
	for _, want := range []string{"125.5000 credits", "Pro monthly", "35.0000 credits used", "65.0000 credits remaining", "Monthly package", "20.0000 credits remaining", "Old starter · expired", "2026-11-01 00:00 UTC"} {
		if !strings.Contains(content, want) {
			t.Errorf("account is missing %q", want)
		}
	}
	if got := packageSummary(dashboardDoc(t, walletFixture)); got != "Pro monthly, Extra credits" {
		t.Fatalf("inactive package appeared in summary: %s", got)
	}
	s.update(m, fetchedMsg{key: s.key(), doc: dashboardDoc(t, `{"balanceUnits":0,"packages":[]}`)})
	if view := ansi.Strip(s.content(m)); !strings.Contains(view, "No packages.") || !strings.Contains(view, "0 credits") {
		t.Fatal("empty account state is unclear")
	}
}

func TestDashboardFetchesReadOnlyEndpointsThroughRunner(t *testing.T) {
	d := deps(t)
	var mu sync.Mutex
	seen := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("dashboard attempted a mutation: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer cch_auth_live_dashboard_fixture" {
			t.Error("dashboard did not use the runner credential")
		}
		mu.Lock()
		seen[r.URL.Path]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/account/usage/summary":
			if r.URL.Query().Get("timezone") != "UTC" {
				t.Error("summary calendar uses a different timezone")
			}
			fmt.Fprint(w, summaryFixture)
		case "/api/account/usage/by-model":
			fmt.Fprint(w, modelsFixture)
		case "/api/account/usage/timeseries":
			query := r.URL.Query()
			if query.Get("granularity") != "day" || (query.Get("groupBy") != "" && query.Get("groupBy") != "none") || query.Get("timezone") != "UTC" || query.Get("start") != "2025-10-17T00:00:00Z" {
				t.Errorf("incorrect calendar request: %v", query)
			}
			fmt.Fprint(w, `{"buckets":[{"bucket":"2026-10-16T00:00:00Z","requests":1280,"inputTokens":2560000,"outputTokens":184000,"costUnits":125000000000}]}`)
		case "/api/account/wallet":
			fmt.Fprint(w, walletFixture)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	id := &auth.Identity{UserID: "dana", OrganizationID: "acme"}
	d.Runner.UseCredential(&auth.Credential{Server: server.URL, Token: "cch_auth_live_dashboard_fixture", Identity: id})
	m := NewModel(context.Background(), d, false, "")
	s := newUsageScreen()
	s.now = func() time.Time { return time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC) }
	batch, ok := s.init(m)().(tea.BatchMsg)
	if !ok || len(batch) != 4 {
		t.Fatal("dashboard did not start four independent reads")
	}
	for _, cmd := range batch {
		msg, ok := cmd().(fetchedMsg)
		if !ok || msg.err != nil {
			t.Fatalf("dashboard read failed: %+v", msg)
		}
		s.update(m, msg)
	}
	if len(s.days) != 365 || s.days[364].requests != 1280 || s.wallet.err != nil || s.wallet.doc == nil {
		t.Fatal("server responses did not populate the dashboard")
	}
	account := newAccountScreen()
	account.update(m, account.init(m)())
	if account.wallet.err != nil || account.wallet.doc == nil {
		t.Fatalf("account read failed: %v", account.wallet.err)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen["/api/account/wallet"] != 2 || len(seen) != 4 {
		t.Fatalf("unexpected read surface: %v", seen)
	}
}
