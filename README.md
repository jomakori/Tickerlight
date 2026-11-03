# Tickerlight · ops console (v1)

Read-only dashboard over sample paper-trading activity. Static HTML/CSS/JS,
no build step, no framework, no network calls. All data is inline mock data
in `data.js` — this build does not talk to Alpaca or any live venue.

## Run it

Any static file server works, e.g.:

```sh
python3 -m http.server 8000
```

Then open `http://localhost:8000/index.html`. Opening `index.html` directly
via `file://` also works (no fetch/XHR is used).

## Scope (v1)

- T1–T6, T9 implemented: shell/nav, Overview, Journal + filters, attempt
  detail chain, Calibration, read-only Risk state, and empty states.
- T7 (arm/kill controls) is shipped **disabled**: buttons render but have no
  click handler and no backing logic (`FEATURE_T7_ENABLED = false` in
  `app.js`). Needs owner approval before any real wiring.
- T8 (live Alpaca-MCP data) is **not wired**. The UI reads only the local
  mock arrays in `data.js`.
- "Sample data · paper only" is always visible in the header and cannot be
  dismissed.
- The arming flag lives only in an in-memory JS object (`state.armed`); it
  is never written to `localStorage`/`sessionStorage`/cookies, so it cannot
  survive a page reload.

## Files

- `index.html` — page shell, tab panels, dialog markup (unused while T7 is gated).
- `styles.css` — layout primitives + visual design tokens.
- `data.js` — mock attempt rows, calibration buckets, risk limits, derive().
- `app.js` — render functions, event wiring, escaping (`esc()`).
