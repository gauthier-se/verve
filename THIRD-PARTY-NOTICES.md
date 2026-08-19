# Third-party notices

Verve is licensed under Apache-2.0 (see `LICENSE`). This file credits third-party
work Verve adapts.

## Palettes

Eight of Verve's nine **Palettes** are named after color themes maintained by other
projects, and are adapted from their published palettes. Verve ships no code from
these projects: a Palette is a set of CSS custom properties in `web/src/index.css`,
derived from the upstream colors and mapped onto Verve's own tokens.

The names identify the source of the colors. They do not imply endorsement by, or
affiliation with, the projects below.

| Palette | Upstream project | Licence |
| --- | --- | --- |
| Catppuccin | [catppuccin/catppuccin](https://github.com/catppuccin/catppuccin) | MIT |
| Dracula | [Dracula Theme](https://draculatheme.com) | MIT |
| GitHub | [primer/primitives](https://github.com/primer/primitives) | MIT |
| Gruvbox | [morhetz/gruvbox](https://github.com/morhetz/gruvbox) | MIT |
| Nord | [Nord](https://www.nordtheme.com) | MIT |
| Rosé Pine | [Rosé Pine](https://rosepinetheme.com) | MIT |
| Solarized | [Solarized](https://ethanschoonover.com/solarized) | MIT |
| Tokyo Night | [enkia/tokyo-night-vscode-theme](https://github.com/enkia/tokyo-night-vscode-theme) | MIT |

The ninth Palette, **Verve**, is Verve's own.

### Where Verve's palettes depart from upstream

Verve's Appearance is a pair of orthogonal axes, Mode × Palette (ADR 0024), so
**every** Palette must exist in both a light and a dark variant. Verve also holds
each Palette to AA contrast (4.5:1) on its text-on-surface pairs and to hue and
lightness separation across its four chart series, in both variants (ADR 0026).
Upstream themes were not designed against those two constraints, so some values are
Verve's own derivation rather than an upstream color:

- **Dracula**: the free Dracula palette is dark only. Verve's light Dracula is
  **Verve's own derivation** from the free palette's hues. It is **not** Alucard,
  the light variant that ships with Dracula PRO, which is a paid product; no part
  of Dracula PRO is reproduced here.
- **Nord**: Nord has no canonical light variant. Verve's is its own derivation.
- **Solarized**: Solarized deliberately runs at low contrast. Verve darkens the
  light variant's body text and blue accent, and brightens two of the dark
  variant's chart colors, to clear AA.
- **Tokyo Night**, **Catppuccin**: a small number of surface and accent values are
  nudged for the same reason.

The full palette definitions, with the checks they must pass, are in
`web/src/index.css` and `CONTRIBUTING.md`.
