package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/jomakori/tickerlight/internal/decisionsread"
)

var decNow = time.Date(2026, 11, 14, 16, 30, 0, 0, time.UTC)

// dec is one decision row: hold with a gate reason, an executed buy, or an
// engine refusal, which is what the panel has to tell apart.
func dec(at time.Time, symbol, reason string, placed bool, latency int64) decisionsread.Entry {
	e := decisionsread.Entry{
		Time:      at,
		Symbol:    symbol,
		Jev:       &decisionsread.Jev{Model: "jev-1.14.0", Choice: "hold", Probabilities: map[string]float64{"hold": 0.86, "buy": 0.14}, Score: 0.25, Noul: 0.64},
		Gate:      decisionsread.Gate{Reason: reason},
		LatencyMS: latency,
	}
	if placed {
		e.Jev.Choice = "buy"
		e.Jev.Probabilities = map[string]float64{"buy": 0.86, "hold": 0.14}
		e.Gate = decisionsread.Gate{Passed: true}
		e.Order = &decisionsread.Order{Placed: true, Qty: 0.08164598, Outcome: "filled", Status: "filled"}
	}
	return e
}

func TestDecisionsPageSummary(t *testing.T) {
	tests := []struct {
		name      string
		rows      []decisionsread.Entry
		wantToday int
		wantDone  int
		wantHeld  []ReasonCount
		wantP50   int64
	}{
		{
			name: "counts today, executed and held by reason",
			rows: []decisionsread.Entry{
				dec(decNow.Add(-3*time.Hour), "SPY", "low_setup_score", false, 90),
				dec(decNow.Add(-2*time.Hour), "AAPL", "low_setup_score", false, 110),
				dec(decNow.Add(-time.Hour), "MSFT", "high_risk", false, 130),
				dec(decNow.Add(-30*time.Minute), "QQQ", "", true, 70),
			},
			wantToday: 4,
			wantDone:  1,
			wantHeld: []ReasonCount{
				{Reason: "low_setup_score", Count: 2},
				{Reason: "high_risk", Count: 1},
			},
			wantP50: 110,
		},
		{
			name: "yesterday is not today",
			rows: []decisionsread.Entry{
				dec(decNow.Add(-25*time.Hour), "SPY", "not_armed", false, 0),
				dec(decNow.Add(-time.Minute), "SPY", "not_armed", false, 0),
			},
			wantToday: 1,
			wantHeld:  []ReasonCount{{Reason: "not_armed", Count: 2}},
		},
		{
			name: "empty journal",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := View{Decisions: tc.rows, Now: decNow}.DecisionsPage()
			if d.TicksToday != tc.wantToday {
				t.Errorf("ticks today = %d, want %d", d.TicksToday, tc.wantToday)
			}
			if d.Executed != tc.wantDone {
				t.Errorf("executed = %d, want %d", d.Executed, tc.wantDone)
			}
			if len(d.Held) != len(tc.wantHeld) {
				t.Fatalf("held = %+v, want %+v", d.Held, tc.wantHeld)
			}
			for i, want := range tc.wantHeld {
				if d.Held[i] != want {
					t.Errorf("held[%d] = %+v, want %+v", i, d.Held[i], want)
				}
			}
			if d.P50LatencyMS != tc.wantP50 {
				t.Errorf("p50 = %d, want %d", d.P50LatencyMS, tc.wantP50)
			}
		})
	}
}

// TestDecisionsPageIsNewestFirst pins the row order the panel shows: the most
// recent decision is the first thing an operator reads.
func TestDecisionsPageIsNewestFirst(t *testing.T) {
	old := dec(decNow.Add(-2*time.Hour), "SPY", "high_risk", false, 1)
	newer := dec(decNow.Add(-time.Hour), "AAPL", "low_setup_score", false, 2)

	d := View{Decisions: []decisionsread.Entry{old, newer}, Now: decNow}.DecisionsPage()
	if len(d.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(d.Rows))
	}
	if d.Rows[0].Symbol != "AAPL" || d.Rows[1].Symbol != "SPY" {
		t.Errorf("order = %s, %s; want AAPL, SPY", d.Rows[0].Symbol, d.Rows[1].Symbol)
	}
}

func TestDecisionReason(t *testing.T) {
	tests := []struct {
		name string
		row  decisionsread.Entry
		want string
	}{
		{name: "gate reason", row: dec(decNow, "SPY", "not_armed", false, 0), want: "not_armed"},
		{name: "executed has no reason", row: dec(decNow, "SPY", "", true, 0), want: ""},
		{
			name: "engine refusal",
			row: decisionsread.Entry{
				Symbol: "SPY",
				Gate:   decisionsread.Gate{Passed: true},
				Order:  &decisionsread.Order{Outcome: "blocked"},
			},
			want: "blocked",
		},
		{
			name: "tick error",
			row:  decisionsread.Entry{Symbol: "SPY", Error: "jev: 502"},
			want: "error",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecisionReason(tc.row); got != tc.want {
				t.Errorf("reason = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestChoiceLabel(t *testing.T) {
	for _, tc := range []struct {
		name string
		jev  *decisionsread.Jev
		want string
	}{
		{name: "no answer", want: "—"},
		{
			name: "choice with its own probability",
			jev:  &decisionsread.Jev{Choice: "hold", Probabilities: map[string]float64{"hold": 0.86}},
			want: "hold 86%",
		},
		{
			name: "choice without a probability",
			jev:  &decisionsread.Jev{Choice: "buy"},
			want: "buy",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := decisionsread.Entry{Jev: tc.jev}
			if got := ChoiceLabel(e); got != tc.want {
				t.Errorf("label = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDecisionsRendersMixedOutcomes is the acceptance fixture: one panel must
// carry an executed order, gate refusals and an error without confusing them.
func TestDecisionsRendersMixedOutcomes(t *testing.T) {
	rows := []decisionsread.Entry{
		dec(decNow.Add(-time.Hour), "SPY", "low_confidence", false, 88),
		dec(decNow.Add(-45*time.Minute), "QQQ", "", true, 92),
		{Symbol: "MSFT", Time: decNow.Add(-30 * time.Minute), Error: "jev: timeout"},
		dec(decNow.Add(-15*time.Minute), "AAPL", "not_armed", false, 0),
	}
	html := render(t, Layout(View{Decisions: rows, Now: decNow, Tab: TabDecisions, DecisionsNote: "decisions journal: /data/decisions.jsonl"}))

	for _, want := range []string{
		`aria-label="Decisions"`,
		"low_confidence", "executed · filled", "jev: timeout", "not_armed",
		"hold 86%", "buy 86%",
		"Ticks today", "Executed", "Jev latency p50", "92",
		"decisions journal: /data/decisions.jsonl",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("panel missing %q", want)
		}
	}
	if strings.Contains(html, "No decisions recorded yet") {
		t.Error("populated panel shows the empty state")
	}
}

// TestDecisionsEmptyState pins the copy a fresh install shows: no rows, no
// zero-count chips, and an explanation rather than a blank table.
func TestDecisionsEmptyState(t *testing.T) {
	html := render(t, Layout(View{Now: decNow, Tab: TabDecisions, DecisionsNote: "decisions journal: /data/decisions.jsonl (no decisions yet)"}))

	if !strings.Contains(html, "No decisions recorded yet") {
		t.Error("empty panel is missing its explanation")
	}
	if strings.Contains(html, "<table") {
		t.Error("empty panel renders a table")
	}
	if strings.Contains(html, "<tbody") {
		t.Error("empty panel renders table rows")
	}
}

// TestDecisionsUnavailable pins the third state: a journal that could not be
// read must not look like a quiet day.
func TestDecisionsUnavailable(t *testing.T) {
	html := render(t, Layout(View{
		Now:                  decNow,
		Tab:                  TabDecisions,
		DecisionsNote:        "decisions journal unreadable: /data/decisions.jsonl: permission denied",
		DecisionsUnavailable: true,
	}))

	if !strings.Contains(html, "journal unavailable") {
		t.Error("unavailable panel does not say so")
	}
	if !strings.Contains(html, "permission denied") {
		t.Error("unavailable panel hides the reason")
	}
	if strings.Contains(html, "No decisions recorded yet") {
		t.Error("unavailable panel shows the empty state")
	}
}
