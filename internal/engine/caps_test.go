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
// TRADING_MCP_PER_ORDER_CAP_CENTS=5000 and TRADING_MCP_DAILY_CAP_CENTS=15000,
// recorded from the binary so these tests pin the wire format the console will
// actually meet, not one invented here.
const capturedJSON = `{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"per_order_cap_cents=5000 daily_cap_cents=15000 daily_spent_cents=0 daily_remaining_cents=15000"}]}}`

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
	want := Caps{PerOrderCents: 5000, DailyCents: 15000, SpentCents: 0, RemainingCents: 15000}
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
