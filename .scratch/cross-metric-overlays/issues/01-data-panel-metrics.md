# 01 — Data: panel_metrics join table

Status: done
Blocked by: —

## Goal

Make the Panel→Metric relation one-to-many with a per-Metric chart type, so a
Panel can carry up to four Metrics (ADR 0020), without keeping two
representations of the same concept.

## Scope

- **Migration `0007_panel_metrics.sql`**: create
  `panel_metrics (id, panel_id → panels ON DELETE CASCADE, account_id → accounts
  ON DELETE CASCADE, metric TEXT NOT NULL, chart_type TEXT NOT NULL, position
  INTEGER NOT NULL DEFAULT 0)` STRICT, indexed `(panel_id, position)`;
  `account_id` denormalized like `panels.account_id` (strict isolation,
  ADR 0007). Copy each existing `panels.metric`/`chart_type` into one row at
  position 0, then drop both columns from `panels` (SQLite: table rebuild).
- **Store** (`internal/data`): the Panel model carries `Metrics []PanelMetric`
  (`{Metric, ChartType, Position}`); create/update replace the Panel's rows
  transactionally; reads load them in position order. Deleting a Panel cascades.
- The axis side is **not** stored anywhere — derived from units at render time.
- Do **not** enforce the ≤4 / ≤2-units caps in SQL: the units live in the
  Catalog (Go), so validation belongs to the save path (issue 02). The store
  only guarantees shape.

## Out of scope

- API contract and validation (issue 02).
- Any query-engine or SPA change.
- Seeded "Aperçu" dashboard content — its Panels stay single-Metric.

## Acceptance

- Fresh DB and migrated DB converge on the same schema; every pre-existing
  Panel ends up with exactly one `panel_metrics` row carrying its old
  metric/chart_type.
- Store round-trip: a Panel saved with 3 Metrics reads back the same list in
  order; replacing the list leaves no orphan rows; deleting the Panel (or its
  Dashboard, or the Account) leaves zero rows.
- All existing data-layer tests pass against the new shape.

## Refs

ADR 0020, ADR 0007. CONTEXT.md: Panel.
`internal/data/migrations/0005_dashboards.sql`, `internal/data/dashboard.go`,
`internal/data/provision.go` (seeded dashboard uses the new shape).

## Comments

Implemented on branch `feat/panel-metrics`.

- Migration `0007_panel_metrics.sql`: join table + index, backfill (one row at
  position 0 per legacy panel), then `ALTER TABLE … DROP COLUMN` for
  `metric`/`chart_type` — simpler than the prescribed table rebuild and legal
  here (no index or constraint on those columns, SQLite ≥ 3.35).
- `PanelMetric` is `{Metric, ChartType}` without a `Position` field: the slice
  order is the display order, persisted as the row's position on write —
  storing it on the struct too would be a second representation.
- `Panel.Insert`/`Update` wrap panel + metric rows in one transaction; `Update`
  replaces the metric rows wholesale (pinned by
  `TestPanelUpdateReplacesMetricsWithoutOrphans`). Cascade pinned for panel,
  dashboard, and account deletes (`TestPanelDeleteCascadesMetricRows`); the
  backfill by `TestMigrationBackfillsPanelMetrics`.
- `internal/api/dashboardhandlers.go` got a compile bridge only: the JSON
  contract still exposes a single `metric`/`chart_type` (the first row) until
  issue 02 lands the metrics-list contract.
- Deferred to issue 02: a `withTx` helper if a third transactional writer
  appears (Insert/Update duplicate the envelope today, flagged in review).
