# Tickerlight mark concepts

Three alternatives to the placeholder lime "TL" roundel. Each is a distinct
geometric idea, not a recolour of the others. Palette used: background
`#060807`, full text `#E8EDE6`, muted `#7C8A80`, accent `#D2F23C`.

All three marks keep their meaningful geometry inside the central 80% safe
box (51.2–460.8 on both axes in the 0–512 viewBox), so a circular
maskable-icon crop does not clip anything load-bearing. Checked
numerically against the box bounds, not visually.

## A — Ticker Tape & Flare (`logo-a.svg`)

**Geometry.** A single thick stroked polyline (3 points, 2 segments) folds
once, like a strip of paper tape bent at an angle rather than a smooth
S-curve. Four short perpendicular tick-marks are cut across the ribbon
(two per segment) to read as the serrations/printed ticks on actual ticker
tape, not a plain ribbon or swoosh. At the fold vertex, a narrow lime
triangle (the flare/beam) points outward past the ribbon's own silhouette,
as if the bend itself is throwing light. Round line caps/joins on the
ribbon stroke keep the fold soft; the flare is a sharp hard-edged triangle,
the only angular lime shape in the piece, so it reads as a distinct "emission"
rather than part of the tape structure.

**Why it fits.** Tickerlight watches a moving tape and lights up when
something clears; this is the most literal reading of the name — tape
that bends, and light that fires exactly at the bend. No chart, no
candles, nothing financial-clipart about it: it is an abstract strip +
beam.

**What breaks at small size.** The tick-marks are the first thing to die —
they're thin, low-contrast (dark on light ribbon) strokes, and at 24–32px
they will likely merge into the ribbon or disappear into anti-aliasing
mush. The flare triangle's base is narrow (~57px in the 512 canvas,
~2.7px at 24px), so its distinct shape may also blur into a soft lime
blob rather than a crisp wedge — acceptable as a secondary fallback read,
but it stops being "a flare" and becomes "a highlight."

**Honest weakness.** The ribbon fold alone, without the flare, could be
mistaken for an arrow, a check-mark, or generic "progress" iconography —
it doesn't scream "tape" without the tick-marks, and those are the
first detail to disappear at small size. The concept depends on a detail
that is also its most fragile part.

## B — Threshold Gate (`logo-b.svg`)

**Geometry.** A chevron/bracket shape — like a stylised upward arrow or
rising instrument needle, built from straight edges only — is split into
two solid pieces (tip and body) with a horizontal gap between them. A
lime rectangle (the beam/slit) fills that gap and extends slightly past
the chevron's own width on both sides, so the light reads as cutting
*through* the form rather than sitting inside it. The chevron has a
shallow inward notch at its base (two inner edges meeting below the slit),
giving it a bracket/caliper feel instead of a flat triangle. 18 path
commands total across 3 `<path>` elements (8 + 3 + 4 points, each closed).

**Why it fits.** This is the "gate" literally: a rising signal shape that
only passes through when a slit of light appears at a specific height —
the judge threshold made physical. No chart grid, no currency symbol,
no candle wicks; the rising form is reduced to a single clean silhouette
and a single precise cut, which is closer to an instrument marking (a
sight, a level, a gauge) than finance iconography.

**What breaks at small size.** The gap between tip and body is a fixed
30px band in the 512 canvas (~1.4px at 24px) — at very small sizes the
lime slit will likely collapse to a hairline or merge with anti-aliasing,
at which point the piece reads as a single solid triangle/chevron rather
than a gated one. The base notch (the inward cut making it a bracket
rather than a plain triangle) is a secondary detail that may also soften
away, leaving a plain upward chevron — still legible, just less specific.

**Honest weakness.** Without the lime slit surviving, this mark is just
an upward chevron/arrow, which is extremely common in generic "growth" or
"up" iconography and carries no Tickerlight-specific identity on its own.
The whole concept's distinctiveness lives in one thin feature.

## C — Aperture / Tick Ring (`logo-c.svg`)

**Geometry.** Ten identical short rectangles ("ticks"), each rotated 36°
from the last around the centre, form a broken ring — gaps between ticks
read as the aperture opening rather than a closed gear or sunburst. A
solid lime circle sits at the centre as the lit core. No paths: built
from 10 `<rect>` + 1 `<circle>` primitives (11 shapes total, no `<path>`
elements — the simplest of the three by element count, though the brief's
"under 40 path commands" guidance is about geometric complexity, which
this respects via primitive count instead).

**Why it fits.** Ticks are literal: each blade is a tick-mark, arranged
radially like a dial or aperture rather than linearly like a tape — a
second, distinct expression of "ticker" from concept A. The gaps between
blades (rather than a continuous ring) are what should keep it from
reading as a gear (gears have teeth pointing outward with continuous rim
between) or a sunburst (rays converge to a point; these ticks don't — they
sit tangent to a ring, implying rotation/measurement, not emission).

**What breaks at small size.** Ten separate blades at 24px means each
blade occupies only ~2–3px of arc — at that size they will almost
certainly fuse into a single grey ring rather than ten distinct ticks,
and the "broken ring, not gear" read depends entirely on the gaps being
visible. If the blades fuse, this degrades gracefully to a plain
ring-with-dot (a reasonable fallback, but it loses the "tick" idea
entirely and becomes closest in silhouette to a generic record/target
icon).

**Honest weakness.** This is the concept most at risk of reading as "gear"
or "sunburst" — the two things the brief explicitly says to avoid —
depending on rendering/anti-aliasing of the gaps; I could not verify at
actual pixel sizes whether the gaps stay crisp enough to avoid that read,
only that the gap width is nonzero by construction (geometrically checked,
not visually confirmed).

## Process note (what was verified vs. assumed)

- Verified numerically: every vertex/corner of every shape in all three
  SVGs falls within the 51.2–460.8 safe box (checked in Python against
  the actual computed coordinates, including stroke-width overshoot on
  concept A's ribbon).
- Verified: file validity as well-formed SVG with only `<path>`, `<rect>`,
  `<circle>` elements, no raster embeds, no gradients, no filters.
- Not verified (no rendering available to this agent): how any mark
  actually looks rendered, anti-aliased, or at the stated small sizes.
  All "what breaks at small size" claims above are geometric inferences
  (feature width in px at a given canvas size), not observed renders.
  The preview.html page lays out 512/128/32px instances plus a grayscale
  strip and an inverted-on-white 64px instance for a human or a
  vision-capable reviewer to actually judge.
