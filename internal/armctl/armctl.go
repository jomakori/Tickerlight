// Package armctl posts the console's arming window to the engine's
// loopback-only admin route. It can only open a window: the engine owns the
// refusals for kills and for caps above its ceiling, and nothing here can
// disarm, size, cancel or clear the kill switch.
package armctl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrKilled reports that the engine's halt file exists. Arming is refused until
// an operator removes it, which no route and no tool does.
var ErrKilled = errors.New("engine is killed")

// Request is one arming window. Zero caps mean the engine's ceiling.
type Request struct {
	TTLSeconds       int
	PerOrderCapCents int64
	DailyCapCents    int64
}

// Result is the window the engine wrote.
type Result struct {
	ExpiresAt        time.Time
	PerOrderCapCents int64
	DailyCapCents    int64
}

// Client posts to the engine. URL points at the engine's loopback base URL,
// the same listener its MCP tools use.
type Client struct {
	URL  string
	HTTP *http.Client
}

type requestBody struct {
	TTLSeconds       int   `json:"ttl_seconds"`
	PerOrderCapCents int64 `json:"per_order_cap_cents"`
	DailyCapCents    int64 `json:"daily_cap_cents"`
}

type responseBody struct {
	ExpiresAt        time.Time `json:"expires_at"`
	PerOrderCapCents int64     `json:"per_order_cap_cents"`
	DailyCapCents    int64     `json:"daily_cap_cents"`
}

// Arm asks the engine to write a window. The engine's answer is returned as-is:
// a refusal is a fact the page has to show, not something to retry or soften.
func (c Client) Arm(ctx context.Context, req Request) (Result, error) {
	body, err := json.Marshal(requestBody{
		TTLSeconds:       req.TTLSeconds,
		PerOrderCapCents: req.PerOrderCapCents,
		DailyCapCents:    req.DailyCapCents,
	})
	if err != nil {
		return Result{}, err
	}
	url := strings.TrimRight(c.URL, "/") + "/admin/arm"
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	hr.Header.Set("Content-Type", "application/json")

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(hr)
	if err != nil {
		return Result{}, fmt.Errorf("arm request: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return Result{}, fmt.Errorf("read arm response: %w", err)
	}
	detail := strings.TrimSpace(string(payload))
	if resp.StatusCode == http.StatusConflict {
		return Result{}, fmt.Errorf("%w: %s", ErrKilled, detail)
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("engine refused the window (HTTP %d): %s", resp.StatusCode, detail)
	}

	var out responseBody
	if err := json.Unmarshal(payload, &out); err != nil {
		return Result{}, fmt.Errorf("decode arm response: %w", err)
	}
	return Result{ExpiresAt: out.ExpiresAt, PerOrderCapCents: out.PerOrderCapCents, DailyCapCents: out.DailyCapCents}, nil
}
