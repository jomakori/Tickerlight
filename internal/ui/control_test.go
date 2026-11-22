package ui

import (
	"strings"
	"testing"
)

// overviewWith renders the Overview tab with a given control state.
func overviewWith(t *testing.T, c ControlData) string {
	t.Helper()
	v := testView()
	v.Control = c
	return render(t, Overview(OverviewData{View: v}))
}

// wired is a control the console can actually reach: an admin client, an
// Access identity and the token the forms echo.
func wired() ControlData {
	return ControlData{
		Enabled:     true,
		KillWired:   true,
		DisarmWired: true,
		Email:       "trader@example.com",
		CSRF:        "tok",
	}
}

// With no admin client the panel is a sentence, not a pair of buttons the
// console cannot honour.
func TestControlWithoutAdminClientRendersNoForm(t *testing.T) {
	html := overviewWith(t, ControlData{})
	for _, banned := range []string{`action="/kill"`, `action="/disarm"`, "<details", "<form", "disabled"} {
		if strings.Contains(html, banned) {
			t.Errorf("no-admin-client panel contains %q", banned)
		}
	}
	if !strings.Contains(html, "This console carries no engine admin client") {
		t.Error("no-admin-client panel must say why it offers nothing")
	}
}

// The kill form is a details reveal with a required checkbox and the submit
// label the ticket names, and it states plainly that undo is not here.
func TestKillFormIsARequiredConfirmReveal(t *testing.T) {
	html := overviewWith(t, wired())
	for _, want := range []string{
		`<details class="ctl">`,
		`<summary>Kill trading…</summary>`,
		`<form class="arm-form" method="post" action="/kill">`,
		`<input type="checkbox" name="confirm" value="on" required`,
		`<button class="btn dng" type="submit">Kill trading</button>`,
		"This console cannot undo one",
		"filesystem access to the engine volume by a human",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("kill panel is missing %q", want)
		}
	}
	if strings.Contains(html, "Re-arm") {
		t.Error("the dead Re-arm affordance is still rendered")
	}
	// No window is open in this view, so disarm is not stated.
	if strings.Contains(html, `action="/disarm"`) {
		t.Error("disarm is stated with no window open")
	}
}

// Disarm is stated only while a window is open, and says what it leaves alone.
func TestDisarmStatedOnlyWhileAWindowIsOpen(t *testing.T) {
	c := wired()
	c.Armed = true
	html := overviewWith(t, c)
	if !strings.Contains(html, `<form class="arm-form" method="post" action="/disarm">`) {
		t.Fatal("armed view offers no disarm form")
	}
	for _, want := range []string{
		"Disarm closes the open window only.",
		"Open orders and positions are untouched",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("disarm panel is missing %q", want)
		}
	}
}

// An engine that is already killed gets no kill form: the kill is not a thing
// this page can repeat, and there is no window left to disarm either.
func TestKilledEngineOffersNoControl(t *testing.T) {
	c := wired()
	c.Kill = true
	c.Armed = true
	html := overviewWith(t, c)
	for _, banned := range []string{`action="/kill"`, `action="/disarm"`} {
		if strings.Contains(html, banned) {
			t.Errorf("killed engine still renders %q", banned)
		}
	}
	if !strings.Contains(html, "The engine reports the kill switch set") {
		t.Error("killed panel must say the kill is already set")
	}
}

// A client with no Access identity, or with no token, draws no form and says
// what is missing.
func TestControlWithoutIdentityOrTokenRendersNoForm(t *testing.T) {
	c := wired()
	c.Email = ""
	c.Error = "No Cloudflare Access identity reached this request, so no control is offered. Reload through the Access-protected host."
	html := overviewWith(t, c)
	for _, banned := range []string{`action="/kill"`, `action="/disarm"`, "<form"} {
		if strings.Contains(html, banned) {
			t.Errorf("refused panel contains %q", banned)
		}
	}
	if !strings.Contains(html, "No Cloudflare Access identity reached this request") {
		t.Error("refused panel must state the refusal")
	}
}

// The banner states what the engine answered, in the engine's own words,
// including the two answers that mean nothing changed.
func TestControlBannerStatesTheEngineAnswer(t *testing.T) {
	for _, tc := range []struct {
		name         string
		action       string
		answered     string
		journalError string
		want         []string
		wantTone     string
	}{
		{
			name:     "killed",
			action:   "killed",
			answered: "killed",
			want:     []string{"The engine answered \"killed\"", "trading is halted", "cannot undo it"},
			wantTone: "err",
		},
		{
			name:     "already killed",
			action:   "killed",
			answered: "already_killed",
			want:     []string{"The engine answered \"already_killed\"", "already halted", "nothing changed"},
			wantTone: "warn",
		},
		{
			name:     "disarmed",
			action:   "disarmed",
			answered: "disarmed",
			want:     []string{"The engine answered \"disarmed\"", "Open orders and positions were left untouched"},
			wantTone: "ok",
		},
		{
			name:     "not armed",
			action:   "disarmed",
			answered: "not_armed",
			want:     []string{"The engine answered \"not_armed\"", "no window was open", "nothing changed"},
			wantTone: "warn",
		},
		{
			name:         "journal error rides along",
			action:       "killed",
			answered:     "killed",
			journalError: "append: no space left on device",
			want:         []string{"The engine answered \"killed\"", "The engine's journal reported: append: no space left on device"},
			wantTone:     "err",
		},
		{
			name:     "an answer the console does not know",
			action:   "killed",
			answered: "engine_restarting",
			want:     []string{"Kill requested. The engine answered \"engine_restarting\"."},
			wantTone: "err",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := wired()
			c.Action, c.Answered, c.JournalError = tc.action, tc.answered, tc.journalError
			tone, text := c.Banner()
			if tone != tc.wantTone {
				t.Errorf("tone = %q, want %q (text %q)", tone, tc.wantTone, text)
			}
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("banner %q is missing %q", text, want)
				}
			}
			// And it reaches the page, in the tone's class.
			html := overviewWith(t, c)
			if !strings.Contains(html, `class="arm-`+tc.wantTone+`" role="status"`) {
				t.Errorf("page does not carry the banner in class arm-%s", tc.wantTone)
			}
			// templ escapes the double quotes the engine's answer is quoted in.
			pageWant := strings.ReplaceAll(tc.want[0], `"`, "&#34;")
			if !strings.Contains(html, pageWant) {
				t.Errorf("page does not carry the banner text %q", tc.want[0])
			}
		})
	}
}

// No redirect, no banner: an opened page says nothing about a control nobody
// touched.
func TestControlBannerNeedsAnAnswer(t *testing.T) {
	if tone, text := (ControlData{}).Banner(); tone != "" || text != "" {
		t.Errorf("bare page carries a banner: %q %q", tone, text)
	}
	if strings.Contains(overviewWith(t, wired()), `role="status"`) {
		t.Error("page with no answer renders a status banner")
	}
}
