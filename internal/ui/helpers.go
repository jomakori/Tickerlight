// Package ui renders the Tickerlight ops console as templ components.
//
// All values are server-rendered from the local sample ledger; the only
// client-side work left is progressive enhancement (chart crosshair, cooldown
// ticker, keyboard shortcuts) in web/static/app.js.
package ui

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jomakori/tickerlight/internal/engine"
	"github.com/jomakori/tickerlight/internal/tickerdata"
)

// Tab names, in nav order.
const (
	TabOverview    = "overview"
	TabJournal     = "journal"
	TabCalibration = "calibration"
	TabRisk        = "risk"
	TabPolicy      = "policy"
)

// Tabs is the nav order, shared by the sidebar and the bottom tab bar.
var Tabs = []struct{ Key, Label string }{
	{TabOverview, "Overview"},
	{TabJournal, "Journal"},
	{TabCalibration, "Calibration"},
	{TabRisk, "Risk state"},
	{TabPolicy, "Policy"},
}

// View is everything a page or HTMX fragment needs to render.
type View struct {
	Tab      string
	Range    tickerdata.Range
	Filter   tickerdata.Filter
	Rows     []tickerdata.Attempt
	Now      time.Time
	Limits   tickerdata.Limits
	Selected *tickerdata.Attempt
	// Armed and Kill are the session flags. Both are fixed off in this build:
	// there is no order path, and the arm/kill controls are gated and inert, so
	// reporting anything but "disarmed" would misdescribe the binary.
	Armed bool
	Kill  bool
	// Source names where the rows came from: empty means the built-in
	// sample ledger. The header repeats it so a page showing real journal
	// rows never claims to be sample data.
	Source string
	// Policy is the engine policy reading for the Policy tab. Its zero value
	// renders the unavailable panel: no caps, and nothing inferred from their
	// absence.
	Policy PolicyData
}

// PolicyData is what the Policy tab renders: the engine's caps_status reading,
// when the engine answered. Every field it shows — money, mode, judge threshold
// and alert-sink state — comes from that one response, so the panel has one
// source of truth. The zero value renders the unavailable panel: no caps, and
// nothing inferred from their absence.
type PolicyData struct {
	// Available reports whether Caps holds a reading from the engine. When it
	// is false the panel shows the failure and no numbers at all.
	Available bool
	Caps      engine.Caps
	// Reason is the engine's failure, shown verbatim under the unavailable
	// panel.
	Reason string
}

// UsedPct is the share of the daily cap already dispatched, for the meter. A
// cap of zero or less has no utilisation to draw, so it reports none.
func (d PolicyData) UsedPct() float64 {
	if d.Caps.DailyCents <= 0 {
		return 0
	}
	return float64(d.Caps.SpentCents) / float64(d.Caps.DailyCents)
}

// SourceLabel is the origin phrase used in the title and session panel.
func (v View) SourceLabel() string {
	if v.Source == "" {
		return "sample data"
	}
	return v.Source
}

// SourceBadge is the corner badge: the origin plus the standing paper-only
// reminder.
func (v View) SourceBadge() string {
	if v.Source == "" {
		return "Sample data · paper only"
	}
	return "Attempt journal · paper only"
}

// SourceLine is the session-panel sentence: the origin plus the standing
// reminder that nothing here spends real money.
func (v View) SourceLine() string {
	switch v.SourceLabel() {
	case "sample data":
		return "Sample data · no real funds"
	default:
		return "Attempt journal · no real funds"
	}
}

// ArmKillEnabled reports whether the arm/kill controls are wired to anything.
// False in v1: the console renders them disabled and there is no handler.
const ArmKillEnabled = false

// ArmingFlag is the session arming state shown in the shell and session panel.
// There is no order path, so it is always false and never persisted.
func ArmingFlag() bool { return false }

// NewView builds the default view over a ledger.
func NewView(rows []tickerdata.Attempt, now time.Time) View {
	return View{
		Tab:    TabOverview,
		Range:  tickerdata.RangeAll,
		Filter: tickerdata.Filter{},
		Rows:   rows,
		Now:    now,
		Limits: tickerdata.DefaultLimits(),
		Armed:  ArmingFlag(),
		Kill:   false,
	}
}

// Filtered returns the journal rows matching the current filter, newest first.
func (v View) Filtered() []tickerdata.Attempt {
	return tickerdata.FilterRows(v.Rows, v.Filter)
}

// InRange returns the overview rows for the current range.
func (v View) InRange() []tickerdata.Attempt {
	return tickerdata.InRange(v.Rows, v.Range, v.Now)
}

// RollingUsed is the trailing-24h dispatched notional.
func (v View) RollingUsed() float64 {
	return tickerdata.RollingUsed(v.Rows, v.Now)
}

// ---------- formatting helpers ----------

// USD renders an unsigned-ish money value: "−$12.40" for negatives, "$12.40"
// otherwise (U+2212 minus, matching the dashboard's typography).
func USD(n float64) string {
	if n < 0 {
		return "−$" + money(-n)
	}
	return "$" + money(n)
}

// USDSigned always shows a sign, used for realized P&L: "+$12.40" / "−$8.15".
func USDSigned(n float64) string {
	switch {
	case n > 0:
		return "+$" + money(n)
	case n < 0:
		return "−$" + money(-n)
	default:
		return "$0.00"
	}
}

func money(n float64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	whole := int64(n)
	cents := int64((n-float64(whole))*100 + 0.5)
	if cents >= 100 {
		whole++
		cents -= 100
	}
	s := group(whole) + "." + fmt.Sprintf("%02d", cents)
	if neg {
		return "−" + s
	}
	return s
}

func group(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	rem := len(s) % 3
	if rem > 0 {
		b.WriteString(s[:rem])
	}
	for i := rem; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// Pct formats a ratio as a percentage. d is the number of decimals.
func Pct(n float64, d int) string {
	return strconv.FormatFloat(n*100, 'f', d, 64) + "%"
}

// Qty formats a quantity: whole numbers as integers, fractions to 2dp.
func Qty(n float64) string {
	if n >= 1 {
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	return strconv.FormatFloat(n, 'f', 2, 64)
}

// Px formats a price with thousands separators and 2 decimals.
func Px(n float64) string {
	return "$" + money(n)
}

// HMS is the UTC clock portion of a timestamp ("09:12:04Z").
func HMS(t time.Time) string { return t.UTC().Format("15:04:05") + "Z" }

// MD is the UTC month-day portion ("10-29").
func MD(t time.Time) string { return t.UTC().Format("01-02") }

// Pad4 is the zero-padded attempt number.
func Pad4(n int) string { return fmt.Sprintf("%04d", n) }

// ReasonText is the human explanation shown for an attempt, shared by the
// event list, the chart tooltip and the detail chain.
//
// A row that carries its own reason code (one read from the attempt
// journal) reports that code instead of a sentence computed from the
// sample policy: the console does not know the live caps, and inventing a
// figure would tell an operator that a real refusal happened against a
// limit that was never in force.
func ReasonText(r tickerdata.Attempt) string {
	l := tickerdata.DefaultLimits()
	switch r.Outcome {
	case tickerdata.Cap:
		if r.Detail != "" {
			return "Blocked by cap — " + r.Detail
		}
		if r.Notional > l.PerOrder {
			return fmt.Sprintf("Blocked by cap — %s exceeds the per-order cap of %s", USD(r.Notional), USD(l.PerOrder))
		}
		return fmt.Sprintf("Blocked by cap — %s would exceed the rolling-24h cap of %s", USD(r.Notional), USD(l.Daily))
	case tickerdata.Kill:
		return "Blocked by kill switch — " + r.Detail
	case tickerdata.Judge:
		p := r.P
		if score, ok := r.Score(); ok {
			p = score
		}
		if p == 0 && r.Detail != "" {
			return "Blocked by judge — " + r.Detail
		}
		return fmt.Sprintf("Blocked by judge — p=%.2f is below the %.2f threshold", p, l.JudgeMin)
	case tickerdata.Venue:
		return fmt.Sprintf("Venue rejected — %s: %s", tickerdata.VenueName(r.Venue), r.Detail)
	default:
		return "Filled"
	}
}

// StageStates returns the per-stage status word for the spine aria label.
func StageStates(o tickerdata.Outcome) []string {
	meta := o.Meta()
	out := make([]string, len(tickerdata.Stages))
	for i := range tickerdata.Stages {
		switch {
		case meta.Stop < 0 || i < meta.Stop:
			out[i] = "passed"
		case i == meta.Stop && meta.Kind == tickerdata.KindBad:
			out[i] = "failed"
		case i == meta.Stop:
			out[i] = "blocked"
		default:
			out[i] = "not reached"
		}
	}
	return out
}

// ---------- URL helpers ----------

// JournalURL builds a journal URL carrying the filter state and optional
// selected attempt.
func JournalURL(f tickerdata.Filter, sel int) string {
	q := url.Values{}
	if f.Outcome != "" && f.Outcome != "all" {
		q.Set("outcome", f.Outcome)
	}
	if f.Venue != "" && f.Venue != "all" {
		q.Set("venue", f.Venue)
	}
	if f.Symbol != "" && f.Symbol != "all" {
		q.Set("symbol", f.Symbol)
	}
	if f.From != "" {
		q.Set("from", f.From)
	}
	if f.To != "" {
		q.Set("to", f.To)
	}
	if sel > 0 {
		q.Set("sel", strconv.Itoa(sel))
	}
	if len(q) == 0 {
		return "/journal"
	}
	return "/journal?" + q.Encode()
}

// DetailURL is the fragment route for one attempt detail panel, carrying the
// current filter so Close can restore the table state.
func DetailURL(f tickerdata.Filter, id int) string {
	return JournalURL(f, id)
}

// RangeURL switches the overview range, dropping nothing else.
func RangeURL(r tickerdata.Range) string {
	if r == tickerdata.RangeAll {
		return "/"
	}
	return "/?range=" + string(r)
}

// FilterCopy returns a copy of f with one field set — used by the outcome
// chips and tile links so each carries the rest of the filter state.
func FilterCopy(f tickerdata.Filter, outcome string) tickerdata.Filter {
	f.Outcome = outcome
	return f
}

// CooldownText renders the sample cooldown as mm:ss.
func CooldownText(seconds int) string {
	if seconds <= 0 {
		return "READY"
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}
