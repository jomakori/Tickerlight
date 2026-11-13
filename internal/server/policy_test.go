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
// TRADING_MCP_PER_ORDER_CAP_CENTS=5000 and TRADING_MCP_DAILY_CAP_CENTS=15000.
const capsPayload = `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"per_order_cap_cents=5000 daily_cap_cents=15000 daily_spent_cents=1250 daily_remaining_cents=13750"}]}}`

// policyConsole wires the real handler to a real engine client pointed at a
// stub /mcp endpoint, so these tests cover the whole path the browser takes:
// request → engine client → caps_status → rendered HTML.
func policyConsole(t *testing.T, engineURL, token string, declared engine.Declared) http.Handler {
	t.Helper()
	c := &engine.Client{URL: engineURL, Token: token, HTTP: &http.Client{Timeout: 2 * time.Second}}
	return New(Options{
		Rows:   tickerdata.Sample(),
		Now:    func() time.Time { return tickerdata.SampleNow },
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Caps:   c.Caps,
		Policy: declared,
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

func TestPolicyPanelReadsEngineCaps(t *testing.T) {
	srv, tools := stubMCP(t, capsPayload)
	h := policyConsole(t, srv.URL, "read-token", engine.Declared{
		Mode:           "paper",
		JudgeThreshold: "0.55",
		AlertSink:      "configured",
	})

	res := get(t, h, "/policy")
	if res.status != http.StatusOK {
		t.Fatalf("GET /policy = %d, want 200", res.status)
	}
	for _, want := range []string{
		"$50.00",  // per-order cap
		"$150.00", // daily cap and remaining headroom
		"$12.50",  // dispatched today
		"$137.50", // remaining headroom
		"paper",
		"0.55",
		"configured",
		"Remaining headroom",
		"Per-order cap",
		"Daily cap",
	} {
		if !strings.Contains(res.body, want) {
			t.Errorf("policy panel missing %q", want)
		}
	}
	if strings.Contains(res.body, "Caps unavailable") {
		t.Error("panel rendered the unavailable state with a live engine")
	}
	if len(*tools) != 1 || (*tools)[0] != "caps_status" {
		t.Errorf("engine tools called = %v, want exactly [caps_status]", *tools)
	}
}

func TestPolicyPanelRendersUnavailableWhenEngineFails(t *testing.T) {
	// A closed listener: the engine is not up, which is the ordinary shape of
	// "unreachable" during a rollout.
	srv, _ := stubMCP(t, capsPayload)
	url := srv.URL
	srv.Close()

	h := policyConsole(t, url, "read-token", engine.Declared{})

	res := get(t, h, "/policy")
	if res.status != http.StatusOK {
		t.Fatalf("GET /policy = %d, want 200: an unreachable engine is not a page error", res.status)
	}
	if !strings.Contains(res.body, "Caps unavailable") {
		t.Error("panel did not render the unavailable state")
	}
	if !strings.Contains(res.body, "unreachable") {
		t.Errorf("panel did not show the engine failure; body:\n%s", res.body)
	}
	// No fabricated caps: none of the engine's numbers, and no zero either.
	for _, unwanted := range []string{"$50.00", "$150.00", "$0.00", "$12.50"} {
		if strings.Contains(res.body, unwanted) {
			t.Errorf("unavailable panel showed %q, which the engine never reported", unwanted)
		}
	}
	// With no declared policy either, each guard says so rather than guessing.
	if n := strings.Count(res.body, `<b class="mono">not reported</b>`); n != 3 {
		t.Errorf("guards reported %d times, want 3 (mode, threshold, alert sink); body:\n%s", n, res.body)
	}
}

func TestPolicyPanelRendersUnavailableWithoutEngineClient(t *testing.T) {
	res := get(t, fixture(t), "/policy")
	if res.status != http.StatusOK {
		t.Fatalf("GET /policy = %d, want 200", res.status)
	}
	if !strings.Contains(res.body, "no engine client is configured") {
		t.Errorf("panel did not explain the missing engine client; body:\n%s", res.body)
	}
	if !strings.Contains(res.body, "Caps unavailable") {
		t.Error("panel did not render the unavailable state")
	}
}

func TestPolicyPanelReportsEngineErrorText(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"caps_status: journal: open: permission denied"}],"isError":true}}`
	srv, _ := stubMCP(t, body)
	h := policyConsole(t, srv.URL, "read-token", engine.Declared{Mode: "live", JudgeThreshold: "0.40", AlertSink: "configured"})

	res := get(t, h, "/policy")
	if !strings.Contains(res.body, "permission denied") {
		t.Errorf("panel swallowed the engine's error; body:\n%s", res.body)
	}
	// The guards it can see are still shown; only the caps are missing.
	for _, want := range []string{"live", "0.40", "configured"} {
		if !strings.Contains(res.body, want) {
			t.Errorf("panel missing %q", want)
		}
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
	h := policyConsole(t, srv.URL, "read-token", engine.Declared{Mode: "paper"})

	res := get(t, h, "/policy")
	for _, verb := range []string{"hx-post", "hx-put", "hx-patch", "hx-delete", `method="post"`, "<button", "<form", "<input"} {
		if strings.Contains(res.body, verb) {
			t.Errorf("policy panel carries %q; it must be read-only", verb)
		}
	}
	// The nav is the only thing that links anywhere, and it links to panels.
	for _, path := range []string{"/arm", "/kill", "/order"} {
		if strings.Contains(res.body, `href="`+path+`"`) {
			t.Errorf("policy panel links to %s", path)
		}
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
	h := policyConsole(t, srv.URL, "read-token", engine.Declared{})

	res := get(t, h, "/policy")
	if !strings.Contains(res.body, "Caps unavailable") {
		t.Error("empty engine answer rendered as a reading")
	}
	if !strings.Contains(res.body, "no text") {
		t.Errorf("panel did not explain the empty answer; body:\n%s", res.body)
	}
}
