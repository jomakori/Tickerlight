// Package tickerdata is the read-only sample ledger behind the Tickerlight ops
// console: attempt rows, the derived rolling-cap/dispatch fields, aggregate
// stats and filters.
//
// Everything here is local fixture data. The package performs no I/O and never
// reaches a venue: the sample rows are the only source of truth, exactly like
// the data.js fixture the static build shipped with.
package tickerdata

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Outcome is the terminal state of an order attempt.
type Outcome string

const (
	Filled Outcome = "filled"
	Cap    Outcome = "cap"
	Kill   Outcome = "kill"
	Judge  Outcome = "judge"
	Venue  Outcome = "venue"
)

// Kind classifies an outcome for styling and semantics: ok = fill,
// blk = benign block by a safety gate (expected), bad = venue fault.
type Kind string

const (
	KindOK  Kind = "ok"
	KindBlk Kind = "blk"
	KindBad Kind = "bad"
)

// OutcomeMeta carries the presentation contract for one outcome.
type OutcomeMeta struct {
	Label string
	Kind  Kind
	Icon  string
	// Stop is the zero-based stage index the chain halts at; -1 means the
	// attempt cleared every stage.
	Stop int
}

// Outcomes is the outcome taxonomy: the single place that maps an outcome to
// its label, style kind, icon and halting stage.
var Outcomes = map[Outcome]OutcomeMeta{
	Filled: {Label: "FILLED", Kind: KindOK, Icon: "check", Stop: -1},
	Cap:    {Label: "BLOCKED · CAP", Kind: KindBlk, Icon: "slash", Stop: 2},
	Kill:   {Label: "BLOCKED · KILL", Kind: KindBlk, Icon: "slash", Stop: 2},
	Judge:  {Label: "BLOCKED · JUDGE", Kind: KindBlk, Icon: "slash", Stop: 1},
	Venue:  {Label: "VENUE REJECTED", Kind: KindBad, Icon: "bang", Stop: 3},
}

// Stages are the four chain stages every attempt walks, in order.
var Stages = []string{"intent", "judge", "gate", "venue"}

// Meta returns the presentation record for an outcome, falling back to a
// neutral "unknown" record instead of panicking on an unmapped value.
func (o Outcome) Meta() OutcomeMeta {
	if m, ok := Outcomes[o]; ok {
		return m
	}
	return OutcomeMeta{Label: "UNKNOWN", Kind: KindBlk, Icon: "minus", Stop: -1}
}

// Stage returns the stage name the attempt halted at, or "" when it cleared
// every stage.
func (o Outcome) Stage() string {
	m := o.Meta()
	if m.Stop < 0 || m.Stop >= len(Stages) {
		return ""
	}
	return Stages[m.Stop]
}

// Limits is the risk policy shown read-only in Overview and Risk state.
type Limits struct {
	PerOrder  float64
	Daily     float64
	Drawdown  float64
	JudgeMin  float64
	MinN      int
	CooldownS int
}

// DefaultLimits mirrors the sample policy the static mock shipped with.
func DefaultLimits() Limits {
	return Limits{PerOrder: 500, Daily: 3500, Drawdown: 100, JudgeMin: 0.60, MinN: 20, CooldownS: 720}
}

// Attempt is one order attempt with its derived rolling-cap and dispatch
// fields.
type Attempt struct {
	ID      int
	TS      time.Time
	Venue   string
	Symbol  string
	Side    string
	Qty     float64
	Px      float64
	Outcome Outcome
	P       float64
	PnL     float64
	Fees    float64
	Detail  string

	Notional   float64
	CapBefore  float64
	CapAfter   float64
	Armed      bool
	DispatchID string
	Fill       *float64
}

// Dispatched reports whether the attempt ever left the gate and reached a
// venue.
func (a Attempt) Dispatched() bool {
	return a.Outcome == Filled || a.Outcome == Venue
}

// SampleNow is the fixed clock the sample ledger is written against, so the
// 24h/today views and the derived rolling cap stay deterministic.
var SampleNow = time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)

// raw rows: id, ts, venue, symbol, side, qty, px, outcome, judge p,
// realized pnl, fees, detail.
var sampleRaw = []Attempt{
	{ID: 1, Venue: "alpaca-paper", Symbol: "AAPL", Side: "buy", Qty: 2, Px: 225.40, Outcome: Filled, P: .64, PnL: 12.40, Fees: .10},
	{ID: 2, Venue: "alpaca-paper", Symbol: "MSFT", Side: "buy", Qty: 1, Px: 410.20, Outcome: Filled, P: .71, PnL: -8.15, Fees: .10},
	{ID: 3, Venue: "binance-testnet", Symbol: "BTCUSDT", Side: "buy", Qty: .02, Px: 68000, Outcome: Cap, P: .77},
	{ID: 4, Venue: "alpaca-paper", Symbol: "NVDA", Side: "sell", Qty: 3, Px: 132.50, Outcome: Filled, P: .82, PnL: 21.60, Fees: .10},
	{ID: 5, Venue: "binance-testnet", Symbol: "ETHUSDT", Side: "buy", Qty: .15, Px: 2600, Outcome: Filled, P: .68, PnL: -14.30, Fees: .39},
	{ID: 6, Venue: "alpaca-paper", Symbol: "TSLA", Side: "buy", Qty: 3, Px: 245.00, Outcome: Cap, P: .66},
	{ID: 7, Venue: "alpaca-paper", Symbol: "AAPL", Side: "buy", Qty: 2, Px: 226.10, Outcome: Filled, P: .73, PnL: 9.80, Fees: .10},
	{ID: 8, Venue: "binance-testnet", Symbol: "SOLUSDT", Side: "buy", Qty: 4, Px: 170.00, Outcome: Cap, P: .74},
	{ID: 9, Venue: "alpaca-paper", Symbol: "MSFT", Side: "sell", Qty: 1, Px: 411.00, Outcome: Filled, P: .86, PnL: 17.25, Fees: .10},
	{ID: 10, Venue: "binance-testnet", Symbol: "ETHUSDT", Side: "sell", Qty: .1, Px: 2610, Outcome: Venue, P: .75, Detail: "insufficient testnet balance (code 40110)"},
	{ID: 11, Venue: "alpaca-paper", Symbol: "NVDA", Side: "buy", Qty: 3, Px: 133.00, Outcome: Filled, P: .91, PnL: -11.40, Fees: .10},
	{ID: 12, Venue: "alpaca-paper", Symbol: "AAPL", Side: "buy", Qty: 2, Px: 226.50, Outcome: Kill, P: .70, Detail: "engaged 12:14:02Z–12:19:48Z"},
	{ID: 13, Venue: "alpaca-paper", Symbol: "MSFT", Side: "buy", Qty: 1, Px: 412.00, Outcome: Filled, P: .67, PnL: 5.10, Fees: .10},
}

var sampleTS = []string{
	"2026-10-29T09:12:04Z", "2026-10-29T10:41:31Z", "2026-10-29T13:05:12Z",
	"2026-10-29T15:30:47Z", "2026-10-29T17:02:20Z", "2026-10-30T08:20:09Z",
	"2026-10-30T09:03:55Z", "2026-10-30T09:45:18Z", "2026-10-30T10:10:40Z",
	"2026-10-30T10:58:02Z", "2026-10-30T11:30:26Z", "2026-10-30T12:15:33Z",
	"2026-10-30T13:20:15Z",
}

// Sample returns the derived sample ledger, newest row last (fixture order).
func Sample() []Attempt {
	rows := make([]Attempt, len(sampleRaw))
	copy(rows, sampleRaw)
	for i := range rows {
		ts, err := time.Parse(time.RFC3339, sampleTS[i])
		if err != nil {
			panic(fmt.Sprintf("tickerdata: bad fixture timestamp %q: %v", sampleTS[i], err))
		}
		rows[i].TS = ts
	}
	return Derive(rows)
}

// Empty returns the zero-row fixture used by the empty-state views.
func Empty() []Attempt { return nil }

// Derive fills in the fields the console shows but the raw feed does not
// carry: notional, the rolling-24h cap before/after, the arming flag at the
// time, the dispatch id and the fill price.
func Derive(rows []Attempt) []Attempt {
	out := make([]Attempt, len(rows))
	copy(out, rows)
	for i := range out {
		r := &out[i]
		r.Notional = r.Qty * r.Px
		var before float64
		for j := range out {
			o := out[j]
			if !o.Dispatched() || !o.TS.Before(r.TS) {
				continue
			}
			if r.TS.Sub(o.TS) <= 24*time.Hour {
				before += o.Notional
			}
		}
		r.CapBefore = before
		r.CapAfter = before
		if r.Dispatched() {
			r.CapAfter += r.Notional
		}
		r.Armed = true
		if r.Dispatched() {
			r.DispatchID = fmt.Sprintf("dsp_%04d_%x", r.ID, r.TS.UnixNano()%9973)
		}
		if r.Outcome == Filled {
			px := r.Px
			r.Fill = &px
		}
	}
	return out
}

// Counts is the per-outcome attempt count.
type Counts map[Outcome]int

// Stats is the aggregate the Overview mini-stats and tiles read.
type Stats struct {
	Counts Counts
	Filled int
	Wins   int
	Eq     float64
	DD     float64
	MaxDD  float64
	Fees   float64
	Total  int
}

// StatsFor aggregates filled P&L, win rate inputs, fees and drawdown.
func StatsFor(rows []Attempt) Stats {
	s := Stats{Counts: Counts{}}
	for _, r := range rows {
		s.Counts[r.Outcome]++
	}
	var eq, peak, maxdd float64
	for _, r := range rows {
		if r.Outcome != Filled {
			continue
		}
		s.Filled++
		if r.PnL > 0 {
			s.Wins++
		}
		s.Fees += r.Fees
		eq += r.PnL
		if eq > peak {
			peak = eq
		}
		if d := peak - eq; d > maxdd {
			maxdd = d
		}
	}
	s.Eq = eq
	s.DD = peak - eq
	s.MaxDD = maxdd
	s.Total = len(rows)
	return s
}

// RollingUsed sums the notional of dispatched attempts inside the trailing
// 24h window ending at now.
func RollingUsed(rows []Attempt, now time.Time) float64 {
	var used float64
	for _, r := range rows {
		if r.Dispatched() && !r.TS.After(now) && now.Sub(r.TS) <= 24*time.Hour {
			used += r.Notional
		}
	}
	return used
}

// Range is the Overview time-range pill.
type Range string

const (
	RangeAll   Range = "all"
	Range24h   Range = "24h"
	RangeToday Range = "today"
)

// ParseRange validates a range query value, defaulting to all time. Values are
// matched case-insensitively so a hand-typed URL does not silently widen.
func ParseRange(v string) Range {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case string(Range24h):
		return Range24h
	case string(RangeToday):
		return RangeToday
	default:
		return RangeAll
	}
}

// Label is the human range name used in the chart heading.
func (r Range) Label() string {
	switch r {
	case Range24h:
		return "Last 24h"
	case RangeToday:
		return "Today"
	default:
		return "All time"
	}
}

// InRange filters rows to the selected Overview range.
func InRange(rows []Attempt, rg Range, now time.Time) []Attempt {
	switch rg {
	case Range24h:
		return keep(rows, func(r Attempt) bool { return !r.TS.After(now) && now.Sub(r.TS) <= 24*time.Hour })
	case RangeToday:
		y, m, d := now.UTC().Date()
		return keep(rows, func(r Attempt) bool {
			ry, rm, rd := r.TS.UTC().Date()
			return ry == y && rm == m && rd == d
		})
	default:
		return rows
	}
}

// Filter is the Journal filter state. Empty or "all" means unfiltered.
type Filter struct {
	Outcome string
	Venue   string
	Symbol  string
	From    string
	To      string
}

// IsZero reports whether no filter clause is active.
func (f Filter) IsZero() bool {
	unset := func(v string) bool { return v == "" || v == "all" }
	return unset(f.Outcome) && unset(f.Venue) && unset(f.Symbol) && f.From == "" && f.To == ""
}

// Matches reports whether an attempt passes every active filter clause.
func (f Filter) Matches(r Attempt) bool {
	if f.Outcome != "" && f.Outcome != "all" {
		if f.Outcome == "blocked" {
			if r.Outcome.Meta().Kind != KindBlk {
				return false
			}
		} else if r.Outcome != Outcome(f.Outcome) {
			// Includes unknown values: they match nothing rather than
			// silently widening the tape.
			return false
		}
	}
	if f.Venue != "" && f.Venue != "all" && r.Venue != f.Venue {
		return false
	}
	if f.Symbol != "" && f.Symbol != "all" && r.Symbol != f.Symbol {
		return false
	}
	day := r.TS.UTC().Format("2006-01-02")
	if f.From != "" && day < f.From {
		return false
	}
	if f.To != "" && day > f.To {
		return false
	}
	return true
}

// FilterRows applies f and returns the matches newest first.
func FilterRows(rows []Attempt, f Filter) []Attempt {
	out := make([]Attempt, 0, len(rows))
	for _, r := range rows {
		if f.Matches(r) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS.After(out[j].TS) })
	return out
}

// Venues lists the distinct venues in fixture order.
func Venues(rows []Attempt) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if !seen[r.Venue] {
			seen[r.Venue] = true
			out = append(out, r.Venue)
		}
	}
	return out
}

// Symbols lists the distinct symbols, sorted.
func Symbols(rows []Attempt) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if !seen[r.Symbol] {
			seen[r.Symbol] = true
			out = append(out, r.Symbol)
		}
	}
	sort.Strings(out)
	return out
}

// ByID finds an attempt by id.
func ByID(rows []Attempt, id int) (Attempt, bool) {
	for _, r := range rows {
		if r.ID == id {
			return r, true
		}
	}
	return Attempt{}, false
}

// VenueName is the short venue label ("alpaca-paper" -> "ALPACA").
func VenueName(v string) string {
	for i := 0; i < len(v); i++ {
		if v[i] == '-' {
			return upper(v[:i])
		}
	}
	return upper(v)
}

func upper(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 'a' - 'A'
		}
	}
	return string(b)
}

func keep(rows []Attempt, pred func(Attempt) bool) []Attempt {
	out := make([]Attempt, 0, len(rows))
	for _, r := range rows {
		if pred(r) {
			out = append(out, r)
		}
	}
	return out
}

// Bucket is one calibration bucket.
type Bucket struct {
	Label string
	N     int
	Wins  int
}

// Midpoint is the stated judge probability the bucket is drawn at.
func (b Bucket) Midpoint(i int) float64 {
	mids := []float64{0.55, 0.65, 0.75, 0.85, 0.95}
	if i < 0 || i >= len(mids) {
		return 0
	}
	return mids[i]
}

// CalSample is the aggregate calibration sample (not derived from the journal
// rows, matching the spec).
func CalSample() []Bucket {
	return []Bucket{
		{Label: "0.50–0.59", N: 14, Wins: 7},
		{Label: "0.60–0.69", N: 31, Wins: 18},
		{Label: "0.70–0.79", N: 27, Wins: 19},
		{Label: "0.80–0.89", N: 22, Wins: 17},
		{Label: "0.90+", N: 9, Wins: 8},
	}
}

// CalEmpty is the zero-observation calibration fixture.
func CalEmpty() []Bucket {
	out := CalSample()
	for i := range out {
		out[i].N, out[i].Wins = 0, 0
	}
	return out
}
