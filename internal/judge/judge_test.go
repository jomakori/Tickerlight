package judge

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"
)

// Table-driven: every rule in the table gets a case, plus the boundary at the
// ceiling itself (exactly at the ceiling must pass, one unit over must be
// penalised). All notionals are in cents (integer minor units), matching the
// engine's wire contract.
func TestScore(t *testing.T) {
	cfg := Config{NotionalCeilingCents: 100000, Allowlist: []string{"AAPL", "MSFT"}}

	cases := []struct {
		name   string
		cfg    Config
		order  Order
		score  float64
		reason string
	}{
		{
			name:   "allowed order within limits",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 2, NotionalCents: 50000},
			score:  1.0,
			reason: "within configured limits",
		},
		{
			name:   "exactly at the ceiling passes",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 1, NotionalCents: 100000},
			score:  1.0,
			reason: "within configured limits",
		},
		{
			name:   "below threshold order still scores full",
			cfg:    cfg,
			order:  Order{Symbol: "MSFT", Side: "sell", Qty: 1, NotionalCents: 1},
			score:  1.0,
			reason: "within configured limits",
		},
		{
			name:   "non-positive qty rejected",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 0, NotionalCents: 50000},
			score:  0,
			reason: "qty must be positive",
		},
		{
			name:   "negative qty rejected",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: -5, NotionalCents: 50000},
			score:  0,
			reason: "qty must be positive",
		},
		{
			name:   "non-positive notional rejected",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 2, NotionalCents: 0},
			score:  0,
			reason: "notional must be positive",
		},
		{
			name:   "negative notional rejected",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 2, NotionalCents: -10},
			score:  0,
			reason: "notional must be positive",
		},
		{
			name:   "unknown side rejected",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "hold", Qty: 1, NotionalCents: 10000},
			score:  0,
			reason: "unknown side",
		},
		{
			name:   "side is case-insensitive",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "BUY", Qty: 1, NotionalCents: 10000},
			score:  1.0,
			reason: "within configured limits",
		},
		{
			name:   "symbol outside allowlist rejected",
			cfg:    cfg,
			order:  Order{Symbol: "TSLA", Side: "buy", Qty: 1, NotionalCents: 10000},
			score:  0,
			reason: "symbol not in allowlist",
		},
		{
			name:   "allowlist match is case-insensitive",
			cfg:    cfg,
			order:  Order{Symbol: "aapl", Side: "buy", Qty: 1, NotionalCents: 10000},
			score:  1.0,
			reason: "within configured limits",
		},
		{
			name:   "empty allowlist permits any symbol",
			cfg:    Config{NotionalCeilingCents: 100000},
			order:  Order{Symbol: "DOGE-USD", Side: "buy", Qty: 1, NotionalCents: 10000},
			score:  1.0,
			reason: "within configured limits",
		},
		{
			name:   "notional one unit over ceiling is penalised not rejected",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 1, NotionalCents: 100001},
			score:  0.999995, // 1 - 0.5*(1/100000)
			reason: "notional exceeds per-order ceiling",
		},
		{
			name:   "notional double the ceiling halves the score",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 1, NotionalCents: 200000},
			score:  0.5, // 1 - 0.5*(100000/100000)
			reason: "notional exceeds per-order ceiling",
		},
		{
			name:   "extreme overshoot floors at 0.05, never 0",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 1, NotionalCents: 10_000_000},
			score:  0.05,
			reason: "notional exceeds per-order ceiling",
		},
		{
			name:   "zero ceiling disables the ceiling check",
			cfg:    Config{NotionalCeilingCents: 0, Allowlist: []string{"AAPL"}},
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 1, NotionalCents: 100_000_000},
			score:  1.0,
			reason: "within configured limits",
		},
		{
			name:   "rejects are checked before the ceiling penalty",
			cfg:    cfg,
			order:  Order{Symbol: "TSLA", Side: "buy", Qty: 1, NotionalCents: 500000},
			score:  0,
			reason: "symbol not in allowlist",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Score(c.cfg, c.order)
			if got.Score != c.score {
				t.Errorf("Score = %v, want %v", got.Score, c.score)
			}
			if got.Reason != c.reason {
				t.Errorf("Reason = %q, want %q", got.Reason, c.reason)
			}
			if got.Score < 0 || got.Score > 1 {
				t.Errorf("Score %v out of [0,1]", got.Score)
			}
		})
	}
}

// TestScoreHandlerEnginePayload feeds the engine's literal wire payload
// through the real HTTP handler (not the pure Score function) to pin the
// actual JSON contract: notional_cents as an integer, not a float dollar
// amount. This is the regression test for the notional/notional_cents
// mismatch.
func TestScoreHandlerEnginePayload(t *testing.T) {
	// Unrestricted config: this test pins the wire contract (notional_cents
	// decodes and scores), not the ceiling math — the ceiling boundary
	// cases are already covered by TestScore.
	cfg := Config{}
	handler := NewHandler(cfg, slog.New(slog.DiscardHandler))

	body := []byte(`{"symbol":"F","side":"buy","qty":7.9365,"notional_cents":10000}`)
	req := httptest.NewRequest("POST", "/score", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var got Result
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	if got.Score != 1.0 {
		t.Errorf("Score = %v, want 1.0", got.Score)
	}
	if got.Reason != "within configured limits" {
		t.Errorf("Reason = %q, want %q", got.Reason, "within configured limits")
	}
}

func TestParseAllowlist(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty string", "", nil},
		{"whitespace only", "   ", nil},
		{"single symbol", "AAPL", []string{"AAPL"}},
		{"trims whitespace", " AAPL , MSFT ", []string{"AAPL", "MSFT"}},
		{"drops empty entries", "AAPL,,MSFT,", []string{"AAPL", "MSFT"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseAllowlist(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("ParseAllowlist(%q) = %v, want %v", c.in, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("ParseAllowlist(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
				}
			}
		})
	}
}
