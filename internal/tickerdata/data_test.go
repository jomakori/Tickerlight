package tickerdata

import (
	"math"
	"testing"
	"time"
)

func TestSampleIsDerivedAndConsistent(t *testing.T) {
	rows := Sample()
	if len(rows) == 0 {
		t.Fatal("Sample() returned no rows")
	}

	seen := make(map[int]bool, len(rows))
	for _, r := range rows {
		if seen[r.ID] {
			t.Errorf("duplicate attempt id %d", r.ID)
		}
		seen[r.ID] = true

		if r.Notional <= 0 {
			t.Errorf("attempt %d has non-positive notional %v", r.ID, r.Notional)
		}
		if r.Qty <= 0 || r.Px <= 0 {
			t.Errorf("attempt %d has non-positive qty/px: %v/%v", r.ID, r.Qty, r.Px)
		}
		if r.CapBefore < 0 || r.CapAfter < 0 {
			t.Errorf("attempt %d has negative cap fields: %v/%v", r.ID, r.CapBefore, r.CapAfter)
		}
		if r.Outcome == Filled {
			if r.Fill == nil {
				t.Errorf("filled attempt %d has no fill price", r.ID)
			}
			if got, want := r.CapAfter, r.CapBefore+r.Notional; !almostEqual(got, want) {
				t.Errorf("filled attempt %d cap after = %v, want %v", r.ID, got, want)
			}
			if r.DispatchID == "" {
				t.Errorf("filled attempt %d has no dispatch id", r.ID)
			}
		}
		if r.Outcome != Filled && r.DispatchID == "" && r.CapAfter != r.CapBefore {
			t.Errorf("attempt %d was not dispatched but moved the rolling cap", r.ID)
		}
	}
}

func TestDeriveIsIdempotent(t *testing.T) {
	rows := Sample()
	once := Derive(rows)
	twice := Derive(once)
	for i := range once {
		if once[i].CapBefore != twice[i].CapBefore || once[i].CapAfter != twice[i].CapAfter {
			t.Fatalf("Derive not idempotent at %d: %+v vs %+v", i, once[i], twice[i])
		}
	}
}

func TestStatsForMatchesRows(t *testing.T) {
	rows := Sample()
	st := StatsFor(rows)

	var wantPnL float64
	var wantFills int
	for _, r := range rows {
		if r.Outcome == Filled {
			wantFills++
			wantPnL += r.PnL
		}
	}
	if st.Filled != wantFills {
		t.Errorf("Filled = %d, want %d", st.Filled, wantFills)
	}
	if st.Total != len(rows) {
		t.Errorf("Total = %d, want %d", st.Total, len(rows))
	}
	if !almostEqual(st.Eq, wantPnL) {
		t.Errorf("Eq = %v, want %v", st.Eq, wantPnL)
	}
	var total int
	for o, n := range st.Counts {
		if n < 0 {
			t.Errorf("negative count for %s", o)
		}
		total += n
	}
	if total != len(rows) {
		t.Errorf("counts sum to %d, want %d", total, len(rows))
	}
	if st.Wins > st.Filled {
		t.Errorf("wins %d exceed fills %d", st.Wins, st.Filled)
	}
}

func TestInRangeWindows(t *testing.T) {
	rows := Sample()
	all := InRange(rows, RangeAll, SampleNow)
	if len(all) != len(rows) {
		t.Errorf("RangeAll returned %d of %d rows", len(all), len(rows))
	}

	day := InRange(rows, Range24h, SampleNow)
	if len(day) > len(all) {
		t.Fatalf("24h window larger than all: %d > %d", len(day), len(all))
	}
	cutoff := SampleNow.Add(-24 * time.Hour)
	for _, r := range day {
		if r.TS.Before(cutoff) {
			t.Errorf("row %d at %s leaked into the 24h window", r.ID, r.TS)
		}
	}

	today := InRange(rows, RangeToday, SampleNow)
	for _, r := range today {
		y1, m1, d1 := r.TS.UTC().Date()
		y2, m2, d2 := SampleNow.UTC().Date()
		if y1 != y2 || m1 != m2 || d1 != d2 {
			t.Errorf("row %d at %s leaked into today", r.ID, r.TS)
		}
	}
}

func TestParseRange(t *testing.T) {
	cases := map[string]Range{
		"":         RangeAll,
		"all":      RangeAll,
		"24h":      Range24h,
		"today":    RangeToday,
		"nonsense": RangeAll,
		// Range values are matched case-insensitively (and trimmed), so a
		// hand-typed ?range=24H still selects the 24h window.
		"24H":          Range24h,
		"  today ":     RangeToday,
		"today?from=x": RangeAll,
	}
	for in, want := range cases {
		if got := ParseRange(in); got != want {
			t.Errorf("ParseRange(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFilterRows(t *testing.T) {
	rows := Sample()

	capOnly := FilterRows(rows, Filter{Outcome: string(Cap)})
	if len(capOnly) == 0 {
		t.Fatal("expected cap-blocked rows in the sample ledger")
	}
	for _, r := range capOnly {
		if r.Outcome != Cap {
			t.Errorf("outcome filter leaked %s", r.Outcome)
		}
	}

	blocked := FilterRows(rows, Filter{Outcome: "blocked"})
	for _, r := range blocked {
		if r.Outcome.Meta().Kind != KindBlk {
			t.Errorf("blocked filter leaked %s", r.Outcome)
		}
	}

	venue := rows[0].Venue
	byVenue := FilterRows(rows, Filter{Venue: venue})
	if len(byVenue) == 0 {
		t.Fatalf("venue filter %q matched nothing", venue)
	}
	for _, r := range byVenue {
		if r.Venue != venue {
			t.Errorf("venue filter leaked %s", r.Venue)
		}
	}

	sym := rows[0].Symbol
	bySym := FilterRows(rows, Filter{Symbol: sym})
	for _, r := range bySym {
		if r.Symbol != sym {
			t.Errorf("symbol filter leaked %s", r.Symbol)
		}
	}

	from := SampleNow.Add(-6 * time.Hour).Format("2006-01-02")
	dated := FilterRows(rows, Filter{From: from})
	for _, r := range dated {
		if r.TS.UTC().Format("2006-01-02") < from {
			t.Errorf("from filter leaked %s", r.TS)
		}
	}

	if n := len(FilterRows(rows, Filter{})); n != len(rows) {
		t.Errorf("empty filter returned %d of %d rows", n, len(rows))
	}
	// Unknown outcome values match nothing rather than silently widening the tape.
	if n := len(FilterRows(rows, Filter{Outcome: "does-not-exist"})); n != 0 {
		t.Errorf("unknown outcome matched %d rows, want none", n)
	}
}

func TestFilterIsZero(t *testing.T) {
	if !(Filter{}).IsZero() {
		t.Error("zero filter reported as non-zero")
	}
	// The doc contract is "empty or all means unfiltered", and Matches
	// treats both the same way, so both spellings report zero.
	if !(Filter{Outcome: "all"}).IsZero() {
		t.Error("outcome=all reported as an active filter, want unfiltered")
	}
	if (Filter{Outcome: "filled"}).IsZero() {
		t.Error("outcome=filled reported as no filter")
	}
	// Every clause counts: a venue-only or date-only filter is active even
	// when the outcome clause is unset.
	active := []Filter{
		{Venue: "alpaca-paper"},
		{Symbol: "AAPL"},
		{From: "2026-10-29"},
		{To: "2026-10-30"},
		{Outcome: "all", Venue: "alpaca-paper"},
	}
	for _, f := range active {
		if f.IsZero() {
			t.Errorf("filter %+v reported as no filter", f)
		}
	}
}

func TestRollingUsedIgnoresStaleAndUndispatched(t *testing.T) {
	now := SampleNow
	rows := []Attempt{
		{ID: 1, TS: now.Add(-1 * time.Hour), Notional: 100, Outcome: Filled, DispatchID: "D1"},
		{ID: 2, TS: now.Add(-30 * time.Hour), Notional: 200, Outcome: Filled, DispatchID: "D2"},
		{ID: 3, TS: now.Add(-2 * time.Hour), Notional: 50, Outcome: Cap},
	}
	if got := RollingUsed(rows, now); !almostEqual(got, 100) {
		t.Errorf("RollingUsed = %v, want 100", got)
	}
}

func TestByID(t *testing.T) {
	rows := Sample()
	first := rows[0]
	got, ok := ByID(rows, first.ID)
	if !ok || got.ID != first.ID {
		t.Fatalf("ByID(%d) = %+v, %v", first.ID, got, ok)
	}
	if _, ok := ByID(rows, 9999); ok {
		t.Error("ByID found a row that does not exist")
	}
}

func TestVenuesAndSymbolsAreSortedUnique(t *testing.T) {
	rows := Sample()
	for name, list := range map[string][]string{"venues": Venues(rows), "symbols": Symbols(rows)} {
		if len(list) == 0 {
			t.Fatalf("%s empty", name)
		}
		for i := 1; i < len(list); i++ {
			if list[i-1] >= list[i] {
				t.Errorf("%s not sorted/unique: %q then %q", name, list[i-1], list[i])
			}
		}
	}
}

func TestCalibrationBuckets(t *testing.T) {
	buckets := CalSample()
	if len(buckets) == 0 {
		t.Fatal("no calibration buckets")
	}
	var n, wins int
	for i, b := range buckets {
		n += b.N
		wins += b.Wins
		if b.Wins > b.N {
			t.Errorf("bucket %q has more wins than calls", b.Label)
		}
		if mid := b.Midpoint(i); mid <= 0 || mid >= 1 {
			t.Errorf("bucket %q midpoint %v out of range", b.Label, mid)
		}
	}
	if n == 0 || wins == 0 {
		t.Error("calibration sample is empty")
	}
	if buckets[0].Midpoint(-1) != 0 || buckets[0].Midpoint(len(buckets)) != 0 {
		t.Error("out-of-range midpoint should be zero")
	}

	// CalEmpty is the zero-observation shape: every tenth of the range present
	// and nothing counted, so the page can draw the frame without inventing
	// observations.
	empty := CalEmpty()
	if len(empty) != 10 {
		t.Fatalf("CalEmpty has %d bands, want 10", len(empty))
	}
	for i := range empty {
		if empty[i].N != 0 || empty[i].Wins != 0 {
			t.Errorf("CalEmpty bucket %q not zeroed", empty[i].Label)
		}
		if mid := empty[i].Midpoint(i); mid <= 0 || mid >= 1 {
			t.Errorf("CalEmpty bucket %q midpoint %v out of range", empty[i].Label, mid)
		}
		if empty[i].High <= empty[i].Low {
			t.Errorf("CalEmpty bucket %q has no bounds", empty[i].Label)
		}
	}
}

func TestOutcomeMetaCoversEveryOutcome(t *testing.T) {
	for _, o := range []Outcome{Filled, Cap, Kill, Judge, Venue} {
		m := o.Meta()
		if m.Label == "" || m.Icon == "" {
			t.Errorf("outcome %q has incomplete meta: %+v", o, m)
		}
		if m.Kind == "" {
			t.Errorf("outcome %q has no kind", o)
		}
	}
	// A fill clears every stage, so Stage reports "" (Stop = -1) rather than
	// the last stage name.
	if stage := Filled.Stage(); stage != "" {
		t.Errorf("filled cleared every stage, got stage %q", stage)
	}
	if stage := Cap.Stage(); stage != "gate" {
		t.Errorf("cap stage = %q, want gate", stage)
	}
}

func TestDefaultLimits(t *testing.T) {
	l := DefaultLimits()
	if l.Daily <= 0 || l.PerOrder <= 0 || l.PerOrder >= l.Daily {
		t.Errorf("incoherent caps: %+v", l)
	}
	if l.JudgeMin <= 0 || l.JudgeMin >= 1 {
		t.Errorf("judge threshold out of range: %v", l.JudgeMin)
	}
	if l.MinN <= 0 || l.CooldownS <= 0 || l.Drawdown <= 0 {
		t.Errorf("non-positive limits: %+v", l)
	}
}

func almostEqual(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

// confidence is a recorded judge score, the shape the attempt carries.
func confidence(p float64) *float64 { return &p }

func TestCalibrateBucketsConfidenceAndRealizedWinRate(t *testing.T) {
	rows := []Attempt{
		{ID: 1, Outcome: Filled, Confidence: confidence(0.05)}, // band 0, win
		{ID: 2, Outcome: Venue, Confidence: confidence(0.05)},  // band 0, loss
		{ID: 3, Outcome: Filled, Confidence: confidence(0.1)},  // band 1 (lower edge), win
		{ID: 4, Outcome: Filled, Confidence: confidence(0.5)},  // band 5 (lower edge), win
		{ID: 5, Outcome: Venue, Confidence: confidence(0.5)},   // band 5, loss
		{ID: 6, Outcome: Venue, Confidence: confidence(0.9)},   // band 9 (lower edge), loss
		{ID: 7, Outcome: Filled, Confidence: confidence(1)},    // band 9 (upper edge), win
		{ID: 8, Outcome: Filled},                               // no score: excluded
		{ID: 9, Outcome: Outcome("pending")},                   // not closed: not a bucket input
		// A gate-blocked attempt carries a score but never reached a venue, so
		// it must not move any bucket's win rate.
		{ID: 10, Outcome: Cap, Confidence: confidence(0.55)},
		{ID: 11, Outcome: Kill, Confidence: confidence(0.05)},
	}

	cal := Calibrate(rows)

	want := []struct {
		label     string
		low, high float64
		n, wins   int
		midpoint  float64
		winRate   float64
	}{
		{"0.00–0.10", 0, 0.1, 2, 1, 0.05, 0.5},
		{"0.10–0.20", 0.1, 0.2, 1, 1, 0.15, 1},
		{"0.50–0.60", 0.5, 0.6, 2, 1, 0.55, 0.5},
		{"0.90–1.00", 0.9, 1, 2, 1, 0.95, 0.5},
	}
	if len(cal.Buckets) != len(want) {
		t.Fatalf("buckets = %+v, want %d non-empty buckets", cal.Buckets, len(want))
	}
	for i, w := range want {
		b := cal.Buckets[i]
		if b.Label != w.label || b.Low != w.low || b.High != w.high {
			t.Errorf("bucket %d = %q [%v,%v), want %q [%v,%v)", i, b.Label, b.Low, b.High, w.label, w.low, w.high)
		}
		if b.N != w.n || b.Wins != w.wins {
			t.Errorf("bucket %q n/wins = %d/%d, want %d/%d", b.Label, b.N, b.Wins, w.n, w.wins)
		}
		if mid := b.Midpoint(i); math.Abs(mid-w.midpoint) > 1e-9 {
			t.Errorf("bucket %q midpoint = %v, want %v", b.Label, mid, w.midpoint)
		}
		if rate := float64(b.Wins) / float64(b.N); rate != w.winRate {
			t.Errorf("bucket %q win rate = %v, want %v", b.Label, rate, w.winRate)
		}
	}
	if cal.Excluded != 1 {
		t.Errorf("Excluded = %d, want the single closed attempt with no score", cal.Excluded)
	}
	if cal.Scored() != 7 {
		t.Errorf("Scored = %d, want 7", cal.Scored())
	}
}

func TestCalibrateSaturatesOutOfRangeConfidence(t *testing.T) {
	cal := Calibrate([]Attempt{
		{ID: 1, Outcome: Filled, Confidence: confidence(-0.4)},
		{ID: 2, Outcome: Venue, Confidence: confidence(1.7)},
	})
	if len(cal.Buckets) != 2 {
		t.Fatalf("buckets = %+v, want the two edge bands", cal.Buckets)
	}
	if cal.Buckets[0].Label != "0.00–0.10" || cal.Buckets[1].Label != "0.90–1.00" {
		t.Errorf("out-of-range scores landed in %q/%q, want the edge bands", cal.Buckets[0].Label, cal.Buckets[1].Label)
	}
}

func TestCalibrateWithNoScoresIsEmpty(t *testing.T) {
	for name, rows := range map[string][]Attempt{
		"no rows":   nil,
		"unscored":  {{ID: 1, Outcome: Filled}, {ID: 2, Outcome: Cap}},
		"open only": {{ID: 3, Outcome: Outcome("pending"), Confidence: confidence(0.6)}},
	} {
		cal := Calibrate(rows)
		if len(cal.Buckets) != 0 || cal.Scored() != 0 {
			t.Errorf("%s: buckets = %+v, want none", name, cal.Buckets)
		}
	}
	if cal := Calibrate([]Attempt{{ID: 1, Outcome: Filled}}); cal.Excluded != 1 {
		t.Errorf("Excluded = %d, want the unscored closed attempt counted", cal.Excluded)
	}
}
