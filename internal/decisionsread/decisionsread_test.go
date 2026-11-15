package decisionsread

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// line is one decision as the decider writes it.
const line = `{"ts":"2026-11-14T15:30:00Z","symbol":"SPY","jev":{"model":"jev-1.14.0","choice":"hold","confidence":0.3,"probabilities":{"buy":0.14,"sell":0,"hold":0.86},"score":0.25,"noul":0.64},"gate":{"passed":false,"reason":"low_setup_score"},"latency_ms":96}`

func TestRead(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantRows   int
		wantMal    int
		wantSym    string
		wantReason string
	}{
		{
			name:       "one decision",
			input:      line + "\n",
			wantRows:   1,
			wantSym:    "SPY",
			wantReason: "low_setup_score",
		},
		{
			name:     "blank lines are not decisions",
			input:    "\n" + line + "\n\n" + line + "\n\n",
			wantRows: 2,
			wantSym:  "SPY",
		},
		{
			name:     "torn tail is skipped, not fatal",
			input:    line + "\n" + `{"ts":"2026-11-14T15:45:00Z","sym`,
			wantRows: 1,
			wantMal:  1,
			wantSym:  "SPY",
		},
		{
			name:     "garbage line is counted",
			input:    "not json\n" + line + "\n",
			wantRows: 1,
			wantMal:  1,
		},
		{
			name:  "empty journal",
			input: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, mal, err := Read(strings.NewReader(tc.input))
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if len(rows) != tc.wantRows {
				t.Fatalf("rows = %d, want %d", len(rows), tc.wantRows)
			}
			if mal != tc.wantMal {
				t.Errorf("malformed = %d, want %d", mal, tc.wantMal)
			}
			if tc.wantSym != "" && rows[0].Symbol != tc.wantSym {
				t.Errorf("symbol = %q, want %q", rows[0].Symbol, tc.wantSym)
			}
			if tc.wantReason != "" && rows[0].Gate.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", rows[0].Gate.Reason, tc.wantReason)
			}
		})
	}
}

// TestReadDecodesWhatThePanelRenders pins the fields the console depends on, so
// a decider-side rename shows up here rather than as dashes on the page.
func TestReadDecodesWhatThePanelRenders(t *testing.T) {
	rows, _, err := Read(strings.NewReader(line + "\n"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	e := rows[0]
	if got, want := e.Time.UTC().Format(time.RFC3339), "2026-11-14T15:30:00Z"; got != want {
		t.Errorf("ts = %q, want %q", got, want)
	}
	if e.Jev == nil {
		t.Fatal("jev is nil")
	}
	if e.Jev.Model != "jev-1.14.0" || e.Jev.Choice != "hold" {
		t.Errorf("jev = %+v", *e.Jev)
	}
	if got := e.Jev.Probabilities["hold"]; got != 0.86 {
		t.Errorf("hold probability = %v, want 0.86", got)
	}
	if e.Jev.Score != 0.25 || e.Jev.Noul != 0.64 {
		t.Errorf("score/noul = %v/%v", e.Jev.Score, e.Jev.Noul)
	}
	if e.Gate.Passed || e.Gate.Reason != "low_setup_score" {
		t.Errorf("gate = %+v", e.Gate)
	}
	if e.LatencyMS != 96 {
		t.Errorf("latency = %d, want 96", e.LatencyMS)
	}
}

func TestLoadErrors(t *testing.T) {
	dir := t.TempDir()

	if _, err := Load(filepath.Join(dir, "missing.jsonl")); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing file err = %v, want ErrNotFound", err)
	}
	if _, err := Load(dir); !errors.Is(err, ErrUnreadable) {
		t.Errorf("directory err = %v, want ErrUnreadable", err)
	}

	path := filepath.Join(dir, "decisions.jsonl")
	if err := os.WriteFile(path, []byte(line+"\n"), 0o640); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	rows, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
}
