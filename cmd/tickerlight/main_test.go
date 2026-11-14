package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func writeStatus(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "status.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write status fixture: %v", err)
	}
	return path
}

func TestStatusPathFor(t *testing.T) {
	for _, tc := range []struct {
		name     string
		journal  string
		explicit string
		want     string
	}{
		{
			name:    "defaults to the journal's directory",
			journal: "/srv/engine/journal/attempts.jsonl",
			want:    "/srv/engine/journal/status.json",
		},
		{
			name:     "an explicit -status wins",
			journal:  "/srv/engine/journal/attempts.jsonl",
			explicit: "/etc/tickerlight/status.json",
			want:     "/etc/tickerlight/status.json",
		},
		{
			name:    "a bare journal name sits in the working directory",
			journal: "attempts.jsonl",
			want:    "status.json",
		},
		{
			name:    "sample mode has no snapshot",
			journal: "",
			want:    "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusPathFor(tc.journal, tc.explicit); got != tc.want {
				t.Errorf("statusPathFor(%q, %q) = %q, want %q", tc.journal, tc.explicit, got, tc.want)
			}
		})
	}
}

func TestLoadStatusReadsASnapshot(t *testing.T) {
	path := writeStatus(t, `{"mode":"live","armed":true,"daily_cap_cents":15000,"daily_remaining_cents":13750}`)

	snap := loadStatus(path, discardLog())
	if !snap.Available || !snap.Live() || !snap.Armed {
		t.Fatalf("snapshot = %+v, want an available, live, armed reading", snap)
	}
	if snap.DailyRemainingCents != 13750 {
		t.Errorf("remaining = %d, want 13750", snap.DailyRemainingCents)
	}
}

// A missing file is the ordinary case: the engine has not written a snapshot
// yet. The console must come up and keep its built-in wording, not fail.
func TestLoadStatusMissingFileFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")

	if snap := loadStatus(path, discardLog()); snap.Available {
		t.Fatalf("snapshot = %+v, want the zero value for a missing file", snap)
	}
}

// A corrupt file must never be believed and never be fatal: falling back is the
// quiet, safe failure — it can under-claim real funds but never invent them.
func TestLoadStatusMalformedFileFallsBack(t *testing.T) {
	for _, body := range []string{
		`{"mode":"live",`,
		`{"mode":"turbo","armed":true}`,
		`not json at all`,
	} {
		if snap := loadStatus(writeStatus(t, body), discardLog()); snap.Available {
			t.Errorf("body %q produced %+v, want the zero value", body, snap)
		}
	}
}
