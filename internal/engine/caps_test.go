package engine

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The payloads below are what the real engine answers. capturedJSON is the
// verbatim body of a tools/call caps_status response from trading-mcp with
// TRADING_MCP_PER_ORDER_CAP_CENTS=5000, TRADING_MCP_DAILY_CAP_CENTS=15000, paper
// mode, the default 0.5 judge floor and no alert sidecar, recorded from the
// binary so these tests pin the wire format the console will actually meet, not
// one invented here.
const capturedJSON = `{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"per_order_cap_cents=5000 daily_cap_cents=15000 daily_spent_cents=0 daily_remaining_cents=15000 mode=paper judge_threshold=0.5 alerts_configured=false"}]}}`

const capturedSSE = "event: message\ndata: " + capturedJSON + "\n\n"

// mcpServer stands in for the engine's /mcp endpoint. It records the request it
// saw so a test can assert the console asked for caps_status and nothing else.
type mcpServer struct {
	*httptest.Server
	gotAuth  string
	gotBody  string
	calls    int
	nonTools int // requests that were not tools/call caps_status
}

func newMCP(t *testing.T, contentType, body string, status int) *mcpServer {
	t.Helper()
	m := &mcpServer{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			m.nonTools++
		}
		m.calls++
		m.gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		m.gotBody = string(raw)

		var req struct {
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		_ = json.Unmarshal(raw, &req)
		if req.Method != "tools/call" || req.Params.Name != "caps_status" {
			m.nonTools++
		}

		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(m.Close)
	return m
}

func client(m *mcpServer, token string) *Client {
	return &Client{URL: m.URL + "/mcp", Token: token, HTTP: m.Client()}
}

func TestCapsReadsEnginePayload(t *testing.T) {
	m := newMCP(t, "application/json", capturedJSON, http.StatusOK)

	caps, err := client(m, "tok").Caps(context.Background())
	if err != nil {
		t.Fatalf("Caps: %v", err)
	}
	want := Caps{
		PerOrderCents:    5000,
		DailyCents:       15000,
		SpentCents:       0,
		RemainingCents:   15000,
		Mode:             "paper",
		JudgeThreshold:   "0.5",
		AlertsConfigured: "not configured",
	}
	if caps != want {
		t.Errorf("Caps = %+v, want %+v", caps, want)
	}
	if m.gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q, want the bearer token from config", m.gotAuth)
	}
	if m.nonTools != 0 {
		t.Errorf("client made %d non caps_status calls; it must only read caps", m.nonTools)
	}
}

func TestCapsAcceptsSSETransport(t *testing.T) {
	m := newMCP(t, "text/event-stream", capturedSSE, http.StatusOK)

	caps, err := client(m, "tok").Caps(context.Background())
	if err != nil {
		t.Fatalf("Caps over SSE: %v", err)
	}
	if caps.RemainingCents != 15000 {
		t.Errorf("remaining = %d, want 15000", caps.RemainingCents)
	}
}

func TestCapsReportsAuthRejection(t *testing.T) {
	m := newMCP(t, "application/json", `{"jsonrpc":"2.0","id":1,"error":{"code":-32001,"message":"unauthorized"}}`, http.StatusUnauthorized)

	_, err := client(m, "wrong").Caps(context.Background())
	if err == nil {
		t.Fatal("Caps accepted a 401")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "token") {
		t.Errorf("error = %q, want it to name the rejected token and the status", err)
	}
}

func TestCapsSurfacesToolError(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"caps_status: journal: open: permission denied"}],"isError":true}}`
	m := newMCP(t, "application/json", body, http.StatusOK)

	_, err := client(m, "tok").Caps(context.Background())
	if err == nil {
		t.Fatal("Caps accepted an isError result")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error = %q, want the engine's own message", err)
	}
}

func TestCapsRejectsUnparsablePayload(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "not key=value",
			body: `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"cannot read caps"}]}}`,
			want: "malformed field",
		},
		{
			name: "non integer value",
			body: `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"per_order_cap_cents=lots daily_cap_cents=15000 daily_spent_cents=0 daily_remaining_cents=15000"}]}}`,
			want: "not an integer",
		},
		{
			name: "missing daily cap",
			body: `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"per_order_cap_cents=5000 daily_spent_cents=0 daily_remaining_cents=15000"}]}}`,
			want: "missing daily_cap_cents",
		},
		{
			name: "empty text",
			body: `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`,
			want: "no text",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMCP(t, "application/json", tc.body, http.StatusOK)
			caps, err := client(m, "tok").Caps(context.Background())
			if err == nil {
				t.Fatalf("Caps returned %+v for an unusable payload", caps)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want %q", err, tc.want)
			}
		})
	}
}

func TestCapsReportsUnreachableEngine(t *testing.T) {
	// A closed listener is the real shape of "the engine is not up yet": the
	// console renders that state instead of numbers.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	c := &Client{URL: "http://" + addr + "/mcp", Token: "tok", HTTP: &http.Client{Timeout: 2 * time.Second}}
	caps, err := c.Caps(context.Background())
	if err == nil {
		t.Fatalf("Caps returned %+v with no engine listening", caps)
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("error = %q, want it to say the engine is unreachable", err)
	}
}

func TestCapsWithoutEndpointFails(t *testing.T) {
	if _, err := (&Client{}).Caps(context.Background()); err == nil {
		t.Fatal("Caps without a URL must fail, not invent a reading")
	}
}

func TestParseCapsToleratesNewKeysAndSigns(t *testing.T) {
	caps, err := ParseCaps("per_order_cap_cents=5000 daily_cap_cents=15000 daily_spent_cents=16000 daily_remaining_cents=-1000 future_cap_cents=7")
	if err != nil {
		t.Fatalf("ParseCaps: %v", err)
	}
	if caps.RemainingCents != -1000 {
		t.Errorf("remaining = %d, want -1000: an over-cap day is negative, not clamped to zero", caps.RemainingCents)
	}
}

// The engine's policy keys are read from the same line as the money keys. These
// are the shapes trading-mcp actually renders: lowercase mode, shortest
// round-trip decimal, and a JSON boolean.
func TestParseCapsReadsEnginePolicy(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want Caps
	}{
		{
			name: "paper with alerts off",
			text: "per_order_cap_cents=10000 daily_cap_cents=50000 daily_spent_cents=0 daily_remaining_cents=50000 mode=paper judge_threshold=0.5 alerts_configured=false",
			want: Caps{PerOrderCents: 10000, DailyCents: 50000, RemainingCents: 50000, Mode: "paper", JudgeThreshold: "0.5", AlertsConfigured: "not configured"},
		},
		{
			name: "live with alerts on",
			text: "per_order_cap_cents=10000 daily_cap_cents=50000 daily_spent_cents=0 daily_remaining_cents=50000 mode=live judge_threshold=0.85 alerts_configured=true",
			want: Caps{PerOrderCents: 10000, DailyCents: 50000, RemainingCents: 50000, Mode: "live", JudgeThreshold: "0.85", AlertsConfigured: "configured"},
		},
		{
			name: "threshold of one and a long decimal",
			text: "per_order_cap_cents=1 daily_cap_cents=2 daily_spent_cents=0 daily_remaining_cents=2 mode=live judge_threshold=0.123456789 alerts_configured=true",
			want: Caps{PerOrderCents: 1, DailyCents: 2, RemainingCents: 2, Mode: "live", JudgeThreshold: "0.123456789", AlertsConfigured: "configured"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCaps(tc.text)
			if err != nil {
				t.Fatalf("ParseCaps: %v", err)
			}
			if got != tc.want {
				t.Errorf("Caps = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// An engine that predates the policy keys, or one whose values the console
// cannot use, must leave those fields empty. The four money keys are still a
// reading and must survive, and a bad value blanks only its own row: the panel
// shows the caps and says "not reported" for exactly what it could not read.
func TestParseCapsLeavesUnreportedPolicyEmpty(t *testing.T) {
	const money = "per_order_cap_cents=5000 daily_cap_cents=15000 daily_spent_cents=0 daily_remaining_cents=15000"
	for _, tc := range []struct {
		name                    string
		text                    string
		mode, threshold, alerts string
	}{
		{"absent", money, "", "", ""},
		{"unrecognised values", money + " mode=production judge_threshold=1.5 alerts_configured=maybe", "", "", ""},
		{"threshold not a number", money + " mode=paper judge_threshold=high alerts_configured=true", "paper", "", "configured"},
		{"threshold above one", money + " mode=paper judge_threshold=1.5 alerts_configured=false", "paper", "", "not configured"},
		{"threshold below zero", money + " mode=live judge_threshold=-0.2 alerts_configured=false", "live", "", "not configured"},
		{"blank values", money + " mode= judge_threshold= alerts_configured=", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCaps(tc.text)
			if err != nil {
				t.Fatalf("ParseCaps: %v", err)
			}
			if got.PerOrderCents != 5000 || got.DailyCents != 15000 || got.RemainingCents != 15000 {
				t.Errorf("money = %+v, want the reported caps", got)
			}
			if got.Mode != tc.mode || got.JudgeThreshold != tc.threshold || got.AlertsConfigured != tc.alerts {
				t.Errorf("policy = mode %q threshold %q alerts %q, want mode %q threshold %q alerts %q",
					got.Mode, got.JudgeThreshold, got.AlertsConfigured, tc.mode, tc.threshold, tc.alerts)
			}
		})
	}
}
