package armctl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestArm(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantErr    error
		wantExpiry bool
	}{
		{
			name:       "engine writes the window",
			status:     http.StatusOK,
			body:       `{"expires_at":"2026-11-15T09:00:00Z","per_order_cap_cents":5000,"daily_cap_cents":15000}`,
			wantExpiry: true,
		},
		{
			name:    "killed engine is reported as killed",
			status:  http.StatusConflict,
			body:    "killed: the halt file is present",
			wantErr: ErrKilled,
		},
		{
			name:   "cap above ceiling is a refusal",
			status: http.StatusBadRequest,
			body:   "per_order_cap_cents may only lower the configured ceiling",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody requestBody
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/admin/arm" {
					t.Errorf("request = %s %s, want POST /admin/arm", r.Method, r.URL.Path)
				}
				if err := decode(r, &gotBody); err != nil {
					t.Errorf("decode request: %v", err)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			got, err := (Client{URL: srv.URL}).Arm(context.Background(), Request{TTLSeconds: 1800, PerOrderCapCents: 5000, DailyCapCents: 15000})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if tc.status == http.StatusOK {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got.ExpiresAt.IsZero() {
					t.Error("expiry was not decoded")
				}
				if got.PerOrderCapCents != 5000 || got.DailyCapCents != 15000 {
					t.Errorf("caps = %d/%d, want 5000/15000", got.PerOrderCapCents, got.DailyCapCents)
				}
				if gotBody.TTLSeconds != 1800 {
					t.Errorf("ttl sent = %d, want 1800", gotBody.TTLSeconds)
				}
				return
			}
			if err == nil {
				t.Fatal("expected a refusal, got nil")
			}
		})
	}
}

// decode reads the posted JSON body into v.
func decode(r *http.Request, v any) error { return json.NewDecoder(r.Body).Decode(v) }

func TestKill(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantErr    error
		wantStatus string
	}{
		{
			name:       "engine kills",
			status:     http.StatusOK,
			body:       `{"status":"killed","at":"2026-11-15T09:00:00Z"}`,
			wantStatus: "killed",
		},
		{
			name:       "already killed is idempotent, reason reaches the page",
			status:     http.StatusOK,
			body:       `{"status":"already_killed","at":"2026-11-15T09:00:00Z"}`,
			wantStatus: "already_killed",
		},
		{
			name:    "no admin listener is a distinct sentinel",
			status:  http.StatusNotFound,
			body:    "404 page not found",
			wantErr: ErrNoAdminListener,
		},
		{
			name:    "other refusal carries the engine's body",
			status:  http.StatusInternalServerError,
			body:    "disk full",
			wantErr: nil, // checked by substring below, not errors.Is
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/admin/kill" {
					t.Errorf("request = %s %s, want POST /admin/kill", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			got, err := (Client{URL: srv.URL}).Kill(context.Background())
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if tc.status != http.StatusOK {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if !strings.Contains(err.Error(), tc.body) {
					t.Errorf("err = %v, want it to carry %q", err, tc.body)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tc.wantStatus)
			}
		})
	}
}

func TestDisarm(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantErr    error
		wantStatus string
	}{
		{
			name:       "engine disarms",
			status:     http.StatusOK,
			body:       `{"status":"disarmed","at":"2026-11-15T09:00:00Z","killed":false}`,
			wantStatus: "disarmed",
		},
		{
			name:       "not armed reason reaches the page unmodified",
			status:     http.StatusOK,
			body:       `{"status":"not_armed","killed":false}`,
			wantStatus: "not_armed",
		},
		{
			name:    "no admin listener is a distinct sentinel",
			status:  http.StatusNotFound,
			body:    "404 page not found",
			wantErr: ErrNoAdminListener,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/admin/disarm" {
					t.Errorf("request = %s %s, want POST /admin/disarm", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			got, err := (Client{URL: srv.URL}).Disarm(context.Background())
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tc.wantStatus)
			}
		})
	}
}

// TestKillStateUnknown covers the connection-refused/timeout sentinel: the
// engine is never reached, so nothing changed, but what its state actually
// is cannot be said. A closed listener on a real loopback port reproduces
// "connection refused" without a flaky timeout.
func TestKillStateUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // now refused: nothing is listening on this port anymore

	_, err := (Client{URL: url}).Kill(context.Background())
	if !errors.Is(err, ErrStateUnknown) {
		t.Fatalf("err = %v, want %v", err, ErrStateUnknown)
	}
}

// TestDisarmTimeoutIsStateUnknown covers the same sentinel via a context
// deadline rather than a refused connection.
func TestDisarmTimeoutIsStateUnknown(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	// Deferred after srv.Close(), so it runs first (LIFO): the blocked
	// handler is released before Close waits on it, instead of deadlocking.
	defer srv.Close()
	defer close(block)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := (Client{URL: srv.URL}).Disarm(ctx)
	if !errors.Is(err, ErrStateUnknown) {
		t.Fatalf("err = %v, want %v", err, ErrStateUnknown)
	}
}
