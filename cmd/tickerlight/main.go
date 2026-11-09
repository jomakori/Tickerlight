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
	"syscall"
	"time"

	"github.com/jomakori/tickerlight/internal/journalread"
	"github.com/jomakori/tickerlight/internal/server"
	"github.com/jomakori/tickerlight/internal/tickerdata"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	journalPath := flag.String("journal", "", "trading-mcp attempt journal (JSONL) to render; empty serves the built-in sample ledger")
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
		if err != nil {
			log.Error("read journal", "path", *journalPath, "err", err)
			os.Exit(1)
		}
		rows = loaded
		mode = "journal"
		source = "attempt journal"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(drain)
	}()

	log.Info("tickerlight console", "addr", *addr, "rows", len(rows), "mode", mode)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
}
