package ui

import (
	"time"

	"github.com/jomakori/tickerlight/internal/tickerdata"
)

// OverviewData is the prepared Overview panel state.
type OverviewData struct {
	View
	Rows    []tickerdata.Attempt
	Stats   tickerdata.Stats
	Used    float64
	DayPnL  float64
	Blocked int
	AllLen  int
}

// Overview prepares the Overview panel.
func (v View) Overview() OverviewData {
	rows := v.InRange()
	st := tickerdata.StatsFor(rows)
	var day float64
	for _, r := range v.Rows {
		if r.Outcome == tickerdata.Filled && !r.TS.After(v.Now) && v.Now.Sub(r.TS) <= 24*time.Hour {
			day += r.PnL
		}
	}
	blocked := 0
	for _, r := range rows {
		if r.Outcome.Meta().Kind == tickerdata.KindBlk {
			blocked++
		}
	}
	return OverviewData{
		View:    v,
		Rows:    rows,
		Stats:   st,
		Used:    v.RollingUsed(),
		DayPnL:  day,
		Blocked: blocked,
		AllLen:  len(v.Rows),
	}
}

// WinRate is the filled win rate (0 when nothing filled).
func (d OverviewData) WinRate() float64 {
	if d.Stats.Filled == 0 {
		return 0
	}
	return float64(d.Stats.Wins) / float64(d.Stats.Filled)
}

// Recent is the newest gate events, newest first, capped at n.
func (d OverviewData) Recent(n int) []tickerdata.Attempt {
	rows := tickerdata.FilterRows(d.Rows, tickerdata.Filter{})
	if len(rows) > n {
		return rows[:n]
	}
	return rows
}

// JournalData is the prepared Journal panel state.
type JournalData struct {
	View
	Rows     []tickerdata.Attempt
	Selected *tickerdata.Attempt
	Venues   []string
	Symbols  []string
	Total    int
}

// Journal prepares the Journal panel.
func (v View) Journal() JournalData {
	rows := v.Filtered()
	return JournalData{
		View:     v,
		Rows:     rows,
		Selected: v.Selected,
		Venues:   tickerdata.Venues(v.Rows),
		Symbols:  tickerdata.Symbols(v.Rows),
		Total:    len(v.Rows),
	}
}

// CalRow is one probability bucket with the numbers the table and plot need.
type CalRow struct {
	Label  string
	N      int
	Wins   int
	Stated float64
	Rate   float64
	Delta  float64
	Low    bool
}

// CalibrationData is the prepared Calibration panel state.
type CalibrationData struct {
	View
	Buckets []tickerdata.Bucket
	Rows    []CalRow
	Sample  int
	Wins    int
	MeetsN  int
	MinN    int
	// Excluded is the number of closed attempts left out of the buckets
	// because the journal recorded no judge score for them.
	Excluded int
	// Empty is true when no closed attempt carried a score, so the panel is
	// showing the zero-observation shape rather than an observation set.
	Empty bool
}

// Calibration prepares the Calibration panel from the journal's attempts.
//
// Nothing here is fixture data: the buckets are derived from the rows the
// console was started with. A ledger whose attempts carry no judge score
// renders the zero-observation shape (every band present, nothing counted)
// instead of borrowing the sample.
func (v View) Calibration() CalibrationData {
	cal := tickerdata.Calibrate(v.Rows)
	buckets := cal.Buckets
	empty := len(buckets) == 0
	if empty {
		buckets = tickerdata.CalEmpty()
	}
	rows := make([]CalRow, 0, len(buckets))
	var n, wins, meets int
	for i, b := range buckets {
		n += b.N
		wins += b.Wins
		if b.N >= v.Limits.MinN {
			meets++
		}
		stated := b.Midpoint(i)
		rate := 0.0
		if b.N > 0 {
			rate = float64(b.Wins) / float64(b.N)
		}
		rows = append(rows, CalRow{
			Label:  b.Label,
			N:      b.N,
			Wins:   b.Wins,
			Stated: stated,
			Rate:   rate,
			Delta:  rate - stated,
			Low:    b.N < v.Limits.MinN,
		})
	}
	return CalibrationData{
		View:     v,
		Buckets:  buckets,
		Rows:     rows,
		Sample:   n,
		Wins:     wins,
		MeetsN:   meets,
		MinN:     v.Limits.MinN,
		Excluded: cal.Excluded,
		Empty:    empty,
	}
}

// RiskData is the prepared Risk state panel state.
type RiskData struct {
	View
	Used      float64
	DailyPct  float64
	Drawdown  float64
	MeanP     float64
	CalN      int
	LastFill  string
	Kills     int
	Blocks    int
	VenueFail int
}

// RiskState prepares the Risk state panel. Its dispatched total and utilisation
// follow the same Caps the shell chip draws: the engine's own spent/remaining
// numbers when a snapshot was read, the journal's rolling notional otherwise.
func (v View) RiskState() RiskData {
	caps := v.Caps()
	dailyPct := caps.Util()
	used := caps.Used
	var kills, blocks, venueFail int
	eq, peak, dd, lastFill := 0.0, 0.0, 0.0, "—"
	var pSum float64
	var pN int
	for _, r := range v.Rows {
		switch r.Outcome {
		case tickerdata.Kill:
			kills++
		case tickerdata.Cap, tickerdata.Judge:
			blocks++
		case tickerdata.Venue:
			venueFail++
		}
		if r.Outcome == tickerdata.Filled {
			eq += r.PnL
			peak = mathMax(peak, eq)
			dd = mathMax(dd, peak-eq)
			lastFill = HMS(r.TS)
		}
		score, scored := r.Score()
		if !scored {
			score = r.P
		}
		pSum += score
		pN++
	}
	meanP := 0.0
	if pN > 0 {
		meanP = pSum / float64(pN)
	}
	// The calibration sample is the set of attempts that actually carry a
	// judge score, so the floor check describes this ledger rather than the
	// retired fixture's total.
	calN := tickerdata.Calibrate(v.Rows).Scored()
	return RiskData{
		View:      v,
		Used:      used,
		DailyPct:  dailyPct,
		Drawdown:  dd,
		MeanP:     meanP,
		CalN:      calN,
		LastFill:  lastFill,
		Kills:     kills,
		Blocks:    blocks,
		VenueFail: venueFail,
	}
}

func mathMax(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
