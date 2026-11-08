<div align="center">

# Tickerlight

**Your trade made a decision. Can you prove it was the right one?**

Trade review, AI decision auditing, and risk visibility in one fast, self-hosted console.

`Go` · `templ` · `HTMX` · `one binary, no build step`

</div>

![Tickerlight overview — P&L, caps, and gate outcomes](docs/screenshots/overview-1280.png)

---

## Most trading dashboards show you what happened. None of them show you why.

You got filled. You got blocked. You got rejected by the venue. The P&L line moves and you're
left reverse-engineering the reason from logs scattered across three systems.

And if an AI model is in the loop, the harder question has no answer at all: **was its confidence
actually worth anything?**

Tickerlight is built around that question. Every attempt is one row with one traceable answer —
intent, AI judge, risk gate, venue — and every AI decision is scored against what actually
happened afterwards.

## What you get

**Trade performance, without the spreadsheets.**
Realized P&L, fills, win rate, max drawdown, and fees across any window you care about — all
rendered from the same ledger that feeds the journal, so the numbers can't drift apart.

**Every trade traced end to end.**
Pick an attempt and walk the chain: what was intended, what the judge scored it, which gate
stopped it, what the venue said. Blocks stop looking like outages and start looking like the
system working.

**AI decisions held to account.**
A calibration view plots the judge's stated probability against realized win rate, bucket by
bucket. Minimum-sample floors mean thin buckets can't quietly promote a model that only looks
confident. This is the part you can't get from a P&L chart.

**Risk state, not risk vibes.**
Rolling 24h exposure against daily and per-order caps, drawdown versus limit, cooldown, and every
pre-trade check visible with the value it compared.

![Attempt journal with per-attempt decision chain](docs/screenshots/journal-detail-1280.png)

**Fast because it's boring.**
Go `net/http` and server-rendered templ, with HTMX swapping panels in place. No SPA, no client
router, no `node_modules`, no CDN dependency at runtime. One static binary that starts instantly
and serves HTML.

**Review from wherever you are.**
The same console, reflowed for a phone — because the moment you want to check exposure is rarely
the moment you're at your desk.

<p align="center">
  <img src="docs/screenshots/overview-390.png" alt="Tickerlight mobile view" width="320">
</p>

## Quickstart

Requires Go 1.27+ and `templ`:

```sh
go install github.com/a-h/templ/cmd/templ@v0.3.1100
templ generate
go run ./cmd/tickerlight
```

Then open <http://localhost:8080>.

## Current status

Tickerlight currently runs on a **local sample ledger**. Trade outcomes and AI judge scores are
sample data — there is no live broker, market-data feed, or model provider connected, and the
binary has no order path. The console is read-only; arming and kill-switch controls render
disabled and are inert server-side.

That's deliberate: the review surfaces are the product, and they're validated against a fixed
ledger so the numbers are reproducible before any live wiring exists.

## Documentation

- [Development guide](docs/development.md) — build, test, dev loop, project layout
- [Editor setup](docs/dev/lsp-setup.md) — templ LSP and gopls

## License

No license file is included yet; licensing will be declared before publication.
