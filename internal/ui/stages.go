package ui

import (
	"fmt"
	"strings"

	"github.com/jomakori/tickerlight/internal/tickerdata"
)

// KV is one key/value row in a detail stage.
type KV struct{ Key, Value string }

// Check is one visible pass/fail line on a stage.
type Check struct {
	Label string
	Value string
	OK    bool
}

// Stage is one rendered step of the attempt chain.
type Stage struct {
	Name   string
	Class  string
	Time   string
	KV     []KV
	Checks []Check
	Reason string
}

// StagesOf walks the four stages for one attempt and returns what the detail
// panel shows for each: values seen, checks run, and the plain-language reason
// when the chain stops.
func StagesOf(r tickerdata.Attempt) []Stage {
	lim := tickerdata.DefaultLimits()
	meta := r.Outcome.Meta()
	out := make([]Stage, 0, len(tickerdata.Stages))
	names := []string{"Intent", "Judge", "Gate", "Venue"}
	prevBlocked := false

	for i := range tickerdata.Stages {
		st := Stage{Name: names[i], Time: "—"}
		if i == 0 {
			st.Time = HMS(r.TS)
		}
		switch {
		case meta.Stop < 0 || i < meta.Stop:
			st.Class = "done"
		case i == meta.Stop && meta.Kind == tickerdata.KindBad:
			st.Class = "stop-bad"
		case i == meta.Stop:
			st.Class = "stop-blk"
		default:
			st.Class = "nr"
		}

		switch i {
		case 0:
			st.KV = []KV{
				{"Side", strings.ToUpper(r.Side)},
				{"Qty", Qty(r.Qty)},
				{"Limit px", Px(r.Px)},
				{"Notional", USD(r.Notional)},
			}
			st.Checks = []Check{
				{"symbol allowlisted", r.Symbol, true},
				{"side is buy or sell", strings.ToUpper(r.Side), true},
			}
		case 1:
			// The score comes from the journal's confidence when one was
			// recorded; an attempt whose score was never logged shows a
			// dash rather than a fabricated 0.00.
			score, scored := r.Score()
			pText := "—"
			if scored {
				pText = fmt.Sprintf("%.2f", score)
			}
			st.KV = []KV{{"score", pText}, {"threshold", fmt.Sprintf("%.2f", lim.JudgeMin)}}
			st.Checks = []Check{
				{"score ≥ threshold", fmt.Sprintf("%s ≥ %.2f", pText, lim.JudgeMin), scored && score >= lim.JudgeMin},
			}
			if r.Outcome == tickerdata.Judge {
				st.Reason = ReasonText(r)
			}
		case 2:
			st.KV = []KV{
				{"per-order cap", USD(lim.PerOrder)},
				{"daily cap", USD(lim.Daily)},
				{"rolling 24h before", USD(r.CapBefore)},
				{"rolling 24h after", USD(r.CapAfter)},
			}
			orderOK := r.Notional <= lim.PerOrder
			st.Checks = []Check{
				{"notional ≤ per-order cap", USD(r.Notional), orderOK},
				{"rolling 24h after ≤ daily cap", USD(r.CapAfter), r.CapAfter <= lim.Daily},
			}
			if r.Outcome == tickerdata.Cap || r.Outcome == tickerdata.Kill {
				st.Reason = ReasonText(r)
			}
		case 3:
			dispatch := r.DispatchID
			if dispatch == "" {
				dispatch = "—"
			}
			fill := "—"
			if r.Fill != nil {
				fill = Px(*r.Fill)
			}
			st.KV = []KV{
				{"dispatch id", dispatch},
				{"fill px", fill},
				{"fees", USD(r.Fees)},
				{"realized p&l", USDSigned(r.PnL)},
			}
			if r.Outcome == tickerdata.Venue {
				st.Reason = ReasonText(r)
			}
		}

		if st.Class == "nr" && !prevBlocked {
			st.Reason = "Not reached — the chain stopped at the " + strings.ToLower(names[meta.Stop]) + " stage."
			prevBlocked = true
		}
		out = append(out, st)
	}
	return out
}

// boolStr renders a boolean as a string for data attributes.
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// boolWord renders a boolean as the word the console uses.
func boolWord(b bool) string {
	if b {
		return "ARMED"
	}
	return "DISARMED"
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// Opt is one <option> or quick-chip entry.
type Opt struct {
	Value string
	Label string
}

func outcomeOptions() []Opt {
	return []Opt{
		{"all", "All outcomes"},
		{"filled", "Filled"},
		{"cap", "Blocked · cap"},
		{"kill", "Blocked · kill"},
		{"judge", "Blocked · judge"},
		{"venue", "Venue rejected"},
	}
}

func chipOptions() []Opt {
	return []Opt{
		{"all", "All"},
		{"filled", "Filled"},
		{"blocked", "Blocked"},
		{"cap", "Cap"},
		{"kill", "Kill"},
		{"judge", "Judge"},
		{"venue", "Venue"},
	}
}

// chipOn reports whether a quick chip is the active filter.
func chipOn(cur, v string) bool {
	if cur == "" {
		cur = "all"
	}
	return cur == v
}

// FilterOutcomeLabel is the readable name of an active outcome filter.
func FilterOutcomeLabel(v string) string {
	for _, o := range outcomeOptions() {
		if o.Value == v {
			return o.Label
		}
	}
	return "All outcomes"
}
