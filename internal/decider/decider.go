package decider

// Tick orchestration: guards, Jev call, gate, sizing, order, journal.
// Nothing in this file can arm trading, raise a cap, or clear the kill
// switch — arm_state and kill_status are read as guards only.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"time"
)

// Config is the decider's whole runtime configuration.
type Config struct {
	Interval         time.Duration
	Symbols          []string
	PerOrderCapCents int64
	MaxQtyPerSymbol  float64
	MinProbability   float64
	MinSetupScore    float64
	MaxRisk          float64
	JournalPath      string
}

// Defaults returns the production thresholds.
func Defaults() Config {
	return Config{
		Interval:         15 * time.Minute,
		Symbols:          []string{"AAPL", "MSFT", "SPY", "QQQ"},
		PerOrderCapCents: 5000,
		MaxQtyPerSymbol:  25,
		MinProbability:   0.65,
		MinSetupScore:    0.5,
		MaxRisk:          0.35,
		JournalPath:      "/data/decisions.jsonl",
	}
}

// Engine is the read/write engine surface the tick needs.
type Engine interface {
	ArmState(ctx context.Context) (ArmState, error)
	KillState(ctx context.Context) (KillState, error)
	Account(ctx context.Context) (Account, error)
	Positions(ctx context.Context) ([]Position, error)
	CapsStatus(ctx context.Context) (CapsStatus, error)
	Quote(ctx context.Context, symbol string) (Quote, error)
	PlaceOrder(ctx context.Context, req PlaceOrderRequest) (PlaceOrderResult, error)
}

// Gate is the verdict on one Jev answer.
type Gate struct {
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
}

// Evaluate applies the four confidence conditions in order.
func Evaluate(a Answer, cfg Config) Gate {
	switch {
	case a.Choice != "buy" && a.Choice != "sell":
		return Gate{Reason: ReasonHold}
	case a.Probabilities[a.Choice] < cfg.MinProbability:
		return Gate{Reason: ReasonLowConfidence}
	case a.Score < cfg.MinSetupScore:
		return Gate{Reason: ReasonLowSetupScore}
	case a.Noul >= cfg.MaxRisk:
		return Gate{Reason: ReasonHighRisk}
	}
	return Gate{Passed: true}
}

// Size returns the fixed-notional quantity for price. Eight decimals so
// the engine's own recomputation stays inside its 1e-6 agreement window.
func Size(price float64, cfg Config) (float64, bool) {
	if price <= 0 {
		return 0, false
	}
	qty := math.Round((float64(cfg.PerOrderCapCents)/100/price)*1e8) / 1e8
	if qty <= 0 || qty > cfg.MaxQtyPerSymbol {
		return 0, false
	}
	return qty, true
}

// Entry is one journal line.
type Entry struct {
	Time      string       `json:"ts"`
	Symbol    string       `json:"symbol,omitempty"`
	Quote     *QuoteRecord `json:"quote,omitempty"`
	Jev       *JevRecord   `json:"jev,omitempty"`
	Gate      Gate         `json:"gate"`
	Order     *OrderRecord `json:"order,omitempty"`
	LatencyMS int64        `json:"latency_ms"`
	Error     string       `json:"error,omitempty"`
}

// QuoteRecord is the quote portion of a journal line.
type QuoteRecord struct {
	Last      float64 `json:"last"`
	PrevClose float64 `json:"prev_close"`
	ChangePct float64 `json:"change_pct"`
}

// JevRecord is the decision portion of a journal line.
type JevRecord struct {
	Model         string             `json:"model"`
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
	Score         float64            `json:"score"`
	Noul          float64            `json:"noul"`
}

// OrderRecord is the engine outcome portion of a journal line.
type OrderRecord struct {
	Placed        bool    `json:"placed"`
	Qty           float64 `json:"qty,omitempty"`
	ID            string  `json:"id,omitempty"`
	Outcome       string  `json:"outcome"`
	Status        string  `json:"status"`
	BrokerReached bool    `json:"broker_reached"`
}

// Decider runs one symbol per tick, rotating through the symbol list.
type Decider struct {
	Cfg    Config
	Engine Engine
	Jev    Caller
	Now    func() time.Time
	next   int
}

func (d *Decider) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// NextSymbol returns the symbol for this tick and advances the rotation.
func (d *Decider) NextSymbol() string {
	if len(d.Cfg.Symbols) == 0 {
		return ""
	}
	s := d.Cfg.Symbols[d.next%len(d.Cfg.Symbols)]
	d.next++
	return s
}

// Tick performs one decision cycle and returns the journal entry it wrote.
func (d *Decider) Tick(ctx context.Context) Entry {
	start := d.now()
	e := Entry{Time: start.UTC().Format(time.RFC3339)}

	arm, err := d.Engine.ArmState(ctx)
	if err != nil {
		e.Gate.Reason = ReasonJevError
		e.Error = "arm_state: " + err.Error()
		return d.finish(e, start)
	}
	if !arm.Armed {
		e.Gate.Reason = ReasonNotArmed
		return d.finish(e, start)
	}
	kill, err := d.Engine.KillState(ctx)
	if err != nil {
		e.Gate.Reason = ReasonJevError
		e.Error = "kill_status: " + err.Error()
		return d.finish(e, start)
	}
	if kill.Killed {
		e.Gate.Reason = ReasonKilled
		return d.finish(e, start)
	}

	symbol := d.NextSymbol()
	e.Symbol = symbol
	acct, err := d.Engine.Account(ctx)
	if err != nil {
		e.Error = "account: " + err.Error()
		return d.finish(e, start)
	}
	positions, err := d.Engine.Positions(ctx)
	if err != nil {
		e.Error = "positions: " + err.Error()
		return d.finish(e, start)
	}
	caps, err := d.Engine.CapsStatus(ctx)
	if err != nil {
		e.Error = "caps_status: " + err.Error()
		return d.finish(e, start)
	}
	quote, err := d.Engine.Quote(ctx, symbol)
	if err != nil {
		e.Error = "quote: " + err.Error()
		return d.finish(e, start)
	}
	change := 0.0
	if quote.PrevClose > 0 {
		change = (quote.Last - quote.PrevClose) / quote.PrevClose * 100
	}
	e.Quote = &QuoteRecord{Last: quote.Last, PrevClose: quote.PrevClose, ChangePct: change}

	state := map[string]any{
		"now":       e.Time,
		"symbol":    symbol,
		"quote":     map[string]any{"bid": quote.Bid, "ask": quote.Ask, "last": quote.Last, "prev_close": quote.PrevClose, "change_pct": change},
		"account":   map[string]any{"equity_usd": acct.Equity, "cash_usd": acct.Cash, "buying_power_usd": acct.BuyingPower},
		"positions": positionState(positions),
		"caps": map[string]any{
			"per_order_cents":       caps.PerOrderCapCents,
			"daily_cap_cents":       caps.DailyCapCents,
			"daily_remaining_cents": caps.DailyRemainingCents,
			"mode":                  caps.Mode,
		},
	}
	answer, err := d.Jev.Decide(ctx, state)
	if err != nil {
		e.Gate.Reason = ReasonJevError
		var hr *jevHoldReason
		if errors.As(err, &hr) && hr.reason != "" {
			e.Gate.Reason = hr.reason
		}
		e.Error = err.Error()
		return d.finish(e, start)
	}
	e.Jev = &JevRecord{
		Model:         answer.Model,
		Choice:        answer.Choice,
		Confidence:    answer.Confidence,
		Probabilities: answer.Probabilities,
		Score:         answer.Score,
		Noul:          answer.Noul,
	}

	if gate := Evaluate(answer, d.Cfg); !gate.Passed {
		e.Gate = gate
		return d.finish(e, start)
	}

	qty, ok := Size(quote.Last, d.Cfg)
	if !ok {
		e.Gate = Gate{Reason: ReasonSizeInvalid}
		return d.finish(e, start)
	}

	e.Order = &OrderRecord{Qty: qty, Status: "submitted"}
	result, err := d.Engine.PlaceOrder(ctx, PlaceOrderRequest{
		Symbol:    symbol,
		Side:      answer.Choice,
		Qty:       qty,
		OrderType: "market",
		Price:     quote.Last,
	})
	if err != nil {
		e.Gate.Reason = "engine_error"
		e.Error = "place_order: " + err.Error()
		return d.finish(e, start)
	}
	e.Order.Placed = result.Executed
	e.Order.ID = result.BrokerOrderID
	e.Order.Outcome = result.Outcome
	if result.Executed {
		e.Gate.Passed = true
		e.Order.Status = "executed"
		e.Order.BrokerReached = true
	} else {
		e.Gate.Reason = result.Outcome
		e.Order.Status = "blocked"
	}
	return d.finish(e, start)
}

func positionState(ps []Position) []map[string]any {
	out := make([]map[string]any, 0, len(ps))
	for _, p := range ps {
		out = append(out, map[string]any{"symbol": p.Symbol, "qty": p.Qty})
	}
	return out
}

func (d *Decider) finish(e Entry, start time.Time) Entry {
	e.LatencyMS = d.now().Sub(start).Milliseconds()
	if err := AppendEntry(d.Cfg.JournalPath, e); err != nil {
		fmt.Fprintf(os.Stderr, "decider: journal %s: %v\n", d.Cfg.JournalPath, err)
	}
	return e
}

// AppendEntry appends one JSON line, creating the file 0640.
func AppendEntry(path string, e Entry) error {
	if path == "" {
		return nil
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}
