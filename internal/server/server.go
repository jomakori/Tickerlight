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
	"github.com/jomakori/tickerlight/internal/status"
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
	// Status reads the engine's status snapshot from the file beside the
	// journal and is called once per render, so a page left open follows the
	// engine's arming and caps instead of the state it booted with. Nil means
	// no snapshot: the console then renders its built-in paper-only wording
	// rather than guessing a mode.
	Status func() status.Snapshot
	// Caps reads the engine's spend caps and policy through the engine's
	// read-only caps_status tool. Nil renders the Policy panel's unavailable
	// state: the console never substitutes a default cap for a missing engine,
	// and never falls back to its own environment for mode, judge threshold or
	// alert-sink state.
	Caps func(context.Context) (engine.Caps, error)
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
	s := &server{rows: opts.Rows, now: opts.Now, log: opts.Log, source: opts.Source, status: opts.Status, caps: opts.Caps}

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
	status func() status.Snapshot
	caps   func(context.Context) (engine.Caps, error)
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
		if s.status != nil {
			v.Status = s.status()
		}
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

// policyReading asks the engine for its caps and the policy it reports running
// under. Both come from the same caps_status response, so the panel has one
// source of truth and never consults this process's own environment.
//
// Failure is a first-class state. When the engine is unreachable, rejects the
// token, or answers something unparsable, the panel says so and shows no
// numbers — caps or policy: a fabricated cap is worse than a blank one, because
// someone might trade against it, and an inferred mode is worse than none,
// because someone might trust it.
func (s *server) policyReading(parent context.Context) ui.PolicyData {
	var d ui.PolicyData
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
