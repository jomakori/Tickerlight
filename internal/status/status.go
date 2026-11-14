// Package status reads the engine's status snapshot: a small JSON file the
// engine writes beside its attempt journal saying which mode it is running in,
// whether it is armed, whether the kill switch is engaged, and the caps it is
// actually enforcing.
//
// The snapshot is untrusted input and the console treats it that way. Anything
// this package cannot vouch for — unreadable, malformed, or naming a mode other
// than live or paper — is an error, and the caller keeps the console's built-in
// wording instead of guessing. The failure mode is deliberately the quiet one:
// a corrupt file renders as "paper only", never as "live, real funds" on the
// strength of bytes nobody validated.
package status

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Modes a snapshot may name. Anything else is malformed.
const (
	ModeLive  = "live"
	ModePaper = "paper"
)

// Snapshot is one reading of the engine's status file.
type Snapshot struct {
	// Available reports that this Snapshot came from a file that parsed and
	// named a known mode. The zero value is unavailable: the console then
	// renders exactly as it did before the snapshot existed.
	Available bool
	Mode      string
	Armed     bool
	Kill      bool

	PerOrderCapCents    int64
	DailyCapCents       int64
	DailySpentCents     int64
	DailyRemainingCents int64

	// JudgeThreshold is the score floor the engine reports enforcing. Zero
	// means the file carried none; the console does not infer one.
	JudgeThreshold float64
	// UpdatedAt is when the engine wrote the file. Zero when absent or not
	// RFC 3339: a bad timestamp does not make the rest of the reading wrong,
	// so it is kept as unknown rather than poisoning the whole snapshot.
	UpdatedAt time.Time
}

// Live reports whether the engine says it is trading real funds.
func (s Snapshot) Live() bool { return s.Available && s.Mode == ModeLive }

// file is the on-disk shape. Unknown keys are ignored so a newer engine can add
// fields without forcing an older console to fall back.
type file struct {
	Mode                string  `json:"mode"`
	Armed               bool    `json:"armed"`
	Kill                bool    `json:"kill"`
	PerOrderCapCents    int64   `json:"per_order_cap_cents"`
	DailyCapCents       int64   `json:"daily_cap_cents"`
	DailySpentCents     int64   `json:"daily_spent_cents"`
	DailyRemainingCents int64   `json:"daily_remaining_cents"`
	JudgeThreshold      float64 `json:"judge_threshold"`
	UpdatedAt           string  `json:"updated_at"`
}

// Load reads and validates the snapshot at path. The returned error wraps the
// underlying read or parse failure so a caller can distinguish a missing file
// (os.ErrNotExist) from a file it refused to trust.
func Load(path string) (Snapshot, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		return Snapshot{}, fmt.Errorf("status %s: %w", path, err)
	}
	mode := strings.ToLower(strings.TrimSpace(f.Mode))
	if mode != ModeLive && mode != ModePaper {
		return Snapshot{}, fmt.Errorf("status %s: mode %q is neither %q nor %q", path, f.Mode, ModeLive, ModePaper)
	}
	s := Snapshot{
		Available:           true,
		Mode:                mode,
		Armed:               f.Armed,
		Kill:                f.Kill,
		PerOrderCapCents:    f.PerOrderCapCents,
		DailyCapCents:       f.DailyCapCents,
		DailySpentCents:     f.DailySpentCents,
		DailyRemainingCents: f.DailyRemainingCents,
		JudgeThreshold:      f.JudgeThreshold,
	}
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(f.UpdatedAt)); err == nil {
		s.UpdatedAt = t.UTC()
	}
	return s, nil
}
