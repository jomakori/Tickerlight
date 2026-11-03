'use strict';
/* Render logic. No fetch/XHR anywhere — T8 (live Alpaca data) intentionally not wired. */

// T7 GATE: arm/kill controls shipped disabled; flip only with owner approval + real wiring.
const FEATURE_T7_ENABLED = false;

// In-memory only; never written to localStorage/sessionStorage, so it can't survive reload (spec: arming flag must not persist).
const state = {
  tab: 'overview', rows: derive(RAW), armed: true, kill: false, cooldown: 720, range: 'all',
  f: { outcome: 'all', venue: 'all', symbol: 'all', from: '', to: '' }, sel: null
};

/* ---------- small helpers (T1-T6 shared) ---------- */
const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => Array.from(el.querySelectorAll(s));
// All displayed/untrusted text goes through esc() before reaching innerHTML (spec §8 risk: prompt-injection-safe escaping).
const esc = s => String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const ICONS = {
  check: '<path d="M5 12.5l4.5 4.5L19 7.5"/>',
  slash: '<circle cx="12" cy="12" r="8.5"/><path d="M6 6l12 12"/>',
  bang: '<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5v5.5M12 16.5v.01"/>',
  hourglass: '<path d="M6 3h12M6 21h12M7 3c0 5 5 6 5 9s-5 4-5 9M17 3c0 5-5 6-5 9s5 4 5 9"/>',
  dot: '<circle cx="12" cy="12" r="5" fill="currentColor"/>',
  ring: '<circle cx="12" cy="12" r="5"/>',
  minus: '<path d="M6 12h12"/>',
  x: '<path d="M6 6l12 12M18 6L6 18"/>',
  up: '<path d="M12 5l7 8H5z" fill="currentColor"/>',
  down: '<path d="M12 19l-7-8h14z" fill="currentColor"/>',
  lock: '<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>'
};
const icon = (n, size) => `<svg class="od-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"${size ? ` style="--od-icon-size:${size}px"` : ''}>${ICONS[n]}</svg>`;
const usd = (n, signed) => { const a = Math.abs(n).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 }); return (signed ? (n < 0 ? '−' : '+') : (n < 0 ? '−' : '')) + '$' + a; };
const pnlHtml = n => n === 0 ? `<span class="nd">${usd(0)}</span>` : n > 0 ? `<span class="pos nw"><span aria-hidden="true">▲</span> ${usd(n, true)}</span>` : `<span class="nw"><span aria-hidden="true">▼</span> ${usd(n, true)}</span>`;
const qtyFmt = n => n >= 1 ? String(n) : n.toFixed(2);
const pxFmt = n => n.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
const hms = t => new Date(t).toISOString().slice(11, 19) + 'Z';
const md = t => new Date(t).toISOString().slice(5, 10);
const pct = (n, d = 1) => (n * 100).toFixed(d) + '%';
const venueName = v => v.split('-')[0].toUpperCase();
const pad4 = n => String(n).padStart(4, '0');
const badge = o => `<span class="bd ${OUT[o].kind}">${icon(OUT[o].icon, 12)}${esc(OUT[o].label)}</span>`;

// Human reason string per outcome type, used in events list and detail chain (T4).
function reasonText(r) {
  const n = usd(r.notional);
  if (r.outcome === 'cap') return r.notional > LIMITS.perOrder ? `Blocked by cap — ${n} exceeds the per-order cap of ${usd(LIMITS.perOrder)}` : `Blocked by cap — ${n} would exceed the rolling-24h cap of ${usd(LIMITS.daily)}`;
  if (r.outcome === 'kill') return `Blocked by kill switch — ${r.detail}`;
  if (r.outcome === 'judge') return `Blocked by judge — p=${r.p.toFixed(2)} is below the ${LIMITS.judgeMin.toFixed(2)} threshold`;
  if (r.outcome === 'venue') return `Venue rejected — ${venueName(r.venue)}: ${r.detail}`;
  return 'Filled';
}
function stageOf(r) { const s = OUT[r.outcome].stop; return s < 0 ? null : STAGES[s]; }
// Renders the 4-segment stage spine shown in Journal rows and the detail header (T3/T4).
function spine(r) {
  const o = OUT[r.outcome], seg = STAGES.map((nm, i) => {
    let c, st;
    if (o.stop < 0) { c = 'pf'; st = 'passed'; }
    else if (i < o.stop) { c = 'p'; st = 'passed'; }
    else if (i === o.stop) { c = o.kind === 'bad' ? 'xr' : 'xb'; st = o.kind === 'bad' ? 'failed' : 'blocked'; }
    else { c = ''; st = 'not reached'; }
    return [`<i class="sg ${c}"></i>`, `${nm} ${st}`];
  });
  return `<span class="spine" role="img" aria-label="${esc(seg.map(s => s[1]).join(', '))}" title="${esc(seg.map(s => s[1]).join(' · '))}">${seg.map(s => s[0]).join('')}</span>`;
}
function stats(rows) {
  const c = { filled: 0, cap: 0, kill: 0, judge: 0, venue: 0 };
  rows.forEach(r => c[r.outcome]++);
  const filled = rows.filter(r => r.outcome === 'filled');
  const wins = filled.filter(r => r.pnl > 0).length;
  let eq = 0, peak = 0, maxdd = 0;
  filled.forEach(r => { eq += r.pnl; peak = Math.max(peak, eq); maxdd = Math.max(maxdd, peak - eq); });
  const fees = filled.reduce((a, r) => a + r.fees, 0);
  return { c, filled: c.filled, wins, eq, dd: peak - eq, maxdd, fees, total: rows.length };
}
function rollingUsed() { return state.rows.filter(r => r.dispatched && NOW - r.t <= 864e5).reduce((a, r) => a + r.notional, 0); }
function inRange(rows) {
  if (state.range === '24h') return rows.filter(r => NOW - r.t <= 864e5);
  if (state.range === 'today') return rows.filter(r => r.ts.slice(0, 10) === '2026-10-30');
  return rows;
}
const ticksHtml = (p, n = 36, warn = .7) => `<div class="ticks" aria-hidden="true">${Array.from({ length: n }, (_, i) => { const f = (i + 1) / n; return `<i class="${f <= p + 1e-9 ? (f > warn ? 'warn' : 'on') : ''}"></i>`; }).join('')}</div>`;

/* ---------- chrome: status chips + session panel (T1) ---------- */
function armWord(size = 15) {
  if (state.kill) return `<span class="gl" style="color:var(--red);font-size:${size}px;font-weight:600">${icon('bang', size)}HALTED</span>`;
  return state.armed
    ? `<span class="gl" style="font-size:${size}px;font-weight:600">${icon('dot', size)}ARMED</span>`
    : `<span class="gl tx2" style="font-size:${size}px;font-weight:600">${icon('ring', size)}DISARMED</span>`;
}
function killStatus(size = 20) {
  return state.kill
    ? `<span class="gl" style="color:var(--red);font-size:${size}px;font-weight:700">${icon('bang', size)}ENGAGED</span>`
    : `<span class="gl" style="font-size:${size}px;font-weight:700">${icon('check', size)}RELEASED</span>`;
}
function renderChrome() {
  const used = rollingUsed(), p = used / LIMITS.daily;
  $('#chips').innerHTML = `
    <div class="chip-k"><span class="lbl">Arming flag</span><b>${armWord(15)}</b></div>
    <div class="chip-k"><span class="lbl">Kill switch</span><b>${killStatus(15)}</b></div>
    <div class="chip-k"><span class="lbl">Caps · 24h</span><b class="nw">${pct(p, 0)} <span class="den" style="font-size:13px">of ${usd(LIMITS.daily)}</span></b></div>`;
  $('#sess').innerHTML = `<span class="lbl">Session</span><div>${armWord(14)}</div><div class="tx2">Paper account<br>Sample data · no real funds</div>`;
}

/* ---------- OVERVIEW (T2) ---------- */
function tile(kind, glyph, label, count, total, cap, goto, zero) {
  const w = total ? count / total * 100 : 0;
  return `<button type="button" class="tile ${kind}${zero ? ' zero' : ''}" data-outcome-go="${esc(goto)}" aria-label="${esc(label)}: ${count}. Open in journal">
    <span class="t-top">${icon(glyph, 14)}${esc(label)}</span>
    <span class="num n36">${count}</span>
    <span><span class="share"><i style="width:${w.toFixed(1)}%"></i></span><span class="tx2" style="display:block;margin-top:8px;color:inherit;opacity:.85">${esc(cap)}</span></span></button>`;
}
function overview() {
  const all = state.rows, A = stats(all), n = A.total, empty = !n;
  // T9: zero-row rendering goes through the same code paths below (empty flags), no separate branch to drift.
  const sub = inRange(all), S = stats(sub);
  const c = A.c, blockedN = c.cap + c.kill, nonFill = n - c.filled;
  const used = rollingUsed(), cp = used / LIMITS.daily, ddp = A.dd / LIMITS.drawdown;
  const vr = all.find(r => r.outcome === 'venue');
  const rangeLbl = { all: 'All time', '24h': 'Last 24h', today: 'Today' }[state.range];
  const thesis = empty
    ? 'Waiting for the first attempt. The system is idle, not broken.'
    : `${nonFill} of ${n} attempts did not fill. ${blockedN + c.judge} were blocked on purpose by a gate (expected) and ${c.venue} failed at the venue (a fault).`;
  const wr = S.filled ? S.wins / S.filled : null;
  const delta = !S.filled ? '' : `<span class="delta ${S.eq < 0 ? 'neu' : ''}">${S.eq < 0 ? icon('down', 12) : icon('up', 12)}${S.filled} fills · ${S.wins} won</span>`;
  return `<div class="grid">
    <section class="card glow c8" aria-label="Cumulative P&amp;L">
      <div class="row" style="align-items:flex-start;flex-wrap:wrap">
        <div><h2>Cumulative realized P&amp;L · paper · ${esc(rangeLbl)}</h2>
          <div class="base" style="margin-top:12px"><span class="num n56">${!S.filled ? '<span class="nd">—</span>' : usd(S.eq, true)}</span>${delta}</div></div>
        <div class="pills" role="group" aria-label="Time range">
          ${['all', '24h', 'today'].map(k => `<button type="button" data-range="${k}" aria-pressed="${state.range === k}">${{ all: 'All', '24h': '24h', today: 'Today' }[k]}</button>`).join('')}</div>
      </div>
      <div class="chart-box" data-chart></div>
      <div class="keys" aria-label="Chart key">
        <span style="color:var(--lime)">${icon('check', 14)}<span class="tx2">filled · solid column</span></span>
        <span style="color:var(--amb)">${icon('slash', 14)}<span class="tx2">blocked on purpose · dashed column</span></span>
        <span style="color:var(--red)">${icon('bang', 14)}<span class="tx2">venue fault · red column</span></span>
        <span class="nd">column height = order notional</span>
      </div>
      <div class="mini">
        <div><span class="lbl">Win rate</span><span class="num n20">${wr === null ? '<span class="nd">—</span>' : pct(wr)}</span></div>
        <div><span class="lbl">Attempts in range</span><span class="num n20">${S.total}</span></div>
        <div><span class="lbl">Fees</span><span class="num n20">${usd(S.fees)}</span></div>
        <div><span class="lbl">Max drawdown</span><span class="num n20">${S.filled ? usd(S.maxdd) : '<span class="nd">—</span>'}</span></div>
      </div>
    </section>

    <div class="c4 stack">
      <section class="card" aria-label="Risk gates">
        <div class="row"><h2>Caps · rolling 24h</h2><span class="pill">${pct(cp, 0)}</span></div>
        <div class="base" style="margin:12px 0"><span class="num n28 nw">${usd(used)}</span><span class="den nw">/ ${usd(LIMITS.daily)}</span></div>
        <div role="meter" aria-label="Rolling 24 hour cap consumption" aria-valuemin="0" aria-valuemax="${LIMITS.daily}" aria-valuenow="${used.toFixed(2)}">${ticksHtml(cp)}</div>
        <div class="tx2" style="margin-top:8px">${pct(cp, 0)} used${cp >= .7 ? ' · approaching the cap' : ''} · ${usd(LIMITS.perOrder)} per order</div>
        <div class="gate-row" style="margin-top:16px"><div class="row"><span class="lbl">Drawdown vs hard limit</span><span class="nw"><b>${empty ? '—' : usd(A.dd)}</b> <span class="nd">/ ${usd(LIMITS.drawdown)}</span></span></div>
          <div role="meter" aria-label="Current drawdown" aria-valuemin="0" aria-valuemax="${LIMITS.drawdown}" aria-valuenow="${A.dd.toFixed(2)}">${ticksHtml(ddp)}</div></div>
        <div class="gate-row"><div class="row"><span class="lbl">Cooldown</span><span class="n20 num cdv">${cdText()}</span></div></div>
      </section>
      <section class="card" aria-label="Kill switch">
        <div class="row"><h2>Kill switch</h2>${killStatus(16)}</div>
        <p class="tx2" style="margin:12px 0 16px">${state.kill ? 'Every new intent is being recorded as blocked by kill. Release it to let intents reach the gates again.' : 'Engaging it stops all new dispatch. Intents are still recorded, as blocked by kill.'}</p>
        <button type="button" class="btn sm" data-goto="risk">Open risk controls</button>
      </section>
    </div>

    <section class="c12" aria-label="Why nothing happened">
      <div class="row" style="flex-wrap:wrap;align-items:flex-end"><div><h2>Why nothing happened · all ${n} recorded attempts</h2><p class="tx2" style="margin:8px 0 0;max-width:70ch">${esc(thesis)}</p></div></div>
      <div class="tiles">
        ${tile('ok', 'check', 'Filled', c.filled, n, `${A.wins} won · ${c.filled - A.wins} lost`, 'filled', !c.filled)}
        ${tile('blk', 'slash', 'Blocked by cap · kill', blockedN, n, `cap ${c.cap} · kill ${c.kill} — expected`, 'blocked', !blockedN)}
        ${tile('blk', 'slash', 'Judge-rejected', c.judge, n, 'judge p below threshold', 'judge', !c.judge)}
        ${tile('bad', 'bang', 'Venue rejected' + (vr ? ' · ' + venueName(vr.venue) : ''), c.venue, n, vr ? esc(vr.detail) : 'no venue faults', 'venue', !c.venue)}
      </div>
    </section>

    <section class="card c12" aria-label="Recent gate events">
      <h2>Recent gate events</h2>
      ${eventsHtml()}
    </section>
  </div>`;
}
function eventsHtml() {
  const ev = state.rows.filter(r => r.outcome !== 'filled').sort((a, b) => b.t - a.t).slice(0, 5);
  if (!ev.length) return `<div class="empty" style="margin-top:12px">${icon('hourglass', 20)}<div><strong>No gate events yet</strong>Blocked and rejected attempts will be listed here as they happen.</div></div>`;
  return `<div class="events" role="list">${ev.map(r => {
    const o = OUT[r.outcome], st = stageOf(r);
    return `<button type="button" class="ev ${o.kind === 'bad' ? 'bad' : ''}" role="listitem" data-open="${r.id}" aria-label="Open attempt ${r.id}: ${esc(reasonText(r))}">
      <span class="ts">${md(r.t)} ${hms(r.t)}</span><span class="why">${esc(reasonText(r))}</span>
      <span class="bd ${o.kind}">${icon(o.icon, 12)}${esc(String(st).toUpperCase())}</span></button>`;
  }).join('')}</div>`;
}

/* ---------- charts (T2 equity chart + T5 calibration scatter) ---------- */
function chartBuild(W) {
  const rows = inRange(state.rows), H = 320, L = 56, R = 24, T = 16, B = 36, bot = H - B;
  const mono = 'font-family="ui-monospace,Menlo,monospace"';
  if (!rows.length) return { html: `<div class="empty">${icon('hourglass', 24)}<div><strong>${state.rows.length ? 'No attempts in this range' : 'Waiting for first fill'}</strong>${state.rows.length ? 'Pick a wider range to see the tape.' : 'The curve starts at $0.00 when the first order fills, and every gate that fires is marked on it. The journal is empty — nothing is broken.'}</div></div>` };
  const tMin = rows[0].t, tMax = rows[rows.length - 1].t;
  const x = t => L + (t - tMin) / ((tMax - tMin) || 1) * (W - L - R - 24);
  let cum = 0; const pts = rows.map(r => { if (r.outcome === 'filled') cum += r.pnl; return { r, x: x(r.t), v: cum, kind: OUT[r.outcome].kind }; });
  const vals = pts.map(p => p.v);
  let lo = Math.min(0, ...vals), hi = Math.max(10, ...vals);
  const step = Math.max(10, Math.ceil((hi - lo) / 4 / 10) * 10);
  lo = Math.floor(lo / step) * step; hi = lo + step * 4; if (hi < Math.max(...vals)) hi += step;
  const y = v => T + (bot - T) * (1 - (v - lo) / (hi - lo));
  let g = '';
  for (let v = lo; v <= hi + 1e-9; v += step) {
    g += `<line x1="${L}" x2="${W - R}" y1="${y(v)}" y2="${y(v)}" stroke="${v === 0 ? '#ffffff33' : '#ffffff0d'}" ${v === 0 ? 'stroke-dasharray="4 4"' : ''}/><text x="${L - 10}" y="${y(v) + 4}" text-anchor="end" fill="#7F877C" font-size="11" ${mono}>${v === 0 ? '$0' : '$' + v}</text>`;
  }
  const maxN = Math.max(...rows.map(r => r.notional));
  const col = { ok: ['#D2F23C', '.42', ''], blk: ['#E8B33C', '.7', '2 3'], bad: ['#FF5A5F', '.85', ''] };
  let cols = '';
  pts.forEach(p => {
    const [c, op, dash] = col[p.kind], h = (bot - T) * 0.62 * Math.max(.12, p.r.notional / maxN);
    for (let k = -2; k <= 2; k++) cols += `<line x1="${(p.x + k * 3).toFixed(1)}" x2="${(p.x + k * 3).toFixed(1)}" y1="${bot}" y2="${(bot - h).toFixed(1)}" stroke="${c}" stroke-opacity="${op}" stroke-width="1" ${dash ? `stroke-dasharray="${dash}"` : ''}/>`;
  });
  let curve = '', dots = '';
  const line = pts.map((p, i) => (i ? 'L' : 'M') + p.x.toFixed(1) + ' ' + y(p.v).toFixed(1)).join(' ');
  curve = `<defs><linearGradient id="eqfill" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#D2F23C" stop-opacity=".28"/><stop offset="1" stop-color="#D2F23C" stop-opacity="0"/></linearGradient></defs>
    <path d="${line} L${pts[pts.length - 1].x.toFixed(1)} ${bot} L${pts[0].x.toFixed(1)} ${bot}Z" fill="url(#eqfill)"/>
    <path d="${line}" fill="none" stroke="#D2F23C" stroke-width="2" stroke-linejoin="round"/>`;
  pts.forEach(p => {
    const cy = y(p.v).toFixed(1), cx = p.x.toFixed(1);
    if (p.kind === 'blk') dots += `<circle cx="${cx}" cy="${cy}" r="5" fill="#060807" stroke="#E8B33C" stroke-width="1.5" stroke-dasharray="2 2"/>`;
    else if (p.kind === 'bad') dots += `<rect x="${(p.x - 4.5).toFixed(1)}" y="${(y(p.v) - 4.5).toFixed(1)}" width="9" height="9" fill="#FF5A5F" stroke="#060807" stroke-width="1.5"/>`;
    else dots += `<circle cx="${cx}" cy="${cy}" r="3" fill="#D2F23C"/>`;
  });
  let xl = '';
  for (let i = 0; i <= 4; i++) { const t = tMin + (tMax - tMin) * i / 4; xl += `<text x="${(x(t)).toFixed(1)}" y="${H - 10}" text-anchor="${i === 0 ? 'start' : i === 4 ? 'end' : 'middle'}" fill="#7F877C" font-size="11" ${mono}>${md(t)} ${hms(t).slice(0, 5)}</text>`; }
  const html = `<svg viewBox="0 0 ${W} ${H}" width="${W}" height="${H}" role="img" aria-label="Cumulative realized P&amp;L over time with every attempt marked; use the recent gate events list for the details">${g}${cols}${curve}${dots}<g id="xh" pointer-events="none"></g>${xl}<rect x="0" y="0" width="${W}" height="${H}" fill="transparent" data-hit/></svg>`;
  return { html, pts, y, W, T, bot };
}
function setXh(svg, o, i) {
  const p = o.pts[i], r = p.r, g = svg.querySelector('#xh'); if (!g) return;
  const cy = o.y(p.v), left = p.x > o.W * 0.6;
  const l1 = `#${pad4(r.id)} · ${r.symbol} ${r.side.toUpperCase()}`;
  let l2, c2;
  if (p.kind === 'ok') { l2 = `${usd(r.pnl, true)} · total ${usd(p.v, true)}`; c2 = '#D2F23C'; }
  else if (p.kind === 'blk') { l2 = reasonText(r).split(' — ')[0]; c2 = '#E8B33C'; }
  else { l2 = 'Venue rejected'; c2 = '#FF5A5F'; }
  const w = Math.max(l1.length, l2.length) * 6.6 + 28, bx = left ? p.x - 14 - w : p.x + 14, by = Math.max(o.T, cy - 56 > o.T ? cy - 56 : o.T);
  g.innerHTML = `<line x1="${p.x}" x2="${p.x}" y1="${o.T}" y2="${o.bot}" stroke="#EEF1EA" stroke-opacity=".55" stroke-dasharray="3 4"/>
    <circle cx="${p.x}" cy="${cy}" r="7" fill="none" stroke="${c2}" stroke-width="2"/>
    <rect x="${bx.toFixed(1)}" y="${by}" width="${w.toFixed(1)}" height="46" rx="10" fill="#1B201D" stroke="#ffffff2e"/>
    <text x="${(bx + 14).toFixed(1)}" y="${by + 19}" fill="#EEF1EA" font-size="12" font-weight="700" font-family="ui-monospace,Menlo,monospace">${esc(l1)}</text>
    <text x="${(bx + 14).toFixed(1)}" y="${by + 36}" fill="${c2}" font-size="12" font-weight="600" font-family="ui-monospace,Menlo,monospace">${esc(l2)}</text>`;
  svg.dataset.active = i;
}
function attachChart(el, o) {
  const svg = $('svg', el); if (!svg || !o.pts) return;
  let last = o.pts.map(p => p.kind).lastIndexOf('ok'); if (last < 0) last = o.pts.length - 1;
  setXh(svg, o, last);
  const nearest = e => { const b = svg.getBoundingClientRect(), mx = (e.clientX - b.left) * (o.W / b.width); let bi = 0, bd = 1e9; o.pts.forEach((p, i) => { const d = Math.abs(p.x - mx); if (d < bd) { bd = d; bi = i; } }); return bi; };
  svg.addEventListener('pointermove', e => setXh(svg, o, nearest(e)));
  // Click on the chart jumps to Journal pre-filtered to that row, same as a tile/event click (T2 acceptance).
  svg.addEventListener('click', e => { const i = nearest(e); state.sel = o.pts[i].r.id; state.f = { outcome: 'all', venue: 'all', symbol: 'all', from: '', to: '' }; state.tab = 'journal'; render(); window.scrollTo(0, 0); });
}
function calSvg(data, W) {
  const H = Math.round(W * 0.82), L = 44, R = 12, T = 20, B = 40, lo = 0.4, hi = 1.0;
  const x = v => L + (W - L - R) * (v - lo) / (hi - lo), y = v => T + (H - T - B) * (1 - (v - lo) / (hi - lo));
  const mids = [0.55, 0.65, 0.75, 0.85, 0.95], mono = 'font-family="ui-monospace,Menlo,monospace"';
  let g = '';
  for (let i = 0; i <= 6; i++) { const v = 0.4 + i * 0.1;
    g += `<line x1="${L}" x2="${W - R}" y1="${y(v)}" y2="${y(v)}" stroke="#ffffff0d"/><line y1="${T}" y2="${H - B}" x1="${x(v)}" x2="${x(v)}" stroke="#ffffff0d"/><text x="${L - 8}" y="${y(v) + 4}" text-anchor="end" fill="#7F877C" font-size="11" ${mono}>${v.toFixed(1)}</text><text x="${x(v)}" y="${H - B + 16}" text-anchor="middle" fill="#7F877C" font-size="11" ${mono}>${v.toFixed(1)}</text>`; }
  const diag = `<line x1="${x(.4)}" y1="${y(.4)}" x2="${x(1)}" y2="${y(1)}" stroke="#7F877C" stroke-width="1.5" stroke-dasharray="6 5"/><text x="${x(.62)}" y="${y(.62) - 8}" fill="#7F877C" font-size="11" transform="rotate(-39 ${x(.62)} ${y(.62) - 8})">perfectly calibrated</text>`;
  const good = [], marks = [];
  data.forEach((b, i) => {
    if (!b[1]) return;
    const wr = b[2] / b[1], ok = b[1] >= LIMITS.minN, cx = x(mids[i]), cy = y(wr);
    if (ok) { good.push([cx, cy]); marks.push(`<circle cx="${cx}" cy="${cy}" r="7" fill="#D2F23C" stroke="#0D100F" stroke-width="2"><title>${esc(b[0])}: n=${b[1]}, win rate ${pct(wr)}</title></circle>`); }
    else marks.push(`<rect x="${cx - 9}" y="${cy - 9}" width="18" height="18" rx="4" fill="url(#hatch)" stroke="#A9B0A6" stroke-width="1.5" stroke-dasharray="3 2"><title>${esc(b[0])}: n=${b[1]} — insufficient (n&lt;${LIMITS.minN})</title></rect><text x="${Math.min(cx, W - R - 64)}" y="${cy + 28}" text-anchor="middle" fill="#A9B0A6" font-size="11" ${mono}>n=${b[1]} · insufficient</text>`);
  });
  const line = good.length > 1 ? `<path d="${good.map((p, i) => (i ? 'L' : 'M') + p[0] + ' ' + p[1]).join(' ')}" fill="none" stroke="#D2F23C" stroke-width="2"/>` : '';
  return `<svg viewBox="0 0 ${W} ${H}" width="${W}" height="${H}" role="img" aria-label="Calibration plot: realized win rate against stated judge probability"><defs><pattern id="hatch" width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><rect width="6" height="6" fill="#141816"/><line x1="0" y1="0" x2="0" y2="6" stroke="#A9B0A6" stroke-width="2"/></pattern></defs>${g}${diag}${line}${marks.join('')}<text x="${(L + W - R) / 2}" y="${H - 4}" text-anchor="middle" fill="#7F877C" font-size="11">Stated judge probability (bucket midpoint)</text><text transform="translate(12 ${(T + H - B) / 2}) rotate(-90)" text-anchor="middle" fill="#7F877C" font-size="11">Realized win rate</text></svg>`;
}
function drawCharts() {
  $$('[data-chart]').forEach(el => { const o = chartBuild(Math.max(300, Math.floor(el.clientWidth))); el.innerHTML = o.html; attachChart(el, o); });
  $$('[data-cal]').forEach(el => { el.innerHTML = calSvg(CAL_SAMPLE, Math.max(300, Math.min(600, Math.floor(el.clientWidth)))); });
}

/* ---------- JOURNAL (T3/T4 — not yet implemented in this commit) ---------- */
function filtered() { return []; }
function journal() { return `<div class=\"empty\">${icon('hourglass', 24)}<div><strong>Journal not yet implemented</strong>Lands in a follow-up commit (T3/T4).</div></div>`; }
function detail(r) { return ''; }
/* ---------- CALIBRATION (T5 — not yet implemented in this commit) ---------- */
function calibration() { return `<div class=\"empty\">${icon('hourglass', 24)}<div><strong>Calibration not yet implemented</strong>Lands in a follow-up commit (T5).</div></div>`; }

/* ---------- RISK (T6/T7 — not yet implemented in this commit) ---------- */
function fmtCd(s) { return String(Math.floor(s / 60)).padStart(2, '0') + ':' + String(s % 60).padStart(2, '0'); }
function cdText() { return state.cooldown > 0 ? fmtCd(state.cooldown) : '<span class=\"pos gl\">' + icon('check', 16) + 'READY</span>'; }
function risk() { return `<div class=\"empty\">${icon('hourglass', 24)}<div><strong>Risk state not yet implemented</strong>Lands in a follow-up commit (T6, with T7 gated).</div></div>`; }
/* ---------- render + wiring (no network listeners anywhere; all state is local) ---------- */
function render() {
  renderChrome();
  $$('[role=tab]').forEach(t => { const on = t.dataset.tab === state.tab; t.setAttribute('aria-selected', String(on)); t.tabIndex = on ? 0 : -1; });
  const map = { overview, journal, calibration, risk };
  Object.keys(map).forEach(k => { const el = $('#p-' + k); el.hidden = k !== state.tab; if (k === state.tab) el.innerHTML = map[k](); });
  drawCharts();
}
function closeDetail() {
  const id = state.sel; state.sel = null; render();
  const b = $(`.rowbtn[data-id="${id}"]`) || $('[data-outcome="all"]'); if (b) b.focus();
}
// Shared navigation used by tile clicks, event-row clicks and chart clicks (T2 acceptance: pre-filtered Journal).
function openAttempt(id) {
  state.sel = id; state.f = { outcome: 'all', venue: 'all', symbol: 'all', from: '', to: '' }; state.tab = 'journal'; render(); window.scrollTo(0, 0);
  const d = $('#detail'); if (d) d.focus({ preventScroll: true });
}
document.addEventListener('click', e => {
  const t = e.target;
  const tab = t.closest('[data-tab]'); if (tab) { state.tab = tab.dataset.tab; render(); return; }
  const go = t.closest('[data-goto]'); if (go) { state.tab = go.dataset.goto; render(); window.scrollTo(0, 0); return; }
  const rg = t.closest('[data-range]'); if (rg) { state.range = rg.dataset.range; render(); const n = $(`[data-range="${state.range}"]`); if (n) n.focus(); return; }
  const og = t.closest('[data-outcome-go]'); if (og) { state.f = { outcome: og.dataset.outcomeGo, venue: 'all', symbol: 'all', from: '', to: '' }; state.sel = null; state.tab = 'journal'; render(); window.scrollTo(0, 0); return; }
  const op = t.closest('[data-open]'); if (op) { openAttempt(+op.dataset.open); return; }
  const oc = t.closest('[data-outcome]'); if (oc) { const k = oc.dataset.outcome; state.f.outcome = k; render(); const n = $(`[data-outcome="${k}"]`); if (n) n.focus(); return; }
  if (t.closest('[data-reset]')) { state.f = { outcome: 'all', venue: 'all', symbol: 'all', from: '', to: '' }; render(); return; }
  if (t.closest('[data-close]')) { closeDetail(); return; }
  const tr = t.closest('tbody tr[data-id]');
  if (tr && state.tab === 'journal') { state.sel = +tr.dataset.id; render(); const d = $('#detail'); if (d) d.focus({ preventScroll: true }); return; }
  // Note: no [data-act] handling here — T7 arm/kill actions are not wired in this build (buttons are disabled).
});
document.addEventListener('change', e => {
  const f = e.target.dataset && e.target.dataset.f; if (!f) return;
  state.f[f] = e.target.value; render(); const n = $(`[data-f="${f}"]`); if (n) n.focus();
});
document.addEventListener('keydown', e => {
  if (e.key === 'Escape' && state.sel && state.tab === 'journal') { closeDetail(); return; }
  const t = e.target.closest && e.target.closest('[role=tab]');
  const fwd = e.key === 'ArrowRight' || e.key === 'ArrowDown', back = e.key === 'ArrowLeft' || e.key === 'ArrowUp';
  if (t && (fwd || back)) {
    e.preventDefault();
    const order = ['overview', 'journal', 'calibration', 'risk'];
    const i = (order.indexOf(t.dataset.tab) + (fwd ? 1 : order.length - 1)) % order.length;
    state.tab = order[i]; render(); $('#tab-' + order[i]).focus();
  }
});
let rz; window.addEventListener('resize', () => { clearTimeout(rz); rz = setTimeout(drawCharts, 120); });
setInterval(() => {
  if (state.cooldown > 0) { state.cooldown--; $$('.cdv').forEach(c => { c.innerHTML = cdText(); }); }
}, 1000);
render();
