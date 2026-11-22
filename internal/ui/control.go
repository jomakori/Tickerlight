package ui

import "strconv"

// ControlData is the kill-and-disarm panel's state on the Overview tab.
//
// The panel is a control only where this process carries an engine admin
// client. Without one it renders no form at all and says why in one sentence:
// a disabled button under a control that cannot reach an engine would promise
// something the console cannot do.
type ControlData struct {
	// Enabled reports that at least one of kill and disarm reaches an engine
	// admin client in this process.
	Enabled bool
	// KillWired and DisarmWired name which of the two routes this console can
	// reach: a deployment may carry one and not the other.
	KillWired   bool
	DisarmWired bool
	// Email is the Cloudflare Access identity that reached this request. Empty
	// means the request did not come through the Access gateway.
	Email string
	// CSRF is the double-submit token the forms must echo. Empty with Enabled
	// set means no form is offered and Error says why.
	CSRF string
	// Kill mirrors the engine's kill-switch flag: with it set, a kill would be
	// a no-op and the panel says so instead of offering the form again.
	Kill bool
	// Armed reports that a trading window is open, so the disarm control is
	// stated. With no window open there is nothing to disarm.
	Armed bool
	// Action is the ?ctl= word the last control wrote: "killed" or "disarmed".
	Action string
	// Answered is the engine's own reason code from ?status=: "killed",
	// "already_killed", "disarmed" or "not_armed".
	Answered string
	// JournalError is the engine's journal note from ?journal_error=, when it
	// sent one.
	JournalError string
	// Error is a refusal, or the reason no form is offered.
	Error string
}

// KillForm reports whether the kill form may render: an admin client, an Access
// identity, the token the form echoes, and an engine that is not already
// killed. A form without an identity would be refused by the handler, so it is
// not drawn.
func (c ControlData) KillForm() bool {
	return c.Enabled && c.KillWired && c.Email != "" && c.CSRF != "" && !c.Kill
}

// DisarmForm reports whether the disarm form may render. Disarm is stated only
// while a window is open: with nothing open there is nothing to close, and a
// killed engine has no window to close either.
func (c ControlData) DisarmForm() bool {
	return c.Enabled && c.DisarmWired && c.Email != "" && c.CSRF != "" && c.Armed && !c.Kill
}

// Banner is the result line the Overview control panel shows after a redirect.
// It is built from the engine's own words: ?ctl= names the control that ran,
// ?status= carries the reason code the engine answered with, and
// ?journal_error= carries a journal note when the engine sent one.
//
// The tone is the CSS suffix ("ok", "warn", "err"); empty text means no banner
// at all. Every answer the engine can give gets its own sentence, including the
// two that mean nothing changed.
func (c ControlData) Banner() (tone, text string) {
	answer := c.Answered
	if answer == "" {
		answer = c.Action
	}
	if answer == "" {
		return "", ""
	}
	switch answer {
	case "killed":
		tone = "err"
		text = "Kill requested. The engine answered \"killed\": trading is halted, and this console cannot undo it — a human has to remove the halt file from the engine volume."
	case "already_killed":
		tone = "warn"
		text = "Kill requested. The engine answered \"already_killed\": trading was already halted, so nothing changed."
	case "disarmed":
		tone = "ok"
		text = "Disarm requested. The engine answered \"disarmed\": the window is closed. Open orders and positions were left untouched."
	case "not_armed":
		tone = "warn"
		text = "Disarm requested. The engine answered \"not_armed\": no window was open, so nothing changed."
	default:
		switch c.Action {
		case "killed":
			tone = "err"
			text = "Kill requested. The engine answered " + strconv.Quote(answer) + "."
		case "disarmed":
			tone = "warn"
			text = "Disarm requested. The engine answered " + strconv.Quote(answer) + "."
		default:
			tone = "warn"
			text = "The engine answered " + strconv.Quote(answer) + "."
		}
	}
	if c.JournalError != "" {
		text += " The engine's journal reported: " + c.JournalError
	}
	return tone, text
}
