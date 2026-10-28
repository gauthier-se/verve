# Observability

- **Status:** Planned
- **Milestone:** M0 (health endpoints, logs), M4 (Prometheus metrics)
- **Related:** [Configuration and deployment](platform-configuration-and-deployment.md), [Audit log](audit-log.md)

## Summary

Verve exposes health endpoints for orchestrators, structured logs, and a Prometheus endpoint with technical metrics about the application itself. Health data never leaves through these channels.

## User stories

- As an operator, I want Docker or Kubernetes to restart Verve when it is unhealthy.
- As a homelabber with Prometheus and Grafana, I want to monitor import durations, job queues and backup status.

## Scope

### In scope

- `/healthz` and `/readyz`.
- Structured logs with request ids.
- `/metrics` in Prometheus format with technical metrics only.

### Out of scope

- Health values as Prometheus metrics (high cardinality, wrong tool, privacy).
- Distributed tracing.

## Functional requirements

- **FR-1**: `/healthz` (`GET` or `HEAD`) returns 200 `{"status":"ok"}` as long as the process is running and the HTTP server responds, including while migrations run at startup. No authentication.
- **FR-2**: `/readyz` returns 200 `{"status":"ready","version":"<version>"}` when every check passes, otherwise 503 `{"status":"not_ready","version":"<version>","reasons":[...]}` with one reason per failing check, prefixed with the name of the check. No authentication. The checks:
  - `database`: the writer and reader connection pools answer a ping within 2 seconds, for example `database: ping writer: <cause>`.
  - `migrations`: every embedded migration is applied, for example `migrations: 2 pending`.
  - `data_dir`: a temporary file can be created in the data directory, for example `data_dir: not writable: permission denied`. The check touches the disk at most once every 10 seconds and returns its last result in between.

  At startup, Verve listens before applying the migrations. Until they are applied, `/readyz` returns 503 with the single reason `starting: applying migrations`, and every other route returns a 503 problem with `code: "starting"`.
- **FR-3**: Logs use `log/slog` with configurable level and format (`VERVE_LOG_LEVEL`, `VERVE_LOG_FORMAT`): JSON lines with `time`, `level` and `msg`, or slog's text format. Each HTTP request logs one line at info level with `request_id`, `method`, `route` (the matched pattern such as `GET /api/v1/meta`, or `unmatched`), `path` (without query string), `status`, `duration_ms`, `bytes` and `client_ip`. Requests to `/healthz` and `/readyz` are logged at debug level only.
  - The request id is the incoming `X-Request-Id` when the request comes from a trusted proxy (`VERVE_TRUSTED_PROXIES`) and the value matches `^[A-Za-z0-9._-]{1,64}$`, otherwise a random 16-byte hex id. It is returned in the `X-Request-Id` response header, and every line logged while handling the request carries it.
  - A panic in a handler is logged at error level with its value, stack trace and request id, and answered with 500: a problem body (`code: "internal"`) under `/api/`, plain text elsewhere, without the caching, encoding and cookie headers the handler had set. When the response has already started (written or flushed), it is aborted instead. The server keeps serving.
- **FR-4**: Logs never contain health values, tokens, passwords, session ids or full request bodies. As a safety net, the value of any log attribute whose key contains, case-insensitively, `password`, `passwd`, `secret`, `token`, `authorization`, `cookie`, `session` or `api_key` is written as `[REDACTED]`, including inside groups, and so are all the attributes of a group opened under such a name.
- **FR-5**: `/metrics` (M4) exposes:
  - HTTP requests count and latency by route pattern and status class,
  - ingestion counts by origin (`xml`, `hae`, `native`) and outcome (`inserted`, `updated`, `duplicate`, `blocked`, `invalid`),
  - import and job durations by kind, queue depth, failed jobs,
  - last successful backup timestamp and backup size,
  - database file size and WAL size,
  - standard Go runtime and process metrics.
- **FR-6**: Metrics never have user labels (no usernames, no user ids).
- **FR-7**: `/metrics` is either served on a separate listener (`VERVE_METRICS_ADDR`) or protected by a dedicated bearer token (`VERVE_METRICS_TOKEN`). It is disabled when neither is set.

## UX notes

- The admin page shows a compact system status: version, uptime, database size, last backup, job queue, failed jobs.

## Data and API notes

- No storage needed beyond counters in memory.

## Edge cases

- During a long migration at startup, `/healthz` is up and `/readyz` returns 503 so orchestrators do not route traffic too early.

## Acceptance criteria

- [ ] Docker marks the container unhealthy when the server hangs.
- [ ] `/readyz` returns 503 when the database file is unreadable.
- [ ] `/metrics` output contains no username, no user id and no health value.

## Future ideas

- Example Grafana dashboard for Verve technical metrics.
