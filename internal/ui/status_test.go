package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/jomakori/tickerlight/internal/status"
	"github.com/jomakori/tickerlight/internal/tickerdata"
)

// liveEngine is a snapshot of an armed engine trading real money under $50/$150
// caps with $137.50 of the day left. Every number differs from the sample
// limits on purpose, so a render that quietly kept the sample values fails.
func liveEngine() status.Snapshot {
	return status.Snapshot{
		Available:           true,
		Mode:                status.ModeLive,
		Armed:               true,
		Kill:                false,
		PerOrderCapCents:    5000,
		DailyCapCents:       15000,
		DailySpentCents:     1250,
		DailyRemainingCents: 13750,
		JudgeThreshold:      0.62,
		UpdatedAt:           time.Date(2026, 11, 14, 9, 12, 4, 0, time.UTC),
	}
}

func statusView(s status.Snapshot) View {
	v := NewView(tickerdata.Sample(), tickerdata.SampleNow)
	v.Source = "attempt journal"
	v.Status = s
	return v
}

// liveEngineWithMode is liveEngine wearing the given mode, so the paper row of a
// table exercises the same caps and flags as the live row.
func liveEngineWithMode(mode string) status.Snapshot {
	s := liveEngine()
	s.Mode = mode
	return s
}

// The shell has to say what the engine is doing, not what the console wishes it
// were doing: badge, session line, both session chips and the caps chip all
// follow the snapshot, in both modes.
func TestShellTellsTheTruthAboutTheEngine(t *testing.T) {
	disarmed := liveEngineWithMode(status.ModePaper)
	disarmed.Armed = false

	killed := liveEngineWithMode(status.ModePaper)
	killed.Armed = true
	killed.Kill = true

	for _, tc := range []struct {
		name  string
		snap  status.Snapshot
		want  []string
		never []string
	}{
		{
			name: "live, armed, kill switch released",
			snap: liveEngine(),
			want: []string{
				"Attempt journal · live · real funds",
				"Attempt journal · real funds",
				"Live account",
				"ARMED",
				"RELEASED",
				"$50.00",
				"$150.00",
				"$137.50",
			},
			never: []string{"paper only", "no real funds", "Paper account", "DISARMED", "HALTED", "ENGAGED"},
		},
		{
			name: "paper, disarmed, kill switch released",
			snap: disarmed,
			want: []string{
				"Attempt journal · paper only",
				"Attempt journal · no real funds",
				"Paper account",
				"DISARMED",
				"RELEASED",
				"$50.00",
				"$150.00",
				"$137.50",
			},
			never: []string{"live · real funds", "Live account", "ENGAGED"},
		},
		{
			name: "paper, armed, kill switch engaged",
			snap: killed,
			want: []string{
				"Attempt journal · paper only",
				"Paper account",
				"HALTED",
				"ENGAGED",
			},
			never: []string{"live · real funds", "Live account", "RELEASED"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			html := render(t, Layout(statusView(tc.snap)))
			for _, want := range tc.want {
				if !strings.Contains(html, want) {
					t.Errorf("shell missing %q", want)
				}
			}
			for _, never := range tc.never {
				if strings.Contains(html, never) {
					t.Errorf("shell claims %q, which the snapshot does not support", never)
				}
			}
		})
	}
}

// The caps card is where an operator reads the numbers the engine will block
// on, so it must show the engine's caps and headroom, not the sample limits.
func TestCapsCardShowsEngineCaps(t *testing.T) {
	v := statusView(liveEngine())
	v.Tab = TabOverview

	html := render(t, Layout(v))
	for _, want := range []string{
		"Caps · rolling 24h",
		"Per-order cap",
		"$50.00",
		"Daily cap",
		"$150.00",
		"Remaining today",
		"$137.50",
		"8%", // 1250 of 15000 spent, from the engine's remaining
	} {
		if !strings.Contains(html, want) {
			t.Errorf("caps card missing %q", want)
		}
	}
	for _, sample := range []string{"$3,500.00", "$3500.00"} {
		if strings.Contains(html, sample) {
			t.Errorf("caps card still shows the sample daily cap %q", sample)
		}
	}
}

// Sample mode renders the sample ledger, so it keeps the sample-data wording
// and the sample caps even if a snapshot somehow reached the view.
func TestSampleModeKeepsSampleDataCopy(t *testing.T) {
	v := NewView(tickerdata.Sample(), tickerdata.SampleNow)
	v.Status = liveEngine() // not reachable in practice; the copy must still hold

	html := render(t, Layout(v))
	for _, want := range []string{"Sample data · paper only", "Sample data · no real funds"} {
		if !strings.Contains(html, want) {
			t.Errorf("sample shell missing %q", want)
		}
	}
	if strings.Contains(html, "live · real funds") {
		t.Error("sample shell claims real funds; sample mode has no engine")
	}
	if c := v.Caps(); !strings.Contains(USD(c.PerOrder), "500") {
		t.Errorf("sample caps = %+v, want the sample limits regardless of snapshot", c)
	}
}

// With no snapshot the view falls back to exactly what it rendered before the
// status file existed: sample limits, disarmed, released, paper wording.
func TestViewWithoutSnapshotFallsBack(t *testing.T) {
	v := NewView(tickerdata.Sample(), tickerdata.SampleNow)
	v.Source = "attempt journal"

	if c := v.Caps(); c.Engine || c.PerOrder != v.Limits.PerOrder || c.Daily != v.Limits.Daily {
		t.Errorf("caps = %+v, want the sample limits when no snapshot was read", c)
	}
	if !strings.Contains(v.SourceBadge(), "paper only") || !strings.Contains(v.SourceLine(), "no real funds") {
		t.Errorf("badge/line = %q / %q, want the standing paper-only reminder", v.SourceBadge(), v.SourceLine())
	}
	if v.Armed() || v.Kill() {
		t.Errorf("flags = armed %v kill %v, want both false with no snapshot", v.Armed(), v.Kill())
	}
}
