// Command trading-decider asks Jev (TypeSafe System One) what to do each
// tick and submits the answer to the trading engine, which still enforces
// its own caps, judge and arm window.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jomakori/tickerlight/internal/decider"
)

func main() {
	interval := flag.Duration("interval", envDuration("DECIDER_INTERVAL", 15*time.Minute), "tick interval")
	journal := flag.String("journal", envString("DECIDER_JOURNAL", "/data/decisions.jsonl"), "append-only decision journal")
	once := flag.Bool("once", false, "run a single tick and exit")
	flag.Parse()

	cfg := decider.Defaults()
	cfg.Interval = *interval
	cfg.JournalPath = *journal
	if s := envString("DECIDER_SYMBOLS", ""); s != "" {
		cfg.Symbols = splitList(s)
	}
	if v, ok := envInt("TRADING_MCP_PER_ORDER_CAP_CENTS"); ok {
		cfg.PerOrderCapCents = v
	}
	if v, ok := envFloat("TRADING_MCP_MAX_QTY_PER_SYMBOL"); ok {
		cfg.MaxQtyPerSymbol = v
	}

	key := os.Getenv("TYPESAFE_AI_API_KEY")
	if key == "" {
		log.Fatal("decider: TYPESAFE_AI_API_KEY is required")
	}

	engine := &decider.EngineClient{
		URL:   envString("TRADING_MCP_URL", "http://127.0.0.1:8090/mcp"),
		Token: os.Getenv("TRADING_MCP_HTTP_TOKEN"),
		HTTP:  &http.Client{Timeout: 20 * time.Second},
	}
	d := &decider.Decider{
		Cfg:    cfg,
		Engine: engine,
		Jev:    &decider.Client{APIKey: key, HTTP: &http.Client{Timeout: 20 * time.Second}},
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	run := func() {
		if !decider.MarketOpen(time.Now()) {
			log.Printf("decider: market closed, skipping tick")
			return
		}
		e := d.Tick(ctx)
		log.Printf("decider: symbol=%s choice=%s passed=%v reason=%q order=%v latency=%dms",
			e.Symbol, choiceOf(e), e.Gate.Passed, e.Gate.Reason, e.Order, e.LatencyMS)
	}

	run()
	if *once {
		return
	}

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Print("decider: shutting down")
			return
		case <-ticker.C:
			run()
		}
	}
}

func choiceOf(e decider.Entry) string {
	if e.Jev == nil {
		return ""
	}
	return e.Jev.Choice
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		log.Printf("decider: %s=%q is not a duration, using %s", key, v, def)
	}
	return def
}

func envInt(key string) (int64, bool) {
	v := os.Getenv(key)
	if v == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func envFloat(key string) (float64, bool) {
	v := os.Getenv(key)
	if v == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
