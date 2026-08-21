Status: done

# 04: web: the typographic layer and the token module

## What

- **`components/ui/figure.tsx`**: `Figure` (cva sizes hero/wide/panel/strip/
  stat/inline), `Unit`, `Eyebrow`, `Meta`, `Chip`, `SectionTitle`,
  `ScreenTitle`, `Key`, `Dot`, `LegendItem`, `Track`. Every number in the app
  goes through `Figure`, which means `font-mono tabular-nums`.
- **`lib/chart.ts`**: every chart colour as `hsl(var(--token))` — the categorical
  ramp, `GRID`, `AXIS`, `RECESSED`, the sign pair, the two direction hues, and
  `shade()` for the matrix. `panel-chart.tsx` now imports from it rather than
  declaring its own literals.
- **Tailwind**: the sub-12px scale (`3xs` 10px, `2xs` 11px, `heading` 13px,
  `screen` 19px) and the tracking values the design uses.
- **Shadows removed** from Card, Button, Input, Select, Textarea. Elevation is
  `--card` against `--background` plus a 1px border; floating surfaces (popover,
  dialog, tooltip) keep theirs, because they genuinely detach.
- **`awake` is drawn recessed** rather than in a fourth ramp colour: it is stacked
  so a broken night looks broken and is never counted as sleep (ADR 0027), and a
  categorical colour would make it a fourth kind of sleep.

## Why

`RECESSED` is `muted-foreground` faded rather than a fixed grey because that is
what makes it work in both modes: a literal mid-grey is a legible step down from
white on a dark ground and an almost-black smear on a light one.

## Done when

- No literal colour anywhere under `web/src` (the stylesheet excepted, where the
  palettes live). `TestSpaColoursAreTokensOnly` walks the tree.
- The annotation and baseline tones still resolve to the muted one, checked
  through the indirection rather than as a literal.
  `TestAnnotationOverlayWearsTheRecessedTone`.
- `awake` is recessive and every consumer goes through `stageColor`.
  `TestAwakeIsDrawnRecessed`.
