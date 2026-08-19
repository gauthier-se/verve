# PRD: Known palettes

## Goal

Give the Palette set the thing it currently lacks: **recognition**. Verve ships
six palettes, of which four (Slate, Ember, Forest, Rose) are ports of nothing.
Someone who already lives in Catppuccin or Tokyo Night in their editor and their
terminal should find it here too, by name. The mechanism of ADR 0024 is right;
its roster was chosen for variety rather than for familiarity.

## What this milestone does

- **Nine palettes**: Verve (the default, the product's own identity),
  **Catppuccin**, **Tokyo Night**, **GitHub**, **Dracula**, **Nord**,
  **Gruvbox**, **Rosé Pine**, **Solarized**.
- **Four palettes removed**: Slate, Ember, Forest, Rose. They carry no
  recognition, which is now the criterion, and "Rose" would collide with Rosé
  Pine. **Removal is graceful and needs no migration**: the pre-paint script
  sets `data-palette` without validating it, but an id with no matching CSS
  block falls through to `:root`, which is Verve, and `readPalette()` then
  corrects the stored value on the next render. A user on Ember lands on Verve,
  with no flash and no error.
- **One pair per theme, the flagship variant**: Catppuccin **Latte + Mocha**,
  Tokyo Night **Day + Night**, Gruvbox **light medium + dark medium**, Rosé Pine
  **Dawn + Main**, GitHub **Light + Dark** (not Dimmed), Solarized **Light +
  Dark**. Stated in the ADR, because otherwise the first contribution proposes
  Macchiato and the roster starts doubling.
- **Missing light variants are synthesized**, derived from the theme's official
  accents with contrast re-checked. This concerns **Dracula** and **Nord**,
  neither of which has a canonical light variant. Verve's palettes therefore
  carry a theme's name as a **verified inspiration**, not as a certified
  reproduction, and the ADR says so.
- **Alucard is not copied.** Dracula's free specification is MIT and dark-only;
  its light variant, Alucard, ships with Dracula PRO, a paid product. Verve's
  light Dracula is derived from the free palette. (Confirm the exact upstream
  licence state at implementation time rather than trusting this line.)
- **Attribution**: a `THIRD-PARTY-NOTICES.md` crediting each upstream project
  with its licence and its URL, plus a header comment above each palette block
  in `index.css` pointing at its source. Verve is Apache-2; the colour values
  are arguably not protectable but the names are live projects' marks, and this
  is what a self-hosted open-source project owes them.
- **A token-completeness test.** ADR 0024 states that a token missing from one
  block silently falls through to Verve's, and that this "is a bug, not a
  fallback". A silent failure asserted in prose is exactly what a test should
  hold. Nine palettes times two blocks makes it a matter of when, not if.
- **A contribution checklist** in `CONTRIBUTING.md`: which text-on-surface pairs
  must clear AA, and the hue/lightness separation required across
  `--chart-1..4` in both modes.

## What this milestone does NOT do

- **Nothing changes in the Appearance mechanism.** Mode × Palette stays
  orthogonal, Palette stays the `data-palette` attribute, every palette still
  defines the complete chrome and the categorical ramp in both blocks, and
  `--destructive` / `--chart-positive` / `--chart-negative` stay immune to the
  Palette. ADR 0024 is extended, not contradicted.
- **The set stays closed.** No colour picker, no user-supplied palette. The
  criterion moves from variety to recognition; the posture does not move.
- **No flat theme list.** "Catppuccin Mocha" and "Catppuccin Frappé" as two
  entries is precisely the combinatorial list ADR 0024 rejected, and it would
  leave `system` mode with nothing coherent to follow.
- **No redesign of the Appearance popover.** It keeps its two-column grid and
  gains nine entries over five rows, which is ordinary. It does widen from
  `w-64` to `w-72`: "Catppuccin" and "Tokyo Night" do not fit two-up at the old
  width. The ADR records the threshold at which the control itself needs
  rethinking, twelve palettes, so the next person does not have to rediscover it.
- **No contrast test yet.** It needs an HSL-to-relative-luminance conversion and
  a decision about which pairs to check. Worth doing, second. The completeness
  test catches the silent failure; the contrast one catches a visible one.
- **No palette definitions moved out of CSS into data.** Tempting, but the real
  cost ADR 0024 describes is adding a **token** (which must be replicated across
  every palette), and moving colours to JSON does not touch that.

## Found while building this

The AA and separation audit turned up **two failures in Verve's own light palette**,
predating this work: `--chart-2` at 2.85:1 and `--chart-3` at 2.59:1 against the
card, both under the 3:1 floor for a graphical object. Fixed in place by darkening
them 2 and 4 points of lightness respectively, which is imperceptible as a color and
is the difference between the checklist being true and being a claim.

## Docs

- **A new ADR** extending ADR 0024: the recognition criterion, the roster, the
  one-pair-per-theme rule, the synthesized-light-variant doctrine (with the
  Alucard case), attribution, and the twelve-palette threshold. Plus **one
  cross-reference line added at the top of ADR 0024** pointing at it. ADR 0024
  is **not** rewritten: its reasoning against the flat list is what the next
  contributor will need, and editing it in place would erase it. This repo has
  no `Status:` or `Superseded by` convention, so a cross-reference line is the
  mechanism available.
- **CONTEXT.md**: update the **Palette** entry, which currently names the six.

## Issues

1. `01-palette-roster`, web and docs: the nine palettes in `index.css`, the
   `PALETTES` list, the four removals, the notices file, the ADR and the
   CONTEXT.md update.
2. `02-palette-token-completeness-test`: the Go test over `index.css` and the
   `CONTRIBUTING.md` checklist.
