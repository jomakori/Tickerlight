package engine

import (
	"strconv"
	"strings"
)

// Declared is the part of the engine's policy this process can read from its
// own environment, under the variable names the engine itself reads.
//
// It exists because caps_status reports caps and nothing else: mode, the judge
// threshold and the alert sink are engine configuration, not engine state, and
// no read-only tool exposes them. Each field is empty when this process does
// not have the key, and empty renders as "not reported" — an absent key says
// nothing about how the engine is configured, so the console never reads
// absence as "off" or "paper".
type Declared struct {
	// Mode is "paper" or "live" (engine: TRADING_MCP_MODE).
	Mode string
	// JudgeThreshold is the formatted 0..1 score floor the engine enforces
	// (engine: TRADING_MCP_JUDGE_THRESHOLD).
	JudgeThreshold string
	// AlertSink is "configured" when this process has a non-empty alert
	// webhook URL (engine: TRADING_ALERT_WEBHOOK_URL).
	AlertSink string
}

// DeclaredFromEnv reads the engine-compatible keys through get, so the caller
// decides where the values come from (os.LookupEnv in production, a map in
// tests).
func DeclaredFromEnv(get func(string) (string, bool)) Declared {
	var d Declared

	if v, ok := get("TRADING_MCP_MODE"); ok {
		switch m := strings.ToLower(strings.TrimSpace(v)); m {
		case "paper", "live":
			d.Mode = m
		}
	}

	if v, ok := get("TRADING_MCP_JUDGE_THRESHOLD"); ok {
		// A threshold outside 0..1 cannot be a score floor; report nothing
		// rather than echoing a value the engine would reject.
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && f >= 0 && f <= 1 {
			d.JudgeThreshold = strconv.FormatFloat(f, 'f', 2, 64)
		}
	}

	if v, ok := get("TRADING_ALERT_WEBHOOK_URL"); ok && strings.TrimSpace(v) != "" {
		d.AlertSink = "configured"
	}

	return d
}
