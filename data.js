'use strict';
/* Mock data only (T2/T3 data contract). No fetch/XHR — T8 live wiring is explicitly not done. */

// Risk policy thresholds shown read-only in Overview/Risk (T6). Mock values, not live config.
const LIMITS = { perOrder: 500, daily: 3500, drawdown: 100, judgeMin: 0.60, minN: 20 };
const NOW = Date.parse('2026-10-30T14:00:00Z');

// Outcome taxonomy: kind ok=fill, blk=benign block (expected), bad=fault. stop = chain stage index it halts at.
const OUT = {
  filled: { label: 'FILLED',          kind: 'ok',  icon: 'check', stop: -1 },
  cap:    { label: 'BLOCKED · CAP',   kind: 'blk', icon: 'slash', stop: 2 },
  kill:   { label: 'BLOCKED · KILL',  kind: 'blk', icon: 'slash', stop: 2 },
  judge:  { label: 'BLOCKED · JUDGE', kind: 'blk', icon: 'slash', stop: 1 },
  venue:  { label: 'VENUE REJECTED',  kind: 'bad', icon: 'bang',  stop: 3 }
};
const STAGES = ['intent', 'judge', 'gate', 'venue'];

// Sample attempt rows: [id, ts, venue, symbol, side, qty, px, outcome, judge p, realized pnl, fees, reason detail]
const RAW = [
  [1, '2026-10-29T09:12:04Z', 'alpaca-paper', 'AAPL', 'buy', 2, 225.40, 'filled', .64, 12.40, .10],
  [2, '2026-10-29T10:41:31Z', 'alpaca-paper', 'MSFT', 'buy', 1, 410.20, 'filled', .71, -8.15, .10],
  [3, '2026-10-29T13:05:12Z', 'binance-testnet', 'BTCUSDT', 'buy', .02, 68000, 'cap', .77, 0, 0],
  [4, '2026-10-29T15:30:47Z', 'alpaca-paper', 'NVDA', 'sell', 3, 132.50, 'filled', .82, 21.60, .10],
  [5, '2026-10-29T17:02:20Z', 'binance-testnet', 'ETHUSDT', 'buy', .15, 2600, 'filled', .68, -14.30, .39],
  [6, '2026-10-30T08:20:09Z', 'alpaca-paper', 'TSLA', 'buy', 3, 245.00, 'cap', .66, 0, 0],
  [7, '2026-10-30T09:03:55Z', 'alpaca-paper', 'AAPL', 'buy', 2, 226.10, 'filled', .73, 9.80, .10],
  [8, '2026-10-30T09:45:18Z', 'binance-testnet', 'SOLUSDT', 'buy', 4, 170.00, 'cap', .74, 0, 0],
  [9, '2026-10-30T10:10:40Z', 'alpaca-paper', 'MSFT', 'sell', 1, 411.00, 'filled', .86, 17.25, .10],
  [10, '2026-10-30T10:58:02Z', 'binance-testnet', 'ETHUSDT', 'sell', .1, 2610, 'venue', .75, 0, 0, 'insufficient testnet balance (code 40110)'],
  [11, '2026-10-30T11:30:26Z', 'alpaca-paper', 'NVDA', 'buy', 3, 133.00, 'filled', .91, -11.40, .10],
  [12, '2026-10-30T12:15:33Z', 'alpaca-paper', 'AAPL', 'buy', 2, 226.50, 'kill', .70, 0, 0, 'engaged 12:14:02Z–12:19:48Z'],
  [13, '2026-10-30T13:20:15Z', 'alpaca-paper', 'MSFT', 'buy', 1, 412.00, 'filled', .67, 5.10, .10]
];

// Calibration buckets: [label, n, wins]. Used by T5.
const CAL_SAMPLE = [['0.50–0.59', 14, 7], ['0.60–0.69', 31, 18], ['0.70–0.79', 27, 19], ['0.80–0.89', 22, 17], ['0.90+', 9, 8]];
const CAL_EMPTY = CAL_SAMPLE.map(b => [b[0], 0, 0]);

// Expand RAW rows into full attempt objects with derived cap/dispatch fields (T2 data contract).
function derive(raw) {
  const rows = raw.map(r => ({
    id: r[0], ts: r[1], t: Date.parse(r[1]), venue: r[2], symbol: r[3], side: r[4], qty: r[5], px: r[6],
    outcome: r[7], p: r[8], pnl: r[9], fees: r[10], detail: r[11] || '', notional: r[5] * r[6],
    dispatched: r[7] === 'filled' || r[7] === 'venue'
  }));
  rows.forEach(r => {
    r.capBefore = rows.filter(o => o.dispatched && o.t < r.t && r.t - o.t <= 864e5).reduce((a, o) => a + o.notional, 0);
    r.capAfter = r.capBefore + (r.dispatched ? r.notional : 0);
    r.armed = true;
    r.dispatchId = r.dispatched ? 'dsp_' + String(r.id).padStart(4, '0') + '_' + (r.t % 9973).toString(16) : null;
    r.fill = r.outcome === 'filled' ? r.px : null;
  });
  return rows;
}

// Empty-rows fixture for T9 empty-state verification (no attempts recorded yet).
const RAW_EMPTY = [];
