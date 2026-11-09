<div align="center">

<img src="docs/assets/banner.svg" alt="Tickerlight — every trade explained, every AI decision scored" width="100%">

<p align="center">
  <img src="https://img.shields.io/github/license/jomakori/Tickerlight?logo=apache&logoColor=white&label=License" alt="License">
  <img src="https://img.shields.io/github/last-commit/jomakori/Tickerlight?logo=git&logoColor=white&label=Last%20commit" alt="Last commit">
  <img src="https://img.shields.io/github/stars/jomakori/Tickerlight?logo=github&logoColor=white&label=Stars" alt="Stars">
  <img src="https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white&label=Go" alt="Go 1.27">
  <img src="https://img.shields.io/badge/templ-HTML%20components-ffd400" alt="templ">
  <img src="https://img.shields.io/badge/HTMX-2.0.4-3d72d7" alt="HTMX 2.0.4">
  <img src="https://img.shields.io/badge/deploy-single%20binary-2ea44f" alt="Single binary">
</p>

</div>

## Table of Contents

- [Demo](#demo)
- [What is it?](#what-is-it)
- [Features](#features)
  - [Trade performance](#trade-performance)
  - [Decision trace](#decision-trace)
  - [AI calibration](#ai-calibration)
  - [Risk visibility](#risk-visibility)
  - [Deployment](#deployment)
  - [Mobile review](#mobile-review)
- [Quickstart](#quickstart)
- [Current status](#current-status)
- [Documentation](#documentation)
- [License](#license)

## Demo

![Tickerlight overview — P&L, caps, and gate outcomes](docs/screenshots/overview-1280.png)

## What is it?

Tickerlight is a self-hosted console for reviewing what your trading system did, and why. It puts
trade outcomes, the decision chain behind each attempt, AI judge calibration, and live risk
headroom in one place — served as plain HTML from a single Go binary.

Most dashboards show you what happened. Tickerlight shows you why — and whether the AI that made
the call was actually right.

## Features

### Trade performance

Realized P&L, fills, win rate, max drawdown, and fees across any window you care about — all
rendered from the same ledger that feeds the journal, so the numbers can't drift apart.

### Decision trace

Pick an attempt and walk the chain: what was intended, what the judge scored it, which gate
stopped it, what the venue said. Blocks stop looking like outages and start looking like the
system working.

![Attempt journal with per-attempt decision chain](docs/screenshots/journal-detail-1280.png)

### AI calibration

A calibration view plots the judge's stated probability against realized win rate, bucket by
bucket. The buckets come from the attempt journal itself: attempts that reached a venue are
grouped into tenths of a point of stated probability, and every bucket reports its n beside its
win rate. Attempts the journal recorded without a score are left out and counted, so a missing
score never reads as a confident zero. Gate refusals stay out of the buckets too — an attempt the
judge or a cap stopped never had the chance to fill, and counting it as a loss would flatter the
low end of the curve. Minimum-sample floors mean thin buckets can't quietly promote a model that
only looks confident. This is the part you can't get from a P&L chart.

![Calibration: stated probability against realized win rate, bucket by bucket](docs/screenshots/calibration-1280.png)

### Risk visibility

Rolling 24h exposure against daily and per-order caps, drawdown versus limit, cooldown, and every
pre-trade check visible with the value it compared.

### Deployment

Go `net/http` and server-rendered templ, with HTMX swapping panels in place. No SPA, no client
router, no `node_modules`, no CDN dependency at runtime. One static binary that starts instantly
and serves HTML.

### Mobile review

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

To render a real attempt journal instead of the sample ledger, point the console at the JSONL file
the trading server writes:

```sh
go run ./cmd/tickerlight --journal /path/to/journal.jsonl
```

The file is read-only to this binary and a torn final line (a crash mid-write) is skipped rather
than failing the read. Without the flag the console serves the sample ledger exactly as before; a
path that cannot be read is a startup error, never a silent fallback to sample trades.

## Current status

Tickerlight currently runs on a **local sample ledger**, or on a journal file written by the
paper-only trading server (`--journal`); either way the outcomes it shows are paper trades. There is
no live broker, market-data feed, or model provider connected, and the binary has no order path. The
console is read-only; arming and kill-switch controls render disabled and are inert server-side.

That's deliberate: the review surfaces are the product, and they're validated against a fixed
ledger so the numbers are reproducible before any live wiring exists.

## Documentation

- [Development guide](docs/development.md) — build, test, dev loop, project layout
- [Editor setup](docs/dev/lsp-setup.md) — templ LSP and gopls

## License

Apache License 2.0 — see [LICENSE](LICENSE). Copyright 2026 Joel Makori (jomakori).
