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

## Workflow

Issues live as markdown under `.scratch/<feature>/` (see
`docs/agents/issue-tracker.md`). One issue → a `feat/…` or `fix/…` branch →
implement (tests at agreed seams) → open a PR referencing the issue → CI green +
review → merge. `main` is protected: no direct pushes.
