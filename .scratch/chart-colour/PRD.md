# Chart colour: a ramp that is actually seen, and long enough for what it colours

## Problem

Two complaints that look like one. The charts read as monochrome, and two things
that are not the same thing are drawn the same colour.

They have different causes. The grid is violet because `SERIES_COLORS[i]` is indexed
by a Series' position **within its Panel**, and almost every Panel holds one Metric,
so almost every curve is position 0 and draws in `--chart-1`. Nine Palettes are
verified for four-way separation and a normal Dashboard shows one of the four.

The collisions are elsewhere: the history rail has six kinds of event and four
colours, so `import` and `phase` share a dot; a Night has six Stages and four slots,
resolved by doubling two up on the promise that they never co-occur. Neither
dimension is a Panel Series, so neither was ever bound by ADR 0020's cap of four.

Adding `--chart-5..8` on its own would have fixed neither: a Panel cannot hold five
Metrics, so slots past the fourth would never have been drawn by a Panel at all.

## What we are building

1. A Panel reads the ramp from an offset — its place in the Dashboard — so a grid
   cycles the four instead of repeating one. Rotation is uniform, so within-Panel
   separation is untouched.
2. `--chart-5` and `--chart-6` in all eighteen blocks, and a `CATEGORY_COLORS` ramp
   for the dimensions a Panel does not bound. Series still stop at four.
3. The separation rule from `CONTRIBUTING.md` moved into the build, because six
   values across eighteen blocks is past what a reviewer checks reliably.

## Not doing

- **Migrating to TanStack Charts.** Alpha, v0, React-only, a new grammar, and a
  rewrite of every chart surface for zero extra colour: the ramp is CSS tokens either
  way. Revisit at beta with one Panel behind a flag.
- **Eight colours.** Nothing has eight categories. The two dimensions that overflowed
  have six.
- **A stable colour per Panel across reorder.** Considered and rejected in ADR 0036.

## Decision record

ADR 0036, extending ADR 0020 and ADR 0026.
