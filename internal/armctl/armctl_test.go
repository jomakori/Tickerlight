package armctl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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
