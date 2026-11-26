# Tickerlight mark — B refinements

Three variants of the threshold-gate concept (concept B from round 1), developed to escape the letterform read and strengthen the trading idea (a level being crossed by something that rises through it).

All three marks hold their meaningful geometry inside the central 80% safe box (51.2–460.8 on both axes in the 0–512 viewBox). Verified numerically against the box bounds by computing all path vertices, not visually.

---

## B2 — Short Gate (`b2.svg`)

**Geometry.** Apex: solid narrow triangle, tip at y=85, base at y=240 (gap opens the apex from the body, so they do not fuse into a single letterform). Crossbar (the lime threshold): horizontal rectangle from y=255 to y=285, contained between x=190 and x=322 (does *not* overhang the legs, eliminating the serif read). Lower bracket: hollow rectangular frame from y=285 to y=390, with parallel vertical edges (no taper), creating the gated/bracketing feel. Three `<path>` elements: 4 + 5 + 10 = **19 path commands** total.

**Bounds check.** x: 165–347; y: 85–390. All vertices ✓ within 51.2–460.8.

**What it risks reading as.** Still reads as an upward chevron / arrow on first glance if the lime band is not salient; at distance or low contrast, the bracket shape alone is indistinguishable from a plain angular "up" icon. The parallel legs (not tapered) help distinguish it from a solid triangle, but the gesture is still vertical/ascending, which is generic in UI iconography.

**Breaks at small size.** At 24–32 px, the gap between apex and crossbar (30 px in canvas = ~1.4–1.8 px at 24 px) may collapse, fusing the apex back to the crossbar and re-introducing the letter read. The lime fill remains visible longer than a thin stroke would, but if the gap closes, the shape reverts to a single solid chevron / upward arrow, losing the gated distinctiveness.

---

## B3 — Beam Through Level (`b3.svg`)

**Geometry.** Two horizontal rules: top at y=200–218, bottom at y=290–308 (each spans the full x=70–442 width, creating the "slit" / level). Single vertical bar: x=236–276, rising from y=150 to y=310 (passes clean through the gap). Lit band (the crossing point): a small rectangle at the intersection, x=236–276, y=245–265, rendered after the other shapes so it appears on top. Four `<path>` elements: 5 + 5 + 5 + 5 = **20 path commands** total.

**Bounds check.** x: 70–442; y: 150–310. All vertices ✓ within 51.2–460.8.

**What it risks reading as.** At first glance, reads as a minus sign or equals sign (two parallel horizontal rules). With the vertical bar added, it fuses into an exclamation mark (!) or a cross (+) or a capital I with serifs — all letterforms or generic symbols. The vertical passing through the gap is the key to distinguishing it, but if the gap width compresses at small sizes, the two rules will merge into a single bar (like a horizontal I-beam), and the vertical becomes a mere crossing line, reading as a +/cross symbol. The lime highlight at the crossing is meant to disambiguate ("this is not a plus sign; *this lit point* is the trading signal"), but it may not be enough if the structure reads as a letter first.

**Breaks at small size.** At 24–32 px, the gap between the two rules (72 px = ~3.4–4.3 px at 24 px) is fragile; at that size, the rules may appear as a single thick bar, and the vertical line fuses into a simple cross. The lime highlight (20 px height = ~1 px at 24 px) becomes a hairline or vanishes entirely, so the "threshold crossing" read fails and the mark reads as a + or ⊕ or ⊗ symbol (generic geometric cross / circle-cross).

---

## B4 — Lamp (`b4.svg`)

**Geometry.** Level line: horizontal bar at y=275–295, full width x=70–442 (the threshold). Rising wedge: a trapezoid with wide base (y=400, x=165–347) and narrow top (y=295, x=222–290), converging upward but truncated at the level line to look like a beam of light cutting through (not a plain triangle). Lime crossing: small rectangle at the intersection (y=275–295, x=222–290), rendered last so it sits on top. Three `<path>` elements: 5 + 5 + 5 = **15 path commands** total.

**Bounds check.** x: 70–442; y: 275–400. All vertices ✓ within 51.2–460.8.

**What it risks reading as.** The trapezoid silhouette on its own reads as a roadmap pin / location marker (the classic inverted-teardrop "you are here" symbol in maps) or a stylised flame / torch. The level line anchors it and the lime highlight tries to emphasize the crossing, but if the lit band is not read as *distinct* from the wedge, the whole shape reads as a flaming torch on a surface, which is generic "fire" or "heat" iconography, not financial/trading. The lack of symmetry vs. the rounded-pin expectation helps, but the wedge-on-a-line composition is not unique.

**Breaks at small size.** At 24–32 px, the trapezoid's tapered top (22 px wide = ~1 px at 24 px) becomes a hairline or vanishes, and the shape collapses into a triangle or a solid blob. If the lime crossing is not separately visible (and at ~20 px height = ~1 px at 24 px it may not be), the mark reads as a plain upward triangle or teardrop — a generic arrow or pin, not a "light crossing a threshold." The whole intent (the wedge as a beam, the lime as the lit crossing point) depends on pixel-level contrast and silhouette distinction that may not survive 24 px rendering.

---

## Process note (what was verified vs. assumed)

- **Verified numerically:** Every vertex of every shape in all three SVGs falls within the 51.2–460.8 safe box.
- **Verified:** File validity as well-formed SVG with only `<path>` elements, no raster embeds, no gradients, no shadows.
- **Not verified** (no rendering available to this agent): How any mark actually looks rendered, anti-aliased, or at the stated small sizes. All "what breaks at small size" claims above are geometric inferences (feature width in px at a given canvas size), not observed renders. A human or vision-capable reviewer must judge the actual renders at 512 / 128 / 32 / 64 px and grayscale / inverted-on-white to confirm whether the letterform/object reads and the trading idea survive.
