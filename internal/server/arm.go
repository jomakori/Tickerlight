package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jomakori/tickerlight/internal/armctl"
	"github.com/jomakori/tickerlight/internal/ui"
)

// armCookieName carries the double-submit token for the arming form. It is the
// only cookie this console sets.
const armCookieName = "tl_arm_csrf"

// armTimeout bounds the engine call: a wedged engine costs the operator five
// seconds and a refusal, never a hanging form.
const armTimeout = 5 * time.Second

// ArmOptions configures the console's one mutating endpoint: the arming window.
//
// It is deliberately the narrowest mutation the engine offers. It cannot
// disarm, raise a cap, size an order, cancel anything, or clear a kill switch —
// the engine refuses each of those on its own route, and this handler has no
// code path for them.
type ArmOptions struct {
	// Arm posts the window to the engine's loopback admin route.
	Arm func(context.Context, armctl.Request) (armctl.Result, error)
}

// accessEmail reads the Cloudflare Access identity istio forwards after it has
// validated the JWT at the gateway. The header is trusted because it is written
// by the gateway on this path; a request that reaches the pod some other way
// carries no identity and therefore gets no control.
//
// The Access policy on the public host is the only allowlist. There is no
// second list here: the people who can reach this page are the people the
// gateway already admitted. This check does not verify the JWT signature —
// istio did that before writing the header.
func accessEmail(r *http.Request) string {
	raw := r.Header.Get("x-cf-access-jwt-payload")
	if raw == "" {
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return ""
		}
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}
	return strings.TrimSpace(claims.Email)
}

// newCSRF returns a fresh double-submit token.
func newCSRF() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}

// issueCSRF sets the token cookie and returns the value the form must echo.
func (s *server) issueCSRF(w http.ResponseWriter, r *http.Request) string {
	token := newCSRF()
	if token == "" {
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name:     armCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   requestIsHTTPS(r),
		MaxAge:   3600,
	})
	return token
}

// csrfOK checks the double-submit pair and, when the browser reports it, that
// the post came from this origin.
func csrfOK(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return false
	}
	cookie, err := r.Cookie(armCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	return r.PostFormValue("csrf") == cookie.Value
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// armHandler writes one authorization window. Every refusal below is a plain
// answer to the operator: nothing is retried, softened or inferred.
func (s *server) armHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.arm == nil {
			http.NotFound(w, r)
			return
		}
		email := accessEmail(r)
		if email == "" {
			s.log.Warn("arm refused: no Cloudflare Access identity")
			http.Error(w, "not authorized to arm: this request did not carry a Cloudflare Access identity", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "malformed form", http.StatusBadRequest)
			return
		}
		if !csrfOK(r) {
			http.Error(w, "stale form: reload the policy page and try again", http.StatusForbidden)
			return
		}
		if r.PostFormValue("confirm") != "on" {
			redirectArm(w, r, "confirm", "")
			return
		}

		ttlMinutes, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("ttl_minutes")))
		if err != nil || ttlMinutes <= 0 || ttlMinutes > 24*60 {
			redirectArm(w, r, "invalid", "the window must be between 1 and 1440 minutes")
			return
		}
		perOrder, err := parseOptionalCents(r.PostFormValue("per_order_cap_cents"))
		if err != nil {
			redirectArm(w, r, "invalid", "per-order cap must be a whole number of cents")
			return
		}
		daily, err := parseOptionalCents(r.PostFormValue("daily_cap_cents"))
		if err != nil {
			redirectArm(w, r, "invalid", "daily cap must be a whole number of cents")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), armTimeout)
		defer cancel()
		res, err := s.arm.Arm(ctx, armctl.Request{
			TTLSeconds:       ttlMinutes * 60,
			PerOrderCapCents: perOrder,
			DailyCapCents:    daily,
		})
		if errors.Is(err, armctl.ErrKilled) {
			redirectArm(w, r, "killed", err.Error())
			return
		}
		if err != nil {
			redirectArm(w, r, "refused", err.Error())
			return
		}
		s.log.Info("arming window written", "email", email, "expires_at", res.ExpiresAt)
		q := url.Values{}
		q.Set("arm", "armed")
		q.Set("until", res.ExpiresAt.UTC().Format(time.RFC3339))
		http.Redirect(w, r, "/policy?"+q.Encode(), http.StatusSeeOther)
	}
}

// parseOptionalCents treats an empty field as "the engine's ceiling".
func parseOptionalCents(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, err
	}
	if v < 0 {
		return 0, errors.New("negative cap")
	}
	return v, nil
}

// redirectArm sends the operator back to the policy page with the outcome in
// the query string, so the result is a fact on the page and not a toast.
func redirectArm(w http.ResponseWriter, r *http.Request, kind, msg string) {
	q := url.Values{}
	q.Set("arm", kind)
	if msg != "" {
		q.Set("msg", msg)
	}
	http.Redirect(w, r, "/policy?"+q.Encode(), http.StatusSeeOther)
}

// armReading builds the arming card's state for one policy render. A form is
// offered only when the request carries a Cloudflare Access identity and the
// engine's ceilings are known: the form must not invent a ceiling, and
// a request that reached the pod some other way gets an explanation, not a
// control.
func (s *server) armReading(w http.ResponseWriter, r *http.Request, v ui.View) ui.ArmData {
	d := ui.ArmData{Armed: v.Status.Armed, Kill: v.Status.Kill}
	if s.arm == nil {
		return d
	}
	d.Enabled = true
	d.Email = accessEmail(r)
	if v.Policy.Available {
		d.PerOrderCeilingCents = v.Policy.Caps.PerOrderCents
		d.DailyCeilingCents = v.Policy.Caps.DailyCents
	}

	switch {
	case d.Email == "":
		d.Error = "No Cloudflare Access identity reached this request, so no control is offered. Reload through the Access-protected host."
	case !v.Policy.Available:
		d.Error = "The engine's caps could not be read, so the window's ceilings are unknown and no form is offered."
	default:
		d.CSRF = s.issueCSRF(w, r)
		if d.CSRF == "" {
			d.Error = "Could not issue a form token; reload the page."
		}
	}

	q := r.URL.Query()
	switch q.Get("arm") {
	case "armed":
		d.Notice = "Arming window written."
		if until, err := time.Parse(time.RFC3339, q.Get("until")); err == nil {
			d.ArmedUntil = until
		}
	case "killed":
		d.Error = "Refused: " + q.Get("msg")
	case "refused":
		d.Error = "Refused: " + q.Get("msg")
	case "invalid":
		d.Error = q.Get("msg")
	case "confirm":
		d.Error = "Tick the confirmation box to arm."
	}
	return d
}
