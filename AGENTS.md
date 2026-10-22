# Agent guide

Instructions for coding agents (and humans) working on Verve. Read this file before starting any task.

## Project

Verve is a self-hosted health dashboard for Apple Health data: a single Go binary with an embedded React SPA and a SQLite database.

- Product overview: [README.md](README.md)
- Architecture, data model, cross-cutting rules: [docs/architecture.md](docs/architecture.md)
- Why the main choices were made: [docs/decisions/](docs/decisions/README.md)
- Feature specifications with numbered requirements (`FR-n`): [docs/features/](docs/features/)
- Milestones: [docs/roadmap.md](docs/roadmap.md)
- Commit conventions: [CONTRIBUTING.md](CONTRIBUTING.md)

## Workflow

1. Work only on GitHub issues labeled `agent-ready`. One issue, one branch, one pull request.
2. Read the whole issue, then every spec section, requirement and decision record it links to. When the issue and the spec disagree, stop and ask in the issue instead of guessing.
3. Check the `Dependencies` section: do not start if a blocking issue is still open.
4. Branch name: `<type>/<issue number>-<short-slug>`, for example `feat/12-spa-fallback`.
5. Follow the TDD rules below.
6. Open a pull request using the template. Title in Conventional Commits format (it becomes the squashed commit message). Body contains `Closes #<issue number>`.
7. Stay within the issue scope. Anything else you notice goes into a new issue or a comment, not into the pull request.

## Test-driven development

TDD is mandatory for all production code.

1. Write a failing test first and run it. It must fail for the expected reason (not a compilation error unrelated to the behavior, not a typo).
2. Write the minimum code to make it pass.
3. Refactor with all tests green.
4. Name tests after the Given / When / Then scenarios of the issue's acceptance criteria. Every acceptance criterion is covered by at least one test.
5. The first commit of a pull request contains only tests (`test: ...`). CI is expected to fail on that commit. Implementation follows in later commits. This lets the reviewer verify that tests came first.
6. Follow the issue's `TDD plan` order. If the plan turns out to be wrong, explain why in the pull request.

## Commands

Defined in `Taskfile.yml` once the project skeleton exists.

| Command | Purpose |
|---|---|
| `task dev` | Go server with live reload and Vite dev server |
| `task test` | All Go and web unit and integration tests |
| `task lint` | All linters and format checks |
| `task generate` | sqlc code, OpenAPI spec, TypeScript API client, i18n types |
| `task build` | Production binary with the embedded SPA |
| `task e2e` | Playwright end-to-end tests against the built binary |

Run `task lint` and `task test` before pushing.

## Go conventions

- Go 1.27. Module `github.com/gauthier-se/verve`. Layout as described in [docs/architecture.md](docs/architecture.md).
- `context.Context` is the first parameter of anything that does I/O.
- Wrap errors with context: `fmt.Errorf("open database: %w", err)`. No panics outside `main` for expected failures.
- No package-level mutable state. Dependencies are injected, including time through a `Clock` interface.
- Domain code accesses data only through the interfaces in `internal/store`. Nothing outside `internal/store/sqlite` imports the SQLite driver.
- Logging with `log/slog`. Never log health values, tokens, passwords, session ids or secrets.
- Tests: `testing` with `github.com/stretchr/testify/require`, table-driven, `t.Parallel()` when possible. Database tests use a real SQLite file in `t.TempDir()`, never a mocked database. Prefer handwritten fakes to generated mocks. Parsers use golden files in `testdata/` with an `-update` flag.

## Frontend conventions

- React 19, TypeScript strict mode, no `any`. Package manager: pnpm.
- Routes live in `web/src/routes/` (TanStack Router file-based routing). URL-worthy state (period, metric, filters) goes into typed search params.
- Call the API only through the generated client in `web/src/api/`. Never handwrite fetch calls to `/api`.
- Charts only through the wrapper components in `web/src/components/charts/`. Nothing else imports the chart library.
- Every user-facing string goes through i18next. No hardcoded text in components.
- UI built with Tailwind CSS and shadcn/ui components. Accessible by default: labels, keyboard navigation, sufficient contrast.
- Tests: Vitest, Testing Library with `user-event`, MSW for API responses.

## Generated code

Never edit generated files by hand (sqlc output, OpenAPI spec, API client, i18n types). Change the source and run `task generate`. CI fails when generated files are out of date.

## Dependencies

Use the libraries and versions named in the issue. Adding any other dependency requires a justification in the pull request description.

## Documentation

- When behavior or configuration changes, update the matching feature spec in `docs/features/` in the same pull request.
- A new structural decision gets a new record in `docs/decisions/`.
- Documentation is in English. Do not use the em dash character.

## Commits and pull requests

- Conventional Commits, see [CONTRIBUTING.md](CONTRIBUTING.md).
- Do not add AI attribution: no `Co-Authored-By` trailers for AI tools and no "Generated with" footers in commits or pull requests.
- Never commit secrets, real health data or personal exports. Test fixtures must be synthetic or anonymized.

## Definition of done

- Every acceptance criterion is covered by a test written first.
- `task lint` and `task test` pass locally, CI is green.
- Docs updated when behavior or configuration changed.
- Pull request title in Conventional Commits format, body contains `Closes #<issue number>`.
