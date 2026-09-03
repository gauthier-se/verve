# The chart ramp rotates per Panel, and runs two colors past what a Panel can use

Extends ADR 0020 and ADR 0026, both of which stay in force: a Panel still carries
at most four Metrics on at most two axes, colour is still assigned by position and
never by Metric identity, and the Palette set is still closed and adapted rather
than reproduced. What changes is *which* position a Panel reads the ramp from, and
how long the ramp is for the things that are not Panel Series.

## Context

Two facts about the ramp had quietly merged into one.

The first is ADR 0020's: a Panel holds up to four Metrics, and a Metric's colour is
its position in the Panel, so that no Metric owns a hue across the app. The second
is an accident of that: `SERIES_COLORS[i]` is indexed by the Series' position *within
its own Panel*, and the overwhelming majority of Panels carry exactly one Metric. So
every single-Metric Panel is position 0, every one of them draws in `--chart-1`, and
a Dashboard of eight cards is eight curves in the same violet. Three quarters of a
ramp that nine Palettes are individually verified against is furniture nobody sees.

The second confusion is that four became the length of the ramp everywhere, rather
than the cap on one Panel. A Night has six Stages and had four slots, resolved by
doubling two Stages up on the argument that they never co-occur — a promise about the
data, enforced nowhere, holding up a drawing decision. The history rail has six kinds
of event and had four slots, resolved by giving `import` and `phase` the same dot,
which is not resolution: reading that rail *is* telling the kinds apart. Neither of
those dimensions is bounded by ADR 0020, because neither is a Panel Series.

## Decision

**A Panel reads the ramp from an offset, and the offset is its place in the
Dashboard.** `seriesColor(i, offset)` returns `SERIES_COLORS[(offset + i) % 4]`. The
grid hands each Panel its index; a chart that stands alone on its own page takes 0.

The rotation is **uniform**, which is the property that makes this safe rather than
clever: it moves a Panel's Series together, so it can never fold two of them onto one
colour, and the four-way separation ADR 0026 verifies holds at every offset.

**Colour therefore belongs to the slot, not to the Metric in it — including across
Panels.** Reordering a Dashboard recolours it. That is not a defect to be worked
around with a hash of the Panel id; it is the same rule ADR 0020 already chose, read
one level out. A Metric that kept its hue when it moved would be the identity
colouring ADR 0020 refused.

**The ramp runs to six, and the extra two never carry a Series.** `CATEGORY_COLORS`
is `SERIES_COLORS` plus `--chart-5` and `--chart-6`, and `SERIES_COLORS` is its first
four entries so that a Stage and a Series at the same position are the same colour
rather than two that nearly match. Stages and history events read the long ramp and
get one slot each. Nothing drawing a Panel's curves may index past 3: a fifth curve
is a Panel the server refuses to save.

**All six are held to the separation rule, and the build holds them.** Every pair in
a block must differ by 30 degrees of hue **or** 12 points of lightness, and each must
clear 3:1 against `--card`. ADR 0026 said the completeness of a Palette is tested
rather than reviewed and then left this half to the eye, which was defensible at four
values and is not at six across eighteen blocks.

## Considered Options

- **Offset by the Panel's position in the Dashboard (chosen).**
- **Leave it: single-Metric Panels stay chart-1.** Honest to the letter of ADR 0020
  and the reason the ramp is invisible. The Palette work of ADR 0026 verifies four
  colours per variant that a normal Dashboard never shows. Rejected.
- **Colour by Metric identity, hashed from the slug.** Stable under reorder, and it
  is exactly what ADR 0020 rejected: a hash over a closed Catalog collides, and a
  Metric that owns a hue globally makes two Panels look related when they are not.
  Rejected.
- **Hash the offset from the Panel id.** Keeps a Panel's colour across a reorder
  without giving a Metric a colour. But a hash over four slots distributes badly —
  adjacent cards land on the same hue often enough to look like a bug — and it trades
  a legible rule for an arbitrary one. Rejected; reconsider if reorder-flicker turns
  out to bother anyone in practice.
- **Extend the ramp to eight.** More headroom, and nothing has eight categories: the
  two dimensions that overflowed have six. Every added token is nine palettes times
  two variants of real colour work with no use to justify it. Rejected.
- **Give Stages and events their own palettes, separate from the chart ramp.** More
  freedom per dimension, and it breaks the thing the Palette is for: a Panel's curves
  and a Night's stack would stop belonging to the same world (CONTEXT.md: Palette).
  Rejected.

## Consequences

- Reordering a Dashboard recolours its Panels. Drag-and-drop reorder already exists,
  so this is visible on the first drag; it is the documented behaviour, not a bug
  report.
- `--chart-5` and `--chart-6` are two more values per block, so a new Palette is now
  twelve chart colours rather than eight, and six is where most themes run out of
  canonical accents that stay clear of the four. Gruvbox and Rosé Pine had nothing
  left: theirs are Verve's derivation, recorded in `THIRD-PARTY-NOTICES.md` like every
  other departure.
- `TestChartRampIsSeparated` will fail a Palette contribution that a reviewer would
  have waved through. Building this ADR's own values produced five such failures.
- The Metric page and any other lone chart stay on `--chart-1`. They have no
  neighbours to be confused with, and a page whose hero figure changed hue by
  navigation would be worse, not better.
