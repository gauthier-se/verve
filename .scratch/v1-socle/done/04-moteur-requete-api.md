# 04 — Query engine + aggregated-bucket JSON API

Status: ready-for-agent
Blocked by: 02

## Goal

Expose the data via a JSON API that returns **only server-side aggregated
buckets** (never a raw series) — the foundation of every graph.

## Scope

- **HTTP server** (`net/http` stdlib, `ServeMux` method+pattern, Go ≥ 1.22):
  `verve serve`; JSON helpers (`writeJSON`/`readJSON`), `Validator`, centralized
  error responses, `recoverPanic`, graceful shutdown, slog.
- **Query engine** (`internal/query`): a `metric + time range + bucket` request →
  per-bucket SQL aggregation by the Metric's rule (`sum` / `average` +min/max /
  `latest` / `duration_by_state`). Returns ≤ a few hundred points.
- **Source priority**: read-time resolution (ADR 0003). Per-Metric priority,
  configurable; sensible default (e.g. Watch > iPhone for `steps`).
- **Capped resolution**: reject/clamp a too-fine bucket so the raw series is
  never returned.
- **Account scoping**: every query filtered by owner. Until auth (slice 05), the
  target account may come from a dev flag/header.
- Endpoints: `GET /v1/metrics` (Catalog exposed), `GET /v1/series` (buckets),
  `GET /v1/healthz`.

## Out of scope

Auth (05), UI (06), period comparison / cross-metric (post-v1).

## Acceptance

- `GET /v1/series?metric=steps&range=…&bucket=day` returns steps **summed** per
  day; `heart_rate&bucket=day` returns average + min/max.
- A one-year range returns ~365 points regardless of underlying raw volume.
- Requesting a bucket below the resolution cap is rejected cleanly.

## Refs

ADR 0002 (aggregation rules), 0003 (source priority), 0007 (scoping), 0012.
