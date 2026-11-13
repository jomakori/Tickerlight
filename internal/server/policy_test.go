package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jomakori/tickerlight/internal/engine"
	"github.com/jomakori/tickerlight/internal/tickerdata"
)

// capsPayload is what trading-mcp answers to tools/call caps_status with
// TRADING_MCP_PER_ORDER_CAP_CENTS=5000, TRADING_MCP_DAILY_CAP_CENTS=15000, live
// mode, a 0.85 judge floor and no alert sidecar. The policy keys are the ones
// the engine appends after the money keys; the panel reads all seven from here.
const capsPayload = `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"per_order_cap_cents=5000 daily_cap_cents=15000 daily_spent_cents=1250 daily_remaining_cents=13750 mode=live judge_threshold=0.85 alerts_configured=false"}]}}`

// capsPayloadNoPolicy is the older four-key line: an engine that predates the
// policy keys is still a working engine, and the panel must still show its caps
// while saying nothing about mode, threshold or alerts.
const capsPayloadNoPolicy = `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"per_order_cap_cents=5000 daily_cap_cents=15000 daily_spent_cents=1250 daily_remaining_cents=13750"}]}}`

// policyConsole wires the real handler to a real engine client pointed at a
// stub /mcp endpoint, so these tests cover the whole path the browser takes:
// request → engine client → caps_status → rendered HTML. It reads no console
// environment: every policy fact on the page has to come from the engine.
func policyConsole(t *testing.T, engineURL, token string) http.Handler {
	t.Helper()
	c := &engine.Client{URL: engineURL, Token: token, HTTP: &http.Client{Timeout: 2 * time.Second}}
	return New(Options{
		Rows: tickerdata.Sample(),
		Now:  func() time.Time { return tickerdata.SampleNow },
		Log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Caps: c.Caps,
	})
}

// stubMCP serves one caps_status response and records the tool names it was
// asked for.
func stubMCP(t *testing.T, body string) (*httptest.Server, *[]string) {
	t.Helper()
	var tools []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer read-token" {
			t.Errorf("Authorization = %q, want the configured bearer token", got)
		}
		var req struct {
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method != "tools/call" {
			t.Errorf("method = %q, want tools/call", req.Method)
		}
		tools = append(tools, req.Params.Name)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &tools
}

// guardRow is the exact markup of one Mode-and-guards row, so a test can name
// the value belonging to a label instead of matching a substring of the page.
// The page carries unrelated words like "paper only" in the source badge, so a
// bare substring match would prove nothing.
func guardRow(label, value string) string {
	return `<span class="tx2">` + label + `</span><b class="mono">` + value + `</b>`
}

func has(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Errorf("policy panel missing %q; body:\n%s", want, body)
	}
}

func lacks(t *testing.T, body, unwanted string) {
	t.Helper()
	if strings.Contains(body, unwanted) {
		t.Errorf("policy panel carries %q; body:\n%s", unwanted, body)
	}
}

func TestPolicyPanelReadsEngineCapsAndPolicy(t *testing.T) {
	srv, tools := stubMCP(t, capsPayload)
	h := policyConsole(t, srv.URL, "read-token")

	res := get(t, h, "/policy")
	if res.status != http.StatusOK {
		t.Fatalf("GET /policy = %d, want 200", res.status)
	}
	for _, want := range []string{
		"$50.00",  // per-order cap
		"$150.00", // daily cap and remaining headroom
		"$12.50",  // dispatched today
		"$137.50", // remaining headroom
		"Remaining headroom",
		"Per-order cap",
		"Daily cap",
		guardRow("Mode", "live"),
		guardRow("Judge threshold", "0.85"),
		guardRow("Alert sink", "not configured"),
	} {
		has(t, res.body, want)
	}
	lacks(t, res.body, "Caps unavailable")
	if n := strings.Count(res.body, `<b class="mono">not reported</b>`); n != 0 {
		t.Errorf("guards reported %d missing fields, want 0: the engine reported all three", n)
	}
	if len(*tools) != 1 || (*tools)[0] != "caps_status" {
		t.Errorf("engine tools called = %v, want exactly [caps_status]", *tools)
	}
}

// The engine's line is the only source: a console whose own environment names a
// different mode must still render what the engine said.
func TestPolicyPanelIgnoresConsoleEnvironment(t *testing.T) {
	t.Setenv("TRADING_MCP_MODE", "paper")
	t.Setenv("TRADING_MCP_JUDGE_THRESHOLD", "0.10")
	t.Setenv("TRADING_ALERT_WEBHOOK_URL", "https://hooks.example/console")

	srv, _ := stubMCP(t, capsPayload)
	h := policyConsole(t, srv.URL, "read-token")

	body := get(t, h, "/policy").body
	for _, want := range []string{
		guardRow("Mode", "live"),
		guardRow("Judge threshold", "0.85"),
		guardRow("Alert sink", "not configured"),
	} {
		has(t, body, want)
	}
	lacks(t, body, "0.10")
	lacks(t, body, "hooks.example")
}

// An engine that does not send the policy keys leaves those three rows empty.
// The caps are still real, so they are still shown — absence is not a zero.
func TestPolicyPanelReportsAbsentPolicyFields(t *testing.T) {
	srv, _ := stubMCP(t, capsPayloadNoPolicy)
	h := policyConsole(t, srv.URL, "read-token")

	res := get(t, h, "/policy")
	if res.status != http.StatusOK {
		t.Fatalf("GET /policy = %d, want 200", res.status)
	}
	lacks(t, res.body, "Caps unavailable")
	has(t, res.body, "$150.00")
	if n := strings.Count(res.body, `<b class="mono">not reported</b>`); n != 3 {
		t.Errorf("not-reported guards = %d, want 3 (mode, threshold, alert sink); body:\n%s", n, res.body)
	}
	for _, unwanted := range []string{
		guardRow("Mode", "paper"),
		guardRow("Mode", "live"),
		guardRow("Judge threshold", "0.00"),
		guardRow("Alert sink", "not configured"),
		guardRow("Alert sink", "configured"),
	} {
		lacks(t, res.body, unwanted)
	}
}

// One absent key costs one row, not the whole panel.
func TestPolicyPanelReportsOneAbsentPolicyField(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"per_order_cap_cents=5000 daily_cap_cents=15000 daily_spent_cents=0 daily_remaining_cents=15000 mode=paper alerts_configured=true"}]}}`
	srv, _ := stubMCP(t, body)
	h := policyConsole(t, srv.URL, "read-token")

	res := get(t, h, "/policy")
	if n := strings.Count(res.body, `<b class="mono">not reported</b>`); n != 1 {
		t.Errorf("not-reported guards = %d, want 1 (the absent judge threshold); body:\n%s", n, res.body)
	}
	has(t, res.body, guardRow("Mode", "paper"))
	has(t, res.body, guardRow("Alert sink", "configured"))
}

func TestPolicyPanelRendersUnavailableWhenEngineFails(t *testing.T) {
	// A closed listener: the engine is not up, which is the ordinary shape of
	// "unreachable" during a rollout.
	srv, _ := stubMCP(t, capsPayload)
	url := srv.URL
	srv.Close()

	h := policyConsole(t, url, "read-token")

	res := get(t, h, "/policy")
	if res.status != http.StatusOK {
		t.Fatalf("GET /policy = %d, want 200: an unreachable engine is not a page error", res.status)
	}
	has(t, res.body, "Caps unavailable")
	has(t, res.body, "unreachable")
	// No fabricated caps: none of the engine's numbers, and no zero either.
	for _, unwanted := range []string{"$50.00", "$150.00", "$0.00", "$12.50"} {
		lacks(t, res.body, unwanted)
	}
	// Unreachable means every policy fact is unknown too: each guard says so
	// rather than guessing or falling back to the console's own environment.
	if n := strings.Count(res.body, `<b class="mono">not reported</b>`); n != 3 {
		t.Errorf("guards reported %d times, want 3 (mode, threshold, alert sink); body:\n%s", n, res.body)
	}
	for _, unwanted := range []string{
		guardRow("Mode", "paper"),
		guardRow("Mode", "live"),
		guardRow("Alert sink", "configured"),
	} {
		lacks(t, res.body, unwanted)
	}
}

func TestPolicyPanelRendersUnavailableWithoutEngineClient(t *testing.T) {
	res := get(t, fixture(t), "/policy")
	if res.status != http.StatusOK {
		t.Fatalf("GET /policy = %d, want 200", res.status)
	}
	has(t, res.body, "no engine client is configured")
	has(t, res.body, "Caps unavailable")
	if n := strings.Count(res.body, `<b class="mono">not reported</b>`); n != 3 {
		t.Errorf("guards reported %d times, want 3; body:\n%s", n, res.body)
	}
}

func TestPolicyPanelReportsEngineErrorText(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"caps_status: journal: open: permission denied"}],"isError":true}}`
	srv, _ := stubMCP(t, body)
	h := policyConsole(t, srv.URL, "read-token")

	res := get(t, h, "/policy")
	has(t, res.body, "permission denied")
	// Nothing was read, so no policy fact is known: the panel does not pair
	// the failure with a mode or threshold from anywhere else.
	if n := strings.Count(res.body, `<b class="mono">not reported</b>`); n != 3 {
		t.Errorf("guards reported %d times, want 3; body:\n%s", n, res.body)
	}
}

func TestPolicyPanelHonoursACancelledRequest(t *testing.T) {
	// A request the client abandoned must not produce a reading: the panel
	// reports the failure instead of stale numbers.
	srv, _ := stubMCP(t, capsPayload)
	c := &engine.Client{URL: srv.URL, Token: "read-token", HTTP: srv.Client()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Caps(ctx); err == nil {
		t.Fatal("Caps with a cancelled context returned a reading")
	}
}

func TestPolicyPanelHasNoWriteControls(t *testing.T) {
	srv, _ := stubMCP(t, capsPayload)
	h := policyConsole(t, srv.URL, "read-token")

	res := get(t, h, "/policy")
	for _, verb := range []string{"hx-post", "hx-put", "hx-patch", "hx-delete", `method="post"`, "<button", "<form", "<input"} {
		lacks(t, res.body, verb)
	}
	// The nav is the only thing that links anywhere, and it links to panels.
	for _, path := range []string{"/arm", "/kill", "/order"} {
		lacks(t, res.body, `href="`+path+`"`)
	}
}

func TestPolicyRejectsWriteMethods(t *testing.T) {
	h := fixture(t)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		res := fetch(t, h, m, "/policy", nil)
		if res.status != http.StatusNotFound && res.status != http.StatusMethodNotAllowed {
			t.Errorf("%s /policy = %d, want 404 or 405", m, res.status)
		}
	}
}

// The unavailable path must also survive a nil result on the engine side — a
// server that answers 200 with nothing useful is not a reading.
func TestPolicyPanelTreatsEmptyEngineAnswerAsUnavailable(t *testing.T) {
	srv, _ := stubMCP(t, `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`)
	h := policyConsole(t, srv.URL, "read-token")

	res := get(t, h, "/policy")
	has(t, res.body, "Caps unavailable")
	has(t, res.body, "no text")
	if n := strings.Count(res.body, `<b class="mono">not reported</b>`); n != 3 {
		t.Errorf("guards reported %d times, want 3; body:\n%s", n, res.body)
	}
}
