package ui

import (
	"testing"
	"time"
)

// State is the arming card's headline. The engine's caps_status snapshot only
// refreshes on its own 15-minute cadence, so a window this render just wrote
// must count as armed on its own, without waiting for the snapshot to catch
// up — but only while it has not already expired.
func TestArmDataState(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name string
		a    ArmData
		want string
	}{
		{
			name: "killed",
			a:    ArmData{Kill: true},
			want: "killed",
		},
		{
			name: "armed from snapshot",
			a:    ArmData{Armed: true},
			want: "armed",
		},
		{
			name: "armed from a window just written this render",
			a:    ArmData{Armed: false, ArmedUntil: now.Add(30 * time.Minute)},
			want: "armed",
		},
		{
			name: "disarmed",
			a:    ArmData{},
			want: "disarmed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.State(); got != tc.want {
				t.Errorf("State() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Kill still wins over a fresh window: the halt file refuses arming outright,
// so State must not report armed just because ArmedUntil is still in force.
func TestArmDataStateKillOverridesFreshWindow(t *testing.T) {
	a := ArmData{Kill: true, ArmedUntil: time.Now().Add(time.Hour)}
	if got := a.State(); got != "killed" {
		t.Errorf("State() = %q, want %q", got, "killed")
	}
}

// An expired window must not count as armed; the engine's own refresal is
// what should decide, not a stale ArmedUntil from an earlier render.
func TestArmDataStateExpiredWindowIsDisarmed(t *testing.T) {
	a := ArmData{ArmedUntil: time.Now().Add(-time.Minute)}
	if got := a.State(); got != "disarmed" {
		t.Errorf("State() = %q, want %q", got, "disarmed")
	}
}
