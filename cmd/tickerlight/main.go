// Command tickerlight serves the read-only ops console over a local sample
// ledger. It reads the engine's spend caps over loopback; it has no exchange
// client and no order path.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jomakori/tickerlight/internal/armctl"
	"github.com/jomakori/tickerlight/internal/decisionsread"
	"github.com/jomakori/tickerlight/internal/engine"
	"github.com/jomakori/tickerlight/internal/journalread"
	"github.com/jomakori/tickerlight/internal/judge"
	"github.com/jomakori/tickerlight/internal/server"
	"github.com/jomakori/tickerlight/internal/status"
	"github.com/jomakori/tickerlight/internal/tickerdata"
)

// decisionsPathFor locates the decider's journal: explicit wins, otherwise
// decisions.jsonl beside the attempt journal, and sample mode has neither.
func decisionsPathFor(journal, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if journal == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(journal), "decisions.jsonl")
}

// loadDecisions reads the decisions journal for one render. A missing file is
// normal before the decider's first tick; an unreadable one is shown as
// unavailable rather than as an empty day.
func loadDecisions(path string, log *slog.Logger) server.DecisionsReading {
	entries, err := decisionsread.Load(path)
	switch {
	case errors.Is(err, decisionsread.ErrNotFound):
		return server.DecisionsReading{Note: "decisions journal: " + path + " (no decisions yet)"}
	case err != nil:
		log.Error("read decisions", "path", path, "err", err)
		return server.DecisionsReading{Note: err.Error(), Unavailable: true}
	}
	note := "decisions journal: " + path
	if len(entries) == 0 {
		note += " (no decisions yet)"
	}
	return server.DecisionsReading{Rows: entries, Note: note}
}

// statusPathFor resolves where the engine's status snapshot lives: the -status
// the caller gave, otherwise status.json beside the journal it belongs to.
// Sample mode has no journal, so it has no snapshot to read.
func statusPathFor(journal, explicit string) string {
	if journal == "" {
		return ""
	}
	if explicit != "" {
		return explicit
	}
	return filepath.Join(filepath.Dir(journal), "status.json")
}

// loadStatus reads the snapshot and keeps the console's built-in wording when it
// cannot. A missing file is normal — the engine may not have written one yet —
// and a corrupt one must neither take the console down nor be believed. It runs
// once per render, so each failure kind is warned about once per process:
// repeating it per request would bury the line that matters.
var reportedStatusFailure sync.Once

func loadStatus(path string, log *slog.Logger) status.Snapshot {
	snap, err := status.Load(path)
	switch {
	case err == nil:
		return snap
	case errors.Is(err, fs.ErrNotExist):
		reportedStatusFailure.Do(func() {
			log.Warn("status snapshot not found; rendering without engine state", "path", path)
		})
	default:
		reportedStatusFailure.Do(func() {
			log.Warn("status snapshot unusable; rendering without engine state", "path", path, "err", err)
		})
	}
	return status.Snapshot{}
}

// envOr, envBool and envFloat let every judge flag be set by environment
// variable too (TICKERLIGHT_JUDGE_*), matching the console's existing
// flag-based configuration mechanism without hard-coding any threshold: an
// explicit flag always wins over the environment, and the environment wins
// over the compiled-in default.
func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt64(key string, def int64) int64 {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	journalPath := flag.String("journal", "", "trading-mcp attempt journal (JSONL) to render; empty serves the built-in sample ledger")
	statusPath := flag.String("status", "", "engine status snapshot (JSON) to render mode, arming, kill switch and caps from; default: status.json beside the journal")
	decisionsPath := flag.String("decisions", envOr("TICKERLIGHT_DECISIONS", ""), "decider decisions journal (JSONL) for the Decisions panel; default: decisions.jsonl beside the journal")

	// Judge scorer: off by default, loopback-only by default. It is never
	// mounted on the public console mux (server.New) — it is its own
	// listener on its own http.Server, so there is no route or ingress path
	// that can reach it unless an operator points traffic at judgeAddr
	// directly.
	judgeEnabled := flag.Bool("judge-enabled", envBool("TICKERLIGHT_JUDGE_ENABLED", false), "enable the rule-based order judge scorer (POST /score) on its own loopback listener")
	judgeAddr := flag.String("judge-addr", envOr("TICKERLIGHT_JUDGE_ADDR", "127.0.0.1:8091"), "listen address for the judge scorer; must be loopback unless -judge-allow-non-loopback is set")
	judgeAllowNonLoopback := flag.Bool("judge-allow-non-loopback", envBool("TICKERLIGHT_JUDGE_ALLOW_NON_LOOPBACK", false), "allow the judge scorer to bind a non-loopback address (dangerous: exposes the scorer off-host)")
	judgeNotionalCeilingCents := flag.Int64("judge-notional-ceiling-cents", envInt64("TICKERLIGHT_JUDGE_NOTIONAL_CEILING_CENTS", 5000), "per-order notional ceiling in cents (minor units) above which the judge score is penalised; default 5000 = $50.00; <=0 disables the check")
	judgeAllowlist := flag.String("judge-allowlist", envOr("TICKERLIGHT_JUDGE_ALLOWLIST", ""), "comma-separated symbol allowlist for the judge scorer; empty permits any symbol")

	// Policy panel: the console reads the engine's caps through caps_status on
	// the engine's loopback MCP endpoint. The bearer token is never a flag —
	// argv is visible in ps — it comes from the engine's own variable,
	// TRADING_MCP_HTTP_TOKEN.
	engineMCPURL := flag.String("engine-mcp-url", envOr("TICKERLIGHT_ENGINE_MCP_URL", "http://127.0.0.1:8090/mcp"), "engine MCP endpoint the Policy panel reads caps_status from; must be loopback")

	// Arming: the console's one control. It is off unless this environment
	// names the engine's admin URL, so a console deployed without it exposes no
	// route and no form. Who may use the control is the Cloudflare Access
	// policy on the public host, not a list in here.
	engineAdminURL := flag.String("engine-admin-url", envOr("TICKERLIGHT_ENGINE_ADMIN_URL", ""), "engine admin base URL the arming window is posted to; empty disables the control; must be loopback")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Two sources, one shape: with --journal the console shows the real
	// attempt ledger, without it the built-in sample. A journal path that
	// cannot be read is fatal on purpose — falling back to the sample
	// would show plausible trades that never happened.
	rows := tickerdata.Sample()
	mode := "sample"
	source := ""
	if *journalPath != "" {
		loaded, err := journalread.Load(*journalPath)
		switch {
		case errors.Is(err, journalread.ErrNotFound):
			// The engine writes the journal on its first attempt, so a console
			// that starts first must show "nothing yet", not crash and not the
			// sample ledger.
			log.Warn("journal not found yet", "path", *journalPath)
			rows = nil
			mode = "journal"
			source = "attempt journal (no attempts recorded yet)"
		case err != nil:
			log.Error("read journal", "path", *journalPath, "err", err)
			os.Exit(1)
		default:
			rows = loaded
			mode = "journal"
			source = "attempt journal"
		}
	}

	// The status snapshot is the engine's own account of itself, and it is read
	// only alongside a journal: sample mode has no engine and keeps its
	// sample-data copy. A snapshot that is missing or unusable is not fatal —
	// the console then renders its built-in paper-only wording, which is the
	// honest default when nothing on disk can be vouched for.
	var statusSource func() status.Snapshot
	switch {
	case *journalPath != "":
		// Read per request, not at startup: the console outlives an engine
		// restart, and arming changes while the page is open. Reading the file
		// once at boot pinned a live engine's page to "paper only" whenever the
		// console came up first.
		path := statusPathFor(*journalPath, *statusPath)
		statusSource = func() status.Snapshot { return loadStatus(path, log) }
	case *statusPath != "":
		log.Warn("status snapshot ignored: sample mode has no engine", "path", *statusPath)
	}

	// One outbound client for the whole console, bounded so a silent engine
	// cannot hold a render open.
	engineHTTP := &http.Client{Timeout: 5 * time.Second}
	var capsSource func(context.Context) (engine.Caps, error)
	if token := strings.TrimSpace(os.Getenv("TRADING_MCP_HTTP_TOKEN")); token != "" {
		capsSource = (&engine.Client{URL: *engineMCPURL, Token: token, HTTP: engineHTTP}).Caps
		log.Info("policy panel", "engine", *engineMCPURL)
	} else {
		log.Warn("policy panel: TRADING_MCP_HTTP_TOKEN is empty, caps will render unavailable", "engine", *engineMCPURL)
	}

	// The decisions journal belongs to the decider, which writes it whether or
	// not this console is up, so it is read per request like the status
	// snapshot and never at boot.
	var decisionsSource func() server.DecisionsReading
	if path := decisionsPathFor(*journalPath, *decisionsPath); path != "" {
		decisionsSource = func() server.DecisionsReading { return loadDecisions(path, log) }
	}

	// Arming is opt-in per environment: no admin URL, no route. The URL must
	// stay loopback for the same reason the MCP URL does — the window is
	// written by the engine beside this process, not by anything off-host.
	var armOptions *server.ArmOptions
	if *engineAdminURL != "" {
		if err := requireLoopback("engine-admin-url", *engineAdminURL); err != nil {
			log.Error("refusing to start with a non-loopback admin URL", "err", err)
			os.Exit(1)
		}
		client := armctl.Client{URL: *engineAdminURL}
		armOptions = &server.ArmOptions{Arm: client.Arm}
		log.Info("arming control enabled", "engine", *engineAdminURL)
	}

	handler := server.New(server.Options{
		Rows:      rows,
		Log:       log,
		Source:    source,
		Status:    statusSource,
		Caps:      capsSource,
		Decisions: decisionsSource,
		Arm:       armOptions,
	})
	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Judge scorer runs on its own http.Server with its own listener: it is
	// never mounted on the console's mux, so there is no shared route or
	// ingress path that could expose /score alongside the public pages.
	var judgeSrv *http.Server
	if *judgeEnabled {
		judgeCfg := judge.Config{
			NotionalCeilingCents: *judgeNotionalCeilingCents,
			Allowlist:            judge.ParseAllowlist(*judgeAllowlist),
		}
		if err := judge.ValidateBindAddr(*judgeAddr, *judgeAllowNonLoopback); err != nil {
			log.Error("judge addr rejected", "err", err)
			os.Exit(1)
		}
		judgeSrv = &http.Server{
			Addr:              *judgeAddr,
			Handler:           judge.NewHandler(judgeCfg, log),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(drain)
		if judgeSrv != nil {
			_ = judgeSrv.Shutdown(drain)
		}
	}()

	if judgeSrv != nil {
		judgeCfg := judge.Config{NotionalCeilingCents: *judgeNotionalCeilingCents, Allowlist: judge.ParseAllowlist(*judgeAllowlist)}
		log.Info("tickerlight judge scorer", "addr", *judgeAddr, "config", judgeCfg.String())
		go func() {
			if err := judgeSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("judge listen", "err", err)
			}
		}()
	}

	log.Info("tickerlight console", "addr", *addr, "rows", len(rows), "mode", mode)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
}

// requireLoopback rejects an engine URL that is not loopback. The arming window
// is written by the engine beside this process; posting it across the network
// would move the one control off the pod.
func requireLoopback(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s %q: %w", name, raw, err)
	}
	switch host := u.Hostname(); {
	case host == "127.0.0.1", host == "::1", strings.EqualFold(host, "localhost"):
		return nil
	default:
		return fmt.Errorf("%s %q is not loopback", name, raw)
	}
}
