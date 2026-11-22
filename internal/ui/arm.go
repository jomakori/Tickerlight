package ui

import "time"

// ArmData is the arming card's state: it opens a trading window and can do
// nothing else. Kill and disarm are separate controls, on the Overview tab.
type ArmData struct {
	// Enabled reports that this environment carries the control at all. When
	// it is false the card explains where arming lives instead.
	Enabled bool
	// Email is the Cloudflare Access identity that reached this request.
	// Empty means the request did not come through the Access gateway.
	Email string
	// CSRF is the double-submit token the form must echo. Empty with Enabled
	// true means no form is offered and Error says why.
	CSRF string
	// PerOrderCeilingCents and DailyCeilingCents are the engine's configured
	// ceilings. The form may only offer values at or below them, and offers
	// nothing when they are unknown.
	PerOrderCeilingCents int64
	DailyCeilingCents    int64
	// Armed and Kill mirror the engine snapshot.
	Armed bool
	Kill  bool
	// ArmedUntil is the window this request just wrote, when it did.
	ArmedUntil time.Time
	// Notice is a success line; Error is a refusal or a reason no form shows.
	Notice string
	Error  string
}

// Form reports whether the card may render the arming form.
func (a ArmData) Form() bool {
	return a.Enabled && a.CSRF != "" && !a.Kill && a.PerOrderCeilingCents > 0 && a.DailyCeilingCents > 0
}

// State is the card's headline: the engine's arming state, or the kill switch.
// A window this request just wrote counts as armed even before the engine's
// caps_status snapshot catches up on its own cadence, as long as it has not
// already expired.
func (a ArmData) State() string {
	switch {
	case a.Kill:
		return "killed"
	case a.Armed:
		return "armed"
	case !a.ArmedUntil.IsZero() && a.ArmedUntil.After(time.Now()):
		return "armed"
	default:
		return "disarmed"
	}
}
