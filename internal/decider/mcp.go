package decider

// JSON-RPC 2.0 wire plumbing shared by every engine MCP tool call
// (internal/decider/engine.go). Self-contained here rather than reusing
// internal/engine's unexported types, since this package must stay an
// additive, independent client of the engine's tool surface.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// httpDoer is the one HTTP seam both the engine MCP client (engine.go)
// and the Jev client (jev.go) depend on, so every table-driven test in
// this package injects a fake instead of touching a real socket.
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

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

// text joins every text part of a tool result, or returns "" when it
// carries none.
func (r *rpcResult) text() string {
	var parts []string
	for _, c := range r.Content {
		if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
			parts = append(parts, strings.TrimSpace(c.Text))
		}
	}
	return strings.Join(parts, "\n")
}

// callTool makes exactly one tools/call request and returns the tool
// result's text and its isError flag. It never retries and never loops:
// a transport failure, non-200, or unparsable body is returned as err.
func callTool(ctx context.Context, url, token, name string, args any, doer httpDoer) (text string, isError bool, err error) {
	body, err := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  rpcCall{Name: name, Arguments: args},
	})
	if err != nil {
		return "", false, fmt.Errorf("decider: encode %s request: %w", name, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", false, fmt.Errorf("decider: build %s request: %w", name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	// The engine's streamable-HTTP transport may answer either with a
	// JSON body or a one-event SSE stream; advertise both so it picks.
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := doer.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("decider: %s: engine unreachable: %w", name, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", false, fmt.Errorf("decider: %s: read response: %w", name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("decider: %s: engine returned HTTP %d", name, resp.StatusCode)
	}

	payload, err := ssePayload(resp.Header.Get("Content-Type"), raw)
	if err != nil {
		return "", false, fmt.Errorf("decider: %s: %w", name, err)
	}

	var out rpcResponse
	if err := json.Unmarshal(payload, &out); err != nil {
		return "", false, fmt.Errorf("decider: %s: decode response: %w", name, err)
	}
	if out.Error != nil {
		return "", false, fmt.Errorf("decider: %s: engine error %d: %s", name, out.Error.Code, out.Error.Message)
	}
	if out.Result == nil {
		return "", false, fmt.Errorf("decider: %s: response carries no result", name)
	}
	return out.Result.text(), out.Result.IsError, nil
}

// ssePayload unwraps a one-event server-sent-events body; a plain JSON
// body is returned unchanged.
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
		return nil, errors.New("SSE response carried no data event")
	}
	return payload, nil
}

// parseFields reads a space-separated key=value text payload, the shape
// every engine tool in this server returns.
func parseFields(text string) map[string]string {
	fields := strings.Fields(text)
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		k, v, ok := strings.Cut(f, "=")
		if ok {
			out[k] = v
		}
	}
	return out
}

// extractReason pulls the reason=<code> token out of a blocked tool
// result's error text, e.g. "place_order: outcome=blocked reason=killed".
func extractReason(text string) string {
	return parseFields(text)["reason"]
}
