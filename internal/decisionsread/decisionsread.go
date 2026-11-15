// Package decisionsread turns the decider's append-only decisions journal into
// the console's decision rows.
//
// The journal is a JSONL file the decider writes and this console only ever
// reads: the format is the contract between the two, so this package owns its
// own decoder rather than importing the decider's package — the console must
// not link a client that can reach Jev or the engine's order path.
package decisionsread

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Entry is one decoded decision line. Only the fields the console renders are
// decoded; the decider may add fields without breaking this reader.
type Entry struct {
	Time      time.Time `json:"ts"`
	Symbol    string    `json:"symbol"`
	Jev       *Jev      `json:"jev"`
	Gate      Gate      `json:"gate"`
	Order     *Order    `json:"order"`
	LatencyMS int64     `json:"latency_ms"`
	Error     string    `json:"error"`
}

// Jev is what the decider asked and what it was told.
type Jev struct {
	Model         string             `json:"model"`
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
	Score         float64            `json:"score"`
	Noul          float64            `json:"noul"`
}

// Gate is the verdict on one answer: passed, or the reason it held.
type Gate struct {
	Passed bool   `json:"passed"`
	Reason string `json:"reason"`
}

// Order is the outcome of a decision that reached the engine.
type Order struct {
	Placed  bool    `json:"placed"`
	Qty     float64 `json:"qty"`
	ID      string  `json:"id"`
	Outcome string  `json:"outcome"`
	Status  string  `json:"status"`
}

// ErrNotFound and ErrUnreadable distinguish the two ways a configured journal
// path can fail to produce data, so the caller can say which happened instead
// of rendering an empty panel that looks like a quiet day.
var (
	ErrNotFound   = errors.New("decisions journal not found")
	ErrUnreadable = errors.New("decisions journal unreadable")
)

// Load reads every decision at path, oldest first. A path that cannot be
// opened or read returns ErrNotFound or ErrUnreadable rather than an empty
// slice: an absent journal and a journal with no decisions read differently to
// an operator, and only one of them is normal the first time the decider runs.
func Load(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s: %v", ErrNotFound, path, err)
		}
		return nil, fmt.Errorf("%w: %s: %v", ErrUnreadable, path, err)
	}
	defer f.Close()

	entries, malformed, err := Read(f)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrUnreadable, path, err)
	}
	_ = malformed // counted, not fatal: a torn tail is expected after a restart

	return entries, nil
}

// Read decodes a decisions stream. A line that does not parse is skipped and
// counted: the decider can be killed mid-write, and one torn tail must not hide
// every decision before it.
func Read(r io.Reader) (entries []Entry, malformed int, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			malformed++
			continue
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, malformed, err
	}
	return entries, malformed, nil
}
