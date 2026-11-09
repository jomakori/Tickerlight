package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/jomakori/tickerlight/internal/tickerdata"
)

func mustTS(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

func TestUSDRendersMoney(t *testing.T) {
	for _, c := range []struct {
		in   float64
		want string
	}{
		{0, "$0.00"},
		{12.4, "$12.40"},
		{-8.15, "−$8.15"},
		{1234.5, "$1,234.50"},
		{1234567.89, "$1,234,567.89"},
		{1.999, "$2.00"}, // cents must carry, never print "1.100"
	} {
		if got := USD(c.in); got != c.want {
			t.Errorf("USD(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUSDSignedAlwaysSigns(t *testing.T) {
	for _, c := range []struct {
		in   float64
		want string
	}{
		{12.4, "+$12.40"},
		{-8.15, "−$8.15"},
		{0, "$0.00"},
	} {
		if got := USDSigned(c.in); got != c.want {
			t.Errorf("USDSigned(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPct(t *testing.T) {
	for _, c := range []struct {
		in   float64
		d    int
		want string
	}{
		{0, 0, "0%"},
		{0.69, 0, "69%"},
		{0.1234, 1, "12.3%"},
		{1.0, 0, "100%"},
		{0.05, 0, "5%"},
	} {
		if got := Pct(c.in, c.d); got != c.want {
			t.Errorf("Pct(%v, %d) = %q, want %q", c.in, c.d, got, c.want)
		}
	}
}

func TestQtyPxPad4(t *testing.T) {
	for in, want := range map[float64]string{1: "1", 10: "10", 0.25: "0.25", 2.5: "2.5", 0.5: "0.50"} {
		if got := Qty(in); got != want {
			t.Errorf("Qty(%v) = %q, want %q", in, got, want)
		}
	}
	if got := Px(187.4); got != "$187.40" {
		t.Errorf("Px(187.4) = %q", got)
	}
	if got := Pad4(4); got != "0004" {
		t.Errorf("Pad4(4) = %q", got)
	}
	if got := Pad4(12345); got != "12345" {
		t.Errorf("Pad4(12345) = %q", got)
	}
}

func TestClocks(t *testing.T) {
	ts := mustTS(t, "2026-10-30T09:12:04Z")
	if got := HMS(ts); got != "09:12:04Z" {
		t.Errorf("HMS = %q", got)
	}
	if got := MD(ts); got != "10-30" {
		t.Errorf("MD = %q", got)
	}
}

func TestCooldownText(t *testing.T) {
	for in, want := range map[int]string{0: "READY", -5: "READY", 65: "01:05", 720: "12:00"} {
		if got := CooldownText(in); got != want {
			t.Errorf("CooldownText(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestReasonTextNamesTheActualGate(t *testing.T) {
	lim := tickerdata.DefaultLimits()
	// An order above the per-order cap must say so, not blame the rolling cap.
	overOrder := ReasonText(tickerdata.Attempt{Outcome: tickerdata.Cap, Notional: lim.PerOrder + 100})
	if !strings.Contains(overOrder, "per-order cap") {
		t.Errorf("cap reason = %q, want the per-order cap named", overOrder)
	}
	rolling := ReasonText(tickerdata.Attempt{Outcome: tickerdata.Cap, Notional: lim.PerOrder})
	if !strings.Contains(rolling, "rolling-24h cap") || strings.Contains(rolling, "per-order") {
		t.Errorf("cap reason = %q, want only the rolling-24h cap", rolling)
	}
	judge := ReasonText(tickerdata.Attempt{Outcome: tickerdata.Judge, P: 0.42})
	if !strings.Contains(judge, "0.42") || !strings.Contains(judge, "0.60") {
		t.Errorf("judge reason = %q, want both p and threshold", judge)
	}
	venue := ReasonText(tickerdata.Attempt{Outcome: tickerdata.Venue, Venue: "binance-testnet", Detail: "insufficient margin"})
	if !strings.Contains(venue, "BINANCE") || !strings.Contains(venue, "insufficient margin") {
		t.Errorf("venue reason = %q, want venue and detail", venue)
	}
	kill := ReasonText(tickerdata.Attempt{Outcome: tickerdata.Kill, Detail: "operator halt"})
	if !strings.Contains(kill, "kill switch") || !strings.Contains(kill, "operator halt") {
		t.Errorf("kill reason = %q", kill)
	}
	if got := ReasonText(tickerdata.Attempt{Outcome: tickerdata.Filled}); got != "Filled" {
		t.Errorf("filled reason = %q", got)
	}
}

// A row read from the real journal carries the server's own reason code and
// no probability. The console must repeat that code rather than compute a
// cap sentence from the sample policy, which would name a limit that was
// never in force.
func TestReasonTextPrefersTheJournalledReason(t *testing.T) {
	capRow := ReasonText(tickerdata.Attempt{
		Outcome:  tickerdata.Cap,
		Notional: 1200,
		Detail:   "position_size_cap_exceeded",
	})
	if !strings.Contains(capRow, "position_size_cap_exceeded") {
		t.Fatalf("cap reason = %q, want the journalled reason code", capRow)
	}
	if strings.Contains(capRow, "$500") {
		t.Fatalf("cap reason = %q, want no invented sample limit", capRow)
	}

	judgeRow := ReasonText(tickerdata.Attempt{
		Outcome: tickerdata.Judge,
		Detail:  "judge_below_threshold",
	})
	if !strings.Contains(judgeRow, "judge_below_threshold") {
		t.Fatalf("judge reason = %q, want the journalled reason code", judgeRow)
	}
	if strings.Contains(judgeRow, "p=") {
		t.Fatalf("judge reason = %q, want no fabricated probability", judgeRow)
	}
}

func TestStageStatesWalkTheChain(t *testing.T) {
	for _, c := range []struct {
		o    tickerdata.Outcome
		want []string
	}{
		{tickerdata.Filled, []string{"passed", "passed", "passed", "passed"}},
		{tickerdata.Judge, []string{"passed", "blocked", "not reached", "not reached"}},
		{tickerdata.Cap, []string{"passed", "passed", "blocked", "not reached"}},
		{tickerdata.Kill, []string{"passed", "passed", "blocked", "not reached"}},
		{tickerdata.Venue, []string{"passed", "passed", "passed", "failed"}},
	} {
		got := StageStates(c.o)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: StageStates = %v, want %v", c.o, got, c.want)
		}
	}
}

func TestJournalURLCarriesOnlyRealFilterState(t *testing.T) {
	for _, c := range []struct {
		name string
		f    tickerdata.Filter
		sel  int
		want string
	}{
		{"empty", tickerdata.Filter{}, 0, "/journal"},
		{"all is the default", tickerdata.Filter{Outcome: "all", Venue: "all", Symbol: "all"}, 0, "/journal"},
		{"one filter", tickerdata.Filter{Outcome: "filled"}, 0, "/journal?outcome=filled"},
		{"selection", tickerdata.Filter{}, 4, "/journal?sel=4"},
		{"filters and selection", tickerdata.Filter{Outcome: "cap", Venue: "alpaca-paper"}, 7, "/journal?outcome=cap&sel=7&venue=alpaca-paper"},
		{"dates", tickerdata.Filter{From: "2026-10-01", To: "2026-10-31"}, 0, "/journal?from=2026-10-01&to=2026-10-31"},
	} {
		if got := JournalURL(c.f, c.sel); got != c.want {
			t.Errorf("%s: JournalURL = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDetailURLCarriesTheFilter(t *testing.T) {
	f := tickerdata.Filter{Outcome: "venue", Symbol: "BTCUSDT"}
	got, want := DetailURL(f, 9), "/journal?outcome=venue&sel=9&symbol=BTCUSDT"
	if got != want {
		t.Errorf("DetailURL = %q, want %q", got, want)
	}
}

func TestRangeURL(t *testing.T) {
	for in, want := range map[tickerdata.Range]string{
		tickerdata.RangeAll:   "/",
		tickerdata.Range24h:   "/?range=24h",
		tickerdata.RangeToday: "/?range=today",
	} {
		if got := RangeURL(in); got != want {
			t.Errorf("RangeURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFilterCopyDoesNotMutate(t *testing.T) {
	orig := tickerdata.Filter{Outcome: "filled", Venue: "alpaca-paper", Symbol: "AAPL"}
	got := FilterCopy(orig, "blocked")
	if got.Outcome != "blocked" {
		t.Errorf("Outcome = %q, want blocked", got.Outcome)
	}
	if got.Venue != orig.Venue || got.Symbol != orig.Symbol {
		t.Errorf("FilterCopy dropped state: %+v", got)
	}
	if orig.Outcome != "filled" {
		t.Errorf("FilterCopy mutated its argument: %+v", orig)
	}
}

func TestNewViewDefaults(t *testing.T) {
	now := mustTS(t, "2026-10-30T14:00:00Z")
	v := NewView(tickerdata.Sample(), now)
	if v.Tab != TabOverview || v.Range != tickerdata.RangeAll {
		t.Errorf("defaults = tab %q range %q", v.Tab, v.Range)
	}
	// Both session flags are fixed off in this build: there is no order path
	// and the arm/kill controls are inert, so the console must report
	// disarmed and released rather than implying a live, armed session.
	if v.Armed || v.Kill {
		t.Errorf("session flags = armed %v kill %v, want both false in the paper-only build", v.Armed, v.Kill)
	}
	if v.Limits.Daily <= 0 || v.Limits.PerOrder <= 0 || v.Limits.MinN <= 0 {
		t.Errorf("limits not populated: %+v", v.Limits)
	}
	if len(v.Rows) != len(tickerdata.Sample()) {
		t.Errorf("rows = %d, want the fixture ledger", len(v.Rows))
	}
}

func TestOverviewAggregates(t *testing.T) {
	now := tickerdata.SampleNow
	v := NewView(tickerdata.Sample(), now)
	d := v.Overview()

	if d.AllLen != len(v.Rows) {
		t.Errorf("AllLen = %d, want %d", d.AllLen, len(v.Rows))
	}
	if len(d.Rows) != len(v.InRange()) {
		t.Errorf("range rows = %d, want %d", len(d.Rows), len(v.InRange()))
	}
	if d.Used != v.RollingUsed() {
		t.Errorf("Used = %v, want %v", d.Used, v.RollingUsed())
	}
	if d.Stats.Eq != tickerdata.StatsFor(d.Rows).Eq {
		t.Errorf("Stats.Eq = %v, want the range stats", d.Stats.Eq)
	}

	var day float64
	var blocked int
	for _, r := range v.Rows {
		if r.Outcome == tickerdata.Filled && !r.TS.After(now) && now.Sub(r.TS) <= 24*time.Hour {
			day += r.PnL
		}
		if r.Outcome.Meta().Kind == tickerdata.KindBlk {
			blocked++
		}
	}
	if d.DayPnL != day {
		t.Errorf("DayPnL = %v, want %v", d.DayPnL, day)
	}
	// Blocked counts follow the range, so recompute over d.Rows.
	blocked = 0
	for _, r := range d.Rows {
		if r.Outcome.Meta().Kind == tickerdata.KindBlk {
			blocked++
		}
	}
	if d.Blocked != blocked {
		t.Errorf("Blocked = %d, want %d", d.Blocked, blocked)
	}
	if d.WinRate() != float64(d.Stats.Wins)/float64(d.Stats.Filled) {
		t.Errorf("WinRate = %v", d.WinRate())
	}
}

func TestWinRateOfEmptyLedgerIsZeroNotNaN(t *testing.T) {
	v := NewView(nil, tickerdata.SampleNow)
	if got := v.Overview().WinRate(); got != 0 {
		t.Errorf("WinRate = %v, want 0 (must not be NaN)", got)
	}
}

func TestRecentIsNewestFirstAndCapped(t *testing.T) {
	v := NewView(tickerdata.Sample(), tickerdata.SampleNow)
	d := v.Overview()
	got := d.Recent(6)
	if len(got) != 6 {
		t.Fatalf("len(Recent(6)) = %d, want 6", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].TS.After(got[i-1].TS) {
			t.Fatalf("Recent not newest-first at %d", i)
		}
	}
	if all := d.Recent(999); len(all) != len(d.Rows) {
		t.Errorf("Recent(999) = %d rows, want %d", len(all), len(d.Rows))
	}
	if empty := d.Recent(0); len(empty) != 0 {
		t.Errorf("Recent(0) = %d rows, want 0", len(empty))
	}
}

func TestOverviewWithinRangeWarning(t *testing.T) {
	v := NewView(tickerdata.Sample(), tickerdata.SampleNow)
	v.Range = tickerdata.Range24h
	if got, all := len(v.Overview().Rows), len(v.Rows); got >= all {
		t.Errorf("24h range kept %d of %d rows; the fixture should exercise the filter", got, all)
	}
}

func TestCalibrationDataDerivesRows(t *testing.T) {
	v := NewView(tickerdata.Sample(), tickerdata.SampleNow)
	d := v.Calibration()

	if d.MinN != v.Limits.MinN {
		t.Errorf("MinN = %d, want %d", d.MinN, v.Limits.MinN)
	}
	if len(d.Rows) != len(d.Buckets) || len(d.Rows) == 0 {
		t.Fatalf("rows = %d, buckets = %d", len(d.Rows), len(d.Buckets))
	}
	var n, wins, meets int
	for i, row := range d.Rows {
		n += row.N
		wins += row.Wins
		if row.N >= d.MinN {
			meets++
		}
		if row.Label != d.Buckets[i].Label {
			t.Errorf("row %d label = %q, want %q", i, row.Label, d.Buckets[i].Label)
		}
		wantRate := 0.0
		if row.N > 0 {
			wantRate = float64(row.Wins) / float64(row.N)
		}
		if row.Rate != wantRate {
			t.Errorf("row %d rate = %v, want %v", i, row.Rate, wantRate)
		}
		if wantStated := d.Buckets[i].Midpoint(i); row.Stated != wantStated {
			t.Errorf("row %d stated = %v, want %v", i, row.Stated, wantStated)
		}
		if diff := row.Delta - (row.Rate - row.Stated); diff > 1e-9 || diff < -1e-9 {
			t.Errorf("row %d delta = %v, want rate-stated", i, row.Delta)
		}
		if row.Low != (row.N < d.MinN) {
			t.Errorf("row %d Low = %v with n=%d, floor=%d", i, row.Low, row.N, d.MinN)
		}
	}
	if d.Sample != n || d.Wins != wins || d.MeetsN != meets {
		t.Errorf("totals = sample %d wins %d meets %d, want %d/%d/%d",
			d.Sample, d.Wins, d.MeetsN, n, wins, meets)
	}
}

func TestRiskStateCountsEveryGate(t *testing.T) {
	v := NewView(tickerdata.Sample(), tickerdata.SampleNow)
	d := v.RiskState()

	var kills, blocks, venueFail int
	for _, r := range v.Rows {
		switch r.Outcome {
		case tickerdata.Kill:
			kills++
		case tickerdata.Cap, tickerdata.Judge:
			blocks++
		case tickerdata.Venue:
			venueFail++
		}
	}
	if d.Kills != kills || d.Blocks != blocks || d.VenueFail != venueFail {
		t.Errorf("counts = kills %d blocks %d venue %d, want %d/%d/%d",
			d.Kills, d.Blocks, d.VenueFail, kills, blocks, venueFail)
	}
	if d.Drawdown <= 0 {
		t.Errorf("Drawdown = %v, want the ledger's max drawdown", d.Drawdown)
	}
	if d.Used != v.RollingUsed() {
		t.Errorf("Used = %v, want %v", d.Used, v.RollingUsed())
	}
	if want := v.RollingUsed() / v.Limits.Daily; d.DailyPct != want {
		t.Errorf("DailyPct = %v, want %v", d.DailyPct, want)
	}
	if d.CalN != 103 {
		t.Errorf("CalN = %d, want 103", d.CalN)
	}
	var lastFilled time.Time
	var pSum float64
	for _, r := range v.Rows {
		pSum += r.P
		if r.Outcome == tickerdata.Filled {
			lastFilled = r.TS
		}
	}
	if d.LastFill != HMS(lastFilled) {
		t.Errorf("LastFill = %q, want %q", d.LastFill, HMS(lastFilled))
	}
	if want := pSum / float64(len(v.Rows)); (d.MeanP-want) > 1e-9 || (d.MeanP-want) < -1e-9 {
		t.Errorf("MeanP = %v, want %v", d.MeanP, want)
	}
}

func TestRiskStateWithoutLimitsDoesNotDivideByZero(t *testing.T) {
	v := NewView(tickerdata.Sample(), tickerdata.SampleNow)
	v.Limits.Daily = 0
	if got := v.RiskState().DailyPct; got != 0 {
		t.Errorf("DailyPct with no cap = %v, want 0", got)
	}
}

func TestRiskChecksReflectState(t *testing.T) {
	v := NewView(tickerdata.Sample(), tickerdata.SampleNow)
	checks := riskChecks(v.RiskState())
	if len(checks) != 6 {
		t.Fatalf("len(checks) = %d, want the six pre-trade checks", len(checks))
	}
	wantLabels := []string{
		"rolling 24h within daily cap",
		"drawdown within limit",
		"judge score above threshold",
		"calibration sample above floor",
		"symbol allowlisted",
		"arming flag set",
	}
	for i, want := range wantLabels {
		if checks[i].Label != want {
			t.Errorf("check %d label = %q, want %q", i, checks[i].Label, want)
		}
		if checks[i].Value == "" {
			t.Errorf("check %d (%s) has no observed value", i, checks[i].Label)
		}
	}
	// The ledger side of the six checks passes on the sample fixture; the
	// arming flag does not, because the default view is disarmed.
	if !checks[0].OK || !checks[1].OK || !checks[2].OK || !checks[3].OK || !checks[4].OK {
		t.Errorf("sample ledger should pass cap/drawdown/judge/calibration/symbols: %+v", checks)
	}
	if checks[5].OK || !strings.Contains(checks[5].Value, "DISARMED") {
		t.Errorf("arming check = %+v, want a failing DISARMED line on the default view", checks[5])
	}

	// Arming is view state; the check must follow it, not hard-code FAIL.
	armed := NewView(tickerdata.Sample(), tickerdata.SampleNow)
	armed.Armed = true
	if c := riskChecks(armed.RiskState())[5]; !c.OK || !strings.Contains(c.Value, "ARMED") {
		t.Errorf("arming check with Armed=true = %+v, want a passing ARMED line", c)
	}

	// A blown drawdown must flip its check.
	blown := NewView(tickerdata.Sample(), tickerdata.SampleNow)
	d := blown.RiskState()
	d.Drawdown = d.Limits.Drawdown + 1
	if c := riskChecks(d)[1]; c.OK {
		t.Errorf("drawdown check = %+v, want failing when drawdown exceeds the limit", c)
	}

	// A cap breach must flip the daily check.
	over := d
	over.Used = d.Limits.Daily + 1
	if c := riskChecks(over)[0]; c.OK {
		t.Errorf("daily check = %+v, want failing when utilisation exceeds the cap", c)
	}

	// A judge mean under threshold must flip the judge check.
	lowP := d
	lowP.MeanP = d.Limits.JudgeMin - 0.01
	if c := riskChecks(lowP)[2]; c.OK {
		t.Errorf("judge check = %+v, want failing below threshold", c)
	}
}

func TestStagesOfCoversEveryStage(t *testing.T) {
	rows := tickerdata.Sample()
	for _, r := range rows {
		stages := StagesOf(r)
		if len(stages) != len(tickerdata.Stages) {
			t.Fatalf("attempt %d: %d stages, want %d", r.ID, len(stages), len(tickerdata.Stages))
		}
		for i, st := range stages {
			if st.Name == "" || st.Class == "" {
				t.Errorf("attempt %d stage %d: %+v", r.ID, i, st)
			}
			if len(st.KV) == 0 {
				t.Errorf("attempt %d stage %d (%s) has no values", r.ID, i, st.Name)
			}
		}
		// The stopping stage must carry the plain-language reason.
		stop := r.Outcome.Meta().Stop
		if stop >= 0 {
			if stages[stop].Reason == "" {
				t.Errorf("attempt %d (%s) stage %d has no reason", r.ID, r.Outcome, stop)
			}
			if stages[stop].Class != "stop-blk" && stages[stop].Class != "stop-bad" {
				t.Errorf("attempt %d stopping stage class = %q", r.ID, stages[stop].Class)
			}
		}
	}
}

func TestStagesOfUnreachedStagesSayWhy(t *testing.T) {
	blocked := tickerdata.Attempt{Outcome: tickerdata.Judge, P: 0.4, Qty: 1, Px: 10}
	stages := StagesOf(blocked)
	for i := 2; i < len(stages); i++ {
		if stages[i].Class != "nr" {
			t.Fatalf("stage %d class = %q, want nr", i, stages[i].Class)
		}
	}
	// The explanation is written once, on the first stage the chain never
	// reached, so the rest of the tail does not repeat the same sentence.
	if !strings.Contains(stages[2].Reason, "Not reached") {
		t.Errorf("first unreached stage reason = %q, want an explanation", stages[2].Reason)
	}
	if !strings.Contains(stages[2].Reason, "judge") {
		t.Errorf("first unreached stage reason = %q, want it to name the judge stage", stages[2].Reason)
	}
	for i := 3; i < len(stages); i++ {
		if stages[i].Reason != "" {
			t.Errorf("stage %d reason = %q, want it left blank after the first explanation", i, stages[i].Reason)
		}
	}
}

func TestChipAndOptionHelpers(t *testing.T) {
	if !chipOn("", "all") || !chipOn("all", "all") || chipOn("cap", "all") || !chipOn("cap", "cap") {
		t.Error("chipOn does not treat empty as all")
	}
	if !sel("", "all") || !sel("cap", "cap") || sel("filled", "cap") {
		t.Error("sel mismatch")
	}
	if got := FilterOutcomeLabel("nonsense"); got != "All outcomes" {
		t.Errorf("FilterOutcomeLabel(unknown) = %q", got)
	}
	if got := FilterOutcomeLabel("venue"); got != "Venue rejected" {
		t.Errorf("FilterOutcomeLabel(venue) = %q", got)
	}
	if len(outcomeOptions()) != 6 || len(chipOptions()) != 7 {
		t.Errorf("options = %d outcomes, %d chips", len(outcomeOptions()), len(chipOptions()))
	}
	if got := currentPage(true); got != "page" {
		t.Errorf("currentPage(true) = %q", got)
	}
	if got := currentPage(false); got != "false" {
		t.Errorf("currentPage(false) = %q", got)
	}
	if got := tabURL(TabOverview); got != "/" {
		t.Errorf("tabURL(overview) = %q", got)
	}
	if got := tabURL(TabRisk); got != "/risk" {
		t.Errorf("tabURL(risk) = %q", got)
	}
	if boolStr(true) != "true" || boolStr(false) != "false" {
		t.Error("boolStr")
	}
	if boolWord(false) != "DISARMED" || dashIfEmpty("") != "—" || dashIfEmpty("x") != "x" {
		t.Error("small render helpers")
	}
}
