package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jomakori/tickerlight/internal/armctl"
	"github.com/jomakori/tickerlight/internal/status"
	"github.com/jomakori/tickerlight/internal/tickerdata"
)

// ctlSnapshot is the engine reading the panel is rendered over: kill released,
// window open, so both controls are stated.
var ctlSnapshot = status.Snapshot{Kill: false, Armed: true}

// overviewConsole wires the control routes and a status reading, so the
// Overview panel is exercised through the real handler.
func overviewConsole(t *testing.T, opts ArmOptions, snap status.Snapshot) http.Handler {
	t.Helper()
	return New(Options{
		Rows:   tickerdata.Sample(),
		Now:    func() time.Time { return tickerdata.SampleNow },
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Source: "journal",
		Status: func() status.Snapshot { return snap },
		Arm:    &opts,
	})
}

// overviewGet is an ordinary page load through the gateway: the identity
// header istio writes after it validates the Access JWT.
func overviewGet(t *testing.T, h http.Handler, path string) result {
	t.Helper()
	return fetch(t, h, http.MethodGet, path, map[string]string{
		"x-cf-access-jwt-payload": identityHeader(armEmail),
	})
}

func TestOverviewStatesBothControlsWhileAWindowIsOpen(t *testing.T) {
	h := overviewConsole(t, ArmOptions{
		Kill:   func(context.Context) (armctl.KillResult, error) { return armctl.KillResult{}, nil },
		Disarm: func(context.Context) (armctl.DisarmResult, error) { return armctl.DisarmResult{}, nil },
	}, ctlSnapshot)

	res := overviewGet(t, h, "/")
	if res.status != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", res.status)
	}
	for _, want := range []string{
		`action="/kill"`,
		`method="post"`,
		`name="confirm"`,
		`required`,
		"Kill trading",
		"This console cannot undo one",
		"filesystem access to the engine volume by a human",
		`action="/disarm"`,
		"Open orders and positions are untouched",
	} {
		if !strings.Contains(res.body, want) {
			t.Errorf("Overview is missing %q", want)
		}
	}
	if strings.Contains(res.body, "Re-arm") {
		t.Error("Overview still renders the dead Re-arm affordance")
	}
	if ck := findCookie(res.header, armCookieName); ck == nil || ck.Value == "" {
		t.Error("page did not issue the double-submit token the forms echo")
	}
}

// With no window open there is nothing to disarm, so only the kill form is
// stated.
func TestOverviewStatesDisarmOnlyWhileAWindowIsOpen(t *testing.T) {
	opts := ArmOptions{
		Kill:   func(context.Context) (armctl.KillResult, error) { return armctl.KillResult{}, nil },
		Disarm: func(context.Context) (armctl.DisarmResult, error) { return armctl.DisarmResult{}, nil },
	}
	h := overviewConsole(t, opts, status.Snapshot{Armed: false})
	res := overviewGet(t, h, "/")
	if !strings.Contains(res.body, `action="/kill"`) {
		t.Error("kill form is missing while the switch is released")
	}
	if strings.Contains(res.body, `action="/disarm"`) {
		t.Error("disarm is stated with no window open")
	}
}

// The same console, reached without the gateway's identity header, offers no
// form at all and says why.
func TestOverviewNeedsAnIdentityForTheControls(t *testing.T) {
	h := overviewConsole(t, ArmOptions{
		Kill:   func(context.Context) (armctl.KillResult, error) { return armctl.KillResult{}, nil },
		Disarm: func(context.Context) (armctl.DisarmResult, error) { return armctl.DisarmResult{}, nil },
	}, ctlSnapshot)

	res := get(t, h, "/")
	for _, banned := range []string{`action="/kill"`, `action="/disarm"`} {
		if strings.Contains(res.body, banned) {
			t.Errorf("identity-less page renders %q", banned)
		}
	}
	if !strings.Contains(res.body, "No Cloudflare Access identity reached this request") {
		t.Error("page did not say why no control is offered")
	}
}

// The banner is the engine's own answer, including the answers that mean
// nothing changed, plus the journal note when the engine reported one.
func TestOverviewBannerReadsWhatTheEngineAnswered(t *testing.T) {
	h := overviewConsole(t, ArmOptions{
		Kill:   func(context.Context) (armctl.KillResult, error) { return armctl.KillResult{}, nil },
		Disarm: func(context.Context) (armctl.DisarmResult, error) { return armctl.DisarmResult{}, nil },
	}, ctlSnapshot)

	for _, tc := range []struct {
		query    string
		want     []string
		wantTone string
	}{
		{
			query:    "/?ctl=killed&status=already_killed",
			want:     []string{"already_killed", "already halted", "nothing changed"},
			wantTone: "arm-warn",
		},
		{
			query:    "/?ctl=disarmed&status=not_armed",
			want:     []string{"not_armed", "no window was open", "nothing changed"},
			wantTone: "arm-warn",
		},
		{
			query:    "/?ctl=killed&status=killed&journal_error=append+failed%3A+read-only+file+system",
			want:     []string{"killed", "cannot undo it", "The engine&#39;s journal reported: append failed: read-only file system"},
			wantTone: "arm-err",
		},
		{
			query:    "/?ctl=disarmed&status=disarmed",
			want:     []string{"disarmed", "Open orders and positions were left untouched"},
			wantTone: "arm-ok",
		},
	} {
		res := overviewGet(t, h, tc.query)
		for _, want := range tc.want {
			if !strings.Contains(res.body, want) {
				t.Errorf("%s: banner is missing %q", tc.query, want)
			}
		}
		if !strings.Contains(res.body, `class="`+tc.wantTone+`" role="status"`) {
			t.Errorf("%s: banner is not in class %s", tc.query, tc.wantTone)
		}
	}

	// No redirect, no banner.
	if body := overviewGet(t, h, "/").body; strings.Contains(body, `role="status"`) {
		t.Error("an untouched page renders a result banner")
	}
}

// A kill the engine answered but could not journal comes back with the note in
// the query string, and the page states it.
func TestKillRedirectCarriesTheJournalNote(t *testing.T) {
	const csrf = "0123456789abcdef0123456789abcdef"
	h := ctlConsole(t, ArmOptions{Kill: func(context.Context) (armctl.KillResult, error) {
		return armctl.KillResult{Status: "killed", JournalError: "append failed: read-only file system"}, nil
	}})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, ctlPost("/kill", csrf, nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /kill = %d, want 303 (body %s)", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	for _, want := range []string{"ctl=killed", "status=killed", "journal_error="} {
		if !strings.Contains(loc, want) {
			t.Errorf("redirect %q is missing %q", loc, want)
		}
	}

	// The redirect target renders the note, quoting nothing it did not read.
	res := overviewGet(t, h, loc)
	if !strings.Contains(res.body, "append failed: read-only file system") {
		t.Error("page did not state the engine's journal note")
	}
}

// findCookie returns the named cookie from a response's Set-Cookie headers.
func findCookie(h http.Header, name string) *http.Cookie {
	for _, line := range h.Values("Set-Cookie") {
		c, err := http.ParseSetCookie(line)
		if err != nil || c.Name != name {
			continue
		}
		return c
	}
	return nil
}
