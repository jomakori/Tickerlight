package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jomakori/tickerlight/internal/engine"
	"github.com/jomakori/tickerlight/internal/tickerdata"
	"github.com/jomakori/tickerlight/internal/ui"
	"github.com/jomakori/tickerlight/internal/web"
)

// Options configures the console.
type Options struct {
	// Rows is the read-only ledger the console renders. Nil renders the empty
	// state rather than fetching anything.
	Rows []tickerdata.Attempt
	// Now is the wall clock used for the 24h window and the cooldown timer.
	Now func() time.Time
	// Log receives one line per request.
	Log *slog.Logger
	// Source names where Rows came from, so the header can say it. Empty
	// means the built-in sample ledger.
	Source string
	// Caps reads the engine's spend caps through the engine's read-only
	// caps_status tool. Nil renders the Policy panel's unavailable state: the
	// console never substitutes a default cap for a missing engine.
	Caps func(context.Context) (engine.Caps, error)
	// Policy is the engine configuration this process can see for itself —
	// mode, judge threshold and alert sink. Empty fields render as "not
	// reported" rather than as an inferred value.
	Policy engine.Declared
}

// New returns the console handler. It serves HTML and embedded static assets,
// and makes exactly one kind of outbound call: the read-only caps_status tool on
// the engine's loopback MCP endpoint. There is no order path and no mutating
// endpoint.
func New(opts Options) http.Handler {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}
	s := &server{rows: opts.Rows, now: opts.Now, log: opts.Log, source: opts.Source, caps: opts.Caps, policy: opts.Policy}

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheStatic(http.FileServer(web.Static()))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /{$}", s.page(ui.TabOverview))
	mux.HandleFunc("GET /journal", s.page(ui.TabJournal))
	mux.HandleFunc("GET /calibration", s.page(ui.TabCalibration))
	mux.HandleFunc("GET /risk", s.page(ui.TabRisk))
	mux.HandleFunc("GET /policy", s.page(ui.TabPolicy))
	mux.HandleFunc("/", s.notFound)
	return s.withLogging(mux)
}

type server struct {
	rows   []tickerdata.Attempt
	now    func() time.Time
	log    *slog.Logger
	source string
	caps   func(context.Context) (engine.Caps, error)
	policy engine.Declared
}

// page builds one view from the request and renders either the whole document
// or just the swapped <main>, depending on the HX-Request header.
func (s *server) page(tab string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v := ui.NewView(s.rows, s.now())
		v.Tab = tab
		v.Range = tickerdata.ParseRange(r.URL.Query().Get("range"))
		v.Filter = parseFilter(r)
		v.Source = s.source
		if tab == ui.TabPolicy {
			v.Policy = s.policyReading(r.Context())
		}

		detail := false
		if tab == ui.TabJournal {
			if v.Filter.Outcome == "" {
				v.Filter.Outcome = "all"
			}
			// Journal links emit ?sel=; ?a= is kept as an alias so hand-typed
			// and older links keep resolving.
			raw := r.URL.Query().Get("sel")
			if raw == "" {
				raw = r.URL.Query().Get("a")
			}
			if id, err := strconv.Atoi(raw); err == nil && id > 0 {
				row, ok := tickerdata.ByID(s.rows, id)
				if !ok {
					s.notFound(w, r)
					return
				}
				v.Selected = &row
				detail = true
			}
		}
		s.render(w, r, v, detail)
	}
}

// policyTimeout bounds the engine call: a wedged engine costs this panel three
// seconds and then renders as unavailable, rather than hanging the page.
const policyTimeout = 3 * time.Second

// policyReading asks the engine for its caps and pairs them with the policy this
// process can see for itself.
//
// Failure is a first-class state. When the engine is unreachable, rejects the
// token, or answers something unparsable, the panel says so and shows no
// numbers: a fabricated cap is worse than a blank one, because someone might
// trade against it.
func (s *server) policyReading(parent context.Context) ui.PolicyData {
	d := ui.PolicyData{Mode: s.policy.Mode, Threshold: s.policy.JudgeThreshold, AlertSink: s.policy.AlertSink}
	if s.caps == nil {
		d.Reason = "no engine client is configured in this process"
		return d
	}
	ctx, cancel := context.WithTimeout(parent, policyTimeout)
	defer cancel()
	caps, err := s.caps(ctx)
	if err != nil {
		d.Reason = err.Error()
		return d
	}
	d.Available = true
	d.Caps = caps
	return d
}

func (s *server) render(w http.ResponseWriter, r *http.Request, v ui.View, detail bool) {
	var buf bytes.Buffer
	var err error
	// An HTMX request that targets #detail-pane must get the pane back, not the
	// panel: a <main> there replaces the aside with a whole view and leaves two
	// nodes with id="view" for every later hx-target="#view" swap. Both the row
	// (open) and the Close control (close) name their target in HX-Target; the
	// ?sel= check keeps the older hand-written links working.
	htmx := r.Header.Get("HX-Request") == "true"
	pane := v.Tab == ui.TabJournal && (detail || r.Header.Get("HX-Target") == "detail-pane")
	switch {
	case htmx && pane:
		w.Header().Set("Vary", "HX-Request")
		err = ui.DetailPane(v.Journal()).Render(r.Context(), &buf)
	case htmx:
		w.Header().Set("Vary", "HX-Request")
		err = ui.ViewShell(v).Render(r.Context(), &buf)
	default:
		err = ui.Layout(v).Render(r.Context(), &buf)
	}
	if err != nil {
		s.log.Error("render failed", "path", r.URL.Path, "err", err)
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func (s *server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"dur", time.Since(start).Round(time.Microsecond).String(),
			"hx", r.Header.Get("HX-Request") == "true",
		)
	})
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// cacheStatic marks assets immutable but revalidating; the filenames are fixed
// so a long max-age would strand clients across deploys.
func cacheStatic(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=300")
		h.ServeHTTP(w, r)
	})
}

// parseFilter reads the journal filter from the query string. Unknown values
// are ignored rather than rejected: a stale bookmark should still render.
func parseFilter(r *http.Request) tickerdata.Filter {
	q := r.URL.Query()
	f := tickerdata.Filter{
		Outcome: strings.TrimSpace(q.Get("outcome")),
		Venue:   strings.TrimSpace(q.Get("venue")),
		Symbol:  strings.TrimSpace(q.Get("symbol")),
		From:    validDate(q.Get("from")),
		To:      validDate(q.Get("to")),
	}
	if f.Outcome != "" && !knownOutcome(f.Outcome) {
		f.Outcome = "all"
	}
	return f
}

func knownOutcome(v string) bool {
	switch v {
	case "all", "filled", "cap", "kill", "judge", "venue", "blocked":
		return true
	}
	return false
}

// validDate accepts YYYY-MM-DD and drops anything else, so a hand-edited URL
// cannot inject an unparsable value into the date inputs.
func validDate(v string) string {
	if v == "" {
		return ""
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return ""
	}
	return v
}

// notFound renders the 404 page. HTMX requests get the same document because
// a missing route is a navigation-level event, not a panel refresh.
func (s *server) notFound(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	if err := ui.NotFound().Render(r.Context(), &buf); err != nil {
		s.log.Error("render 404 failed", "path", r.URL.Path, "err", err)
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write(buf.Bytes())
}
