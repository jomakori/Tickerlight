package server

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jomakori/tickerlight/internal/status"
	"github.com/jomakori/tickerlight/internal/tickerdata"
)

// An operator reads the badge, the session line and the caps chip to decide
// whether real money is at stake. Wiring the snapshot through the handler is
// what makes those three say what the engine says.
func TestHandlerRendersEngineStatus(t *testing.T) {
	h := New(Options{
		Rows:   tickerdata.Sample(),
		Now:    func() time.Time { return tickerdata.SampleNow },
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Source: "attempt journal",
		Status: status.Snapshot{
			Available:           true,
			Mode:                status.ModeLive,
			Armed:               true,
			PerOrderCapCents:    5000,
			DailyCapCents:       15000,
			DailySpentCents:     1250,
			DailyRemainingCents: 13750,
		},
	})

	for _, path := range []string{"/", "/journal", "/risk"} {
		body := fetch(t, h, http.MethodGet, path, nil).body
		for _, want := range []string{
			"Attempt journal · live · real funds",
			"Live account",
			"$50.00",
			"$137.50",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q with a live snapshot", path, want)
			}
		}
		if strings.Contains(body, "no real funds") {
			t.Errorf("%s: still claims no real funds while the engine says live", path)
		}
	}
}

// No snapshot is the fallback path: the console renders exactly what it rendered
// before the status file existed, sample caps included.
func TestHandlerWithoutStatusKeepsPaperCopy(t *testing.T) {
	h := New(Options{
		Rows:   tickerdata.Sample(),
		Now:    func() time.Time { return tickerdata.SampleNow },
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Source: "attempt journal",
	})

	body := fetch(t, h, http.MethodGet, "/", nil).body
	for _, want := range []string{
		"Attempt journal · paper only",
		"Attempt journal · no real funds",
		"Paper account",
		"DISARMED",
		"RELEASED",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q on the no-snapshot fallback", want)
		}
	}
	for _, never := range []string{"live · real funds", "Live account", "ENGAGED"} {
		if strings.Contains(body, never) {
			t.Errorf("fallback claims %q; no snapshot supports it", never)
		}
	}
}
