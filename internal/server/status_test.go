package server

import (
	"io"
	"log/slog"
	"net/http"
	"os"
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
		Status: func() status.Snapshot {
			return status.Snapshot{
				Available:           true,
				Mode:                status.ModeLive,
				Armed:               true,
				PerOrderCapCents:    5000,
				DailyCapCents:       15000,
				DailySpentCents:     1250,
				DailyRemainingCents: 13750,
			}
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

// The console boots before the engine on a fresh pod, and arming changes while
// a page is open. Reading the snapshot once at boot pinned production to
// "paper only" until someone restarted the console, so the handler must read it
// per render.
func TestHandlerReReadsStatusPerRequest(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/status.json"
	if err := os.WriteFile(path, []byte(`{"mode":"paper","armed":false,"per_order_cap_cents":10000}`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := New(Options{
		Rows:   tickerdata.Sample(),
		Now:    func() time.Time { return tickerdata.SampleNow },
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Source: "attempt journal",
		Status: func() status.Snapshot {
			snap, err := status.Load(path)
			if err != nil {
				return status.Snapshot{}
			}
			return snap
		},
	})

	if body := fetch(t, h, http.MethodGet, "/", nil).body; !strings.Contains(body, "no real funds") {
		t.Fatalf("paper snapshot: missing paper copy")
	}
	if err := os.WriteFile(path, []byte(`{"mode":"live","armed":true,"per_order_cap_cents":5000,"daily_cap_cents":15000,"daily_remaining_cents":15000}`), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	body := fetch(t, h, http.MethodGet, "/", nil).body
	if !strings.Contains(body, "live · real funds") || strings.Contains(body, "no real funds") {
		t.Fatalf("live snapshot written after boot still renders paper copy")
	}
	if !strings.Contains(body, "$50.00") {
		t.Fatalf("live snapshot caps not rendered")
	}
}
