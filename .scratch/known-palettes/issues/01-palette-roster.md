Status: done

# 01: web, docs: the nine-palette roster

## What

- **`web/src/index.css`**: remove the `slate`, `ember`, `forest` and `rose`
  blocks (both variants each). Add, each as a `[data-palette="x"]` +
  `.dark[data-palette="x"]` pair, with a header comment naming the upstream
  project, its licence and its URL:
  - **Catppuccin**: Latte (light) / Mocha (dark).
  - **Tokyo Night**: Day (light) / Night (dark).
  - **GitHub**: Light / Dark (not Dimmed).
  - **Dracula**: dark from the free MIT palette; light **synthesized** from its
    accents, **not** Alucard (Dracula PRO, paid).
  - **Gruvbox**: light medium / dark medium.
  - **Rosé Pine**: Dawn (light) / Main (dark).
  - **Solarized**: Light / Dark.
  `verve` and `nord` stay as they are.
- Every block defines the **complete** token set the Verve block defines:
  `background, foreground, card, card-foreground, popover,
  popover-foreground, primary, primary-foreground, secondary,
  secondary-foreground, muted, muted-foreground, accent, accent-foreground,
  border, input, ring, chart-1..4`. Nothing else: `--destructive`,
  `--chart-positive` and `--chart-negative` stay immune (ADR 0024) and must not
  appear in a palette block.
- **`--chart-1..4` are derived from each theme's own named accents**, picked for
  hue and lightness separation in both variants. They are the categorical
  identities of up to four Metrics on one Panel (ADR 0020); a ramp of four
  neighbouring hues from a theme's pastel range makes a combo Panel unreadable.
  Prefer the theme's canonical distinct accents (Catppuccin: mauve, sky, green,
  peach; Rosé Pine: iris, foam, pine, love; Gruvbox: purple, blue, green,
  orange, and so on).
- **`web/src/components/appearance.tsx`**: `PaletteId` and `PALETTES` updated to
  the nine. Order them so the default leads: Verve, then the rest. No other
  change: `readPalette()` already falls back to `verve` for an unknown stored
  id, which is what makes the removals graceful.
- **`THIRD-PARTY-NOTICES.md`** at the repo root: one entry per themed palette
  with the project name, its licence and its URL. Note explicitly that colour
  values are adapted and that light variants for Dracula and Nord are Verve's
  own derivation, not upstream artefacts.
- **Docs**: the new ADR (see the PRD), the cross-reference line at the top of
  ADR 0024, and the **Palette** entry in CONTEXT.md, which currently enumerates
  the old six.

## Why here

The removals cost nothing because the cascade already handles a stale id: an
unmatched `data-palette` falls through to `:root`, which is Verve, so the
pre-paint script cannot flash a wrong palette, and `readPalette()` rewrites the
stored value afterwards.

The chart ramp is the part of this issue that is not mechanical. ADR 0024 says
"the colour-vision separation claim of ADR 0020 is now a claim that must hold
six times, and it is re-checked per palette rather than inherited". It now holds
nine times, and the themes most at risk are the pastel ones, whose palettes are
deliberately low in contrast between hues.
