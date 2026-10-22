# Contributing

## Workflow

- Work is tracked in GitHub issues: epics group user stories (as sub-issues), and milestones `M0` to `M5` follow the [roadmap](docs/roadmap.md).
- Issue titles carry a stable reference: epics `[M<milestone>-E<epic>] <name>` (for example `[M0-E5] HTTP server and embedded SPA`), stories `[M<milestone>-<epic>.<story>] <summary>` (for example `[M0-5.3] Add the Huma API skeleton`). A new story takes the next free number in its epic. The number is a reference, not an execution order: dependencies define the order. Bugs are not numbered.
- A story is ready to be implemented once it carries the `agent-ready` label, after review.
- One story, one branch (`<type>/<issue number>-<short-slug>`), one pull request, squash merged.
- Development is test-driven. The rules, commands and conventions are in [AGENTS.md](AGENTS.md), which applies to humans as well as coding agents.

## Commit messages

Verve follows [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/). The release changelog is generated from commit messages, so the format matters.

```
<type>(<optional scope>): <description>

<optional body>

<optional footer(s)>
```

### Types

| Type | Use for |
|---|---|
| `feat` | A new user-facing feature |
| `fix` | A bug fix |
| `docs` | Documentation only |
| `refactor` | Code change that neither fixes a bug nor adds a feature |
| `perf` | Performance improvement |
| `test` | Adding or fixing tests |
| `build` | Build system, dependencies, Docker image |
| `ci` | CI configuration |
| `style` | Formatting, no code change |
| `chore` | Maintenance that fits nowhere else |
| `revert` | Reverts a previous commit |

### Scopes

Optional, lowercase, one area of the codebase. Scopes match the `area: *` labels on GitHub: `api`, `web`, `server`, `cli`, `config`, `store`, `ingest`, `auth`, `jobs`, `sleep`, `workouts`, `energy`, `plans`, `notify`, `backup`, `audit`, `i18n`, `obs`, `deploy`, `ci`, `docs`. Dependabot uses `deps` and `deps-dev` (for example `build(deps): bump ...`).

### Rules

- Description in English, imperative mood, lowercase first letter, no trailing period, 72 characters maximum for the first line.
- Breaking changes add `!` after the type or scope and a `BREAKING CHANGE:` footer explaining the migration.
- Reference feature requirements when relevant, for example `Refs: ingestion-pipeline FR-6`.
- Pull request titles follow the same format, since pull requests are squash merged.

### Examples

```
feat(ingest): accept Health Auto Export version 2 payloads
fix(sleep): attribute naps outside the sleep window to the right day
docs: add decision record for the storage interface
feat(api)!: rename series bucket parameter

BREAKING CHANGE: the `granularity` query parameter is now `bucket`.
```
