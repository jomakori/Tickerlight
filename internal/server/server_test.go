package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jomakori/tickerlight/internal/journalread"
	"github.com/jomakori/tickerlight/internal/tickerdata"
	"github.com/jomakori/tickerlight/internal/web"
)

// The console is a read-only sample ledger. These tests pin the HTTP surface to
// that promise: five HTML routes, one health probe, embedded static assets, and
// nothing that mutates or dispatches. Everything goes through the real handler,
// so routing, method handling and headers are exercised as the binary serves
// them.

const banner = "Sample data · paper only"

type result struct {
	status int
	header http.Header
	body   string
}

func fixture(t *testing.T) http.Handler {
	t.Helper()
	return New(Options{
		Rows: tickerdata.Sample(),
		Now:  func() time.Time { return tickerdata.SampleNow },
		Log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func fetch(t *testing.T, h http.Handler, method, path string, headers map[string]string) result {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return result{status: rec.Code, header: rec.Header(), body: rec.Body.String()}
}

func get(t *testing.T, h http.Handler, path string) result {
	t.Helper()
	return fetch(t, h, http.MethodGet, path, nil)
}

var externalRef = regexp.MustCompile(`(?:src|href)="https?://[^"]+"`)

func TestPagesAreHTMLSampleOnlyAndUncached(t *testing.T) {
	h := fixture(t)
	for _, path := range []string{"/", "/journal", "/calibration", "/risk", "/policy"} {
		t.Run(path, func(t *testing.T) {
			res := get(t, h, path)
			if res.status != http.StatusOK {
				t.Fatalf("%s = %d, want 200", path, res.status)
			}
			if ct := res.header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("content type = %q", ct)
			}
			if cc := res.header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
				t.Errorf("cache control = %q, want no-store", cc)
			}
			if !strings.Contains(res.body, banner) {
				t.Error("paper-only banner missing")
			}
			if got := externalRef.FindAllString(res.body, -1); len(got) > 0 {
				t.Errorf("references external origins: %v", got)
			}
			if strings.Contains(res.body, "hx-post") || strings.Contains(res.body, "hx-put") ||
				strings.Contains(res.body, "hx-delete") || strings.Contains(res.body, `method="post"`) {
				t.Error("page carries a write verb; the console is read-only")
			}
		})
	}
}

func TestHTMXRequestsGetFragments(t *testing.T) {
	h := fixture(t)
	for _, path := range []string{"/", "/journal", "/calibration", "/risk", "/policy"} {
		res := fetch(t, h, http.MethodGet, path, map[string]string{"HX-Request": "true"})
		if res.status != http.StatusOK {
			t.Fatalf("%s = %d", path, res.status)
		}
		if strings.Contains(res.body, "<!doctype") || strings.Contains(res.body, "<html") {
			t.Errorf("%s HX response is a full document", path)
		}
		if !strings.Contains(res.body, `id="view"`) {
			t.Errorf("%s HX response has no #view root", path)
		}
		if got := res.header.Get("Vary"); !strings.Contains(got, "HX-Request") {
			t.Errorf("%s Vary = %q, want HX-Request", path, got)
		}
	}
}

func TestNavigationIsCurrentPerPage(t *testing.T) {
	h := fixture(t)
	pages := map[string]string{"/": "/", "/journal": "/journal", "/calibration": "/calibration", "/risk": "/risk"}
	for path, href := range pages {
		res := get(t, h, path)
		if !strings.Contains(res.body, `href="`+href+`"`) {
			t.Errorf("%s has no link to itself", path)
		}
		if !strings.Contains(res.body, `aria-current="page"`) {
			t.Errorf("%s marks no current nav item", path)
		}
	}
}

func TestRangeControlReflectsQuery(t *testing.T) {
	h := fixture(t)
	all := get(t, h, "/")
	if !strings.Contains(all.body, `aria-pressed="false"`) || !strings.Contains(all.body, `aria-pressed="true"`) {
		t.Error("range control needs both pressed and unpressed states")
	}

	res := get(t, h, "/?range=24h")
	if strings.Count(res.body, `aria-pressed="true"`) != 1 {
		t.Error("24h range is not the single pressed control")
	}
	if strings.Contains(res.body, `aria-pressed="true" data-range="all"`) {
		t.Error("all-time still pressed on a 24h view")
	}

	bad := get(t, h, "/?range=forever")
	if !strings.Contains(bad.body, banner) {
		t.Error("unknown range widened to a broken page")
	}
}

func TestJournalRowsFiltersAndEmptyState(t *testing.T) {
	h := fixture(t)
	rows := len(tickerdata.Sample())

	all := get(t, h, "/journal")
	if got := strings.Count(all.body, `class="rowbtn"`); got != rows {
		t.Errorf("journal rendered %d rows, want %d", got, rows)
	}

	capOnly := get(t, h, "/journal?outcome=cap")
	if got := strings.Count(capOnly.body, `class="rowbtn"`); got == 0 || got == rows {
		t.Errorf("outcome=cap rendered %d of %d rows", got, rows)
	}
	if strings.Contains(capOnly.body, ">FILLED<") {
		t.Error("outcome=cap leaked a filled row")
	}

	empty := get(t, h, "/journal?symbol=NOPE-USD")
	if !strings.Contains(empty.body, "No attempts match these filters") {
		t.Error("no empty state for a filter that matches nothing")
	}
	if !strings.Contains(empty.body, "Clear filters") {
		t.Error("empty state offers no way back")
	}

	// Unknown enum values are sanitised at the transport layer, so a stale
	// bookmark renders the full tape instead of an unexplained blank table.
	garbage := get(t, h, "/journal?outcome=nonsense")
	if got := strings.Count(garbage.body, `class="rowbtn"`); got != rows {
		t.Errorf("unknown outcome rendered %d rows, want the full tape", got)
	}

	badDate := get(t, h, "/journal?from=2026-13-45")
	if got := strings.Count(badDate.body, `class="rowbtn"`); got != rows {
		t.Errorf("invalid date rendered %d rows, want the full tape", got)
	}
}

func TestJournalRowOpensDetail(t *testing.T) {
	h := fixture(t)
	rows := tickerdata.Sample()
	first := rows[0]

	for _, param := range []string{"sel", "a"} {
		res := get(t, h, "/journal?"+param+"="+strconv.Itoa(first.ID))
		if res.status != http.StatusOK {
			t.Fatalf("?%s= detail status = %d", param, res.status)
		}
		if !strings.Contains(res.body, `data-open="true"`) {
			t.Errorf("?%s= did not open the detail pane", param)
		}
		if !strings.Contains(res.body, "Close") {
			t.Errorf("?%s= detail has no close control", param)
		}
		lower := strings.ToLower(res.body)
		for _, stage := range tickerdata.Stages {
			if !strings.Contains(lower, strings.ToLower(stage)) {
				t.Errorf("?%s= detail missing stage %q", param, stage)
			}
		}
	}

	missing := get(t, h, "/journal?sel=999999")
	if missing.status != http.StatusNotFound {
		t.Errorf("unknown attempt = %d, want 404", missing.status)
	}
	if !strings.Contains(missing.body, banner) {
		t.Error("unknown attempt lost the paper banner")
	}
}

func TestDetailSwapReturnsOnlyThePane(t *testing.T) {
	h := fixture(t)
	rows := tickerdata.Sample()

	res := fetch(t, h, http.MethodGet, "/journal?sel="+strconv.Itoa(rows[0].ID), map[string]string{"HX-Request": "true"})
	if res.status != http.StatusOK {
		t.Fatalf("HX detail status = %d, want 200", res.status)
	}
	if got := strings.TrimSpace(res.body); !strings.HasPrefix(got, `<aside id="detail-pane"`) {
		t.Errorf("HX detail swaps in something other than the pane: %.80s", got)
	}
	// The row carries hx-target="#detail-pane" + hx-swap="outerHTML": a <main>
	// in this response replaces the pane with a whole panel and leaves two
	// nodes with id="view" for every later hx-target="#view" swap.
	if strings.Contains(res.body, "<main") {
		t.Error("HX detail response nests a <main>; the pane is the swap unit")
	}
	if strings.Contains(res.body, "<!doctype") {
		t.Error("HX detail response is a full document")
	}
	if n := strings.Count(res.body, `id="detail-pane"`); n != 1 {
		t.Errorf("HX detail response has %d #detail-pane roots, want 1", n)
	}
	if !strings.Contains(res.body, `data-open="true"`) {
		t.Error("HX detail response does not open the pane")
	}

	// A plain navigation still gets the whole document, and exactly one panel.
	full := get(t, h, "/journal?sel="+strconv.Itoa(rows[0].ID))
	if n := strings.Count(full.body, "<main"); n != 1 {
		t.Errorf("full document has %d <main> elements, want 1", n)
	}
	if n := strings.Count(full.body, `id="detail-pane"`); n != 1 {
		t.Errorf("full document has %d #detail-pane elements, want 1", n)
	}

	// Closing targets the pane without a ?sel=. It must get the (closed) pane
	// back as well, or the close click nests a second view in the journal.
	closed := fetch(t, h, http.MethodGet, "/journal?outcome=all", map[string]string{"HX-Request": "true", "HX-Target": "detail-pane"})
	if got := strings.TrimSpace(closed.body); !strings.HasPrefix(got, `<aside id="detail-pane"`) {
		t.Errorf("HX close swaps in something other than the pane: %.80s", got)
	}
	if strings.Contains(closed.body, "<main") {
		t.Error("HX close nests a <main>")
	}
	if strings.Contains(closed.body, `data-open="true"`) {
		t.Error("closed pane still reports data-open=true")
	}
}

func TestDetailDoesNotLeakOtherRows(t *testing.T) {
	h := fixture(t)
	rows := tickerdata.Sample()
	res := get(t, h, "/journal?sel="+strconv.Itoa(rows[len(rows)-1].ID)+"&outcome=filled")
	if strings.Count(res.body, `class="rowbtn"`) > 1 && strings.Contains(res.body, `data-open="false"`) {
		t.Error("detail swap should keep the table and the open pane consistent")
	}
}

func TestArmAndKillHaveNoRoute(t *testing.T) {
	h := fixture(t)
	for _, path := range []string{"/arm", "/kill", "/arm/", "/kill/", "/orders", "/trade"} {
		res := get(t, h, path)
		if res.status != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, res.status)
		}
		if !strings.Contains(res.body, banner) {
			t.Errorf("GET %s 404 lost the banner", path)
		}
	}

	for _, path := range []string{"/", "/arm", "/kill", "/journal", "/risk", "/calibration"} {
		res := fetch(t, h, http.MethodPost, path, map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
		if res.status == http.StatusOK || res.status == http.StatusNoContent || res.status == http.StatusCreated {
			t.Errorf("POST %s accepted with %d", path, res.status)
		}
	}
}

func TestGatedControlsRenderDisabled(t *testing.T) {
	h := fixture(t)
	for _, path := range []string{"/", "/risk"} {
		res := get(t, h, path)
		body := res.body
		start := strings.Index(body, "<button")
		if start < 0 {
			t.Fatalf("%s renders no gated control", path)
		}
		for {
			end := strings.Index(body[start:], "</button>")
			if end < 0 {
				break
			}
			tag := body[start : start+strings.Index(body[start:], ">")]
			for _, banned := range []string{"hx-", "onclick", "type=\"submit\""} {
				if strings.Contains(tag, banned) {
					t.Errorf("%s gated button carries %q: %s", path, banned, tag)
				}
			}
			start += end + len("</button>")
			if next := strings.Index(body[start:], "<button"); next < 0 {
				break
			} else {
				start += next
			}
		}
	}
}

func TestStaticAssetsServe(t *testing.T) {
	h := fixture(t)
	for _, path := range []string{"/static/styles.css", "/static/app.js", "/static/htmx.min.js"} {
		res := get(t, h, path)
		if res.status != http.StatusOK {
			t.Fatalf("%s = %d", path, res.status)
		}
		if len(res.body) == 0 {
			t.Errorf("%s is empty", path)
		}
		if cc := res.header.Get("Cache-Control"); !strings.Contains(cc, "max-age") {
			t.Errorf("%s cache control = %q", path, cc)
		}
	}
	if res := get(t, h, "/static/missing.css"); res.status != http.StatusNotFound {
		t.Errorf("missing asset = %d, want 404", res.status)
	}
}

func TestHealthz(t *testing.T) {
	res := get(t, fixture(t), "/healthz")
	if res.status != http.StatusOK {
		t.Fatalf("healthz = %d", res.status)
	}
	if strings.TrimSpace(res.body) != "ok" {
		t.Errorf("healthz body = %q", res.body)
	}
	if ct := res.header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("healthz content type = %q", ct)
	}
}

func TestEmptyLedgerStillRenders(t *testing.T) {
	h := New(Options{Now: func() time.Time { return tickerdata.SampleNow }})
	for _, path := range []string{"/", "/journal", "/calibration", "/risk", "/policy"} {
		res := get(t, h, path)
		if res.status != http.StatusOK {
			t.Errorf("%s over an empty ledger = %d", path, res.status)
		}
		if !strings.Contains(res.body, banner) {
			t.Errorf("%s over an empty ledger lost the banner", path)
		}
	}
}

func TestParseFilterSanitisesInput(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/journal?outcome=BOGUS&venue=coinbase&from=yesterday&to=2026-03-01&symbol=%20BTCUSDT%20", nil)
	f := parseFilter(req)
	if f.Outcome != "all" {
		t.Errorf("Outcome = %q, want all", f.Outcome)
	}
	if f.Venue != "coinbase" {
		t.Errorf("Venue = %q", f.Venue)
	}
	if f.Symbol != "BTCUSDT" {
		t.Errorf("Symbol = %q, want trimmed", f.Symbol)
	}
	if f.From != "" {
		t.Errorf("From = %q, want invalid date dropped", f.From)
	}
	if f.To != "2026-03-01" {
		t.Errorf("To = %q", f.To)
	}
}

func TestRecorderCapturesStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	r := &recorder{ResponseWriter: rec, status: http.StatusOK}
	r.WriteHeader(http.StatusTeapot)
	if r.status != http.StatusTeapot {
		t.Errorf("status = %d", r.status)
	}
}

// TestPagesDeclareSectionMarkers pins the panel marker for each route: the
// request renders the panel it names, not a default one.
func TestPagesDeclareSectionMarkers(t *testing.T) {
	h := fixture(t)
	cases := []struct{ path, marker, tab string }{
		{"/", `aria-label="Overview"`, "overview"},
		{"/journal", `aria-label="Journal"`, "journal"},
		{"/calibration", `aria-label="Calibration"`, "calibration"},
		{"/risk", `aria-label="Risk state"`, "risk"},
		{"/policy", `aria-label="Engine policy"`, "policy"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			res := get(t, h, c.path)
			if !strings.Contains(res.body, c.marker) {
				t.Errorf("%s missing section marker %s", c.path, c.marker)
			}
			if !strings.Contains(res.body, `data-tab="`+c.tab+`"`) {
				t.Errorf("%s does not declare data-tab=%q", c.path, c.tab)
			}
			// The panel lives inside the HTMX swap target, so a fragment
			// carries the same marker.
			frag := fetch(t, h, http.MethodGet, c.path, map[string]string{"HX-Request": "true"})
			if !strings.Contains(frag.body, c.marker) {
				t.Errorf("%s fragment lost the section marker", c.path)
			}
			if len(frag.body) >= len(res.body) {
				t.Errorf("%s fragment (%d B) is not smaller than the document (%d B)", c.path, len(frag.body), len(res.body))
			}
		})
	}
}

// TestMissingResources404 walks the routes that do not exist. A typo must land
// on the 404 page, never on a panel or a redirect that hides the mistake.
func TestMissingResources404(t *testing.T) {
	h := fixture(t)
	for _, path := range []string{
		"/nope", "/journal/", "/journal/extra", "/risk/extra",
		"/index.html", "/overview", "/api",
	} {
		t.Run(path, func(t *testing.T) {
			res := get(t, h, path)
			if res.status != http.StatusNotFound {
				t.Fatalf("GET %s = %d, want 404", path, res.status)
			}
			if ct := res.header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("Content-Type = %q, want text/html", ct)
			}
			if cc := res.header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
				t.Errorf("Cache-Control = %q, want no-store", cc)
			}
			if !strings.Contains(res.body, "No such page") {
				t.Error("404 page is missing its explanation")
			}
			if !strings.Contains(res.body, banner) {
				t.Error("404 page lost the paper-only banner")
			}
			// The 404 keeps the shell's <main id="view"> but must not claim a
			// tab, so HTMX cannot adopt it as a panel.
			if strings.Contains(res.body, `data-tab=`) {
				t.Error("404 rendered a panel tab")
			}
		})
	}

	// The bare /static prefix is not a page: the mux either redirects it to
	// /static/ or leaves it to the 404 handler.
	r := get(t, h, "/static")
	switch r.status {
	case http.StatusMovedPermanently, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		if loc := r.header.Get("Location"); loc != "/static/" {
			t.Errorf("GET /static redirects to %q, want /static/", loc)
		}
	case http.StatusNotFound:
	default:
		t.Errorf("GET /static = %d, want a redirect or 404", r.status)
	}

	// A missing route is a navigation event, so HTMX gets the same document.
	res := fetch(t, h, http.MethodGet, "/nope", map[string]string{"HX-Request": "true"})
	if res.status != http.StatusNotFound || !strings.Contains(res.body, "<!doctype") {
		t.Errorf("HTMX 404 = %d, full-document = %v", res.status, strings.Contains(res.body, "<!doctype"))
	}
}

// TestMethodGateRejectsWrites covers the whole surface: every route is GET
// only, so writes, patches, deletes and preflight all fall through to the 404
// handler without reaching a panel or an asset.
func TestMethodGateRejectsWrites(t *testing.T) {
	h := fixture(t)
	paths := []string{"/", "/journal", "/calibration", "/risk", "/policy", "/healthz", "/static/styles.css"}
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		for _, path := range paths {
			res := fetch(t, h, m, path, nil)
			if res.status != http.StatusNotFound && res.status != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 404 or 405", m, path, res.status)
			}
			if path == "/static/styles.css" && strings.Contains(res.body, "--bg:") {
				t.Errorf("%s %s served the asset body", m, path)
			}
		}
	}
}

// TestNoOrderOrMutationSurface sweeps the paths an order path would plausibly
// occupy. The console must answer none of them, under any method.
func TestNoOrderOrMutationSurface(t *testing.T) {
	h := fixture(t)
	paths := []string{
		"/order", "/orders", "/api/order", "/api/orders", "/trade", "/trades",
		"/dispatch", "/api/dispatch", "/exec", "/submit", "/flatten",
		"/arm", "/disarm", "/kill", "/re-arm", "/rearm", "/reset",
		"/admin", "/config", "/settings", "/session", "/login",
	}
	for _, path := range paths {
		for _, m := range []string{http.MethodGet, http.MethodPost} {
			res := fetch(t, h, m, path, nil)
			if res.status >= 200 && res.status < 300 {
				t.Fatalf("%s %s = %d: mutating route exists", m, path, res.status)
			}
			if res.status != http.StatusNotFound && res.status != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 404 or 405", m, path, res.status)
			}
		}
	}
}

// TestStaticAssetsAreEmbeddedBytes proves the assets come from this binary:
// the response is byte-identical to the embedded filesystem, so nothing is
// proxied to a CDN or a local dev server.
func TestStaticAssetsAreEmbeddedBytes(t *testing.T) {
	h := fixture(t)
	cases := []struct{ path, name, ctype string }{
		{"/static/styles.css", "styles.css", "text/css"},
		{"/static/app.js", "app.js", "text/javascript"},
		{"/static/htmx.min.js", "htmx.min.js", "text/javascript"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := get(t, h, c.path)
			if res.status != http.StatusOK {
				t.Fatalf("%s = %d, want 200", c.path, res.status)
			}
			if ct := res.header.Get("Content-Type"); !strings.HasPrefix(ct, c.ctype) {
				t.Errorf("Content-Type = %q, want prefix %q", ct, c.ctype)
			}
			f, err := web.Static().Open(c.name)
			if err != nil {
				t.Fatalf("embedded %s: %v", c.name, err)
			}
			defer f.Close()
			embedded, err := io.ReadAll(f)
			if err != nil {
				t.Fatalf("read embedded %s: %v", c.name, err)
			}
			if res.body != string(embedded) {
				t.Errorf("served %s (%d B) differs from embedded copy (%d B)", c.name, len(res.body), len(embedded))
			}
		})
	}
	// A miss is the file server's own plain-text 404, not the page shell.
	miss := get(t, h, "/static/nope.css")
	if miss.status != http.StatusNotFound {
		t.Errorf("missing asset = %d, want 404", miss.status)
	}
	if strings.Contains(miss.body, "<!doctype") {
		t.Error("missing asset rendered the HTML shell")
	}
}

func TestRangePillsResolveToARoute(t *testing.T) {
	h := New(Options{Rows: tickerdata.Sample(), Now: func() time.Time { return tickerdata.SampleNow }})
	for _, path := range []string{"/?range=24h", "/?range=today"} {
		t.Run(path, func(t *testing.T) {
			res := get(t, h, path)
			if res.status != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", path, res.status)
			}
		})
	}
}

// journalHandler serves attempts mapped from journal records, the way the
// binary does when it is pointed at an attempt journal instead of the sample.
func journalHandler(t *testing.T, records []journalread.Record) http.Handler {
	t.Helper()
	return New(Options{
		Rows:   journalread.Map(records),
		Now:    func() time.Time { return tickerdata.SampleNow },
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Source: "attempt journal",
	})
}

func judgeRecord(id int64, p float64) journalread.Record {
	return journalread.Record{
		AttemptID:  id,
		Stage:      "judge",
		Outcome:    "blocked",
		ReasonCode: "judge_below_threshold",
		Confidence: &p,
		Timestamp:  tickerdata.SampleNow.Add(-time.Hour),
	}
}

func venueRecord(id int64) journalread.Record {
	return journalread.Record{
		AttemptID: id,
		Stage:     "venue",
		Outcome:   "filled",
		Venue:     "coinbase",
		Symbol:    "BTC-USD",
		Timestamp: tickerdata.SampleNow.Add(-59 * time.Minute),
	}
}

func TestCalibrationRendersDerivedBuckets(t *testing.T) {
	// Three scored attempts land in the same band; two of them filled, so the
	// page has to report the band, its n and a two-of-three win rate.
	h := journalHandler(t, []journalread.Record{
		judgeRecord(1, 0.42), venueRecord(1),
		judgeRecord(2, 0.42), venueRecord(2),
		judgeRecord(3, 0.42), journalread.Record{
			AttemptID: 3,
			Stage:     "venue",
			Outcome:   "rejected",
			Venue:     "coinbase",
			Symbol:    "BTC-USD",
			Timestamp: tickerdata.SampleNow.Add(-58 * time.Minute),
		},
	})
	body := get(t, h, "/calibration").body

	if !strings.Contains(body, "0.40–0.50") {
		t.Errorf("calibration page has no 0.40–0.50 bucket:\n%s", body)
	}
	if !strings.Contains(body, "3 scored attempts · 0 excluded") {
		t.Errorf("calibration page does not report the derived sample:\n%s", body)
	}
	if strings.Contains(body, "0.90+") || strings.Contains(body, " judge calls") {
		t.Error("calibration page still carries the retired sample fixture")
	}
	// Every bucket is under the minimum-sample floor, and the page says so.
	if !strings.Contains(body, "Low sample") {
		t.Error("calibration page does not mark the under-sampled bucket")
	}
}

func TestCalibrationEmptyJournalRendersEmptyState(t *testing.T) {
	h := journalHandler(t, nil)
	body := get(t, h, "/calibration").body

	if !strings.Contains(body, "No scored attempts yet") {
		t.Errorf("empty journal does not render the empty state:\n%s", body)
	}
	if !strings.Contains(body, "0 excluded (no score)") {
		t.Errorf("empty journal does not report the scoreless attempts:\n%s", body)
	}
	// The zero-observation shape still draws all ten bands.
	if !strings.Contains(body, "0.00–0.10") || !strings.Contains(body, "0.90–1.00") {
		t.Errorf("empty state does not draw the band frame:\n%s", body)
	}
}

func TestNoPathServesTheCalibrationSample(t *testing.T) {
	// A journal with no judge scores must never fall back to CalSample, on any
	// page that could show calibration data.
	h := New(Options{
		Rows: []tickerdata.Attempt{{ID: 1, Outcome: tickerdata.Filled, TS: tickerdata.SampleNow}},
		Now:  func() time.Time { return tickerdata.SampleNow },
		Log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	for _, path := range []string{"/", "/calibration", "/risk", "/journal"} {
		res := get(t, h, path)
		if res.status != http.StatusOK {
			t.Fatalf("%s status = %d", path, res.status)
		}
		if strings.Contains(res.body, "0.90+") {
			t.Errorf("%s served the sample calibration fixture", path)
		}
	}
}
