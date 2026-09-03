Status: done

# 01: web, docs: rotate the ramp per Panel, and run it two past a Panel

## What

- **`web/src/lib/chart.ts`**: `seriesColor(i, offset)` returning
  `SERIES_COLORS[(offset + i) % 4]`, and `CATEGORY_COLORS` = `SERIES_COLORS` plus
  `--chart-5`/`--chart-6`. `SERIES_COLORS` stays the first four entries so a Stage
  and a Series at the same position are the *same* colour, not a near-match.
- **Thread `colorOffset`** from `dashboard-grid.tsx` (the Panel's index in the grid)
  through `PanelCard` → `PanelChart`, `PanelLegend`, the tooltip swatches. A chart
  that stands alone on its own page (the Metric page) passes nothing and takes 0.
  The min/max band of a lone Series takes the *next* slot rather than a washed-out
  copy of its line.
- **`web/src/index.css`**: `--chart-5` and `--chart-6` in all eighteen blocks, from
  each theme's own remaining accents where two stay clear of the four. Gruvbox and
  Rosé Pine have nothing canonical left: derive, and record it.
- **`web/src/lib/sleep.ts`**: one slot per Stage on the six-slot ramp. `awake` stays
  recessed — that was never about colours being scarce.
- **`web/src/components/history-page.tsx`**: one colour per event kind. `phase` takes
  a slot of its own rather than borrowing the cut band's colour, since a boundary can
  open a bulk as easily as a cut.
- **`internal/web/palette_test.go`**: `TestChartRampIsSeparated` — every pair in a
  block differs by 30° of hue or 12 points of lightness, and each clears 3:1 against
  `--card`.
- **`internal/web/sleepstages_test.go`**: read the ramp length out of `chart.ts`
  instead of restating it, or the two drift in silence.
- **Docs**: ADR 0036, cross-reference lines on ADR 0020 and ADR 0026, the Palette
  entry in CONTEXT.md, the palette checklist in `CONTRIBUTING.md`, and the extra
  colours per palette in `THIRD-PARTY-NOTICES.md`.

## Why here

The offset belongs to the grid and not to the Panel: a Panel cannot know what is next
to it, and the colour is a property of the slot. That is also why reordering
recolours, which is the documented behaviour rather than a defect.

The separation test is the part that earned itself. Writing thirty-six new colour
values by hand produced five real failures — two under 3:1 against the card, three
pairs under both thresholds — none of which a reviewer would reliably have caught.
