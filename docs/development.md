# Development

## Build and test

```sh
templ generate                  # regenerate *_templ.go from *.templ
go build -o bin/tickerlight ./cmd/tickerlight
go test ./...
```

## Dev loop

While editing components, run templ's own watcher with proxying so the browser
reloads on save — it regenerates Go, rebuilds the binary and reverse-proxies the
app, so nothing else needs to be running:

```sh
templ generate --watch --proxy="http://127.0.0.1:8080" \
  --cmd="go run ./cmd/tickerlight -addr 127.0.0.1:8080"
```

Without the proxy, `templ generate --watch` in one terminal and
`go run ./cmd/tickerlight` in another is enough; hit refresh by hand.

Generated `*_templ.go` files are committed. Always run `templ generate` and
stage the output in the same change as the `.templ` edit — the pre-commit hook
rejects a mismatch.

## Layout

- `cmd/tickerlight` — entrypoint: flags, listen address, wiring.
- `internal/server` — `http.Handler`: GET `/`, `/journal`, `/calibration`,
  `/risk`, `/static/`, `/healthz`; renders the full document, or just `<main>`
  when the request carries `HX-Request`.
- `internal/tickerdata` — fixture ledger: attempt rows, derived rolling-cap and
  dispatch fields, filters, aggregate stats. No I/O.
- `internal/ui` — `templ` components (shell, Overview, Journal, attempt detail
  chain, Calibration, Risk), view state, formatting helpers, escaping.
- `internal/web` — `go:embed` asset bundle (`static/`: `styles.css`,
  `htmx.min.js`) served under `/static/`.
- `docs/` — UI spec, design mock, verification screenshots, dev setup.

## Scope (v1)

- T1–T6, T9 implemented: shell/nav, Overview, Journal + filters, attempt
  detail chain, Calibration, read-only Risk state, and empty states.
- T7 (arm/kill controls) is shipped **disabled and inert**: the controls
  render, have no click handler, and no backing logic. `ArmKillEnabled` is
  false and `ArmingFlag()` always returns false, and the arming indicator is a
  fixed fixture value — it is never mutated and never persisted. Needs owner
  approval before any real wiring.
- T8 (live Alpaca-MCP data) is **not wired**. The UI reads only the local
  sample ledger.
- "Sample data · paper only" is always visible in the header and cannot be
  dismissed.
- No arming state is written to `localStorage`/`sessionStorage`/cookies; a page
  reload cannot restore it because it never existed outside the request.

## Repo hygiene

```sh
pre-commit install          # once per clone
pre-commit run --all-files  # check the whole tree
```

Hooks: merge-conflict markers, trailing whitespace, oversized files, EOF
newline, gitleaks secret scan, `yamllint`, `gofmt`, and a `templ generate`
sync check. `go build` and `go test` stay in the normal loop and CI rather
than the commit path.
