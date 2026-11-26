# Tickerlight brand mark

The mark is the console's own object: a tile with a T knocked out of it. The tile is the
dark surface, the T is the machine's face, and the lime is the only light on the page.

- `mark-tile.svg` — the mark, lime tile with a transparent T, 256 viewBox, one colour
- `mark-app.svg` — app icon master: `#060807` canvas, lime tile, T knocked out
- `mark-maskable.svg` — full-bleed lime, T inside the 80% safe circle
- `mark-mono.svg` — one-colour version, ink tile, T knocked out
- `wordmark-light.svg` — `tickerlight`, outlined from DejaVu Sans Mono, one lime dot
- `wordmark-ink.svg` — the same outlines, single ink, for print and light surfaces
- `mark-proof.png` — asset proof: lockup, PWA sizes, maskable crops, favicon

## Shipped assets

`internal/web/static/icons/` carries the geometry at the sizes the console asks for:
`icon-192.png`, `icon-512.png`, `icon-maskable-512.png`, `apple-touch-icon.png` (180),
plus `favicon.svg`, `favicon-32.png`, `favicon-16.png` and `favicon.ico`. `manifest.json`
and `internal/ui/layout.templ` reference those paths.

## Construction

- Tile 240 units on a 256 grid, corner radius 52. T is 144 wide, 128 tall, stroke 42 —
  stroke and crossbar height are equal, so the mark stays balanced at any weight.
- T's mass centre sits on the tile's centre; the tile's radius is 21.7% of its side.
- The T is cut out of the tile rather than drawn on top: on the dark console the tile is
  the lime surface and the T is the surrounding dark showing through. One shape, no seams.
- Lime `#D2F23C` carries the tile; ink `#060807` is the T. In one colour the values
  invert and the mark still reads, which is why it holds in print and in greyscale.

## Size behaviour (measured, real 16 px raster)

At 16 px the crossbar holds two pixel rows and the stem five, with both notches open —
the T still reads. Below that the tile's radius starts eating the crossbar, so 16 is the
floor, and the favicon cut is used there rather than the app icon.

## Wordmark

Set in DejaVu Sans Mono and converted to outlines (no font dependency at render time), so
the letters ship as paths. One dot is lime: the `i` in "light". The first `i` keeps its
ink dot — one light per wordmark, not two.

## Rejected along the way

- Bars with a rising shaft, slot flares, gate and beam ideas (rounds 1–2): read as flame,
  rocket, or a candle chart; several died at 24 px.
- Round 3: five stacked prints (misregistration at 24 px), raised print in a tape gap
  (lump at 16 px), capsule switch (phone toggle), T with chisel foot (decoration),
  T in tile (kept — this is it), sheared-arm T (dropped: read as a switch or a slider).

Only the chosen marks live in this directory. The rejected rounds are in the history of
this file, not in the tree.
