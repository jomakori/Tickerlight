<div align="center">
  <h1>Tickerlight</h1>
  <p><em>Read-only ops console over sample paper-trading activity — Go + templ + HTMX, one binary.</em></p>
</div>

> Private repo — no CI workflow or LICENSE file yet, so no badges are shown (shields.io can't resolve either without one).

## Quick Links
- [Features](#features) · [Quickstart](#quickstart) · [Documentation](#documentation) · [License](#license)

## Demo
![overview](docs/screenshots/overview-1280.png)

## What is it?
Tickerlight renders a trading-ops dashboard server-side from an in-repo sample ledger — no client build step, no outbound network calls, no real trading venue. Overview, Journal, Calibration, and Risk views are read-only; arm/kill controls render but are disabled and inert.

## Features
- Go `net/http` server rendering `templ` components, HTMX for panel swaps
- Zero runtime network calls — fixture ledger only (`internal/tickerdata`)
- Paper-only banner always visible; arm/kill controls shipped disabled
- `pre-commit` gate: secret scan, `templ generate` sync check, `gofmt`

## Quickstart
```sh
go install github.com/a-h/templ/cmd/templ@v0.3.1100
templ generate
go run ./cmd/tickerlight
# open http://localhost:8080
```

## Documentation
- [Development guide](docs/development.md) — build/test, dev loop, layout, scope, repo hygiene
- [UI spec](docs/design/ui-spec.md) — behaviour and layout contract
- [templ + gopls LSP setup](docs/dev/lsp-setup.md)
- [Original OpenDesign mock](docs/design/mock-trading-mcp-ops-dashboard.html)

## License
Private repository — no LICENSE file yet.
