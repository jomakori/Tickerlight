package judge

import "testing"

// Table-driven: every rule in the table gets a case, plus the boundary at the
// ceiling itself (exactly at the ceiling must pass, one unit over must be
// penalised).
func TestScore(t *testing.T) {
	cfg := Config{NotionalCeiling: 1000, Allowlist: []string{"AAPL", "MSFT"}}

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
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 2, Notional: 500},
			score:  1.0,
			reason: "within configured limits",
		},
		{
			name:   "exactly at the ceiling passes",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 1, Notional: 1000},
			score:  1.0,
			reason: "within configured limits",
		},
		{
			name:   "below threshold order still scores full",
			cfg:    cfg,
			order:  Order{Symbol: "MSFT", Side: "sell", Qty: 1, Notional: 1},
			score:  1.0,
			reason: "within configured limits",
		},
		{
			name:   "non-positive qty rejected",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 0, Notional: 500},
			score:  0,
			reason: "qty must be positive",
		},
		{
			name:   "negative qty rejected",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: -5, Notional: 500},
			score:  0,
			reason: "qty must be positive",
		},
		{
			name:   "non-positive notional rejected",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 2, Notional: 0},
			score:  0,
			reason: "notional must be positive",
		},
		{
			name:   "negative notional rejected",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 2, Notional: -10},
			score:  0,
			reason: "notional must be positive",
		},
		{
			name:   "unknown side rejected",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "hold", Qty: 1, Notional: 100},
			score:  0,
			reason: "unknown side",
		},
		{
			name:   "side is case-insensitive",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "BUY", Qty: 1, Notional: 100},
			score:  1.0,
			reason: "within configured limits",
		},
		{
			name:   "symbol outside allowlist rejected",
			cfg:    cfg,
			order:  Order{Symbol: "TSLA", Side: "buy", Qty: 1, Notional: 100},
			score:  0,
			reason: "symbol not in allowlist",
		},
		{
			name:   "allowlist match is case-insensitive",
			cfg:    cfg,
			order:  Order{Symbol: "aapl", Side: "buy", Qty: 1, Notional: 100},
			score:  1.0,
			reason: "within configured limits",
		},
		{
			name:   "empty allowlist permits any symbol",
			cfg:    Config{NotionalCeiling: 1000},
			order:  Order{Symbol: "DOGE-USD", Side: "buy", Qty: 1, Notional: 100},
			score:  1.0,
			reason: "within configured limits",
		},
		{
			name:   "notional one unit over ceiling is penalised not rejected",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 1, Notional: 1000.0001},
			score:  0.99999995, // 1 - 0.5*(0.0001/1000)
			reason: "notional exceeds per-order ceiling",
		},
		{
			name:   "notional double the ceiling halves the score",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 1, Notional: 2000},
			score:  0.5, // 1 - 0.5*(1000/1000)
			reason: "notional exceeds per-order ceiling",
		},
		{
			name:   "extreme overshoot floors at 0.05, never 0",
			cfg:    cfg,
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 1, Notional: 100000},
			score:  0.05,
			reason: "notional exceeds per-order ceiling",
		},
		{
			name:   "zero ceiling disables the ceiling check",
			cfg:    Config{NotionalCeiling: 0, Allowlist: []string{"AAPL"}},
			order:  Order{Symbol: "AAPL", Side: "buy", Qty: 1, Notional: 1_000_000},
			score:  1.0,
			reason: "within configured limits",
		},
		{
			name:   "rejects are checked before the ceiling penalty",
			cfg:    cfg,
			order:  Order{Symbol: "TSLA", Side: "buy", Qty: 1, Notional: 5000},
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
