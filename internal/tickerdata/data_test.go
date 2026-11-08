package tickerdata

import (
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

	empty := CalEmpty()
	for i := range empty {
		if empty[i].N != 0 || empty[i].Wins != 0 {
			t.Errorf("CalEmpty bucket %q not zeroed", empty[i].Label)
		}
		if empty[i].Label != buckets[i].Label {
			t.Errorf("CalEmpty changed bucket labels")
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
