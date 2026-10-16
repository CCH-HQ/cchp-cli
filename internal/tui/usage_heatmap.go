package tui

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

var usageMetrics = [...]string{"Requests", "Tokens", "Cost"}

type usageDay struct {
	date                                           time.Time
	requests, inputTokens, outputTokens, costUnits int64
}

func (d usageDay) value(metric int) int64 {
	switch metric {
	case 1:
		return d.inputTokens + d.outputTokens
	case 2:
		return d.costUnits
	default:
		return d.requests
	}
}

// usageDays fills successful sparse daily responses with zeroes; failures never
// become a misleading empty calendar. Dates use the same UTC boundaries as KPIs.
func usageDays(doc map[string]any, start time.Time, count int) ([]usageDay, error) {
	buckets, ok := doc["buckets"].([]any)
	if !ok {
		return nil, fmt.Errorf("the server returned an invalid daily usage response")
	}
	days := make([]usageDay, count)
	for i := range days {
		days[i].date = start.AddDate(0, 0, i)
	}
	for _, bucket := range buckets {
		r, ok := bucket.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("the server returned an invalid daily usage bucket")
		}
		stamp, err := time.Parse(time.RFC3339, str(r, "bucket"))
		if err != nil {
			return nil, fmt.Errorf("the server returned an invalid daily usage date")
		}
		stamp = stamp.UTC()
		date := time.Date(stamp.Year(), stamp.Month(), stamp.Day(), 0, 0, 0, 0, time.UTC)
		index := int(date.Sub(start).Hours() / 24)
		if date.Before(start) || index >= len(days) {
			continue
		}
		day := &days[index]
		for _, field := range []struct {
			name string
			dest *int64
		}{{"requests", &day.requests}, {"inputTokens", &day.inputTokens}, {"outputTokens", &day.outputTokens}, {"costUnits", &day.costUnits}} {
			n, valid := r[field.name].(json.Number)
			value, parseErr := n.Int64()
			if !valid || parseErr != nil || (field.name != "costUnits" && value < 0) {
				return nil, fmt.Errorf("the server returned an invalid daily usage %s", field.name)
			}
			if (value > 0 && *field.dest > math.MaxInt64-value) || (value < 0 && *field.dest < math.MinInt64-value) {
				return nil, fmt.Errorf("daily usage %s exceeds the supported range", field.name)
			}
			*field.dest += value
		}
		if day.inputTokens > math.MaxInt64-day.outputTokens {
			return nil, fmt.Errorf("daily token usage exceeds the supported range")
		}
	}
	return days, nil
}

// intensityLevels follows the CCHP contribution wall: nonzero quartiles keep a
// single expensive day from making every other day indistinguishable.
func intensityLevels(days []usageDay, metric int) []int {
	var positive []int64
	for _, day := range days {
		if value := day.value(metric); value > 0 {
			positive = append(positive, value)
		}
	}
	slices.Sort(positive)
	levels := make([]int, len(days))
	if len(positive) == 0 {
		return levels
	}
	thresholds := []int64{positive[(len(positive)-1)/4], positive[(len(positive)-1)/2], positive[3*(len(positive)-1)/4]}
	for i, day := range days {
		value := day.value(metric)
		if value <= 0 {
			continue
		}
		levels[i] = 1
		for _, threshold := range thresholds {
			if value > threshold {
				levels[i]++
			}
		}
	}
	return levels
}

func (s *usageScreen) heatmap(width int) string {
	// Monday starts each column. Padding is blank, rather than a zero-usage day.
	padding := (int(s.days[0].date.Weekday()) + 6) % 7
	weeks := (padding + len(s.days) + 6) / 7
	visible := minInt(weeks, maxInt((width-4)/2, 1))
	selectedWeek := (padding + s.cursor) / 7
	s.firstWeek = minInt(s.firstWeek, weeks-visible)
	if selectedWeek < s.firstWeek {
		s.firstWeek = selectedWeek
	} else if selectedWeek >= s.firstWeek+visible {
		s.firstWeek = selectedWeek - visible + 1
	}
	levels := intensityLevels(s.days, s.metric)
	var b strings.Builder
	b.WriteString("    ")
	lastLabelEnd := -1
	for col := range visible {
		index := (s.firstWeek+col)*7 - padding
		label := ""
		for row := range 7 {
			i := index + row
			if i >= 0 && i < len(s.days) && (s.days[i].date.Day() == 1 || col == 0 && i == maxInt(index, 0)) {
				label = s.days[i].date.Format("Jan")
				break
			}
		}
		if col*2 >= lastLabelEnd && label != "" && col*2+3 <= visible*2 {
			b.WriteString(ui.Muted.Render(label))
			lastLabelEnd = col*2 + 3
		} else if col*2 >= lastLabelEnd {
			b.WriteString("  ")
		} else if col*2+1 == lastLabelEnd {
			b.WriteByte(' ')
		}
	}
	b.WriteByte('\n')
	for weekday, name := range []string{"Mon", "   ", "Wed", "   ", "Fri", "   ", "   "} {
		b.WriteString(ui.Muted.Render(name) + " ")
		for col := range visible {
			index := (s.firstWeek+col)*7 + weekday - padding
			if index < 0 || index >= len(s.days) {
				b.WriteString("  ")
				continue
			}
			glyph := heatCell(levels[index])
			if index == s.cursor {
				glyph = lipgloss.NewStyle().Foreground(ui.Good).Bold(true).Reverse(true).Render("◆")
			}
			b.WriteString(glyph + " ")
		}
		b.WriteByte('\n')
	}
	active := 0
	for _, day := range s.days {
		if day.requests > 0 {
			active++
		}
	}
	b.WriteString(ui.Muted.Render(fmt.Sprintf("%d active days · weeks %d–%d/%d · Less ", active, s.firstWeek+1, s.firstWeek+visible, weeks)))
	for level := range 5 {
		b.WriteString(heatCell(level))
	}
	b.WriteString(ui.Muted.Render(" More · ◆ selected") + "\n")
	d := s.days[s.cursor]
	b.WriteString(ui.Title.Render(d.date.Format("Mon 2006-01-02")) + "  " + num(json.Number(fmt.Sprint(d.requests))) + " requests")
	b.WriteString("\n" + num(json.Number(fmt.Sprint(d.inputTokens))) + " in / " + num(json.Number(fmt.Sprint(d.outputTokens))) + " out · " + cost(json.Number(fmt.Sprint(d.costUnits))))
	return b.String()
}

func heatCell(level int) string {
	glyph := []string{"·", "░", "▒", "▓", "█"}[level]
	if level == 0 {
		return ui.Muted.Render(glyph)
	}
	return lipgloss.NewStyle().Foreground(ui.Good).Render(glyph)
}
