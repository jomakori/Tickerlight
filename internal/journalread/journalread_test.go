package journalread

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jomakori/tickerlight/internal/tickerdata"
)

func rec(attemptID int64, stage, outcome, reason string) Record {
	return Record{
		AttemptID:  attemptID,
		Stage:      stage,
		Outcome:    outcome,
		ReasonCode: reason,
		Timestamp:  time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC),
	}
}

func TestRead_SkipsTornTailAndBlankLines(t *testing.T) {
	body := strings.Join([]string{
		`{"attempt_id":1,"stage":"venue","outcome":"filled","timestamp":"2026-11-09T12:00:00Z"}`,
		``,
		`{"attempt_id":2,"stage":"gate","outcome":"blocked","reason_code":"position_size_cap_exceeded","timestamp":"2026-11-09T12:01:00Z"}`,
		`{"attempt_id":3,"stage":"venue","outcome":"fill`, // torn mid-write
	}, "\n")

	records, malformed, err := Read(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	if malformed != 1 {
		t.Fatalf("malformed = %d, want 1", malformed)
	}
}

func TestLoad_MissingFileIsAnError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.jsonl")); err == nil {
		t.Fatal("expected an error for a missing journal")
	}
}

func TestLoad_ReadsFileWithoutModifyingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	body := `{"attempt_id":7,"stage":"venue","outcome":"filled","notional_cents":22540,"symbol":"AAPL","venue":"alpaca-paper","timestamp":"2026-11-09T12:00:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}

	rows, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(rows) != 1 || rows[0].Outcome != tickerdata.Filled {
		t.Fatalf("rows = %+v, want one filled row", rows)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("journal changed while reading it: %d/%v -> %d/%v",
			before.Size(), before.ModTime(), after.Size(), after.ModTime())
	}
}

func TestMap_AttemptLifecycle(t *testing.T) {
	intent := rec(1, stageIntent, outcomePending, "")
	intent.Symbol, intent.Venue, intent.NotionalCents = "AAPL", "alpaca-paper", 22540
	intent.ClientOrderID = "coid-abc"

	venue := rec(1, stageVenue, outcomeFilled, "")
	venue.ParentAttemptID = 1
	venue.Symbol, venue.Venue, venue.NotionalCents = "AAPL", "alpaca-paper", 22540
	venue.ClientOrderID, venue.BrokerOrderID = "coid-abc", "ord-9"

	rows := Map([]Record{intent, venue})
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (intent collapses into its terminal record)", len(rows))
	}
	row := rows[0]
	if row.ID != 1 || row.Outcome != tickerdata.Filled {
		t.Fatalf("row = %+v, want attempt 1 filled", row)
	}
	if row.Notional != 225.40 {
		t.Fatalf("notional = %v, want 225.40 from notional_cents", row.Notional)
	}
	if row.Symbol != "AAPL" || row.Venue != "alpaca-paper" {
		t.Fatalf("row symbol/venue = %q/%q", row.Symbol, row.Venue)
	}
	if row.DispatchID != "coid-abc" {
		t.Fatalf("dispatch id = %q, want the client order id", row.DispatchID)
	}
	if !strings.Contains(row.Detail, "ord-9") {
		t.Fatalf("detail = %q, want the broker order id", row.Detail)
	}
	if !row.Armed {
		t.Fatal("a filled attempt necessarily passed the arm gate")
	}
}

func TestMap_OutcomeMapping(t *testing.T) {
	cases := []struct {
		name string
		rec  Record
		want tickerdata.Outcome
	}{
		{"kill switch", rec(1, stageGate, outcomeBlocked, "halted_by_kill_switch"), tickerdata.Kill},
		{"not armed", rec(2, stageGate, outcomeBlocked, "not_armed_missing_file"), tickerdata.Cap},
		{"cap", rec(3, stageGate, outcomeBlocked, "position_size_cap_exceeded"), tickerdata.Cap},
		{"judge", rec(4, stageJudge, outcomeBlocked, "judge_below_threshold"), tickerdata.Judge},
		{"venue reject", rec(5, stageVenue, "failed", "insufficient_buying_power"), tickerdata.Venue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := Map([]Record{tc.rec})
			if len(rows) != 1 {
				t.Fatalf("rows = %d, want 1", len(rows))
			}
			if rows[0].Outcome != tc.want {
				t.Fatalf("outcome = %q, want %q", rows[0].Outcome, tc.want)
			}
		})
	}
}

// The judge stage is in the journal vocabulary too; it must render rather
// than disappear, since a judge refusal is the evidence an operator audits.
func TestMap_JudgeRefusalIsRendered(t *testing.T) {
	r := rec(11, stageJudge, outcomeBlocked, "judge_below_threshold")
	r.Symbol, r.Venue, r.NotionalCents = "MSFT", "alpaca-paper", 41020

	rows := Map([]Record{r})
	if len(rows) != 1 {
		t.Fatalf("judge record dropped: %+v", rows)
	}
	if rows[0].Outcome != tickerdata.Judge || rows[0].Outcome.Stage() != "judge" {
		t.Fatalf("row = %+v, want a judge-stage refusal", rows[0])
	}
	if rows[0].Armed {
		t.Fatal("a judge refusal never reached the venue, so it cannot be armed")
	}
}

func TestMap_ProcessRecordsIgnored(t *testing.T) {
	records := []Record{
		rec(1, stageSupervisor, "killed", "halt_file"),
		rec(2, stageRevocation, "revoked", ""),
		rec(3, stageIntent, outcomePending, ""),
	}
	if rows := Map(records); len(rows) != 0 {
		t.Fatalf("rows = %+v, want none: process records and bare intents are not trades", rows)
	}
}

func TestMap_LastTerminalRecordWins(t *testing.T) {
	blocked := rec(5, stageGate, outcomeBlocked, "position_size_cap_exceeded")
	filled := rec(5, stageVenue, outcomeFilled, "")
	rows := Map([]Record{blocked, filled})

	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Outcome != tickerdata.Filled {
		t.Fatalf("outcome = %q, want the later terminal record to win", rows[0].Outcome)
	}
}

func TestMap_UnknownStageDropped(t *testing.T) {
	rows := Map([]Record{rec(1, "something-new", "whatever", "")})
	if len(rows) != 0 {
		t.Fatalf("rows = %+v, want an unmappable stage dropped", rows)
	}
}
