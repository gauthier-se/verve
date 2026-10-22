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

- **FR-1**: `/healthz` returns 200 as long as the process is running and the HTTP server responds. No authentication.
- **FR-2**: `/readyz` returns 200 when the database is reachable, migrations are applied and the data directory is writable, otherwise 503 with a short JSON reason. It includes the version. No authentication.
- **FR-3**: Logs use `log/slog` with configurable level and format. Each HTTP request log line contains a request id, method, route pattern, status and duration.
- **FR-4**: Logs never contain health values, tokens, passwords, session ids or full request bodies.
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
