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

/* ---------- OVERVIEW (T2 — not yet implemented in this commit) ---------- */
function overview() { return `<div class=\"empty\">${icon('hourglass', 24)}<div><strong>Overview not yet implemented</strong>Lands in a follow-up commit (T2).</div></div>`; }
function drawCharts() {}
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
