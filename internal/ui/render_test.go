package ui

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/jomakori/tickerlight/internal/tickerdata"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func testView() View {
	return NewView(tickerdata.Sample(), tickerdata.SampleNow)
}

func TestLayoutIsFullDocumentWithPaperBanner(t *testing.T) {
	html := render(t, Layout(testView()))

	for _, want := range []string{
		"<!doctype html>",
		`lang="en"`,
		"Sample data · paper only",
		`href="/static/styles.css"`,
		`src="/static/htmx.min.js"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("layout missing %q", want)
		}
	}

	for _, href := range []string{`href="/"`, `href="/journal"`, `href="/calibration"`, `href="/risk"`} {
		if !strings.Contains(html, href) {
			t.Errorf("navigation missing %s", href)
		}
	}
}

func TestPaperBannerIsNotDismissible(t *testing.T) {
	html := render(t, Layout(testView()))
	disc := html[strings.Index(html, `class="disc"`):]
	disc = disc[:strings.Index(disc, "</span>")]
	if strings.Contains(disc, "button") || strings.Contains(disc, "hx-") {
		t.Errorf("paper banner must not be dismissible: %s", disc)
	}
}

func TestViewShellIsAFragment(t *testing.T) {
	html := render(t, ViewShell(testView()))
	if strings.Contains(html, "<!DOCTYPE") || strings.Contains(html, "<html") {
		t.Error("ViewShell must not emit a document")
	}
	if !strings.Contains(html, `id="view"`) {
		t.Error("ViewShell has no #view root for HTMX swaps")
	}
}

func TestEveryTabRenders(t *testing.T) {
	v := testView()
	for _, tab := range []string{TabOverview, TabJournal, TabCalibration, TabRisk} {
		v.Tab = tab
		html := render(t, Layout(v))
		if len(html) < 2000 {
			t.Errorf("tab %q rendered suspiciously small (%d bytes)", tab, len(html))
		}
		if !strings.Contains(html, "Sample data · paper only") {
			t.Errorf("tab %q lost the paper banner", tab)
		}
	}
}

func TestArmKillControlsAreDisabled(t *testing.T) {
	html := render(t, Risk(testView().RiskState()))
	if got := strings.Count(html, "disabled"); got < 3 {
		t.Errorf("expected the three gated controls to be disabled, found %d disabled attrs", got)
	}
	for _, banned := range []string{"hx-post", "hx-put", "hx-delete", `method="post"`, "onclick=", "x-on:"} {
		if strings.Contains(html, banned) {
			t.Errorf("risk page contains %q — controls must be inert", banned)
		}
	}
	if !strings.Contains(html, "Gated in this build") {
		t.Error("risk page must explain that arm/kill is gated")
	}
}

func TestNoDocumentPostsAnywhere(t *testing.T) {
	html := render(t, Layout(testView()))
	for _, banned := range []string{"hx-post", "hx-put", "hx-delete", `action="/arm"`, "fetch(", "XMLHttpRequest"} {
		if strings.Contains(strings.ToLower(html), strings.ToLower(banned)) {
			t.Errorf("document contains %q; the console is read-only", banned)
		}
	}
}

var absURL = regexp.MustCompile(`(?:src|href)="https?://[^"]+"`)

func TestNoExternalReferences(t *testing.T) {
	for _, tab := range []string{TabOverview, TabJournal, TabCalibration, TabRisk} {
		v := testView()
		v.Tab = tab
		if got := absURL.FindAllString(render(t, Layout(v)), -1); len(got) > 0 {
			t.Errorf("tab %q references external origins: %v", tab, got)
		}
	}
	if got := absURL.FindAllString(render(t, NotFound()), -1); len(got) > 0 {
		t.Errorf("404 references external origins: %v", got)
	}
}

func TestJournalShowsEveryRowAndFilters(t *testing.T) {
	v := testView()
	v.Tab = TabJournal
	html := render(t, Journal(v.Journal()))

	rows := len(tickerdata.Sample())
	if got := strings.Count(html, `class="rowbtn"`); got != rows {
		t.Errorf("journal rendered %d row buttons, want %d", got, rows)
	}
	for _, name := range []string{`name="outcome"`, `name="venue"`, `name="symbol"`, `name="from"`, `name="to"`} {
		if !strings.Contains(html, name) {
			t.Errorf("filter form missing %s", name)
		}
	}
	if !strings.Contains(html, `hx-get="/journal"`) {
		t.Error("filter form must swap the journal panel via HTMX")
	}
	if !strings.Contains(html, `<noscript>`) {
		t.Error("filters need a no-JS fallback submit")
	}
}

func TestJournalDetailRendersChain(t *testing.T) {
	v := testView()
	rows := tickerdata.Sample()
	v.Selected = &rows[0]
	html := render(t, Journal(v.Journal()))

	if got := strings.Count(html, `class="stage`); got != len(tickerdata.Stages) {
		t.Errorf("detail rendered %d stages, want %d", got, len(tickerdata.Stages))
	}
	if !strings.Contains(html, "Not reached") && rows[0].Outcome != tickerdata.Filled {
		t.Error("blocked attempt should mark later stages as not reached")
	}
	if !strings.Contains(html, `id="detail-pane"`) {
		t.Error("detail pane is not addressable for HTMX swaps")
	}
	if !strings.Contains(html, `href="/journal`) {
		t.Error("detail pane needs a close link back to the filtered tape")
	}
}

func TestJournalEmptyState(t *testing.T) {
	v := testView()
	v.Tab = TabJournal
	v.Filter = tickerdata.Filter{Outcome: string(tickerdata.Cap), Venue: "no-such-venue"}
	html := render(t, Journal(v.Journal()))
	if !strings.Contains(html, "No attempts") {
		t.Error("empty result needs an explicit empty state")
	}
	if !strings.Contains(html, "Clear filters") {
		t.Error("empty result needs a way back to the full tape")
	}
}

func TestChartsRenderSVGOrExplainTheGap(t *testing.T) {
	rows := tickerdata.Sample()
	if svg := EquityChart(rows, len(rows)); !strings.Contains(svg, "<svg") || !strings.Contains(svg, "<path") {
		t.Error("equity chart produced no SVG path")
	}
	if empty := EquityChart(nil, 0); !strings.Contains(strings.ToLower(empty), "no") {
		t.Errorf("empty equity chart should explain itself, got %q", empty)
	}
	cal := CalibrationChart(tickerdata.CalSample(), tickerdata.DefaultLimits())
	if !strings.Contains(cal, "<svg") {
		t.Error("calibration chart produced no SVG")
	}
	if ticks := Ticks(0.5, 10); !strings.Contains(ticks, `<i class="on">`) {
		t.Error("ticks meter produced no rects")
	}
}

func TestOverviewStatesTheCountsItShows(t *testing.T) {
	v := testView()
	html := render(t, Overview(v.Overview()))
	if !strings.Contains(html, "Realized P") {
		t.Error("overview missing the realized P&L headline")
	}
	if !strings.Contains(html, "Why nothing happened") {
		t.Error("overview missing the outcome tiles")
	}
	if !strings.Contains(html, "aria-pressed") {
		t.Error("range segmented control needs aria-pressed state")
	}
}

func TestFormattingHelpers(t *testing.T) {
	if got := CooldownText(90); got != "01:30" {
		t.Errorf("CooldownText(90) = %q", got)
	}
	if got := Pad4(7); got != "0007" {
		t.Errorf("Pad4(7) = %q", got)
	}
	if got := Qty(0.5); got != "0.50" {
		t.Errorf("Qty(0.5) = %q", got)
	}
	if got := Px(184.2); got != "$184.20" {
		t.Errorf("Px(184.2) = %q", got)
	}
	if got := HMS(time.Date(2026, 3, 5, 7, 8, 9, 0, time.UTC)); got != "07:08:09Z" {
		t.Errorf("HMS = %q", got)
	}
}

func TestStageStatesAlwaysDescribeFourStages(t *testing.T) {
	for _, o := range []tickerdata.Outcome{tickerdata.Filled, tickerdata.Cap, tickerdata.Kill, tickerdata.Judge, tickerdata.Venue} {
		states := StageStates(o)
		if len(states) != len(tickerdata.Stages) {
			t.Errorf("outcome %q has %d states", o, len(states))
		}
	}
}

func TestURLBuildersKeepFilterContext(t *testing.T) {
	f := tickerdata.Filter{Outcome: string(tickerdata.Cap), Venue: "coinbase", Symbol: "BTC-USD"}
	u := JournalURL(f, 0)
	for _, want := range []string{"outcome=cap", "venue=coinbase", "symbol=BTC-USD"} {
		if !strings.Contains(u, want) {
			t.Errorf("journal URL %q lost %q", u, want)
		}
	}
	detail := DetailURL(f, 12)
	if !strings.Contains(detail, "sel=12") {
		t.Errorf("detail URL %q has no attempt id", detail)
	}
	if !strings.Contains(detail, "outcome=cap") {
		t.Errorf("detail URL %q lost the filter context", detail)
	}
	if RangeURL(tickerdata.Range24h) == RangeURL(tickerdata.RangeAll) {
		t.Error("range URLs must differ")
	}
}
