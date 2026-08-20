# Contributing to Verve

Thanks for looking. Verve is a small, opinionated codebase: one Go binary with
an embedded React SPA, one SQLite file, no framework and no service mesh. That
makes it easy to run and easy to read, and it means most contributions are
small and local.

The two contributions Verve wants most are **connectors** (a new source of
health data) and **palettes** (a new color set). Both are mostly declarative
data, and both have a section below.

## Getting set up

You need Go (see `go.mod` for the version), Node, and `make`. Everything else
is vendored by the Go module cache and npm.

```sh
git clone https://github.com/gauthier-se/verve.git
cd verve
make ci        # fmt-check, vet, build, test -race, then the SPA build
```

A green `make ci` locally means a green CI, because the `ci` target mirrors
`.github/workflows/ci.yml` exactly: its two jobs are the Go checks and
`npm run build`, which is `tsc --noEmit && vite build` and so typechecks the
front end as well as bundling it.

If you have no Node installed and your change is Go-only, `make ci-go` runs
just the Go half. It is a real subset and not a shortcut: the binary embeds
whatever is in `internal/web/dist` and compiles against a committed placeholder
when nothing has been built, which is why the two jobs are independent in CI
too.

Day to day:

```sh
make run ARGS="serve --secure-cookie=false"   # API + whatever SPA is embedded
make ui-dev                                   # Vite dev server, proxies /v1
make ui                                       # build the SPA into internal/web/dist
make dist                                     # SPA then binary: the real artifact
make test                                     # go test -race ./...
make cover                                    # coverage report in the browser
```

`make` with no target lists everything. The data directory defaults to `./data`
and is created on first run; deleting it resets your local instance.

Create a local account either from the first-run screen in the browser, or with
`make run ARGS="account create --email=you@example.com"`.

## The stack

* **Backend**: Go, standard library `net/http` router, `log/slog`, dependency
  injection through an `application` struct rather than globals. No chi, no
  gin.
* **Storage**: SQLite through the pure-Go `modernc.org/sqlite`, so the binary
  stays static and cross-compiles. Migrations are embedded and apply
  themselves on startup. Large artifacts (GPX routes, ECG waveforms) are files
  on disk referenced from the database (ADR 0004).
* **API**: JSON, serving server-aggregated buckets and never raw series
  (ADR 0012).
* **Front end**: Vite and React, embedded into the binary with `go:embed`
  (ADR 0005). TanStack Router, Query and Table, shadcn/ui, Recharts, Tailwind
  (ADR 0013).
* **Auth**: local argon2id with opaque session cookies, kept extensible toward
  reverse-proxy forward auth (ADR 0008).
* **Packaging**: distroless image, goreleaser for static binaries, one
  `VERVE_DATA_DIR`.

## How the code is laid out

```
cmd/verve/              CLI entry point: serve, migrate, account, import
internal/catalog/       the closed set of canonical Metrics, units, formulas
internal/connector/     sources of data; applehealth/ is the only one so far
internal/units/         unit conversion at import time
internal/data/          storage: SQLite, embedded migrations, one model per family
internal/query/         the read engine: aggregated buckets, source resolution
internal/timeaxis/      time range, baseline and bucket resolution, pure and DB-free
internal/estimate/      inferred quantities: basal and expenditure estimates
internal/api/           HTTP handlers, auth, rate limiting, import jobs
internal/web/           go:embed of the built SPA
web/                    the React source
docs/adr/               why the structure is what it is
.scratch/               PRDs and issues, one directory per milestone
```

Two documents are worth reading before a first change:

* [`CONTEXT.md`](./CONTEXT.md) is the vocabulary. Verve is strict about naming:
  a Panel is not a widget, a Measurement is not a sample, an Estimate is not a
  Metric. Code, docs and UI all use the same words, and reviews will ask you to
  as well.
* [`good_practices.md`](./good_practices.md) is the Go style the backend
  follows.

Structural decisions live in [`docs/adr/`](./docs/adr/), numbered and never
rewritten. If your change contradicts one, that is fine, but it needs a new ADR
rather than a quiet edit.

## Workflow

Issues live as markdown under `.scratch/<milestone>/`, with a PRD next to them
(see `docs/agents/issue-tracker.md`). The loop is:

1. One issue, one branch: `feat/...` or `fix/...`.
2. Implement, with tests at the seams the issue names.
3. Open a pull request referencing the issue.
4. CI green, review, merge.

`main` is protected: no direct pushes. Keep pull requests to one milestone
issue where you can; the history is meant to be readable as a sequence of
decisions.

## Cutting a release

A tag is the only input. Pushing `vX.Y.Z` triggers
[`release.yml`](.github/workflows/release.yml), which builds the static binaries
for every target and publishes the image to `ghcr.io/gauthier-se/verve`:

```sh
goreleaser build --snapshot --clean   # dry run first: no tag, no publish
git tag -a v0.1.0 -m "v0.1.0" && git push origin v0.1.0
```

The version is never written into a file. `main.version` defaults to `"dev"`,
the Makefile stamps `git describe`, the Dockerfile takes `ARG VERSION`, and
goreleaser stamps the tag: all four read from git or from a value that is
obviously not a release. The changelog is generated from commit subjects, which
is the other reason the subject line matters below.

Verve is on `0.x` and the leading zero is a statement about the API, not about
maturity: see [ADR 0029](docs/adr/0029-a-0x-tag-promises-the-data-not-the-interface.md)
for what a tag does and does not promise.

## Commit messages: Conventional Commits

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

Append `!` after the type or scope **and** add a `BREAKING CHANGE:` footer:

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

## Contributing a Connector

A **Connector** reads an external system and maps it into Verve's canonical
families. It is compiled into the binary rather than loaded as a plugin, and
its mapping (source type to Catalog metric, plus unit conversion) is
declarative data, so most of a connector is a table and the code is only "how
to read this format" (ADR 0009).

`internal/connector/applehealth/` is the reference: `mapping.go` is the table,
`families.go` decides which family each record becomes, `import.go` streams the
archive, and a test keeps the mapping in lock-step with the Catalog so a typo
in a slug fails the build rather than silently dropping data.

Two things to know before starting:

* The Catalog is closed and extensible: you add canonical metrics to
  `internal/catalog/`, you do not invent slugs inside a connector, and anything
  a source emits that Verve has no metric for lands in the unmapped bin instead
  of being discarded (ADR 0002).
* The interface and registry ADR 0009 describes are not built yet, because with
  a single connector there is nothing to abstract over. The second connector is
  what introduces that seam, so open an issue first and let us design it with
  you rather than around you.

## Contributing a Palette

A **Palette** is the named color set a **Mode** is painted in (CONTEXT.md). The
set is closed and curated: Verve defines the Palettes, the Account picks one
(ADR 0024), and the criterion for a new one is **recognition** rather than
variety, so in practice a proposal names a theme people already use elsewhere
(ADR 0026).

Adding one is data: two CSS blocks in `web/src/index.css`, one entry in
`PALETTES` in `web/src/components/appearance.tsx`, one row in
`THIRD-PARTY-NOTICES.md`. What takes the time is the checking below, because
some of Verve's colors are load-bearing: `--chart-1..4` are the identities of up
to four Metrics on one Panel (ADR 0020), and an unreadable Panel reads as a
Verve bug, not as the palette author's choice.

### What `make ci` checks for you

`internal/web/palette_test.go` fails the build if a palette omits a variant, if
its token set differs from Verve's in either direction, if it restates a
semantic token (`--destructive`, `--chart-positive`, `--chart-negative`), or if
`PALETTES` and `index.css` disagree. Those are the *silent* failures: nothing
looks broken, the palette is just quietly wrong.

### What you must check yourself

**Both variants exist.** Mode and Palette are orthogonal: a Palette that is dark
only breaks the axis. If the upstream theme has no light variant, derive one
from its published hues and say so in `THIRD-PARTY-NOTICES.md`.

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
differ by at least 30 degrees of hue **or** 12 points of lightness, and each
must clear 3:1 against `--card` (the WCAG floor for a graphical object). Prefer
the theme's own canonical accents, picked for spread rather than for
prettiness: a ramp of four neighbouring pastels makes a four-Metric Panel
unreadable, which is the failure this rule exists to prevent.

**Expect upstream values to miss these.** Building the nine-palette roster
turned up twelve real failures in colors taken straight from upstream projects.
Moving a value is fine and expected; leaving it unrecorded is not. A named
Palette is a *verified adaptation*, never a certified reproduction, so note
every departure in `THIRD-PARTY-NOTICES.md` under that palette.

**Licence and attribution.** Add the upstream project, its licence and its URL
to `THIRD-PARTY-NOTICES.md`, and a header comment above the palette's blocks in
`index.css`. Do not copy values from a paid product: Verve's light Dracula is
its own derivation precisely because Alucard ships with Dracula PRO.

## Reporting bugs and security issues

Bugs go to the issue tracker. Security vulnerabilities go through private
reporting instead: see [SECURITY.md](./SECURITY.md).
