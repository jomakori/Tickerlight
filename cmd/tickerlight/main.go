// Command tickerlight serves the read-only ops console over a local sample
// ledger. It has no exchange client and no order path.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jomakori/tickerlight/internal/journalread"
	"github.com/jomakori/tickerlight/internal/judge"
	"github.com/jomakori/tickerlight/internal/server"
	"github.com/jomakori/tickerlight/internal/tickerdata"
)

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

	handler := server.New(server.Options{Rows: rows, Log: log, Source: source})
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
