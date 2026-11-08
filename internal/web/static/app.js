// Tickerlight console — progressive enhancement only.
//
// Everything that matters is server-rendered and works with JavaScript off:
// navigation is plain links, filters are a GET form, and the attempt detail is
// reachable from the journal. This file adds the crosshair readout, the
// cooldown countdown, and the keyboard shortcuts, then stops.
(() => {
  "use strict";

  const $ = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));
  const SVGNS = "http://www.w3.org/2000/svg";

  /* ---------- equity chart crosshair ---------- */
  function svgEl(name, attrs) {
    const el = document.createElementNS(SVGNS, name);
    for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
    return el;
  }

  function drawCrosshair(pt) {
    const svg = pt.ownerSVGElement;
    if (!svg) return;
    const group = svg.querySelector("g.xh");
    if (!group) return;
    group.replaceChildren();

    const x = Number(pt.dataset.x || 0);
    const y = Number(pt.dataset.y || 0);
    const colour = pt.dataset.c || "#D2F23C";
    const box = svg.viewBox.baseVal;
    const flip = x > box.width * 0.66;
    const tx = flip ? x - 12 : x + 12;
    const label1 = pt.dataset.l1 || "";
    const label2 = pt.dataset.l2 || "";

    group.append(
      svgEl("line", {
        x1: x, x2: x, y1: box.y + 8, y2: box.height - 30,
        stroke: colour, "stroke-width": "1", "stroke-dasharray": "3 3", "stroke-opacity": ".7",
      }),
      svgEl("circle", { cx: x, cy: y, r: "6", fill: "none", stroke: colour, "stroke-width": "1.5" }),
    );

    const text = svgEl("text", {
      x: tx, y: Math.max(box.y + 14, y - 22),
      "text-anchor": flip ? "end" : "start",
      fill: "#EEF1EA", "font-size": "12", "font-family": "ui-monospace,Menlo,monospace",
    });
    const t1 = svgEl("tspan", { x: tx, dy: "0" });
    t1.textContent = label1;
    const t2 = svgEl("tspan", { x: tx, dy: "15", fill: colour });
    t2.textContent = label2;
    text.append(t1, t2);
    group.append(text);
  }

  function clearCrosshair(svg) {
    const group = svg && svg.querySelector("g.xh");
    if (group) group.replaceChildren();
  }

  document.addEventListener("pointerover", (e) => {
    const pt = e.target instanceof Element ? e.target.closest(".pt") : null;
    if (pt) drawCrosshair(pt);
  });
  document.addEventListener("pointerout", (e) => {
    const pt = e.target instanceof Element ? e.target.closest(".pt") : null;
    if (pt) clearCrosshair(pt.ownerSVGElement);
  });

  /* ---------- cooldown countdown ---------- */
  const timers = new WeakMap();

  function startCooldown(el) {
    const total = Number(el.dataset.cd || 0);
    if (!Number.isFinite(total) || total <= 0) return;
    const end = Date.now() + total * 1000;
    if (timers.has(el)) clearInterval(timers.get(el));
    const tick = () => {
      const left = Math.max(0, Math.round((end - Date.now()) / 1000));
      const m = String(Math.floor(left / 60)).padStart(2, "0");
      const s = String(left % 60).padStart(2, "0");
      el.textContent = left === 0 ? "ready" : `${m}:${s}`;
      if (left === 0) {
        clearInterval(timers.get(el));
        timers.delete(el);
      }
    };
    tick();
    timers.set(el, setInterval(tick, 1000));
  }

  function initCooldowns(root = document) {
    $$("[data-cd]", root).forEach(startCooldown);
  }

  /* ---------- keyboard shortcuts ---------- */
  document.addEventListener("keydown", (e) => {
    if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return;
    const tag = (e.target instanceof Element && e.target.tagName) || "";
    if (/^(INPUT|SELECT|TEXTAREA)$/.test(tag)) return;

    if (e.key === "Escape") {
      const close = $("#detail-pane a[aria-label='Close attempt detail']");
      if (close) {
        e.preventDefault();
        close.click();
      }
      return;
    }
    if (/^[1-4]$/.test(e.key)) {
      const link = $$(".tabs .tablink")[Number(e.key) - 1];
      if (link) {
        e.preventDefault();
        link.click();
      }
    }
  });

  /* ---------- lifecycle ---------- */
  initCooldowns();
  document.addEventListener("htmx:afterSwap", (e) => {
    initCooldowns(e.target);
    document.body.classList.toggle("has-detail", Boolean($("#detail-pane[data-open='true']")));
  });
})();
