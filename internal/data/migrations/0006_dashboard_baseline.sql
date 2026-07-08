-- 0006_dashboard_baseline: the Dashboard's Baseline window (ADR 0015).
--
-- Period comparison reads a Dashboard against a second, earlier window — the
-- Baseline — defined by a rule: 'previous' (shift the range back by its own
-- length), 'same_period_last_year' (shift back one year), 'custom' (absolute
-- frozen bounds), or 'none' (comparison off, the default). Only 'custom'
-- carries baseline_from/baseline_to, day-granularity YYYY-MM-DD bounds
-- mirroring range_from/range_to. The DEFAULT backfills every existing
-- dashboard to 'none' (no comparison).
ALTER TABLE dashboards ADD COLUMN baseline_rule TEXT NOT NULL DEFAULT 'none';
ALTER TABLE dashboards ADD COLUMN baseline_from TEXT;
ALTER TABLE dashboards ADD COLUMN baseline_to TEXT;
