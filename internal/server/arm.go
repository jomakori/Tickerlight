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
	// Kill posts to the engine's loopback admin kill route. Nil keeps Kill's
	// 404 behavior: the route exists only when Arm is configured, but a
	// deployment that wires Arm without Kill still gets a safe response
	// rather than a nil-pointer panic.
	Kill func(context.Context) (armctl.KillResult, error)
	// Disarm posts to the engine's loopback admin disarm route. Same nil
	// handling as Kill.
	Disarm func(context.Context) (armctl.DisarmResult, error)
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

		ttlSeconds, err := parseTTLSeconds(r.PostFormValue("ttl_minutes"))
		if err != nil {
			redirectArm(w, r, "invalid", "the window must be between 1 and 5760 minutes")
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
			TTLSeconds:       ttlSeconds,
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
		q := url.Values{}
		q.Set("arm", "armed")
		if ttlSeconds == 0 {
			s.log.Info("arming window written", "email", email, "open_ended", true)
			q.Set("openended", "1")
		} else {
			s.log.Info("arming window written", "email", email, "expires_at", res.ExpiresAt)
			q.Set("until", res.ExpiresAt.UTC().Format(time.RFC3339))
		}
		http.Redirect(w, r, "/policy?"+q.Encode(), http.StatusSeeOther)
	}
}

// killHandler trips the engine's halt file. It reuses armHandler's three
// guards verbatim (Access identity, double-submit CSRF via csrfOK, which
// itself checks Sec-Fetch-Site) and requires the confirmation tick; there is
// nothing to size for a kill.
func (s *server) killHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.arm == nil || s.arm.Kill == nil {
			http.NotFound(w, r)
			return
		}
		email := accessEmail(r)
		if email == "" {
			s.log.Warn("kill refused: no Cloudflare Access identity")
			http.Error(w, "not authorized to kill: this request did not carry a Cloudflare Access identity", http.StatusForbidden)
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
			http.Error(w, "tick the confirmation box to kill", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), armTimeout)
		defer cancel()
		res, err := s.arm.Kill(ctx)
		if err != nil {
			s.log.Warn("kill call failed", "email", email, "err", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		s.log.Info("kill issued", "email", email, "status", res.Status)
		http.Redirect(w, r, "/?"+ctlQuery("killed", res.Status, res.JournalError).Encode(), http.StatusSeeOther)
	}
}

// disarmHandler closes an open arming window. Same guards as killHandler.
func (s *server) disarmHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.arm == nil || s.arm.Disarm == nil {
			http.NotFound(w, r)
			return
		}
		email := accessEmail(r)
		if email == "" {
			s.log.Warn("disarm refused: no Cloudflare Access identity")
			http.Error(w, "not authorized to disarm: this request did not carry a Cloudflare Access identity", http.StatusForbidden)
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

		ctx, cancel := context.WithTimeout(r.Context(), armTimeout)
		defer cancel()
		res, err := s.arm.Disarm(ctx)
		if err != nil {
			s.log.Warn("disarm call failed", "email", email, "err", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		s.log.Info("disarm issued", "email", email, "status", res.Status)
		http.Redirect(w, r, "/?"+ctlQuery("disarmed", res.Status, res.JournalError).Encode(), http.StatusSeeOther)
	}
}

// ctlQuery is the Overview control result, in the query string. ctl names the
// control that ran, status is the engine's own reason code verbatim, and
// journal_error rides along when the engine reported one — so the page states
// what the engine said, not what the console hoped.
func ctlQuery(action, status, journalError string) url.Values {
	q := url.Values{}
	q.Set("ctl", action)
	if status != "" {
		q.Set("status", status)
	}
	if journalError != "" {
		q.Set("journal_error", journalError)
	}
	return q
}

// parseTTLSeconds reads the arming window field. A blank value or "0" means
// open-ended — armed until the operator disarms or kills (the engine caps it
// at four days), sent to the engine as TTLSeconds 0. Any other value must be
// a whole number of minutes in 1..5760, converted to seconds.
func parseTTLSeconds(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" {
		return 0, nil
	}
	minutes, err := strconv.Atoi(raw)
	if err != nil || minutes <= 0 || minutes > 4*24*60 {
		return 0, errors.New("invalid ttl")
	}
	return minutes * 60, nil
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

// controlReading builds the Overview kill-and-disarm panel's state for one
// render: whether this process carries an engine admin client, the identity and
// token that make the forms submittable, and the answer the last control wrote
// into the query string.
//
// A form is offered only where the client exists and the request carries a
// Cloudflare Access identity. Everything else is a sentence, never a disabled
// button: a control the console cannot reach is not a control.
func (s *server) controlReading(w http.ResponseWriter, r *http.Request, v ui.View) ui.ControlData {
	// Kill and armed come from the engine's own snapshot, the same reading the
	// arming card uses. Without a snapshot both are false, and the panel says
	// nothing about state it did not read.
	d := ui.ControlData{Kill: v.Status.Kill, Armed: v.Status.Armed}
	q := r.URL.Query()
	d.Action = q.Get("ctl")
	d.Answered = q.Get("status")
	d.JournalError = q.Get("journal_error")

	if s.arm == nil {
		return d
	}
	d.KillWired = s.arm.Kill != nil
	d.DisarmWired = s.arm.Disarm != nil
	d.Enabled = d.KillWired || d.DisarmWired
	if !d.Enabled {
		return d
	}

	d.Email = accessEmail(r)
	switch {
	case d.Email == "":
		d.Error = "No Cloudflare Access identity reached this request, so no control is offered. Reload through the Access-protected host."
	default:
		d.CSRF = s.issueCSRF(w, r)
		if d.CSRF == "" {
			d.Error = "Could not issue a form token; reload the page."
		}
	}
	return d
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
		if q.Get("openended") == "1" {
			d.Notice = "Arming window opened - armed until you disarm"
		} else {
			d.Notice = "Arming window written."
			if until, err := time.Parse(time.RFC3339, q.Get("until")); err == nil {
				d.ArmedUntil = until
			}
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
