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
}

// Calibration prepares the Calibration panel.
func (v View) Calibration() CalibrationData {
	buckets := tickerdata.CalSample()
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
	return CalibrationData{View: v, Buckets: buckets, Rows: rows, Sample: n, Wins: wins, MeetsN: meets, MinN: v.Limits.MinN}
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

// RiskState prepares the Risk state panel.
func (v View) RiskState() RiskData {
	dailyPct := 0.0
	if v.Limits.Daily > 0 {
		dailyPct = v.RollingUsed() / v.Limits.Daily
	}
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
		pSum += r.P
		pN++
	}
	meanP := 0.0
	if pN > 0 {
		meanP = pSum / float64(pN)
	}
	calN := 0
	for _, b := range tickerdata.CalSample() {
		calN += b.N
	}
	return RiskData{
		View:      v,
		Used:      v.RollingUsed(),
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
