package server

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jomakori/tickerlight/internal/armctl"
	"github.com/jomakori/tickerlight/internal/engine"
	"github.com/jomakori/tickerlight/internal/tickerdata"
)

const armEmail = "joe@example.com"

// errArmRefused is any engine refusal other than a kill.
var errArmRefused = errors.New("engine said no")

// identityHeader is what istio forwards after the gateway validates the Access
// JWT: the payload, base64url, with the email claim.
func identityHeader(email string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(`{"email":"` + email + `"}`))
}

// armConsole wires the handler with an arming control whose engine call is the
// test's own function, so a refusal or a kill is exercised without a pod.
func armConsole(t *testing.T, arm func(context.Context, armctl.Request) (armctl.Result, error)) http.Handler {
	t.Helper()
	return New(Options{
		Rows: tickerdata.Sample(),
		Now:  func() time.Time { return tickerdata.SampleNow },
		Log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Arm:  &ArmOptions{Arm: arm},
	})
}

// armPost builds the form post the browser would send, with the token both in
// the cookie and the body.
func armPost(csrf string, form url.Values) *http.Request {
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf", csrf)
	req := httptest.NewRequest(http.MethodPost, "/arm", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("x-cf-access-jwt-payload", identityHeader(armEmail))
	req.AddCookie(&http.Cookie{Name: armCookieName, Value: csrf})
	return req
}

func TestArmRoute(t *testing.T) {
	armErr := func(err error) func(context.Context, armctl.Request) (armctl.Result, error) {
		return func(context.Context, armctl.Request) (armctl.Result, error) { return armctl.Result{}, err }
	}
	okArm := func(ctx context.Context, req armctl.Request) (armctl.Result, error) {
		return armctl.Result{ExpiresAt: time.Date(2026, 11, 15, 15, 0, 0, 0, time.UTC), PerOrderCapCents: req.PerOrderCapCents, DailyCapCents: req.DailyCapCents}, nil
	}

	t.Run("no control configured is not a route", func(t *testing.T) {
		srv := New(Options{Rows: tickerdata.Sample(), Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, armPost("0123456789abcdef0123456789abcdef", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("an identity the gateway would not have written is forbidden", func(t *testing.T) {
		called := false
		srv := armConsole(t, func(ctx context.Context, req armctl.Request) (armctl.Result, error) {
			called = true
			return armctl.Result{}, nil
		})
		req := armPost("0123456789abcdef0123456789abcdef", url.Values{"confirm": {"on"}, "ttl_minutes": {"30"}})
		req.Header.Set("x-cf-access-jwt-payload", "not-base64-at-all")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (body %q)", rec.Code, rec.Body.String())
		}
		if called {
			t.Fatal("engine was called for an unparseable identity")
		}
	})

	t.Run("no identity means no control", func(t *testing.T) {
		srv := armConsole(t, okArm)
		req := armPost("0123456789abcdef0123456789abcdef", url.Values{"confirm": {"on"}})
		req.Header.Del("x-cf-access-jwt-payload")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("a cross-site post is refused", func(t *testing.T) {
		srv := armConsole(t, okArm)
		req := armPost("0123456789abcdef0123456789abcdef", url.Values{"confirm": {"on"}})
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("a mismatched token is refused", func(t *testing.T) {
		srv := armConsole(t, okArm)
		req := armPost("0123456789abcdef0123456789abcdef", url.Values{"confirm": {"on"}})
		req.Header.Del("Cookie")
		req.AddCookie(&http.Cookie{Name: armCookieName, Value: "ffffffffffffffffffffffffffffffff"})
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("an unconfirmed form does not reach the engine", func(t *testing.T) {
		called := false
		srv := armConsole(t, func(ctx context.Context, req armctl.Request) (armctl.Result, error) {
			called = true
			return armctl.Result{}, nil
		})
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, armPost("0123456789abcdef0123456789abcdef", url.Values{"ttl_minutes": {"30"}}))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/policy?arm=confirm" {
			t.Errorf("Location = %q", loc)
		}
		if called {
			t.Fatal("engine was called without the confirmation box")
		}
	})

	t.Run("a killed engine reports killed", func(t *testing.T) {
		srv := armConsole(t, armErr(armctl.ErrKilled))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, armPost("0123456789abcdef0123456789abcdef", url.Values{"confirm": {"on"}, "ttl_minutes": {"30"}}))
		if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/policy?arm=killed") {
			t.Fatalf("Location = %q, want a killed outcome", loc)
		}
	})

	t.Run("an engine refusal lands on the page", func(t *testing.T) {
		srv := armConsole(t, armErr(errArmRefused))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, armPost("0123456789abcdef0123456789abcdef", url.Values{"confirm": {"on"}, "ttl_minutes": {"30"}}))
		if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/policy?arm=refused") {
			t.Fatalf("Location = %q, want a refused outcome", loc)
		}
	})

	t.Run("the window and caps reach the engine", func(t *testing.T) {
		var got armctl.Request
		srv := armConsole(t, func(ctx context.Context, req armctl.Request) (armctl.Result, error) {
			got = req
			return okArm(ctx, req)
		})
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, armPost("0123456789abcdef0123456789abcdef", url.Values{
			"confirm":             {"on"},
			"ttl_minutes":         {"45"},
			"per_order_cap_cents": {"2500"},
			"daily_cap_cents":     {"10000"},
		}))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303 (body %q)", rec.Code, rec.Body.String())
		}
		if got.TTLSeconds != 45*60 || got.PerOrderCapCents != 2500 || got.DailyCapCents != 10000 {
			t.Fatalf("engine request = %+v", got)
		}
		loc := rec.Header().Get("Location")
		if !strings.HasPrefix(loc, "/policy?arm=armed&until=2026-11-15T15") {
			t.Errorf("Location = %q, want the written window", loc)
		}
	})

	t.Run("an out-of-range window is refused before the engine", func(t *testing.T) {
		called := false
		srv := armConsole(t, func(ctx context.Context, req armctl.Request) (armctl.Result, error) {
			called = true
			return armctl.Result{}, nil
		})
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, armPost("0123456789abcdef0123456789abcdef", url.Values{"confirm": {"on"}, "ttl_minutes": {"1441"}}))
		if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/policy?arm=invalid") {
			t.Fatalf("Location = %q, want an invalid outcome", loc)
		}
		if called {
			t.Fatal("engine was called for an out-of-range window")
		}
	})
}

func TestArmCardRenders(t *testing.T) {
	stub, _ := stubMCP(t, capsPayload)
	defer stub.Close()

	// The card offers the form only when the request carries a
	// identity and the engine's ceilings are known.
	c := &engine.Client{URL: stub.URL, Token: "read-token", HTTP: stub.Client()}
	srv := New(Options{
		Rows: tickerdata.Sample(),
		Now:  func() time.Time { return tickerdata.SampleNow },
		Log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Caps: c.Caps,
		Arm:  &ArmOptions{Arm: func(context.Context, armctl.Request) (armctl.Result, error) { return armctl.Result{}, nil }},
	})

	req := httptest.NewRequest(http.MethodGet, "/policy", nil)
	req.Header.Set("x-cf-access-jwt-payload", identityHeader(armEmail))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `action="/arm"`) {
		t.Error("policy page did not render the arming form")
	}
	if !strings.Contains(body, `name="per_order_cap_cents"`) || !strings.Contains(body, `max="5000"`) {
		t.Error("form did not carry the engine's per-order ceiling")
	}
	var token string
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == armCookieName {
			token = ck.Value
		}
	}
	if token == "" {
		t.Fatal("policy page did not issue the CSRF cookie")
	}
	if !strings.Contains(body, token) {
		t.Error("form did not echo the CSRF token")
	}

	// Same page without the Access identity explains itself and offers nothing.
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/policy", nil))
	body = rec.Body.String()
	if strings.Contains(body, `action="/arm"`) {
		t.Error("form rendered without a Cloudflare Access identity")
	}
	if !strings.Contains(body, "No Cloudflare Access identity") {
		t.Error("page did not explain the missing identity")
	}
}
