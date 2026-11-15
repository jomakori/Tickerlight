package decider

// EngineClient talks JSON-RPC to the trading engine's MCP endpoint over
// loopback (internal/decider/mcp.go carries the shared wire plumbing).
// It calls exactly the tools MAK-51 needs: the two guard reads
// (arm_state, kill_status), four read-only context tools (account,
// positions, caps_status, quote), and place_order. Nothing here can arm
// trading, raise a cap, or clear the kill switch — those are properties
// of files the engine itself reads, never of an argument this client
// sends.
import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// EngineClient is the production EngineReader (see decider.go):
// cmd/trading-decider wires this in with the engine's loopback MCP URL
// and TRADING_MCP_HTTP_TOKEN.
type EngineClient struct {
	// URL is the engine's MCP endpoint, e.g. http://127.0.0.1:8090/mcp.
	URL string
	// Token is the bearer token the engine requires.
	Token string
	// HTTP is the client used for every call. Tests inject a fake
	// implementing Do(*http.Request) (*http.Response, error); production
	// wiring passes a real *http.Client.
	HTTP httpDoer
}

func (c *EngineClient) http() httpDoer {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// ArmState is the engine's answer to "is trading currently authorized".
type ArmState struct {
	Armed bool
}

// ArmState reports whether the engine's operator arm file currently
// authorizes trading. Read-only: this call cannot arm, extend, or clear
// anything.
func (c *EngineClient) ArmState(ctx context.Context) (ArmState, error) {
	text, isErr, err := callTool(ctx, c.URL, c.Token, "arm_state", struct{}{}, c.http())
	if err != nil {
		return ArmState{}, err
	}
	if isErr {
		return ArmState{}, fmt.Errorf("decider: arm_state: %s", text)
	}
	fields := parseFields(text)
	armed, perr := strconv.ParseBool(fields["armed"])
	if perr != nil {
		return ArmState{}, fmt.Errorf("decider: arm_state: unparsable armed=%q in %q", fields["armed"], text)
	}
	return ArmState{Armed: armed}, nil
}

// KillState is the engine's answer to "is the kill switch tripped".
type KillState struct {
	Killed bool
}

// KillState reports whether the engine's halt file is currently tripped.
// Read-only: this call cannot trip or clear the halt file.
func (c *EngineClient) KillState(ctx context.Context) (KillState, error) {
	text, isErr, err := callTool(ctx, c.URL, c.Token, "kill_status", struct{}{}, c.http())
	if err != nil {
		return KillState{}, err
	}
	if isErr {
		return KillState{}, fmt.Errorf("decider: kill_status: %s", text)
	}
	fields := parseFields(text)
	killed, perr := strconv.ParseBool(fields["killed"])
	if perr != nil {
		return KillState{}, fmt.Errorf("decider: kill_status: unparsable killed=%q in %q", fields["killed"], text)
	}
	return KillState{Killed: killed}, nil
}

// Account is the engine's account-level balance snapshot.
type Account struct {
	Equity      float64
	Cash        float64
	BuyingPower float64
}

// Account reads the engine's account tool. Context for the Jev call, not
// used for any gate decision in this package.
func (c *EngineClient) Account(ctx context.Context) (Account, error) {
	text, isErr, err := callTool(ctx, c.URL, c.Token, "account", struct{}{}, c.http())
	if err != nil {
		return Account{}, err
	}
	if isErr {
		return Account{}, fmt.Errorf("decider: account: %s", text)
	}
	fields := parseFields(text)
	equity, _ := strconv.ParseFloat(fields["equity"], 64)
	cash, _ := strconv.ParseFloat(fields["cash"], 64)
	bp, _ := strconv.ParseFloat(fields["buying_power"], 64)
	return Account{Equity: equity, Cash: cash, BuyingPower: bp}, nil
}

// Position is one open venue position as the engine reports it.
type Position struct {
	Symbol string
	Qty    float64
}

// Positions reads the engine's positions tool. Context for the Jev call.
func (c *EngineClient) Positions(ctx context.Context) ([]Position, error) {
	text, isErr, err := callTool(ctx, c.URL, c.Token, "positions", struct{}{}, c.http())
	if err != nil {
		return nil, err
	}
	if isErr {
		return nil, fmt.Errorf("decider: positions: %s", text)
	}
	text = strings.TrimSpace(text)
	if text == "" || text == "(no positions)" {
		return nil, nil
	}
	var out []Position
	for _, line := range strings.Split(text, "\n") {
		fields := parseFields(line)
		qty, _ := strconv.ParseFloat(fields["qty"], 64)
		out = append(out, Position{Symbol: fields["symbol"], Qty: qty})
	}
	return out, nil
}

// CapsStatus is the engine's spend caps and current consumption, as
// reported by its caps_status tool. Context for the Jev call; this
// package's own sizing math (sizing.go) uses the per-order cap and max
// qty from its own Config, not from this struct.
type CapsStatus struct {
	PerOrderCapCents    int64
	DailyCapCents       int64
	DailySpentCents     int64
	DailyRemainingCents int64
	Mode                string
}

// CapsStatus reads the engine's caps_status tool.
func (c *EngineClient) CapsStatus(ctx context.Context) (CapsStatus, error) {
	text, isErr, err := callTool(ctx, c.URL, c.Token, "caps_status", struct{}{}, c.http())
	if err != nil {
		return CapsStatus{}, err
	}
	if isErr {
		return CapsStatus{}, fmt.Errorf("decider: caps_status: %s", text)
	}
	fields := parseFields(text)
	per, _ := strconv.ParseInt(fields["per_order_cap_cents"], 10, 64)
	daily, _ := strconv.ParseInt(fields["daily_cap_cents"], 10, 64)
	spent, _ := strconv.ParseInt(fields["daily_spent_cents"], 10, 64)
	remaining, _ := strconv.ParseInt(fields["daily_remaining_cents"], 10, 64)
	return CapsStatus{
		PerOrderCapCents:    per,
		DailyCapCents:       daily,
		DailySpentCents:     spent,
		DailyRemainingCents: remaining,
		Mode:                fields["mode"],
	}, nil
}

// Quote is a symbol's quote as the engine reports it.
type Quote struct {
	Symbol string
	Bid    float64
	Ask    float64
	// Last is the bid/ask midpoint: the engine's quote tool reports
	// top-of-book bid/ask only, no last-trade price, so this is the best
	// available stand-in and what sizing.go sizes against.
	Last float64
	// PrevClose is always zero: the engine's quote tool does not report
	// one. ChangePct in the journal is therefore always zero too.
	PrevClose float64
}

// Quote reads the engine's quote tool for symbol.
func (c *EngineClient) Quote(ctx context.Context, symbol string) (Quote, error) {
	text, isErr, err := callTool(ctx, c.URL, c.Token, "quote", map[string]any{"symbol": symbol}, c.http())
	if err != nil {
		return Quote{}, err
	}
	if isErr {
		return Quote{}, fmt.Errorf("decider: quote: %s", text)
	}
	fields := parseFields(text)
	bid, berr := strconv.ParseFloat(fields["bid"], 64)
	ask, aerr := strconv.ParseFloat(fields["ask"], 64)
	if berr != nil || aerr != nil {
		return Quote{}, fmt.Errorf("decider: quote: unparsable bid/ask in %q", text)
	}
	return Quote{Symbol: symbol, Bid: bid, Ask: ask, Last: (bid + ask) / 2}, nil
}

// PlaceOrderRequest is the broker-agnostic shape of a place_order call.
type PlaceOrderRequest struct {
	Symbol    string
	Side      string // "buy" or "sell"
	Qty       float64
	OrderType string // "market"
	Price     float64
}

// PlaceOrderResult is the engine's outcome for one place_order call.
// Executed is true only when the engine actually dispatched the order;
// any refusal (kill, not armed, caps, sizing, judge, ...) comes back as
// Executed=false with Outcome carrying the engine's own reason code.
type PlaceOrderResult struct {
	Executed      bool
	BrokerOrderID string
	Outcome       string
}

// PlaceOrder calls the engine's place_order tool. The engine itself is
// the only thing that can refuse an order — this method never second-
// guesses it and never retries.
func (c *EngineClient) PlaceOrder(ctx context.Context, req PlaceOrderRequest) (PlaceOrderResult, error) {
	text, isErr, err := callTool(ctx, c.URL, c.Token, "place_order", map[string]any{
		"symbol":     req.Symbol,
		"side":       req.Side,
		"order_type": req.OrderType,
		"qty":        req.Qty,
		"price":      req.Price,
	}, c.http())
	if err != nil {
		return PlaceOrderResult{}, err
	}
	if isErr {
		reason := extractReason(text)
		if reason == "" {
			reason = text
		}
		return PlaceOrderResult{Executed: false, Outcome: "blocked:" + reason}, nil
	}
	fields := parseFields(text)
	return PlaceOrderResult{
		Executed:      true,
		BrokerOrderID: fields["broker_order_id"],
		Outcome:       "executed",
	}, nil
}
