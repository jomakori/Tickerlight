// Package judge is a small, deterministic rule-based scorer for order
// attempts. It never calls a model and never uses randomness: the same
// input always produces the same score and the same one-line reason.
//
// Rule table (first match wins; hard rejects are checked before the ceiling
// penalty):
//
//	Rule                      Condition                          Score                                   Reason
//	non-positive qty          qty <= 0                            0.0                                     "qty must be positive"
//	non-positive notional     notional <= 0                       0.0                                     "notional must be positive"
//	unknown side              side not "buy"/"sell" (ci)          0.0                                     "unknown side"
//	symbol not allowlisted    allowlist set, symbol not in it     0.0                                     "symbol not in allowlist"
//	notional over ceiling     notional > ceiling (>0)             max(0.05, 1 - 0.5*(notional/ceiling-1)) "notional exceeds per-order ceiling"
//	within configured limits  otherwise                           1.0                                     "within configured limits"
package judge

import (
	"fmt"
	"strings"
)

// Score boundary and penalty weights. Chosen so a 100% overshoot of the
// per-order ceiling (double the allowed notional) halves the score, and the
// penalty floors at 0.05 rather than 0 so a steep overshoot still reads as
// "penalised" rather than indistinguishable from a hard reject.
const (
	scoreReject          = 0.0
	scoreMax             = 1.0
	ceilingPenaltyFactor = 0.5  // score lost per 100% of notional over the ceiling
	ceilingPenaltyFloor  = 0.05 // minimum score a ceiling-penalised (not rejected) order can receive
)

// Config carries every threshold the scorer applies. Zero values are valid:
// a zero NotionalCeiling disables the ceiling check (nothing to compare
// against) and a nil/empty Allowlist disables the allowlist check.
type Config struct {
	// NotionalCeiling is the per-order notional above which the score is
	// penalised. <= 0 disables the check.
	NotionalCeiling float64
	// Allowlist is the set of symbols permitted to score above zero. Empty
	// disables the check so every symbol is allowed.
	Allowlist []string
}

// allowed reports whether symbol passes the configured allowlist. An empty
// allowlist means "no restriction configured", not "nothing is allowed".
// Matching is case-insensitive so "btc-usd" and "BTC-USD" are the same
// symbol.
func (c Config) allowed(symbol string) bool {
	if len(c.Allowlist) == 0 {
		return true
	}
	for _, s := range c.Allowlist {
		if strings.EqualFold(s, symbol) {
			return true
		}
	}
	return false
}

// Order is the minimal shape the scorer needs. Fields beyond these are not
// examined; the scorer does not reach a venue or hold any order state.
type Order struct {
	Symbol   string  `json:"symbol"`
	Side     string  `json:"side"`
	Qty      float64 `json:"qty"`
	Notional float64 `json:"notional"`
}

// Result is the scorer's verdict: a score in [0,1] and the single rule that
// produced it.
type Result struct {
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// Score applies the rule table above to one order. It is pure and
// deterministic: no I/O, no model call, no randomness.
func Score(cfg Config, o Order) Result {
	if o.Qty <= 0 {
		return Result{Score: scoreReject, Reason: "qty must be positive"}
	}
	if o.Notional <= 0 {
		return Result{Score: scoreReject, Reason: "notional must be positive"}
	}
	side := strings.ToLower(strings.TrimSpace(o.Side))
	if side != "buy" && side != "sell" {
		return Result{Score: scoreReject, Reason: "unknown side"}
	}
	if !cfg.allowed(o.Symbol) {
		return Result{Score: scoreReject, Reason: "symbol not in allowlist"}
	}
	if cfg.NotionalCeiling > 0 && o.Notional > cfg.NotionalCeiling {
		over := (o.Notional - cfg.NotionalCeiling) / cfg.NotionalCeiling
		score := scoreMax - ceilingPenaltyFactor*over
		if score < ceilingPenaltyFloor {
			score = ceilingPenaltyFloor
		}
		return Result{Score: score, Reason: "notional exceeds per-order ceiling"}
	}
	return Result{Score: scoreMax, Reason: "within configured limits"}
}

// ParseAllowlist splits a comma-separated allowlist flag/env value into its
// symbols, trimming whitespace and dropping empty entries so "AAPL, ,MSFT"
// and a trailing comma don't produce a phantom empty symbol that matches
// nothing.
func ParseAllowlist(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// String renders the config for a one-line startup log, so an operator can
// see what the judge listener will actually enforce without reading flags.
func (c Config) String() string {
	allow := "any"
	if len(c.Allowlist) > 0 {
		allow = strings.Join(c.Allowlist, ",")
	}
	return fmt.Sprintf("ceiling=%g allowlist=%s", c.NotionalCeiling, allow)
}
