# Tickerlight UI Spec: trading-mcp Ops Dashboard

Source mock: `/opt/data/opendesign-grabs/trading-mcp-ops-dashboard/index.html` (856 lines, single file). Line refs below point to that file.

## 1. Goal

Build Tickerlight's ops console to match the mock: a read-first dashboard over Alpaca-MCP paper-trading activity. It shows arming and kill state, risk caps, a gate-event journal with per-attempt stage chains, and judge calibration. v1 ships no live order-entry controls. Paper only, labelled as sample data.

## 2. Reference inventory

- Shell: "trading-mcp" and "ops" label (L299). Side nav with 4 tabs: Overview (L301), Journal (L302), Calibration (L303), Risk state (L304).
- Top bar: "Sample data · paper only" chip (L312). Status chips "Arming flag", "Kill switch", "Caps · 24h" (L479-481).
- Overview: "Cumulative realized P&L" card with All/24h/Today pills (L508-511). Equity chart legend (L514-519). Mini-stats "Win rate", "Attempts in range", "Fees", "Max drawdown" (L521-524). "Caps · rolling 24h" meter (L529-536). "Kill switch" card (L538-543). "Why nothing happened" tiles: Filled, Blocked by cap · kill, Judge-rejected, Venue rejected (L549-552). "Recent gate events" (L556-572).
- Journal: outcome chips All/Filled/All blocked/Cap/Kill/Judge/Venue rejected (L698). Venue and Symbol selects, From/To dates, Clear filters (L699-704). Ten columns: Time (UTC) through Realized P&L (L695). Detail chain: 1 Intent, 2 Judge + gate, 3 Dispatch, 4 Result (L730-740).
- Calibration: "Judge calibration · live-arming evidence" headline, scatter plot, table with statuses SUFFICIENT / INSUFFICIENT · n<20 / NO DATA (L748-762).
- Risk state: Arm/Disarm card with "Held in memory only" note (L771-774). Engage/Release kill switch (L775-778). Order limits: per-order cap, cooldown, judge threshold (L779-782). Rolling 24h meter (L783-786).
- Confirm dialogs for arm, kill, release (L799, L830-832).
- Breakpoints 1440, 1180, 900 (bottom tab bar), 390/720 (L256-293).

## 3. Coverage table

| Reference area | Status | Evidence (index.html) |
|---|---|---|
| Nav tabs (4) | present in mock | L300-305 |
| Status chips (arm/kill/caps) | present in mock | L310-317, L476-483 |
| Overview equity chart | present in mock | L505-526, L574-639 |
| Why-nothing-happened tiles | present in mock | L546-554, L486-492 |
| Recent gate events | present in mock | L556-572 |
| Journal filters + table | present in mock | L663-710 |
| Attempt stage chain | present in mock | L711-745 |
| Calibration plot + table | present in mock | L747-763 |
| Risk controls + dialogs | present in mock | L765-788, L799-832 |
| Empty / before-first-fill | present in mock | L389-395, L578, L692, L759, L565 |
| Live order-entry controls | absent (no buy/sell form) | n/a |
| Backend / API wiring | absent (inline JS consts, no fetch/XHR) | L340-368 |

## 4. Components and data contract

All fields are mock placeholders (inline JS, L340-368). None are wired to a live API.

| Field | Shown as | Live source needed |
|---|---|---|
| id, ts, venue, symbol, side, qty, px | journal row, stage 1 | Alpaca MCP order/activity feed |
| outcome (filled/cap/kill/judge/venue) | badge, tile | gate decision per intent |
| judge p | Judge p column, calibration | judge model output |
| realized P&L, fees | P&L column, mini-stats | fill/settlement data |
| notional, capBefore, capAfter | derived client-side (L371-382) | rolling-24h ledger |
| dispatchId | stage 3 | venue dispatch id |
| LIMITS (perOrder 500, daily 3500, drawdown 100, judgeMin 0.60, minN 20) | thresholds | risk-policy config |
| armed / kill flags | chips, risk panel | session arming state |
| cooldown | mm:ss timer | cooldown gate state |
| CAL_SAMPLE buckets | calibration | aggregated calibration stats |
| sample/empty mode toggle | dev scaffolding | cut from shipped UI (ASSUMED) |

## 5. Touch / mobile parity

Mock shows: bottom tab bar replaces side nav at 900px and below (L270-279). Cards stack to one column at 1180px and below (L257-269). Detail panel becomes a bottom sheet (L266). Chart uses `touch-action: pan-y` (L142). Tab buttons have 44px min height (L57, L94). Reduced-motion respected (L293).

Unknown (ASSUMED, not verifiable from static HTML): chart crosshair on touch (pointermove, L636-637), scroll performance, safe-area insets, landscape layout.

## 6. Conditions and gates

- Data path: Alpaca MCP server, per the REA-vs-MCP decision doc (ASSUMED for Tickerlight).
- Paper is the only mode this spec covers. Live trading is out of scope.
- Arming flag never persists as true across restart (VERIFIED, L773).
- Arm and kill actions show consequence text before acting (VERIFIED dialogs, L799 pattern).
- Any ticket that adds real control actions needs owner approval before it touches a real venue.

## 7. Tickets

**T1: Static shell, nav, chips (S).** Shell, 4 tabs, status chips, session panel, breakpoints per §5. Acceptance: all 4 tabs render; bottom tab bar at ≤900px; chips show placeholder values. Depends on: none.

**T2: Overview with mock data (M).** Equity chart, legend, mini-stats, 4 outcome tiles, recent gate events, fed by a local mock array in the mock's RAW shape. Acceptance: clicking a tile opens Journal pre-filtered to that outcome (as in L820). Depends on: T1.

**T3: Journal table and filters (M).** Outcome chips, venue/symbol select, date range, sortable 10-column table. Acceptance: filters reduce rows and "Showing X of Y" updates; Clear filters resets state. Depends on: T2.

**T4: Attempt detail, 4-stage chain (M).** Row click opens the Intent / Judge+gate / Dispatch / Result detail. Acceptance: each outcome type (filled, cap, kill, judge, venue) stops at the correct stage with the correct reason text. Depends on: T3.

**T5: Calibration panel (S).** Bucket table and scatter plot with insufficient-n hatching. Acceptance: buckets below minN show INSUFFICIENT; sufficient buckets form a connected line. Depends on: T2.

**T6: Risk state, read-only (S).** Order limits and rolling-24h meter from config. No action buttons. Acceptance: values render from config; meter reflects mock usage. Depends on: T1.

**T7: Risk state, arm and kill controls (M). GATED.** Arm/Disarm and Engage/Release with confirm dialogs. Requires explicit owner approval before enabling anywhere that can reach a real venue. Acceptance: actions fire only after confirm; arming flag does not persist across reload. Depends on: T6.

**T8: Live Alpaca-MCP data wiring (L).** Replace every §4 placeholder with paper-account reads from the Alpaca MCP server. Acceptance: Journal, Overview, and Calibration match MCP responses field-for-field; no live-trading endpoint is called. Depends on: T2-T6.

**T9: Empty states (S).** Empty-state copy and layouts for Overview, Journal, Calibration. Acceptance: zero rows render the empty message with no crash and no "undefined". Depends on: T8.

## 8. Risks

- **Live order controls.** The mock has no order-entry UI. Any ticket that adds order placement is a new surface that needs separate owner approval, defaults to paper, and shows a non-dismissible paper/live indicator.
- **Prompt injection via displayed text.** Venue and judge reason strings are HTML-escaped (`esc()`, L400). Any text passed to an LLM must still be treated as untrusted data.
- **Paper vs live indicator.** The mock hardcodes paper-only labels (L6, L312, L482) and has no live path. T8+ keeps an always-visible indicator and never defaults to live.
- **Config drift.** LIMITS are hardcoded in the mock. A live policy source needs a staleness indicator.

## 9. Provenance and caveats

- Structure, labels, line refs (§2-§5): VERIFIED (full file read).
- No fetch, XHR, or network call in the file: VERIFIED (grep for fetch/XHR/http returned none; the only hits are RAW data rows).
- Arming non-persistence (L773): VERIFIED.
- Alpaca MCP as data path, paper default, owner-approval gating: ASSUMED (from the decision doc, not in the mock).
- Touch and device behavior: ASSUMED (no device testing).
- Mode toggle is dev-only: ASSUMED.
- Ticket sizes and dependencies: ASSUMED (author estimate).
