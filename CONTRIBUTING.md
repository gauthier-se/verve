# Contributing

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

Optional, lowercase, one area of the codebase: `api`, `web`, `ingest`, `store`, `auth`, `jobs`, `sleep`, `workouts`, `energy`, `plans`, `notify`, `backup`, `audit`, `i18n`, `deploy`, `docs`.

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
