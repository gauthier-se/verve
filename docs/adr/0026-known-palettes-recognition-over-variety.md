# The Palette roster is chosen for recognition, and named themes are adapted, not reproduced

Extends ADR 0024, which stays in force: Appearance remains Mode × Palette, the
Palette set remains closed, and the semantic colors remain immune to the Palette.
What changes here is *which* palettes ship, and what naming one after somebody
else's theme commits Verve to.

## Context

ADR 0024 shipped six Palettes: Verve, Slate, Nord, Ember, Forest, Rose. Four of
them are ports of nothing. They were picked to span a range of hues, which is a
reasonable criterion for a set nobody has seen before and a poor one for a set
people choose from. Someone who already lives in Catppuccin in their editor, their
terminal and their window manager does not want "Rose"; they want the thing they
already know, by its name.

Naming a palette after a live project brings two problems that Slate never had.

The first is **structural**: Mode × Palette requires every Palette to exist in both
a light and a dark variant. Several of the best-known themes are dark-first.
Catppuccin has Latte, Tokyo Night has Day, GitHub and Gruvbox and Solarized and
Rosé Pine ship both. Dracula's free palette is dark only, and Nord, already in the
set, has no canonical light variant either. Its light variant has been Verve's own
derivation since ADR 0024, without anyone calling that out.

The second is that **the constraints these palettes must satisfy are not the ones
they were designed for**. Verve holds each Palette to AA contrast on its
text-on-surface pairs and to hue and lightness separation across `--chart-1..4` in
both variants, because those four colors are the identities of up to four Metrics
on one Panel (ADR 0020). Solarized in particular is *designed* around low contrast;
several of its canonical pairs land between 3.9:1 and 4.4:1, just under AA.

## Decision

**The roster criterion is recognition, not variety.** Nine Palettes: Verve,
Catppuccin, Dracula, GitHub, Gruvbox, Nord, Rosé Pine, Solarized, Tokyo Night.
Slate, Ember, Forest and Rose are removed; they carry no recognition, and Rose
would be confusable with Rosé Pine. Verve stays first as the default and the
fallback; the rest are alphabetical, so nine entries are scanned rather than
searched.

**Removing a Palette is free and needs no migration.** The pre-paint script sets
`data-palette` without validating it, but an id with no matching CSS block falls
through to `:root`, which is Verve, and `readPalette()` rewrites the stored value
on the next render. An Account on Ember lands on Verve, with no flash and no error.

**One pair per theme, the flagship variant.** Catppuccin Latte + Mocha, Tokyo Night
Day + Night, Gruvbox light medium + dark medium, Rosé Pine Dawn + Main, GitHub
Light + Dark (not Dimmed), Solarized Light + Dark. Shipping "Catppuccin Mocha" and
"Catppuccin Frappé" as two entries is the flat combinatorial list ADR 0024 already
rejected, and it would leave `system` mode with nothing coherent to follow.

**A named Palette is a verified adaptation, not a certified reproduction.** Where a
theme has no canonical light variant, Verve derives one from its published hues.
Where a canonical value misses AA or leaves two chart series indistinguishable,
Verve moves it. Both are stated in `THIRD-PARTY-NOTICES.md`, per palette, so nobody
has to diff hex values to find out what was changed.

**Dracula's light variant is Verve's own derivation, never Alucard.** Alucard ships
with Dracula PRO, which is a paid product; the free Dracula palette is MIT and dark
only. This is the one case where the constraint is not aesthetic.

**Attribution is explicit.** `THIRD-PARTY-NOTICES.md` credits each upstream project
with its licence and its URL and disclaims endorsement, and each palette block in
`index.css` carries a header comment naming its source.

**The completeness of a palette is tested, not reviewed.** ADR 0024 already states
that a token missing from one block silently falls through to Verve's and that this
"is a bug, not a fallback". That is a silent failure, restated in prose across
eighteen blocks, which is exactly what a test should hold instead.

## Considered Options

- **Recognition, closed set, adaptation stated (chosen).**
- **Keep the four invented palettes and add the famous ones.** Thirteen entries in
  a popover, four of which nobody asked for, and "Rose" sitting next to "Rosé
  Pine". Rejected.
- **Refuse any theme without a canonical light variant.** Principled, and it drops
  Dracula, one of the two or three most recognized themes there are. It would also
  retroactively condemn Nord, which has shipped a derived light variant since ADR
  0024. Rejected.
- **Let a Palette be single-mode, greying out the Mode axis when it is active.**
  This is the flat theme list wearing a disguise: it breaks the orthogonality that
  is the entire point of ADR 0024, and `system` mode stops meaning anything.
  Rejected.
- **Rename the palettes to avoid the marks ("Midnight Tokyo", "Latte").** It
  sidesteps a question nobody was asking and destroys the recognition that is the
  whole reason for the change. The names identify the source of the colors, which
  is what attribution is for. Rejected.
- **Reproduce each theme exactly, and drop the AA and separation requirements for
  named palettes.** Maximal fidelity, and it makes an unreadable Panel a supported
  configuration. A user who picks Solarized has asked for Solarized's colors, not
  for a chart whose third series they cannot see. Rejected.
- **Move the palette definitions out of CSS into a data file that generates the
  blocks.** Tempting with nine palettes. But the cost ADR 0024 actually names is
  adding a *token*, which must be replicated everywhere, and moving colors into
  JSON does not touch that. Rejected for now; the completeness test addresses the
  real failure mode.

## Consequences

- The claim in ADR 0020 about color-vision separation now has to hold nine times.
  The riskiest palettes are the pastel ones (Catppuccin, Rosé Pine, Tokyo Night),
  whose ramps are deliberately uniform in lightness, so hue does most of the work
  there.
- Building the roster surfaced twelve real contrast and separation failures in
  values taken straight from upstream. That number is the argument for the
  checklist in `CONTRIBUTING.md` and for automating what can be automated.
- The Appearance popover goes from `w-64` to `w-72`: "Catppuccin" and "Tokyo Night"
  do not fit two-up at the old width. Nine entries over five rows is still an
  ordinary popover. Past **twelve** palettes the control needs rethinking, and that
  is the number at which to reopen this.
- Adding a palette is still data, as ADR 0024 promised, but it now also means an
  entry in `THIRD-PARTY-NOTICES.md`, a header comment, and passing the checklist.
- Each upstream project's licence state is recorded as of this decision. A palette
  whose upstream relicenses is a thing to notice, and nothing in the build will
  notice it.
