package ui

import (
	"strings"
	"testing"

	"github.com/jomakori/tickerlight/internal/engine"
)

// The Policy panel's job is to be honest under both answers from the engine.
func TestPolicyPanelShowsCapsAndGuards(t *testing.T) {
	html := render(t, Policy(PolicyData{
		Available: true,
		Caps: engine.Caps{
			PerOrderCents:  5000,
			DailyCents:     15000,
			SpentCents:     1250,
			RemainingCents: 13750,
		},
		Mode:      "paper",
		Threshold: "0.55",
		AlertSink: "configured",
	}))

	for _, want := range []string{
		`aria-label="Engine policy"`,
		"$50.00",     // per-order cap
		"$150.00",    // daily cap
		"$12.50",     // dispatched today
		"$137.50",    // remaining headroom
		"8%",         // daily cap utilisation, 1250/15000
		"paper",      //
		"0.55",       //
		"configured", //
	} {
		if !strings.Contains(html, want) {
			t.Errorf("policy panel missing %q", want)
		}
	}
	if strings.Contains(html, "Caps unavailable") {
		t.Error("available panel rendered the unavailable state")
	}
}

func TestPolicyPanelUnavailableShowsNoNumbers(t *testing.T) {
	html := render(t, Policy(PolicyData{
		Reason: "caps_status: engine unreachable: dial tcp 127.0.0.1:8090: connect: connection refused",
	}))

	if !strings.Contains(html, "Caps unavailable") {
		t.Error("unavailable panel did not say so")
	}
	if !strings.Contains(html, "connection refused") {
		t.Error("unavailable panel did not carry the engine's failure")
	}
	for _, unwanted := range []string{"$", "Daily cap", "Per-order cap", "headroom left"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("unavailable panel showed %q", unwanted)
		}
	}
	// Mode, threshold and alert sink are unknown here, and say so.
	if n := strings.Count(html, `<b class="mono">not reported</b>`); n != 3 {
		t.Errorf("not-reported guards = %d, want 3", n)
	}
}

func TestPolicyPanelClampsNothingButSaysWhenOverCap(t *testing.T) {
	html := render(t, Policy(PolicyData{
		Available: true,
		Caps:      engine.Caps{PerOrderCents: 5000, DailyCents: 15000, SpentCents: 16000, RemainingCents: -1000},
	}))
	// An over-cap day is reported as negative headroom, not hidden.
	if !strings.Contains(html, "−$10.00") {
		t.Errorf("over-cap headroom not shown; html:\n%s", html)
	}
	if !strings.Contains(html, "107%") {
		t.Errorf("utilisation not shown above 100%%; html:\n%s", html)
	}
}

func TestPolicyPanelHasNoControls(t *testing.T) {
	html := render(t, Policy(PolicyData{Available: true, Caps: engine.Caps{DailyCents: 100}}))
	for _, verb := range []string{"<button", "<form", "<input", "hx-post", "hx-put", "hx-delete"} {
		if strings.Contains(html, verb) {
			t.Errorf("policy panel carries %q; it must be read-only", verb)
		}
	}
}
