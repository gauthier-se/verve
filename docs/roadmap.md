# Roadmap

Everything listed in M0 to M5 is in scope. The order is designed so that Verve becomes useful day to day as early as possible (end of M2), while safety features such as backups and the audit log land before real data does.

## M0: Foundations

An empty but deployable shell.

| Feature | Notes |
|---|---|
| [Single binary and embedded SPA](features/platform-single-binary.md) | Go binary, embedded web app, CLI subcommands |
| [Configuration and deployment](features/platform-configuration-and-deployment.md) | Env vars, YAML, Docker image, goreleaser |
| [Observability](features/platform-observability.md) | `/healthz` and `/readyz` in M0, `/metrics` in M4 |
| [Internationalization](features/platform-i18n.md) | English and French from day one |

## M1: Data

All of your Apple Health data in Verve, deduplicated and backed up.

| Feature | Notes |
|---|---|
| [Job worker](features/platform-jobs.md) | Persistent jobs, retries, recurring tasks |
| [Accounts and administration](features/accounts-and-administration.md) | Setup wizard, invitations, roles, account deletion |
| [Local accounts and sessions](features/auth-local-accounts-and-sessions.md) | argon2id, server-side sessions, CSRF |
| [API tokens](features/api-tokens.md) | Scoped Bearer tokens (`ingest`, `read`) |
| [Ingestion pipeline and native API](features/ingestion-pipeline.md) | Shared pipeline, native ingest format |
| [Apple Health XML import](features/ingestion-xml-import.md) | Resumable upload and CLI import |
| [Health Auto Export endpoint](features/ingestion-health-auto-export.md) | Continuous sync |
| [Storage and aggregates](features/data-storage-and-aggregates.md) | Generic samples, workouts, routes, aggregates |
| [Deduplication and source priority](features/data-deduplication-and-source-priority.md) | Idempotency, one source per metric per hour |
| [Deletion and block rules](features/data-deletion-and-block-rules.md) | Delete means block by default |
| [Backups](features/backups.md) | Scheduled `VACUUM INTO`, CLI backup and restore |
| [Audit log](features/audit-log.md) | Security and data management events |

### Scope of M1

- **Timezone**: the timezone preference moves to M1, because aggregates depend on it. It is asked at first login. The other preferences stay in M2.
- **Screens**: M1 ships the navigation shell and the screens for setup, login, invitations, password reset, API tokens, XML upload and import history. The other features are first available through the API or the CLI, and their screens (sessions, sources, source priorities, block rules, audit, backups, administration) close the milestone, so that they never block the data work. Acceptance criteria that mention a page of M2, such as the metric explorer, are verified through the API in M1.
- **Deferred requirements**: a requirement that depends on a later feature carries its milestone in the spec: password reset by email and notifications (M3), disabling local login (M5), computations derived from data such as sleep nights, records and energy estimates (M2, M3).
- **Validation**: CI checks memory and time on a synthetic export. Acceptance criteria that need a Raspberry Pi 4 or a real export are checked by hand at the end of the milestone. Real data stays out of the repository.

## M2: Dashboard

First daily use.

| Feature | Notes |
|---|---|
| [User preferences and profile](features/user-preferences-and-profile.md) | Units, language, theme, profile (timezone ships in M1) |
| [Today page](features/page-today.md) | Key metrics of the day with trends |
| [Metric explorer](features/page-metric-explorer.md) | Any stored metric, any period |
| [Sleep page](features/page-sleep.md) | Hypnogram, durations, regularity |
| [Workouts](features/page-workouts.md) | List, detail with map, zones, records, volume |
| [Maps](features/maps.md) | MapLibre, configurable or self-hosted tiles |
| [PWA and mobile](features/pwa-and-mobile.md) | Mobile first, installable, no offline mode |

## M3: Goals

Cut and bulk tracking.

| Feature | Notes |
|---|---|
| [Energy expenditure estimation](features/goals-energy-expenditure.md) | Sensor and adaptive methods |
| [Plans and weekly check-ins](features/goals-plans-and-check-ins.md) | Targets, macros, trajectory, adherence |
| [Notifications](features/notifications.md) | ntfy, webhook, SMTP |

## M4: Depth

The complete product.

| Feature | Notes |
|---|---|
| [Heart page](features/page-heart.md) | Resting HR, HRV, VO2max, zones, vitals |
| [Body page](features/page-body.md) | Weight trend, body composition, BMI |
| [Custom dashboards](features/custom-dashboards.md) | Widget grid, multiple dashboards |
| [Data export](features/data-export.md) | CSV and re-importable JSON |
| [Public API](features/public-api.md) | Documented OpenAPI, read scope |
| [Observability](features/platform-observability.md) | Prometheus `/metrics` |

## M5: Advanced authentication

Homelab identity integration.

| Feature | Notes |
|---|---|
| [TOTP two-factor authentication](features/auth-totp.md) | Recovery codes, optional enforcement |
| [OIDC](features/auth-oidc.md) | Authorization Code + PKCE, account linking |
| [Trusted proxy header](features/auth-proxy-header.md) | Forward-auth with Authelia or Authentik |

## Future

Not scheduled. Each item should get its own feature file before work starts.

| Idea | Notes |
|---|---|
| [iOS companion app](features/future-ios-companion-app.md) | HealthKit background delivery, native ingest format |
| Apple Shortcuts integration | Lightweight push for a few metrics |
| Sharing between accounts | Share all or part of your data with another account |
| Home Assistant integration | MQTT discovery or custom integration (the REST API covers basic sensors until then) |
| Training load | TRIMP, acute to chronic ratio, fitness and fatigue |
| Workout comparison | Compare two sessions on the same route |
| GPX and FIT import | Workouts recorded outside Apple Health |
| Apprise notifications | One URL for dozens of services |
| Helm chart | Kubernetes deployment |
| Route privacy zones | Hide start and end of routes around home |
| More data types | ECG, clinical records (FHIR), audiograms, medications, state of mind, activity rings |
| Threshold alerts | User-defined thresholds on health metrics (no "smart" anomaly detection) |
| Dashboard import and export | Share dashboard layouts as JSON |
| Postgres storage backend | Second implementation of the storage interfaces |
| Calibrated daily expenditure | Sensor-based daily values scaled by the adaptive estimate |
| Manual data entry | Measurements not available in Apple Health |
| Multiple OIDC providers, RP-initiated logout | Advanced OIDC |
| Off-site backup targets | S3-compatible storage |
| Notification quiet hours | Delay non-urgent notifications |
