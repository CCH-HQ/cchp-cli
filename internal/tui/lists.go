package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

type row map[string]any

// entry is one list line; the whole server record travels with it.
type entry struct {
	title, desc string
	data        row
}

func (e entry) Title() string       { return e.title }
func (e entry) Description() string { return e.desc }
func (e entry) FilterValue() string { return e.title + " " + e.desc }

// listSpec describes a searchable list backed by one read operation.
type listSpec struct {
	title     string
	opID      string
	params    map[string][]string
	itemsPath string
	empty     string
	toEntry   func(row) entry
	// open is what enter does; nil shows the record's details.
	open func(row) screen
	// fields orders the detail view.
	fields []string
}

type listScreen struct {
	spec    listSpec
	list    list.Model
	loading bool
	err     error
	key     string
}

func newListScreen(spec listSpec) *listScreen {
	l := list.New(nil, itemDelegate(), 80, 16)
	l.Title = spec.title
	l.Styles = listStyles()
	l.SetShowHelp(false)
	l.SetStatusBarItemName("item", "items")
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()
	return &listScreen{spec: spec, list: l, loading: true, key: spec.opID + fmt.Sprint(spec.params)}
}

func (s *listScreen) init(m *Model) tea.Cmd {
	s.resize(m)
	return m.fetch(s, s.key, s.spec.opID, s.spec.params)
}

func (s *listScreen) resize(m *Model) {
	h := max(m.height-7, 6)
	s.list.SetSize(m.width, h)
}

func (s *listScreen) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.resize(m)
	case fetchedMsg:
		if msg.key != s.key {
			return nil
		}
		s.loading = false
		if msg.err != nil {
			s.err = msg.err
			return nil
		}
		var items []list.Item
		obj, _ := msg.doc.(map[string]any)
		raw, _ := lookupPath(obj, s.spec.itemsPath).([]any)
		for _, it := range raw {
			if r, ok := it.(map[string]any); ok {
				items = append(items, s.spec.toEntry(r))
			}
		}
		return s.list.SetItems(items)
	case tea.KeyPressMsg:
		filtering := s.list.FilterState() == list.Filtering
		switch msg.String() {
		case "esc":
			if !filtering && s.list.FilterState() != list.FilterApplied {
				return pop
			}
		case "q":
			if !filtering {
				return pop
			}
		case "r":
			if !filtering {
				s.loading, s.err = true, nil
				return m.fetch(s, s.key, s.spec.opID, s.spec.params)
			}
		case "enter":
			if !filtering {
				if e, ok := s.list.SelectedItem().(entry); ok {
					if s.spec.open != nil {
						return push(s.spec.open(e.data))
					}
					return push(newDetail(e.title, e.data, s.spec.fields))
				}
				return nil
			}
		}
	}
	var cmd tea.Cmd
	s.list, cmd = s.list.Update(msg)
	return cmd
}

func (s *listScreen) view(m *Model) string {
	switch {
	case s.loading:
		return "  " + ui.Title.Render(s.spec.title) + "\n\n  " + m.spin.View() + " Loading…"
	case s.err != nil:
		return "  " + ui.Title.Render(s.spec.title) + "\n\n  " + strings.ReplaceAll(plainError(s.err), "\n", "\n  ")
	case len(s.list.Items()) == 0:
		return "  " + ui.Title.Render(s.spec.title) + "\n\n  " + ui.Muted.Render(s.spec.empty)
	}
	return s.list.View()
}

func (s *listScreen) hints() string {
	return hints("↑/↓", "move", "/", "search", "enter", "open", "r", "refresh", "esc", "back")
}

// detailScreen shows one record as aligned label/value lines: the useful
// fields first, every field with "a".
type detailScreen struct {
	title string
	data  row
	order []string
	all   bool
	vp    viewport.Model
}

func newDetail(title string, data row, order []string) *detailScreen {
	return &detailScreen{title: title, data: data, order: order, all: len(order) == 0}
}

func (s *detailScreen) init(m *Model) tea.Cmd {
	s.vp = viewport.New(viewport.WithWidth(m.width), viewport.WithHeight(maxInt(m.height-7, 6)))
	s.vp.SetContent(s.content())
	return nil
}

func (s *detailScreen) content() string {
	keys := make([]string, 0, len(s.data))
	seen := map[string]bool{}
	for _, k := range s.order {
		if _, ok := s.data[k]; ok {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	if s.all {
		var rest []string
		for k := range s.data {
			if !seen[k] && k != "$schema" {
				rest = append(rest, k)
			}
		}
		sort.Strings(rest)
		keys = append(keys, rest...)
	}
	width := 0
	for _, k := range keys {
		if l := len(label(k)); l > width {
			width = l
		}
	}
	var b strings.Builder
	b.WriteString("  " + ui.Title.Render(s.title) + "\n\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s  %s\n", ui.Muted.Render(fmt.Sprintf("%-*s", width, label(k))), formatValue(k, s.data[k]))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (s *detailScreen) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.vp.SetWidth(m.width)
		s.vp.SetHeight(maxInt(m.height-7, 6))
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q", "backspace":
			return pop
		case "a":
			if len(s.order) > 0 {
				s.all = !s.all
				s.vp.SetContent(s.content())
				s.vp.GotoTop()
			}
			return nil
		}
	}
	var cmd tea.Cmd
	s.vp, cmd = s.vp.Update(msg)
	return cmd
}

func (s *detailScreen) view(m *Model) string { return s.vp.View() }

func (s *detailScreen) hints() string {
	if len(s.order) == 0 {
		return hints("↑/↓", "scroll", "esc", "back")
	}
	if s.all {
		return hints("↑/↓", "scroll", "a", "fewer fields", "esc", "back")
	}
	return hints("↑/↓", "scroll", "a", "all fields", "esc", "back")
}

// labels names the fields people see most.
var labels = map[string]string{
	"startedAt": "Started", "httpStatus": "HTTP status", "requestedModel": "Requested model",
	"finalModel": "Model used", "providerName": "Provider", "userEmail": "User", "keyName": "LLM token",
	"inputTokens": "Tokens in", "outputTokens": "Tokens out", "costUnits": "Cost", "durationMs": "Duration",
	"ttftMs": "First token after", "errorCategory": "Error", "requestId": "Request ID",
	"website_url": "Website", "cost_multiplier_ppm": "Cost multiplier", "created_at": "Created",
	"updated_at": "Updated", "secret_prefix": "Secret starts with", "secret_last_four": "Secret ends with",
	"credential_type": "Credential type", "routeStrategy": "Routing", "allowedModels": "Allowed models",
	"expires_at": "Expires", "last_used_at": "Last used", "id": "ID",
}

var acronyms = map[string]string{"id": "ID", "url": "URL", "http": "HTTP", "api": "API", "llm": "LLM", "ip": "IP", "tps": "TPS", "sip": "SIP"}

// label turns field names such as created_at or lastUsedAt into "Created at".
func label(k string) string {
	if l, ok := labels[k]; ok {
		return l
	}
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = nil
		}
	}
	runes := []rune(k)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-':
			flush()
		case r >= 'A' && r <= 'Z' && i > 0 && runes[i-1] >= 'a' && runes[i-1] <= 'z':
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	var kept []string
	for _, w := range words {
		switch w {
		case "ppm", "units", "ms":
			continue
		}
		if a, ok := acronyms[w]; ok {
			w = a
		}
		kept = append(kept, w)
	}
	if len(kept) == 0 {
		return k
	}
	s := strings.Join(kept, " ")
	return strings.ToUpper(s[:1]) + s[1:]
}

// formatValue renders a field value in plain words.
func formatValue(k string, v any) string {
	switch t := v.(type) {
	case nil:
		return ui.Muted.Render("—")
	case bool:
		if t {
			return "yes"
		}
		return "no"
	case string:
		if t == "" || t == "NONE" {
			return ui.Muted.Render("—")
		}
		if ts, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return ts.Local().Format("2 Jan 2006 15:04")
		}
		return t
	case json.Number:
		switch {
		case strings.HasSuffix(k, "Ppm") || strings.HasSuffix(k, "_ppm"):
			n, err := t.Float64()
			if err == nil {
				return fmt.Sprintf("%.2f×", n/1e6)
			}
		case strings.HasSuffix(k, "Units") || strings.HasSuffix(k, "_units"):
			return credits(t)
		case strings.HasSuffix(k, "Ms") || strings.HasSuffix(k, "_ms"):
			return groupDigits(t.String()) + " ms"
		}
		return groupDigits(t.String())
	case []any:
		if len(t) == 0 {
			return ui.Muted.Render("none")
		}
		parts := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				parts = append(parts, s)
				continue
			}
			data, err := json.Marshal(x)
			if err != nil {
				return fmt.Sprint(x)
			}
			parts = append(parts, string(data))
		}
		return strings.Join(parts, ", ")
	default:
		data, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(data)
	}
}

// credits shows CCHP credit units (10^10 per credit) as credits.
func credits(n json.Number) string {
	f, err := n.Float64()
	if err != nil {
		return n.String()
	}
	c := f / 1e10
	if c == 0 {
		return "0 credits"
	}
	if c > 0 && c < 0.0001 {
		return "under 0.0001 credits"
	}
	return fmt.Sprintf("%.4f credits", c)
}

func groupDigits(s string) string {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	if strings.ContainsAny(s, ".eE") || len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func lookupPath(m map[string]any, path string) any {
	var cur any = m
	for part := range strings.SplitSeq(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = obj[part]
	}
	return cur
}

func str(r row, k string) string {
	v, ok := r[k]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// dotJoin joins the non-empty parts with a middle dot.
func dotJoin(parts ...string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

func providersScreen() screen {
	return newListScreen(listSpec{
		title: "Providers", opID: "listOrgProviders", params: map[string][]string{"limit": {"200"}}, itemsPath: "providers",
		empty:  "No providers yet. Add one in the console or with cchp providers create.",
		fields: []string{"name", "slug", "enabled", "website_url", "cost_multiplier_ppm", "notes", "created_at", "updated_at", "id"},
		toEntry: func(r row) entry {
			state := "enabled"
			if b, ok := r["enabled"].(bool); ok && !b {
				state = "disabled"
			}
			return entry{title: str(r, "name"), desc: dotJoin(state, str(r, "website_url")), data: r}
		},
	})
}

func providerKeysPicker() screen {
	return newListScreen(listSpec{
		title: "Provider keys: pick a provider", opID: "listOrgProviders", params: map[string][]string{"limit": {"200"}}, itemsPath: "providers",
		empty: "No providers yet, so there are no keys.",
		toEntry: func(r row) entry {
			return entry{title: str(r, "name"), desc: str(r, "website_url"), data: r}
		},
		open: func(r row) screen { return providerKeysScreen(str(r, "id"), str(r, "name")) },
	})
}

func providerKeysScreen(providerID, providerName string) screen {
	return newListScreen(listSpec{
		title: "Keys of " + providerName, opID: "listProviderKeys", params: map[string][]string{"provider-id": {providerID}}, itemsPath: "keys",
		empty:  "This provider has no keys yet.",
		fields: []string{"name", "status", "secret_prefix", "secret_last_four", "priority", "weight", "cost_multiplier_ppm", "credential_type", "notes", "id"},
		toEntry: func(r row) entry {
			secret := strings.TrimSpace(str(r, "secret_prefix") + "…" + str(r, "secret_last_four"))
			return entry{title: nonEmpty(str(r, "name"), secret), desc: dotJoin(str(r, "status"), secret, "priority "+str(r, "priority")), data: r}
		},
	})
}

func llmTokensScreen() screen {
	return newListScreen(listSpec{
		title: "LLM tokens", opID: "listLLMTokens", itemsPath: "tokens",
		empty:  "No LLM tokens yet. Create one with cchp llm-tokens create.",
		fields: []string{"name", "status", "prefix", "routeStrategy", "allowedModels", "expires_at", "last_used_at", "created_at", "id"},
		toEntry: func(r row) entry {
			used := "never used"
			if s := str(r, "last_used_at"); s != "" {
				used = "last used " + formatValue("", s)
			}
			return entry{title: str(r, "name"), desc: dotJoin(str(r, "status"), str(r, "prefix")+"…", used), data: r}
		},
	})
}

func requestsScreen() screen {
	return newListScreen(listSpec{
		title: "Recent requests", opID: "listUsageLogs", params: map[string][]string{"limit": {"50"}}, itemsPath: "page.items",
		empty:  "No requests yet. They show up here once apps call the gateway.",
		fields: []string{"startedAt", "status", "httpStatus", "requestedModel", "finalModel", "providerName", "userEmail", "keyName", "inputTokens", "outputTokens", "costUnits", "durationMs", "ttftMs", "errorCategory", "requestId"},
		toEntry: func(r row) entry {
			when := formatValue("", str(r, "startedAt"))
			return entry{
				title: dotJoin(str(r, "requestedModel"), str(r, "status")),
				desc:  dotJoin(when, str(r, "providerName"), str(r, "userEmail"), str(r, "durationMs")+" ms"),
				data:  r,
			}
		},
	})
}

func nonEmpty(s, alt string) string {
	if strings.TrimSpace(s) == "" || s == "…" {
		return alt
	}
	return s
}
