Status: done

# 01 — web: Appearance (Mode × Palette)

## What

- `web/src/index.css`: restructured into an immune block (`--radius`,
  `--destructive`, `--chart-positive`, `--chart-negative`) plus six Palette
  pairs, `[data-palette="x"]` and `.dark[data-palette="x"]`. The doubled
  selector (0,2,0) outranks both the bare `.dark` and the Palette's own light
  block — that specificity contract is what makes the axes orthogonal with no
  JS arithmetic over tokens, and it is stated at the top of the file.
- `web/src/components/appearance.tsx` (replaces `theme.tsx`):
  `AppearanceProvider` owns `mode` (`light`/`dark`/`system`) and `palette`,
  persists both to `localStorage` (`verve-mode`, `verve-palette`), reads the old
  `verve-theme` key once as a fallback so an Account that had toggled keeps its
  side, and resolves `system` against a `matchMedia` listener.
- `AppearanceMenu`: sidebar-footer popover, Mode segmented control over a
  6-cell Palette grid. Replaces `ThemeToggle`.
- `PaletteSwatch`: renders inside an element carrying that Palette's own
  `data-palette` (plus `dark` when that is the resolved Mode), so it reads its
  colors from the cascade instead of restating hex values in TypeScript and
  cannot drift from the stylesheet.
- `web/index.html`: drop the hard-coded `class="dark"`; add an inline `<head>`
  script applying both axes before first paint.
- `web/src/components/ui/segmented.tsx`: `Segment` extracted from
  `panel-prefs.tsx` (now used by both), `hint` made optional.
- `web/tailwind.config.js`: expose `chart-4` and the semantic pair, which
  existed in CSS but had no utility.

## Why here

Purely client-side: no endpoint, no migration, no Go change. The token
indirection was already in place — nothing in the app hard-codes a color, and
`panel-chart.tsx` only ever reads `var(--chart-*)` — so a Palette is data, not
a code path.

The one design question that needed answering first was whether the categorical
ramp and the diverging pair could collide on the same chart. They cannot:
`panel-chart.tsx` colors a diverging bar by sign only on a single-Metric Panel;
in a combo, identity wins over polarity. So a Palette is free to derive its ramp
from its own accent.

## Comments
