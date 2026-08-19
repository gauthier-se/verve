Status: done

# 02: a token-completeness test over index.css, and the contribution checklist

## What

- **A Go test**, e.g. `internal/web/palette_test.go`, that:
  - reads `web/src/index.css` from the repo (a relative path from the test's
    directory; the file is checked in and CI runs from the repo root),
  - parses every `[data-palette="x"]` and `.dark[data-palette="x"]` block, plus
    the `:root, [data-palette="verve"]` and `.dark, .dark[data-palette="verve"]`
    blocks, collecting each block's set of `--token` names,
  - **fails if any block's token set differs from Verve's light block**, naming
    the palette, the variant and the missing or extra tokens,
  - **fails if a palette declares one variant and not the other**,
  - **fails if a palette block declares a semantic token** (`--destructive`,
    `--destructive-foreground`, `--chart-positive`, `--chart-negative`), since
    ADR 0024 makes those immune to the Palette,
  - **fails if `PALETTES` in `appearance.tsx` and the CSS blocks disagree**, in
    either direction (a parse of the id strings is enough; no TS evaluation).
- **`CONTRIBUTING.md`**: a "Contributing a Palette" section listing what a new
  palette must satisfy before review:
  - both variants, complete token sets, no semantic token,
  - AA contrast (4.5:1) on `foreground`/`background`, `card-foreground`/`card`,
    `popover-foreground`/`popover`, `primary-foreground`/`primary`,
    `secondary-foreground`/`secondary`, `accent-foreground`/`accent`,
    `muted-foreground`/`background`,
  - `--chart-1..4` separated in hue **and** lightness, checked in both variants,
    and legible against `card`,
  - an entry in `THIRD-PARTY-NOTICES.md` when the palette is named after an
    existing project, plus a header comment in `index.css`,
  - a note that a light variant Verve derives itself is acceptable and must be
    labelled as such, and that a paid upstream variant must not be copied.

## Why here

`web/` has **no JS test runner at all**: no vitest, no test file, and `make ci`
runs only Go targets (`fmt-check`, `vet`, `go build`, `go test -race`). Adding a
front-end toolchain for one test costs more than the test is worth, and it would
not run in CI without further work. A Go test reading the stylesheet as text has
no new dependency and is already wired into `make ci`.

Completeness is the right first test because it catches the **silent** failure
ADR 0024 names in its own consequences: a token missing from one block does not
break anything visibly, it inherits Verve's value, so the palette is subtly
wrong in a way review will not reliably catch across eighteen blocks. Contrast
failures, by contrast, are visible to whoever looks at the palette.
