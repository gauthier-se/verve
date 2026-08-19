# Contributing to Verve

## Commit messages — Conventional Commits

Verve uses [Conventional Commits](https://www.conventionalcommits.org/). Every
commit message must be:

```
<type>(<optional scope>): <description>

<optional body>

<optional footer(s)>
```

### Types

| Type | Use for |
|---|---|
| `feat` | a new user-facing capability |
| `fix` | a bug fix |
| `docs` | documentation only (README, ADRs, CONTEXT.md, comments) |
| `refactor` | code change that neither fixes a bug nor adds a feature |
| `perf` | a performance improvement |
| `test` | adding or fixing tests |
| `build` | build system, Docker, Vite, `go.mod`/deps |
| `ci` | CI configuration and workflows |
| `chore` | maintenance that doesn't touch src or tests |
| `style` | formatting only (gofmt, whitespace) |

### Suggested scopes

Scopes are optional but encouraged; use the architectural area touched:

`catalog`, `connector`, `ingestion`, `data`, `query`, `api`, `auth`, `spa`,
`dashboard`, `packaging`, `deps`.

### Breaking changes

Append `!` after the type/scope **and** add a `BREAKING CHANGE:` footer:

```
feat(api)!: return aggregated buckets instead of raw series

BREAKING CHANGE: /v1/series no longer accepts raw=true.
```

### Examples

```
feat(ingestion): stream-parse Apple Health export.zip
fix(query): use sum aggregation for step buckets
docs(adr): record aggregated-bucket API decision
build(deps): add modernc.org/sqlite
ci: run go vet, test and gofmt on pull requests
```

## Contributing a Palette

A **Palette** is the named color set a **Mode** is painted in (CONTEXT.md). The set
is closed and curated: Verve defines the Palettes, the Account picks one (ADR 0024),
and the criterion for a new one is **recognition** rather than variety, so in
practice a proposal names a theme people already use elsewhere (ADR 0026).

Adding one is data: two CSS blocks in `web/src/index.css`, one entry in `PALETTES`
in `web/src/components/appearance.tsx`, one row in `THIRD-PARTY-NOTICES.md`. What
takes the time is the checking below, because some of Verve's colors are load-bearing:
`--chart-1..4` are the identities of up to four Metrics on one Panel (ADR 0020), and
an unreadable Panel reads as a Verve bug, not as the palette author's choice.

### What `make ci` checks for you

`internal/web/palette_test.go` fails the build if a palette omits a variant, if its
token set differs from Verve's in either direction, if it restates a semantic token
(`--destructive`, `--chart-positive`, `--chart-negative`), or if `PALETTES` and
`index.css` disagree. Those are the *silent* failures: nothing looks broken, the
palette is just quietly wrong.

### What you must check yourself

**Both variants exist.** Mode and Palette are orthogonal: a Palette that is dark
only breaks the axis. If the upstream theme has no light variant, derive one from
its published hues and say so in `THIRD-PARTY-NOTICES.md`.

**AA contrast, 4.5:1, in both variants**, on each of these pairs:

| foreground token | on |
| --- | --- |
| `--foreground` | `--background` |
| `--card-foreground` | `--card` |
| `--popover-foreground` | `--popover` |
| `--primary-foreground` | `--primary` |
| `--secondary-foreground` | `--secondary` |
| `--accent-foreground` | `--accent` |
| `--muted-foreground` | `--background` and `--card` |

**Chart ramp separation, in both variants.** Every pair of `--chart-1..4` must
differ by at least 30 degrees of hue **or** 12 points of lightness, and each must
clear 3:1 against `--card` (the WCAG floor for a graphical object). Prefer the
theme's own canonical accents, picked for spread rather than for prettiness: a ramp
of four neighbouring pastels makes a four-Metric Panel unreadable, which is the
failure this rule exists to prevent.

**Expect upstream values to miss these.** Building the nine-palette roster turned up
twelve real failures in colors taken straight from upstream projects. Moving a value
is fine and expected; leaving it unrecorded is not. A named Palette is a *verified
adaptation*, never a certified reproduction, so note every departure in
`THIRD-PARTY-NOTICES.md` under that palette.

**Licence and attribution.** Add the upstream project, its licence and its URL to
`THIRD-PARTY-NOTICES.md`, and a header comment above the palette's blocks in
`index.css`. Do not copy values from a paid product: Verve's light Dracula is its own
derivation precisely because Alucard ships with Dracula PRO.

## Workflow

Issues live as markdown under `.scratch/<feature>/` (see
`docs/agents/issue-tracker.md`). One issue → a `feat/…` or `fix/…` branch →
implement (tests at agreed seams) → open a PR referencing the issue → CI green +
review → merge. `main` is protected: no direct pushes.
