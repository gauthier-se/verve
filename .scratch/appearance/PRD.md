# PRD — Appearance

## Goal

Let an Account choose how Verve is painted, beyond the one violet accent it
shipped with: pick a **Mode** (light, dark, or follow the OS) and a **Palette**
(one of six curated color sets) independently of each other. A health warehouse
is a thing you sit in front of daily; it should be allowed to look the way its
owner wants.

## What this milestone does

- **Two orthogonal axes, `Mode × Palette`** (ADR 0024). Mode stays the `dark`
  class on `<html>`; Palette becomes a `data-palette` attribute on the same
  element. Every Palette defines its complete token set twice — light and dark —
  so picking one axis never decides the other.
- **`system` mode**, resolved to the `dark` class in JS (never by a media query
  in the stylesheet, so the class stays the single source of truth) and kept
  live via `matchMedia`, so the app follows the OS mid-session.
- **Six Palettes** — Verve (the current violet), Slate, Nord, Ember, Forest,
  Rose. A Palette owns the chrome *and* the categorical chart ramp
  (`--chart-1..4`), so a Panel's curves belong to the same world as the page.
- **Semantic colors immune to the Palette**: `--destructive` and the diverging
  `--chart-positive` / `--chart-negative` pair are defined once and never
  restated. A deficit stays cool in every Palette — Verve does not let a
  decoration decide what a sign means.
- **One control**, an Appearance popover in the sidebar footer (Mode segmented
  control over a Palette grid), replacing the sun/moon toggle. The footer keeps
  its three icons.
- **No flash on load**: an inline script in `<head>` applies both axes before
  first paint, fixing a pre-existing bug where `index.html` hard-coded
  `class="dark"` and the provider only corrected it in a `useEffect`.

## What this milestone does NOT do

- **No custom colors, no color picker.** The Palette set is closed, like the
  Catalog (ADR 0002) and the Dashboard template (ADR 0018). Each Palette is
  checked for AA contrast on its text-on-surface pairs and for hue/lightness
  separation across four chart series in both Modes; a hand-typed hue cannot be,
  and its first symptom is an unreadable Panel that reads as a Verve bug.
- **No server persistence.** Appearance is a per-device display preference in
  `localStorage`, alongside the summary prefs — no migration, no endpoint. The
  server-side alternative would also reintroduce the load flash this milestone
  removes.
- **No per-Dashboard or per-Panel color override.** Appearance is
  application-wide; a Panel's colors come from its position (ADR 0020), not from
  a choice.

## Issues

1. `01-web-appearance-mode-palette` — web: the two axes, the six Palettes, the
   Appearance menu, and the pre-paint script.
