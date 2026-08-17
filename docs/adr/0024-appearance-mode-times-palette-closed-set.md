# Appearance is Mode × Palette; the Palette set is closed and semantic colors are immune

## Context

Verve shipped with a dark/light toggle: a `dark` class on `<html>`, one shadcn token
set per side, the choice in `localStorage`. Two things were missing and one was
wrong.

Missing: there was no way to change the *colors*, only their lightness — the violet
accent was the only accent. And there was no `system` mode, so an Account working in
light by day and dark by night had to flip the switch by hand twice a day.

Wrong: the choice was applied in a `useEffect`, after first paint, while `index.html`
hard-coded `class="dark"`. A light-mode Account saw a dark flash on every reload.

Adding palettes to a data-visualization app is not a cosmetic change, because some of
Verve's colors are load-bearing. `--chart-1..4` are the categorical identities of the
up-to-four Metrics on a Panel (ADR 0020), chosen for color-vision separation in both
modes. `--chart-positive` / `--chart-negative` are the diverging pair for a signed
Metric — warm surplus above zero, cool deficit below (ADR 0014). A palette that
repaints those freely can make a four-series Panel unreadable, or invert the reading
of a calorie balance.

One relief, established by reading the renderer: the two groups never appear on the
same chart. `panel-chart.tsx` colors a diverging bar by sign **only** on a
single-Metric Panel; in a combo, identity wins over polarity and the bars wear the
series color. So the categorical ramp and the semantic pair cannot collide, and a
palette is free to derive its ramp from its own accent.

## Decision

**Appearance is a pair of orthogonal axes: Mode × Palette.**

- **Mode** — `light` / `dark` / `system` — remains the `dark` class on `<html>`.
  `system` is resolved to that class **in JS**, never by a `prefers-color-scheme`
  media query in the stylesheet, so the class stays the single source of truth for
  Tailwind's variants and for the palette selectors alike.
- **Palette** — a `data-palette` attribute on the same element. Every Palette defines
  the complete chrome and the categorical ramp **twice**, under `[data-palette="x"]`
  and `.dark[data-palette="x"]`. The doubled selector (0,2,0) outranks both the bare
  `.dark` and the palette's own light block, which is what makes the two axes
  independent with no JS arithmetic over tokens.

**The Palette set is closed.** Six palettes — Verve, Slate, Nord, Ember, Forest,
Rose — defined by Verve, chosen by the Account. No color picker, no custom hex. This
is the same posture as the closed Catalog (ADR 0002) and the seeded Dashboard
template (ADR 0018).

**Semantic colors are immune to the Palette.** `--destructive`,
`--chart-positive` and `--chart-negative` are defined once under `:root`/`.dark` and
never restated in a palette block. They encode meaning, not style.

Both axes stay in `localStorage`, per device, alongside the summary prefs — no
migration, no endpoint, no server round-trip. An inline script in `<head>` applies
them before first paint.

## Considered Options

- **Two orthogonal axes, closed set, immune semantics (chosen).** Six menu entries
  instead of twelve, `system` remains coherent (it follows the OS toward a
  *lightness*, which is meaningless if a theme is one flat thing), and the existing
  `dark`-class mechanism is reused rather than replaced.
- **A flat list of themes ("Nord Dark", "Solarized Light", …).** Simpler to write, and
  it collapses under its own combinatorics: every new palette doubles the list, and
  `system` has nothing to follow. Rejected.
- **A color picker, or fully custom palettes.** The cost is not the picker, it is the
  guarantee. Each palette here was checked for AA contrast on its text-on-surface
  pairs and for hue and lightness separation across its four chart series in both
  modes. A hue typed by hand cannot be checked, and the first symptom is an
  unreadable Panel or invisible button text — which reads as a Verve bug, not as a
  user's choice. Rejected for v1; the closed set does not foreclose it.
- **Let the Palette own the semantic pair too.** Maximally coherent visually, and it
  would let a palette decide that a deficit is warm. Verve deliberately never colors a
  delta good or bad (ADR 0019); it must equally never let a decoration decide what a
  sign means. Rejected.
- **Persist the Appearance on the Account, server-side.** It would follow the Account
  across devices, and it would arrive after the first paint — reintroducing the very
  flash this ADR removes, unless the server inlined it into `index.html`. For a
  cosmetic per-device choice on a self-hosted instance, that is a migration and an
  endpoint bought for very little. Rejected.

## Consequences

- Adding a palette is data: two CSS blocks and one entry in `PALETTES`. Adding a
  *token* is not — it must be added to all six palettes in both variants. A token
  missing from one block silently falls through to Verve's, which is a bug and not a
  fallback; the contract is stated at the top of `index.css`.
- The default Mode is now `system`, not dark. An Account that had already toggled
  keeps its side: the provider reads the old `verve-theme` key once as a fallback.
- The pre-paint script in `index.html` duplicates a few lines of `appearance.tsx` by
  necessity — it must run before the bundle. The duplication is small, marked, and
  the only alternative is the flash.
- The palette swatches in the menu render inside an element carrying that palette's
  own `data-palette`, reading their colors from the cascade rather than restating hex
  values in TypeScript. They therefore cannot drift from the stylesheet.
- Each palette's chart ramp is its own; the color-vision separation claim of ADR 0020
  is now a claim that must hold six times, and it is re-checked per palette rather
  than inherited.
