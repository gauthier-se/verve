# Architecture

This document describes how Verve is put together. Feature-level behavior lives in [features/](features/), and the reasoning behind the main choices lives in [decisions/](decisions/).

## Goals and constraints

- **Single binary**: one executable contains the API, the web app, the job worker and the CLI. The only state is the data directory.
- **Self-hosted first**: no external service is required to run Verve. Optional integrations (OIDC, SMTP, ntfy, map tiles) are opt-in or replaceable.
- **Privacy**: no telemetry, no update checks by default, health values never appear in logs, metrics or notifications unless the user opts in.
- **Small hardware**: must run comfortably on a Raspberry Pi 4. Targets: idle memory under 100 MB, streaming XML import under 300 MB peak memory.
- **Household scale**: a handful of users, tens of millions of samples per user over many years.

## Component overview

```
                         +--------------------------------------------------------------+
                         |                         verve binary                         |
                         |                                                              |
 Browser / PWA  <------->|  HTTP server (net/http)                                      |
                         |   |- static SPA (embed.FS, index.html fallback)              |
                         |   |- REST API /api/v1 (Huma, OpenAPI 3.1)                    |
                         |   |- auth middleware (session cookie, Bearer token,          |
                         |   |                   trusted proxy header)                  |
                         |   |- /healthz /readyz /metrics                               |
                         |                                                              |
 Health Auto Export ---->|  Ingestion                                                   |
 XML upload (tus) ------>|   |- adapters: xmlexport, healthautoexport, native           |
 CLI import ------------>|   |- pipeline: normalize, resolve source, block rules,       |
                         |   |            dedupe, batch upsert, mark dirty ranges       |
                         |                                                              |
                         |  Job worker (persistent jobs table)                          |
                         |   |- imports, aggregate recompute, records, energy           |
                         |   |- weekly check-ins, notifications, backups, exports       |
                         |                                                              |
                         |  Domain services                                             |
                         |   |- catalog (metric types, units)   |- sleep               |
                         |   |- aggregates                      |- workouts            |
                         |   |- energy                          |- plans               |
                         |   |- notify                          |- audit               |
                         |                                                              |
                         |  Storage interfaces  -->  SQLite implementation (sqlc)       |
                         +-------------------------------|------------------------------+
                                                         v
                                              /data/verve.db (WAL)
```

## Repository layout (proposed)

```
cmd/verve/                 entry point and CLI commands (cobra)
internal/
  app/                     dependency wiring, lifecycle
  config/                  env and YAML loading, validation
  httpapi/                 Huma operations, middleware, SPA handler
  auth/                    passwords, sessions, tokens, totp, oidc, proxy header
  ingest/                  pipeline
    xmlexport/             Apple Health export.zip adapter
    healthautoexport/      Health Auto Export JSON adapter
    native/                Verve native ingest format
  catalog/                 metric type catalog, unit conversion
  aggregate/               hourly and daily aggregates, source selection
  sleep/                   night assembly, stages, statistics
  workouts/                zones, splits, best efforts, duplicates
  energy/                  expenditure estimation
  plans/                   plans, targets, check-ins
  notify/                  channels and events
  jobs/                    persistent job queue and worker
  backup/                  scheduled backups, restore
  audit/                   audit events
  store/                   storage interfaces (repositories)
  store/sqlite/            SQLite implementation, sqlc queries, goose migrations
web/                       React SPA (Vite)
  src/routes/              TanStack Router file-based routes
  src/api/                 generated OpenAPI client and TanStack Query hooks
  src/components/charts/   chart wrapper components (the only place importing TanStack Charts)
  src/locales/             translation files
docs/                      this documentation
```

## Backend

| Concern | Choice |
|---|---|
| Language | Go 1.27 |
| HTTP | `net/http` with method and path patterns |
| API | Huma v2, code-first, generates OpenAPI 3.1, validates inputs, RFC 9457 errors ([ADR 0004](decisions/0004-huma-code-first-openapi.md)) |
| Database | SQLite in WAL mode via `modernc.org/sqlite` (pure Go, no CGO) ([ADR 0001](decisions/0001-sqlite-behind-storage-interface.md)) |
| Queries | sqlc generated code behind repository interfaces |
| Migrations | goose, SQL files embedded in the binary, applied at startup after an automatic backup |
| Background work | in-process worker backed by a `jobs` table, survives restarts, no external broker |
| Auth | argon2id, server-side sessions, `coreos/go-oidc`, `pquerna/otp` ([ADR 0005](decisions/0005-server-side-sessions.md)) |
| Logging | `log/slog`, JSON or text |
| CLI | cobra, subcommands of the same binary |
| Release | goreleaser, GitHub Actions, Taskfile for local development |

### Storage interfaces

Domain code depends on repository interfaces declared in `internal/store`, never on SQLite directly. The SQLite implementation lives in `internal/store/sqlite`. A Postgres implementation is not planned but must remain possible without touching domain code. Rules:

- No SQLite-specific types or functions leak out of `internal/store/sqlite`.
- Transactions are exposed through a `store.Tx` abstraction (unit of work), not `*sql.Tx`.
- Time values cross the interface as `time.Time` in UTC. Each implementation chooses its own on-disk representation.
- Aggregation logic that needs heavy SQL stays inside the implementation behind intent-revealing methods (for example `RecomputeHourly(ctx, userID, typeID, from, to)`).

### Job worker

A single worker pool polls the `jobs` table. Jobs have a kind, a JSON payload, a status, an attempt counter, a `run_at` time and a lease (`locked_until`). Failed jobs are retried with exponential backoff, then marked failed and surfaced to admins. Kinds include `import.xml`, `import.hae`, `aggregate.recompute`, `workouts.records`, `energy.daily`, `plan.checkin`, `notify.deliver`, `backup.run`, `export.build`, `cleanup.uploads`, `cleanup.sessions`.

SQLite allows one writer at a time. Writers use short transactions (bounded batches) so that API requests are never blocked for long. A `busy_timeout` is set on every connection.

## Frontend

| Concern | Choice |
|---|---|
| Framework | React 19 SPA built with Vite 8 |
| Routing | TanStack Router 1.x, file-based routes through the Vite plugin, typed search params ([ADR 0002](decisions/0002-tanstack-router-without-start.md)) |
| Server state | TanStack Query 5 |
| Tables | TanStack Table 9 |
| Forms | TanStack Form 1 |
| Charts | TanStack Charts 1.x behind wrapper components ([ADR 0003](decisions/0003-tanstack-charts-behind-wrappers.md)) |
| API client | generated from the OpenAPI spec with Hey API (`@hey-api/openapi-ts`) and its TanStack Query plugin |
| UI | Tailwind CSS 4 and shadcn/ui |
| Dashboard grid | react-grid-layout |
| Internationalization | i18next with react-i18next, i18next-cli ([ADR 0009](decisions/0009-i18next.md)) |
| Tests | Vitest, Testing Library, MSW, Playwright |
| Maps | MapLibre GL, optional PMTiles |

Routing state that users may want to bookmark or share (metric, period, granularity, filters) lives in typed search params, not in component state.

The OpenAPI spec is produced by the binary itself (`verve openapi > web/openapi.json`) and the client is regenerated as part of the build. A change to a Go API type that breaks the frontend fails the frontend type check.

## Data flows

### Ingestion

```
source payload
  -> adapter (xmlexport | healthautoexport | native): parse in streaming mode into normalized records
  -> catalog: map type identifiers, convert units to canonical units, parse time with offset
  -> sources: find or create the source for this user
  -> block rules: drop matching records, count them
  -> dedupe: compute the natural key, upsert in batches
  -> dirty ranges: record (type, time range) per user, enqueue aggregate.recompute
  -> import report: received, inserted, updated, duplicates, blocked, invalid, unsupported
```

See [Ingestion pipeline](features/ingestion-pipeline.md).

### Reads

Charts read from precomputed aggregates whenever the requested granularity allows it (hour, day, week, month, year). Raw samples are read only for short ranges (a single day, a workout, a night of sleep). The API downsamples long raw series server-side so the frontend never receives more points than it can draw.

## Conceptual data model

Every user-owned row carries a `user_id`. All queries are scoped by user, and the storage layer exposes no method that reads health data across users.

| Entity | Purpose |
|---|---|
| `users` | Account, role (`admin`, `member`), status |
| `invitations` | Single-use invitation and password reset links (hashed) |
| `user_preferences`, `user_profile` | Units, timezone, language, theme, week start, sleep window, date of birth, sex, height, heart rate settings |
| `sessions` | Server-side sessions (hashed id, method, expiry, last seen, IP, user agent) |
| `api_tokens` | Hashed tokens with scopes and usage metadata |
| `oidc_identities`, `totp_secrets`, `recovery_codes` | Advanced authentication |
| `metric_types` | Catalog: key, HealthKit identifier, kind (quantity or category), canonical unit, aggregation style (cumulative or discrete) |
| `sources` | Devices and apps that produced data, with a detected kind (watch, phone, scale, app, aggregated) |
| `samples` | Generic quantity and category samples: type, source, start, end, original UTC offset, value, optional min and max for pre-aggregated points, category value, origin, import id, natural key, metadata |
| `workouts` | Activity type, time range, totals, source, external id, metadata |
| `workout_routes` | One compressed, columnar encoded route per workout, plus bounding box and simplified polyline |
| `agg_hourly`, `agg_daily` | Precomputed aggregates after source selection |
| `source_aliases` | Merged sources (old and new device names) |
| `source_priorities` | Per user, per metric ordered list of sources, ignored sources, combine flag |
| `block_rules` | Sample, source, type or source and type rules, optional time range |
| `imports`, `uploads` | Import history and resumable uploads |
| `jobs` | Persistent background jobs |
| `sleep_nights` | Assembled nights with stage statistics |
| `personal_records` | Best efforts and records per sport, with history |
| `energy_estimates` | Daily maintenance estimates with method and confidence |
| `plans`, `plan_checkins` | Goals, targets and weekly check-ins |
| `dashboards` | Custom dashboard definitions and layouts |
| `exports` | Generated user exports and their expiry |
| `notification_channels`, `notification_subscriptions`, `notification_deliveries` | Notifications |
| `audit_events` | Audit log |
| `instance_settings` | Admin-managed settings stored in the database |

## Cross-cutting rules

### Time

- Samples are stored in UTC with their original UTC offset, so a sample can be shown in the local time where it was recorded.
- "Today", daily aggregates and weekly boundaries are computed in the user's timezone preference. Changing the timezone triggers a recompute of daily aggregates.
- Sleep is attributed to nights using a configurable window (default 18:00 to 18:00, attributed to the wake-up date).

### Units

Values are stored in canonical units and converted only for display.

| Dimension | Canonical unit |
|---|---|
| Mass | kg |
| Length, distance | m |
| Energy | kcal |
| Duration | s |
| Heart rate, respiratory rate | count/min |
| Heart rate variability | ms |
| Temperature | degC |
| Percentages (SpO2, body fat) | fraction (0 to 1) |
| Blood glucose | mg/dL |
| Blood pressure | mmHg |
| Speed | m/s |
| VO2max | mL/(kg.min) |

### Security baseline

- Strict security headers: Content Security Policy (with the map tile host added only when configured), `X-Content-Type-Options`, `Referrer-Policy`, `frame-ancestors 'none'`, `Permissions-Policy`.
- Session cookies are `HttpOnly`, `Secure` when `VERVE_PUBLIC_URL` is HTTPS, `SameSite=Lax`, and use the `__Host-` prefix when possible.
- State-changing requests require a custom header and a matching `Origin` (CSRF protection).
- Secrets at rest (TOTP secrets, OIDC client secret, SMTP password, webhook secrets) are encrypted with a key from `VERVE_SECRET_KEY`, or generated once in the data directory.
- `X-Forwarded-*` headers are trusted only from `VERVE_TRUSTED_PROXIES`.
- Logs never contain health values, tokens, passwords or session ids.

### Errors

API errors use RFC 9457 problem details with a stable machine-readable `code`. The frontend translates codes into localized messages.

## Testing strategy (proposed)

- Unit tests for pure computations: unit conversion, source selection, sleep assembly, zones, splits, best efforts, energy estimation, plan targets.
- Integration tests against a real SQLite database for repositories and the ingestion pipeline.
- Anonymized fixture exports (small `export.zip` and Health Auto Export payloads, version 1 and 2) checked into the repository.
- API contract: the generated OpenAPI spec is committed and diffed in CI.
- End-to-end tests for critical flows (setup, import, Today page) once M2 is reached.
