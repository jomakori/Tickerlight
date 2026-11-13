// Package engine reads the trading engine's policy state over its MCP tool
// surface.
//
// It calls exactly one tool, caps_status, which is read-only. There is no
// method here that can place, cancel, size, arm or kill anything: the console
// holds a token for a read tool, and that is all it can do with it.
package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Caps is the engine's spend limits for the current day, in cents as the
// engine reports them, plus the policy the engine says it is running under.
// Nothing in this package supplies a default: a Caps value only ever comes
// from a successful caps_status call, and a policy field is empty when the
// engine did not report it.
type Caps struct {
	PerOrderCents  int64
	DailyCents     int64
	SpentCents     int64
	RemainingCents int64
	// Mode is the mode the engine reports it is running in: "paper" or
	// "live". Empty when the engine did not report one.
	Mode string
	// JudgeThreshold is the engine's own rendering of the judge score floor,
	// e.g. "0.5". Empty when the engine did not report one.
	JudgeThreshold string
	// AlertsConfigured is "configured" when the engine says its alert
	// sidecar started, "not configured" when it says it did not, and empty
	// when it said nothing either way.
	AlertsConfigured string
}

// Client talks JSON-RPC to the engine's MCP endpoint over the loopback
// interface.
type Client struct {
	// URL is the MCP endpoint, e.g. http://127.0.0.1:8090/mcp.
	URL string
	// Token is the bearer token the engine requires. An empty token still
	// sends the request; the engine answers 401 and the caller renders that
	// rather than a number.
	Token string
	// HTTP is the client used for the call. Callers pass the console's shared
	// client so the console keeps one outbound client, not one per request.
	HTTP *http.Client
}

// Caps asks the engine for its current caps and remaining headroom.
//
// Any failure — transport, auth, a JSON-RPC error, an isError tool result, or a
// payload that is not the shape this package understands — is returned as an
// error. Callers render the error; they must not substitute zeros.
func (c *Client) Caps(ctx context.Context) (Caps, error) {
	if c.URL == "" {
		return Caps{}, errors.New("caps_status: no engine endpoint configured")
	}
	body, err := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  rpcCall{Name: "caps_status", Arguments: struct{}{}},
	})
	if err != nil {
		return Caps{}, fmt.Errorf("caps_status: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return Caps{}, fmt.Errorf("caps_status: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// The streamable-HTTP transport may answer either with a JSON body or with
	// a one-event SSE stream; advertise both so the engine picks.
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.http().Do(req)
	if err != nil {
		return Caps{}, fmt.Errorf("caps_status: engine unreachable: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Caps{}, fmt.Errorf("caps_status: read response: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return Caps{}, fmt.Errorf("caps_status: engine rejected the console token (HTTP %d)", resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return Caps{}, fmt.Errorf("caps_status: engine returned HTTP %d", resp.StatusCode)
	}

	payload, err := ssePayload(resp.Header.Get("Content-Type"), raw)
	if err != nil {
		return Caps{}, err
	}

	var out rpcResponse
	if err := json.Unmarshal(payload, &out); err != nil {
		return Caps{}, fmt.Errorf("caps_status: decode response: %w", err)
	}
	if out.Error != nil {
		return Caps{}, fmt.Errorf("caps_status: engine error %d: %s", out.Error.Code, out.Error.Message)
	}
	if out.Result == nil {
		return Caps{}, errors.New("caps_status: response carries no result")
	}
	text := strings.TrimSpace(out.Result.text())
	if out.Result.IsError {
		if text == "" {
			text = "engine reported an error with no message"
		}
		return Caps{}, fmt.Errorf("caps_status: %s", text)
	}
	if text == "" {
		return Caps{}, errors.New("caps_status: engine returned no text")
	}
	return ParseCaps(text)
}

// ParseCaps reads the caps_status text payload:
//
//	per_order_cap_cents=5000 daily_cap_cents=15000 daily_spent_cents=0 daily_remaining_cents=15000 mode=paper judge_threshold=0.5 alerts_configured=false
//
// The four money keys are required. A payload missing one, or carrying a value
// that is not an integer, is an error rather than a partial Caps with zeros in
// it — a zero cap and an unknown cap look nothing alike on screen, and only one
// of them is true.
//
// The policy keys are optional, because an engine that predates them is still a
// working engine: absent, unrecognised or out-of-range values leave the field
// empty so the panel renders "not reported". Absence is never read as paper
// mode, a zero threshold or alerts being off.
func ParseCaps(text string) (Caps, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return Caps{}, errors.New("caps_status: empty payload")
	}
	vals := make(map[string]string, len(fields))
	for _, f := range fields {
		k, v, ok := strings.Cut(f, "=")
		if !ok || k == "" {
			return Caps{}, fmt.Errorf("caps_status: malformed field %q", f)
		}
		vals[k] = v
	}

	var c Caps
	for _, need := range []struct {
		key string
		dst *int64
	}{
		{"per_order_cap_cents", &c.PerOrderCents},
		{"daily_cap_cents", &c.DailyCents},
		{"daily_spent_cents", &c.SpentCents},
		{"daily_remaining_cents", &c.RemainingCents},
	} {
		raw, ok := vals[need.key]
		if !ok {
			return Caps{}, fmt.Errorf("caps_status: payload is missing %s", need.key)
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return Caps{}, fmt.Errorf("caps_status: %s is not an integer: %q", need.key, raw)
		}
		*need.dst = n
	}
	c.Mode = parseMode(vals["mode"])
	c.JudgeThreshold = parseThreshold(vals["judge_threshold"])
	c.AlertsConfigured = parseAlerts(vals["alerts_configured"])
	return c, nil
}

// parseMode returns the engine's mode when it named one it could be running,
// and "" otherwise. The engine itself reports paper for anything that is not
// exactly live; the console only echoes a mode it recognises.
func parseMode(raw string) string {
	switch m := strings.ToLower(strings.TrimSpace(raw)); m {
	case "paper", "live":
		return m
	}
	return ""
}

// parseThreshold returns the engine's judge floor as a decimal string, or ""
// when the value is not a score between 0 and 1. The engine already sends the
// shortest round-trip form, so this re-renders the same digits; anything outside
// a probability range is not a threshold and is not shown.
func parseThreshold(raw string) string {
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || f < 0 || f > 1 {
		return ""
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// parseAlerts maps the engine's boolean onto the panel's words. "" means the
// engine did not say, which is not the same as saying alerts are off.
func parseAlerts(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true":
		return "configured"
	case "false":
		return "not configured"
	}
	return ""
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// ---------- JSON-RPC wire types ----------

type rpcRequest struct {
	JSONRPC string  `json:"jsonrpc"`
	ID      int     `json:"id"`
	Method  string  `json:"method"`
	Params  rpcCall `json:"params"`
}

type rpcCall struct {
	Name      string `json:"name"`
	Arguments any    `json:"arguments"`
}

type rpcResponse struct {
	Result *rpcResult `json:"result"`
	Error  *rpcError  `json:"error"`
}

type rpcResult struct {
	Content []rpcContent `json:"content"`
	IsError bool         `json:"isError"`
}

type rpcContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// text joins every text part of a tool result, or returns "" when it carries
// none.
func (r *rpcResult) text() string {
	var parts []string
	for _, c := range r.Content {
		if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
			parts = append(parts, strings.TrimSpace(c.Text))
		}
	}
	return strings.Join(parts, "\n")
}

// ssePayload unwraps a one-event server-sent-events body. Plain JSON bodies are
// returned unchanged.
func ssePayload(contentType string, body []byte) ([]byte, error) {
	if !strings.Contains(contentType, "text/event-stream") {
		return body, nil
	}
	var payload []byte
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimRight(line, "\r")
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		if d := strings.TrimSpace(data); d != "" {
			payload = []byte(d)
		}
	}
	if payload == nil {
		return nil, errors.New("caps_status: SSE response carried no data event")
	}
	return payload, nil
}
