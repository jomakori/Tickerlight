// Package journalread turns trading-mcp's append-only attempt journal into
// the console's view rows.
//
// The journal is a JSONL file the server writes and this console only ever
// reads: the format is the contract between the two, so this package owns
// its own decoder rather than importing the server's private module. Reading
// never creates, truncates, or appends to the file.
package journalread

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jomakori/tickerlight/internal/tickerdata"
)

// Record is one decoded journal line. Only the fields the console renders
// are decoded; the server may add fields without breaking this reader.
type Record struct {
	AttemptID       int64  `json:"attempt_id"`
	ParentAttemptID int64  `json:"parent_attempt_id"`
	Stage           string `json:"stage"`
	Outcome         string `json:"outcome"`
	ReasonCode      string `json:"reason_code"`
	// Confidence is the judge's stated probability. It is a pointer so a
	// missing score stays distinguishable from a recorded score of 0.
	Confidence    *float64  `json:"confidence"`
	NotionalCents int64     `json:"notional_cents"`
	Symbol        string    `json:"symbol"`
	Venue         string    `json:"venue"`
	ClientOrderID string    `json:"client_order_id"`
	BrokerOrderID string    `json:"broker_order_id"`
	Timestamp     time.Time `json:"timestamp"`
}

// Journal stages and outcomes the console maps. They are the server's
// vocabulary, restated here as strings so a new server-side stage is simply
// ignored instead of failing to compile this package.
const (
	stageIntent     = "intent"
	stageJudge      = "judge"
	stageGate       = "gate"
	stageVenue      = "venue"
	stageSupervisor = "supervisor"
	stageRevocation = "revocation"
	outcomeFilled   = "filled"
	outcomeBlocked  = "blocked"
	outcomePending  = "pending"
)

// ErrNotFound and ErrUnreadable distinguish the two ways a configured
// journal path can fail to produce data: the path does not exist, or it
// exists but could not be read (permissions, a directory instead of a
// file, an I/O error partway through). The caller surfaces this
// distinction to the operator instead of silently falling back to the
// sample ledger, which would show plausible trades that never happened.
var (
	ErrNotFound   = errors.New("journal not found")
	ErrUnreadable = errors.New("journal unreadable")
)

// Load reads every attempt described by the journal at path, oldest first.
// A path that cannot be opened or read returns ErrNotFound or ErrUnreadable
// (wrapped with the underlying error) rather than falling back to any other
// data: the caller decides how to render that, but it must never be the
// sample ledger.
func Load(path string) ([]tickerdata.Attempt, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s: %v", ErrNotFound, path, err)
		}
		return nil, fmt.Errorf("%w: %s: %v", ErrUnreadable, path, err)
	}
	defer f.Close()

	records, malformed, err := Read(f)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrUnreadable, path, err)
	}
	_ = malformed // counted, not fatal: a torn tail is expected after a crash

	return Map(records), nil
}

// Read decodes a journal stream. A line that does not parse is skipped and
// counted: the server can be killed mid-write, and one torn tail must not
// hide every attempt before it.
func Read(r io.Reader) (records []Record, malformed int, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			malformed++
			continue
		}
		records = append(records, rec)
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, malformed, err
	}
	return records, malformed, nil
}

// Map folds journal records into one row per attempt.
//
// Only trade-attempt stages become rows: an intent record alone is not a
// finished attempt, and supervisor/revocation records describe the process
// rather than a trade, so both are ignored. When an attempt has several
// eligible records the last terminal one wins — that is its outcome, the same
// rule the order path itself uses to answer a retry.
//
// A judge record is not one of those terminal records: it reports the score
// the judge gave the attempt, not what became of it. It therefore never
// replaces a venue or gate outcome — it adds the confidence and keeps the
// outcome the attempt actually reached. Only when the judge refusal is the
// sole record does its outcome stand in as the attempt's terminal state.
func Map(records []Record) []tickerdata.Attempt {
	type slot struct {
		attempt tickerdata.Attempt
		seen    bool
		// outcomeSet is true once some record named the attempt's
		// outcome. A judge record sets it too; unlike the others it does
		// not keep the slot from being replaced by a later terminal record.
		outcomeSet bool
	}

	ordered := make([]int64, 0, len(records))
	byID := make(map[int64]*slot, len(records))

	for _, r := range records {
		if r.Stage == stageSupervisor || r.Stage == stageRevocation {
			continue
		}
		if r.Stage == stageIntent {
			// An intent record has no terminal outcome to show yet.
			continue
		}
		if r.Outcome == outcomePending && r.Stage != stageJudge {
			// A still-pending trade record has no terminal outcome. A judge
			// record is the exception: it may be pending and still report
			// the score the attempt was given.
			continue
		}

		outcome, ok := outcomeFor(r)
		if !ok {
			continue
		}

		s, exists := byID[r.AttemptID]
		if !exists {
			s = &slot{}
			byID[r.AttemptID] = s
			ordered = append(ordered, r.AttemptID)
		}

		if r.Stage == stageJudge {
			if !s.outcomeSet {
				s.attempt = attemptFor(r, outcome)
				s.outcomeSet = true
			} else if r.Confidence != nil {
				s.attempt.Confidence = r.Confidence
			}
			s.seen = true
			continue
		}

		// The newest terminal record wins outright, but it must not
		// discard a score the judge already recorded.
		confidence := s.attempt.Confidence
		s.attempt = attemptFor(r, outcome)
		if r.Confidence == nil {
			s.attempt.Confidence = confidence
		}
		s.seen = true
		s.outcomeSet = true
	}

	rows := make([]tickerdata.Attempt, 0, len(ordered))
	for _, id := range ordered {
		if s := byID[id]; s != nil && s.seen {
			rows = append(rows, s.attempt)
		}
	}
	return withRollingCaps(rows)
}

// attemptFor renders one journal record as an attempt row.
func attemptFor(r Record, outcome tickerdata.Outcome) tickerdata.Attempt {
	return tickerdata.Attempt{
		ID:         int(r.AttemptID),
		TS:         r.Timestamp.UTC(),
		Venue:      r.Venue,
		Symbol:     r.Symbol,
		Side:       "", // the journal records no side; shown as unknown
		Notional:   float64(r.NotionalCents) / 100,
		Outcome:    outcome,
		Confidence: r.Confidence,
		Detail:     detailFor(r),
		DispatchID: r.ClientOrderID,
		// A row that reached the venue necessarily passed the arm gate;
		// a blocked row's arm state at the time is not in the journal,
		// so it is reported as not armed rather than guessed.
		Armed: outcome == tickerdata.Filled || outcome == tickerdata.Venue,
	}
}

// withRollingCaps fills the rolling-cap columns from the rows themselves.
//
// tickerdata.Derive cannot be used here: it is the sample fixture's
// derivation, it recomputes notional from a qty/px pair the journal does
// not carry, and it substitutes synthetic dispatch ids for real ones. The
// cap columns are instead the real trailing-24h sum of dispatched notional,
// and every other field keeps the value the journal supplied.
func withRollingCaps(rows []tickerdata.Attempt) []tickerdata.Attempt {
	for i := range rows {
		var before float64
		for _, o := range rows {
			if !o.Dispatched() || !o.TS.Before(rows[i].TS) {
				continue
			}
			if rows[i].TS.Sub(o.TS) <= 24*time.Hour {
				before += o.Notional
			}
		}
		rows[i].CapBefore = before
		rows[i].CapAfter = before
		if rows[i].Dispatched() {
			rows[i].CapAfter += rows[i].Notional
		}
	}
	return rows
}

// outcomeFor maps one terminal journal record onto the console's outcome
// taxonomy. Gate refusals are split by reason code because the console
// distinguishes an operator kill from a cap refusal; anything the taxonomy
// cannot name is dropped rather than mislabelled.
func outcomeFor(r Record) (tickerdata.Outcome, bool) {
	switch r.Stage {
	case stageVenue:
		if r.Outcome == outcomeFilled {
			return tickerdata.Filled, true
		}
		return tickerdata.Venue, true
	case stageJudge:
		return tickerdata.Judge, true
	case stageGate:
		if r.Outcome != outcomeBlocked {
			return "", false
		}
		reason := strings.ToLower(r.ReasonCode)
		if strings.Contains(reason, "kill") || strings.Contains(reason, "halt") {
			return tickerdata.Kill, true
		}
		return tickerdata.Cap, true
	default:
		return "", false
	}
}

// detailFor renders the operator-facing explanation: the reason code, plus
// the venue's order id when the venue accepted the attempt.
func detailFor(r Record) string {
	parts := make([]string, 0, 2)
	if r.ReasonCode != "" {
		parts = append(parts, r.ReasonCode)
	}
	if r.BrokerOrderID != "" {
		parts = append(parts, "order "+r.BrokerOrderID)
	}
	return strings.Join(parts, " · ")
}
