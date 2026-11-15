package ui

import (
	"sort"
	"strconv"
	"time"

	"github.com/jomakori/tickerlight/internal/decisionsread"
)

// DecisionsData is what the Decisions panel renders: the newest decision rows
// plus the counts an operator needs to tell a quiet day from a broken one.
type DecisionsData struct {
	// Rows are the decisions to show, newest first.
	Rows []decisionsread.Entry
	// TicksToday counts decisions recorded in the viewer's current UTC day.
	TicksToday int
	// Executed counts decisions whose order the engine placed.
	Executed int
	// Held counts decisions the gate or the engine stopped, by reason.
	Held []ReasonCount
	// P50LatencyMS is the median Jev round trip across the rows shown.
	P50LatencyMS int64
	// Note says where the rows came from, or why there are none.
	Note string
	// Unavailable reports that the journal could not be read, so the panel
	// says so instead of showing an empty day.
	Unavailable bool
}

// ReasonCount is one reason chip and how many decisions it accounts for.
type ReasonCount struct {
	Reason string
	Count  int
}

// DecisionsPage prepares the decision rows for rendering. The view carries the
// rows the server read on this request, so a page left open shows what the
// decider wrote rather than the state it booted with.
func (v View) DecisionsPage() DecisionsData {
	d := DecisionsData{Note: v.DecisionsNote, Unavailable: v.DecisionsUnavailable}

	rows := make([]decisionsread.Entry, 0, len(v.Decisions))
	for i := len(v.Decisions) - 1; i >= 0; i-- {
		rows = append(rows, v.Decisions[i])
	}
	d.Rows = rows

	today := v.Now.UTC().Format("2006-01-02")
	counts := map[string]int{}
	var latencies []int64
	for _, e := range rows {
		if e.Time.UTC().Format("2006-01-02") == today {
			d.TicksToday++
		}
		if e.Order != nil && e.Order.Placed {
			d.Executed++
		} else if r := DecisionReason(e); r != "" {
			counts[r]++
		}
		if e.LatencyMS > 0 {
			latencies = append(latencies, e.LatencyMS)
		}
	}

	for reason, n := range counts {
		d.Held = append(d.Held, ReasonCount{Reason: reason, Count: n})
	}
	sort.Slice(d.Held, func(i, j int) bool {
		if d.Held[i].Count != d.Held[j].Count {
			return d.Held[i].Count > d.Held[j].Count
		}
		return d.Held[i].Reason < d.Held[j].Reason
	})
	d.P50LatencyMS = median(latencies)

	return d
}

// DecisionReason is the chip word for one decision: what stopped it, or the
// engine's own outcome word when the gate passed. An executed order has no
// reason — it happened.
func DecisionReason(e decisionsread.Entry) string {
	if e.Error != "" {
		return "error"
	}
	if e.Order != nil && e.Order.Placed {
		return ""
	}
	if e.Gate.Reason != "" {
		return e.Gate.Reason
	}
	if e.Order != nil && e.Order.Outcome != "" {
		return e.Order.Outcome
	}
	return ""
}

// ChoiceLabel is the Jev answer plus the probability it gave that answer, so a
// bare "buy" never reads as confident as it looks.
func ChoiceLabel(e decisionsread.Entry) string {
	if e.Jev == nil || e.Jev.Choice == "" {
		return "—"
	}
	p, ok := e.Jev.Probabilities[e.Jev.Choice]
	if !ok {
		return e.Jev.Choice
	}
	return e.Jev.Choice + " " + pct(p)
}

// median returns the middle value of xs, or 0 for none.
func median(xs []int64) int64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int64(nil), xs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

// pct formats a 0..1 probability as a whole percentage.
func pct(p float64) string {
	return strconv.Itoa(int(p*100+0.5)) + "%"
}

// verdictClass picks the badge colour for a reason: errors read as failures,
// a disarmed or killed engine as inert, gate refusals as blocked.
func verdictClass(reason string) string {
	switch reason {
	case "error":
		return "bad"
	case "not_armed", "killed":
		return "nd2"
	default:
		return "blk"
	}
}

// decisionTime is the row's clock time, in the same zone the console shows.
func decisionTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format("15:04")
}
