package decider

import (
	"time"
	// Embedded zone database: the console image is a bare alpine rootfs with no
	// tzdata package, so LoadLocation would panic there.
	_ "time/tzdata"
)

// nyLocation is loaded once; a missing tzdata database is a startup
// error, not a silent UTC fallback, because market hours computed against
// the wrong zone would misfire by hours.
var nyLocation = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic("decider: load America/New_York: " + err.Error())
	}
	return loc
}()

// MarketOpen reports whether US equities are open at t: Monday-Friday,
// 09:30 (inclusive) to 16:00 (exclusive), America/New_York. It does not
// account for market holidays.
func MarketOpen(t time.Time) bool {
	local := t.In(nyLocation)
	switch local.Weekday() {
	case time.Saturday, time.Sunday:
		return false
	}
	open := time.Date(local.Year(), local.Month(), local.Day(), 9, 30, 0, 0, nyLocation)
	close := time.Date(local.Year(), local.Month(), local.Day(), 16, 0, 0, 0, nyLocation)
	return !local.Before(open) && local.Before(close)
}
