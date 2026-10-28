# Single binary and embedded SPA

- **Status:** Planned
- **Milestone:** M0
- **Related:** [Configuration and deployment](platform-configuration-and-deployment.md), [ADR 0002](../decisions/0002-tanstack-router-without-start.md), [ADR 0004](../decisions/0004-huma-code-first-openapi.md)

## Summary

Verve ships as one executable named `verve`. It serves the REST API and the web app, runs background jobs and exposes administrative commands. The SPA is built with Vite and embedded in the binary at build time.

## User stories

- As a self-hoster, I want to download one file or one image and run it, without installing a runtime or a database.
- As an operator, I want CLI commands for imports, backups and user management that work inside the container.
- As a contributor, I want a fast local development loop for both the Go backend and the SPA.

## Scope

### In scope

- `verve serve` as the default command.
- Embedded SPA with client-side routing fallback.
- CLI subcommands sharing the same configuration and storage code as the server.
- Graceful shutdown.
- Development workflow with hot reload.

### Out of scope

- Built-in TLS (handled by a reverse proxy, see [Configuration and deployment](platform-configuration-and-deployment.md)).
- Plugins or dynamic extensions.

## Functional requirements

- **FR-1**: Running `verve` without arguments is equivalent to `verve serve`.
- **FR-2**: The following subcommands exist:

  | Command | Purpose |
  |---|---|
  | `serve` | Start the HTTP server and the job worker |
  | `migrate` | Apply pending migrations (also done automatically by `serve`). `migrate status` lists each migration with its applied time or `pending`. Both refuse to run while `serve` holds the data directory lock |
  | `import xml --user <name> <file>` | Import an Apple Health export, see [XML import](ingestion-xml-import.md) |
  | `import native --user <name> <file>` | Import a Verve native JSON file, for example an export from another instance, see [Data export](data-export.md) |
  | `backup` / `restore <file>` | See [Backups](backups.md) |
  | `user create`, `user list`, `user reset-password`, `user disable`, `user enable` | Break-glass user management |
  | `openapi` | Print the OpenAPI spec (used by the frontend build) |
  | `config check` | Validate configuration and print the effective values with secrets masked |
  | `healthcheck` | Call `/healthz` and exit non-zero on failure (for images without curl) |
  | `version` | Print version, commit and build date |

- **FR-3**: Static assets are served from an embedded filesystem. Hashed assets get `Cache-Control: public, max-age=31536000, immutable`. `index.html` gets `no-cache`.
- **FR-4**: Any `GET` request that is not under `/api/`, not a known static file and not a technical endpoint returns `index.html` so that client-side routes work on reload.
- **FR-5**: Unknown paths under `/api/` return a JSON 404 problem response, never `index.html`.
- **FR-6**: On `SIGTERM` or `SIGINT`, the server stops accepting connections, finishes in-flight requests (with a timeout of 30 seconds, after which the remaining connections are closed and a warning is logged), lets the job worker finish or release its current jobs, checkpoints the WAL and exits. A second signal kills the process immediately.
- **FR-7**: A lock file in the data directory (`verve.lock`) prevents two `serve` processes from using the same data directory: the second one exits with status 1 and `another Verve process is using <data dir>`. The operating system releases the lock when the process exits, so a crash never leaves a stale lock. `serve` creates the data directory with permissions `0750` when it does not exist. CLI commands that write coordinate through the database (for example the per-user import lock) and short transactions.
- **FR-8**: The binary reports its version in the UI footer, in `verve version` and in the `/readyz` response.
- **FR-9**: Development mode: `task dev` runs the Go server with live reload and the Vite dev server, with Vite proxying `/api`, `/healthz` and `/readyz` to Go. The production build (`task build`) builds the SPA, regenerates the API client, then compiles Go with the assets embedded.

## UX notes

- When the SPA cannot reach the API, it shows a dedicated "server unreachable" screen with a retry button.

## Data and API notes

- `verve openapi` must run without a database so that the frontend build does not need a running instance.

## Edge cases

- A deep link to a client route (for example `/workouts/123`) opened in a new tab must load the SPA, not a 404.
- A request for a missing hashed asset after an upgrade (old tab open) returns 404, and the SPA reloads itself when it detects a version change.

## Acceptance criteria

- [ ] `docker run` with a volume starts Verve and serves the web app on the configured port.
- [ ] Reloading any client route returns the SPA.
- [ ] `/api/does-not-exist` returns a JSON 404.
- [ ] Stopping the container does not corrupt the database and leaves no stale lock.
- [ ] All CLI subcommands work inside the container with `docker exec`.

## Future ideas

- Self-update notification (opt-in only, no default network calls).
