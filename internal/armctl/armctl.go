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
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// ErrKilled reports that the engine's halt file exists. Arming is refused until
// an operator removes it, which no route and no tool does.
var ErrKilled = errors.New("engine is killed")

// ErrNoAdminListener reports that the engine answered 404: it exposes no
// admin listener at all, because it predates this route or was never started
// with one. Kill and disarm have no fallback for this — the console cannot
// reach into the engine's filesystem on its own.
var ErrNoAdminListener = errors.New("engine has no admin listener (too old, or not configured with one)")

// ErrStateUnknown reports that the engine could not be reached at all —
// connection refused or a timeout. Nothing was changed by this call, but
// what the engine's state actually is right now cannot be said.
var ErrStateUnknown = errors.New("state unknown, nothing changed")

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

// KillResult is the engine's answer to a kill. Status carries the engine's own
// reason code (e.g. "killed", "already_killed") unmodified, so the page shows
// what actually happened rather than a paraphrase.
type KillResult struct {
	Status string
	At     time.Time
	// JournalError is the engine's note when it killed trading but could not
	// write the journal entry. Empty means the engine said nothing was wrong.
	JournalError string
}

type killResponseBody struct {
	Status       string    `json:"status"`
	At           time.Time `json:"at"`
	JournalError string    `json:"journal_error,omitempty"`
}

// DisarmResult is the engine's answer to a disarm. Status carries the
// engine's own reason code (e.g. "disarmed", "not_armed") unmodified.
type DisarmResult struct {
	Status   string
	At       time.Time
	Killed   bool
	KilledAt time.Time
	// JournalError is the engine's note when it closed the window but could
	// not write the journal entry. Empty means it reported nothing wrong.
	JournalError string
}

type disarmResponseBody struct {
	Status       string     `json:"status"`
	At           *time.Time `json:"at,omitempty"`
	Killed       bool       `json:"killed"`
	KilledAt     *time.Time `json:"killed_at,omitempty"`
	JournalError string     `json:"journal_error,omitempty"`
}

// Kill asks the engine to trip its halt file. The engine's answer — including
// an already_killed reason code — is returned as-is.
func (c Client) Kill(ctx context.Context) (KillResult, error) {
	payload, status, err := c.postAdmin(ctx, "/admin/kill")
	if err != nil {
		return KillResult{}, err
	}
	var out killResponseBody
	if err := json.Unmarshal(payload, &out); err != nil {
		return KillResult{}, fmt.Errorf("decode kill response (HTTP %d): %w", status, err)
	}
	return KillResult{Status: out.Status, At: out.At, JournalError: out.JournalError}, nil
}

// Disarm asks the engine to close any open arming window. The engine's
// answer — including a not_armed reason code — is returned as-is.
func (c Client) Disarm(ctx context.Context) (DisarmResult, error) {
	payload, status, err := c.postAdmin(ctx, "/admin/disarm")
	if err != nil {
		return DisarmResult{}, err
	}
	var out disarmResponseBody
	if err := json.Unmarshal(payload, &out); err != nil {
		return DisarmResult{}, fmt.Errorf("decode disarm response (HTTP %d): %w", status, err)
	}
	res := DisarmResult{Status: out.Status, Killed: out.Killed, JournalError: out.JournalError}
	if out.At != nil {
		res.At = *out.At
	}
	if out.KilledAt != nil {
		res.KilledAt = *out.KilledAt
	}
	return res, nil
}

// postAdmin is the shared transport for Kill and Disarm: same loopback base
// URL, HTTP client and timeout as Arm, with the same body-reading and status
// handling, differing only in the error mapping the ticket calls for:
//   - 404: the engine exposes no admin listener at all (too old, or started
//     without one) — ErrNoAdminListener.
//   - connection refused or a timeout: nothing was changed, but what the
//     engine's state actually is right now cannot be said — ErrStateUnknown.
//   - any other non-2xx: an error carrying the engine's own body text.
func (c Client) postAdmin(ctx context.Context, path string) (body []byte, status int, err error) {
	url := strings.TrimRight(c.URL, "/") + path
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, 0, err
	}

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(hr)
	if err != nil {
		if isUnreachable(err) {
			return nil, 0, ErrStateUnknown
		}
		return nil, 0, fmt.Errorf("admin request: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return nil, 0, fmt.Errorf("read admin response: %w", err)
	}
	detail := strings.TrimSpace(string(payload))
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, resp.StatusCode, ErrNoAdminListener
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, resp.StatusCode, fmt.Errorf("engine refused the request (HTTP %d): %s", resp.StatusCode, detail)
	}
	return payload, resp.StatusCode, nil
}

// isUnreachable reports whether err means the engine was never reached at
// all — a timeout (including the context deadline) or a refused connection —
// as opposed to an error the engine itself produced after accepting the
// request.
func isUnreachable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}
