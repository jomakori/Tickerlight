package engine

import "testing"

func envFrom(pairs map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := pairs[k]
		return v, ok
	}
}

func TestDeclaredFromEnvReadsEngineKeys(t *testing.T) {
	d := DeclaredFromEnv(envFrom(map[string]string{
		"TRADING_MCP_MODE":                "LIVE",
		"TRADING_MCP_JUDGE_THRESHOLD":     "0.55",
		"TRADING_ALERT_WEBHOOK_URL":       "https://hooks.example/tickerlight-alerts",
		"TRADING_ALERT_WEBHOOK_SECRET":    "ignored",
		"TRADING_MCP_PER_ORDER_CAP_CENTS": "999999",
	}))

	if d.Mode != "live" {
		t.Errorf("Mode = %q, want live", d.Mode)
	}
	if d.JudgeThreshold != "0.55" {
		t.Errorf("JudgeThreshold = %q, want 0.55", d.JudgeThreshold)
	}
	if d.AlertSink != "configured" {
		t.Errorf("AlertSink = %q, want configured", d.AlertSink)
	}
}

// An absent key is not an answer. Every field stays empty so the panel can say
// "not reported" instead of asserting a mode or a threshold the console cannot
// see.
func TestDeclaredFromEnvReportsNothingWhenUnset(t *testing.T) {
	d := DeclaredFromEnv(envFrom(nil))
	if d != (Declared{}) {
		t.Errorf("Declared = %+v, want the zero value", d)
	}
}

func TestDeclaredFromEnvIgnoresUnusableValues(t *testing.T) {
	d := DeclaredFromEnv(envFrom(map[string]string{
		"TRADING_MCP_MODE":            "production",
		"TRADING_MCP_JUDGE_THRESHOLD": "1.5",
		"TRADING_ALERT_WEBHOOK_URL":   "   ",
	}))
	if d.Mode != "" {
		t.Errorf("Mode = %q, want empty for an unrecognised mode", d.Mode)
	}
	if d.JudgeThreshold != "" {
		t.Errorf("JudgeThreshold = %q, want empty for a threshold above 1", d.JudgeThreshold)
	}
	if d.AlertSink != "" {
		t.Errorf("AlertSink = %q, want empty for a blank URL", d.AlertSink)
	}
}

func TestDeclaredFromEnvAcceptsPaperAndZeroThreshold(t *testing.T) {
	d := DeclaredFromEnv(envFrom(map[string]string{
		"TRADING_MCP_MODE":            "paper",
		"TRADING_MCP_JUDGE_THRESHOLD": "0",
	}))
	if d.Mode != "paper" {
		t.Errorf("Mode = %q, want paper", d.Mode)
	}
	if d.JudgeThreshold != "0.00" {
		t.Errorf("JudgeThreshold = %q, want 0.00", d.JudgeThreshold)
	}
}
