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
  | `openapi` | Print the OpenAPI spec as JSON (used by the frontend build), without loading the configuration or opening the data directory |
  | `config check` | Validate configuration and print the effective values with secrets masked |
  | `healthcheck` | Call `/healthz` on the configured listen address, through the IPv4 loopback when its host is empty or unspecified (`:8080`, `0.0.0.0:8080`, `[::]:8080`). Exit 1 with the reason on stderr on any status other than 200, a connection error or no answer within 3 seconds (for images without curl) |
  | `version` | Print version, commit and build date |

- **FR-3**: Static assets are served from an embedded filesystem. Hashed assets (everything Vite writes under `assets/`) get `Cache-Control: public, max-age=31536000, immutable`. `index.html` and the other files get `no-cache`. Every file has a strong `ETag` derived from its content, and a request with a matching `If-None-Match` gets a 304 without body. Content types come from an explicit table for the web file extensions (`.html`, `.js`, `.mjs`, `.css`, `.json`, `.svg`, `.png`, `.ico`, `.webmanifest`, `.woff2`, `.txt`), then from the system table, then `application/octet-stream`.
- **FR-4**: Any `GET` or `HEAD` request that is not under `/api/`, not a known static file and not a technical endpoint returns `index.html` so that client-side routes work on reload. The exceptions return 404: paths under `/assets/` (a missing asset is never answered with `index.html`), paths with a segment starting with `.` (dotfiles are never served) and invalid paths such as encoded traversals (`/..%2f..%2fetc/passwd`). Directories are never listed. Other methods return 405 with `Allow: GET, HEAD`.
- **FR-5**: Unknown paths under `/api/` return a JSON 404 problem response, never `index.html`, whatever the method: `{"type":"about:blank","title":"Not Found","status":404,"code":"not_found"}` with `Content-Type: application/problem+json`.
- **FR-6**: On `SIGTERM` or `SIGINT`, the server stops accepting connections, finishes in-flight requests (with a timeout of 30 seconds, after which the remaining connections are closed and a warning is logged), lets the job worker finish or release its current jobs, checkpoints the WAL and exits. A second signal kills the process immediately.
- **FR-7**: A lock file in the data directory (`verve.lock`) prevents two `serve` processes from using the same data directory: the second one exits with status 1 and `another Verve process is using <data dir>`. The operating system releases the lock when the process exits, so a crash never leaves a stale lock. `serve` creates the data directory with permissions `0750` when it does not exist. CLI commands that write coordinate through the database (for example the per-user import lock) and short transactions. Commands that work next to `serve` open the database in shared mode: they do not take the lock and apply no migration. A schema older than the binary fails with `database schema is at version X, this binary needs Y: start verve serve or run verve migrate`, and a newer one with the refusal of `migrate`.
- **FR-8**: The binary reports its version in the UI footer, in `verve version` and in the `/readyz` response.
- **FR-9**: Development mode: `task dev` runs the Go server with live reload and the Vite dev server, with Vite proxying `/api`, `/healthz` and `/readyz` to Go. The production build (`task build`) builds the SPA, regenerates the API client, then compiles Go with the assets embedded. The Vite output (`web/dist`) is synced into `internal/webui/dist`, the embedded directory, by `tools/syncdist`, which removes stale files and keeps the committed `.gitignore`. A binary compiled without the SPA (only that `.gitignore`) answers `index.html` requests with a minimal HTML page explaining that the web UI is not built and to run `task build`, and `serve` logs a warning at startup.

## UX notes

- When the SPA cannot reach the API (a network error, 502 or 504 from a reverse proxy, or a 503 that does not come from Verve, as a proxy answers when its backend is down), it shows a dedicated "server unreachable" screen with a retry button and retries automatically every 5 seconds. While the server applies its migrations (503 with the code `starting`), it shows a "Verve is starting" screen, retried the same way. The layout comes back without reload once the API answers.
- The footer shows `Verve <version>` from `/api/v1/meta`.
- The dark theme follows the system preference (`prefers-color-scheme`) until the theme preference arrives in M2.

## Data and API notes

- `verve openapi` must run without a database so that the frontend build does not need a running instance.

## Edge cases

- A deep link to a client route (for example `/workouts/123`) opened in a new tab must load the SPA, not a 404.
- A request for a missing hashed asset after an upgrade (old tab open) returns 404, and the SPA reloads itself when it detects a version change: it refetches `/api/v1/meta` on window focus and every 5 minutes, and once the version differs from the one it loaded with, the next client-side navigation is a full page load of the target URL.

## Acceptance criteria

- [ ] `docker run` with a volume starts Verve and serves the web app on the configured port.
- [ ] Reloading any client route returns the SPA.
- [ ] `/api/does-not-exist` returns a JSON 404.
- [ ] Stopping the container does not corrupt the database and leaves no stale lock.
- [ ] All CLI subcommands work inside the container with `docker exec`.

## Future ideas

- Self-update notification (opt-in only, no default network calls).
