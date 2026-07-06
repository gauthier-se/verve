-- 0005_dashboards: Dashboards and their Panels (issue 06, ADRs 0005, 0012, 0013).
--
-- A Dashboard is a named, user-arranged grid of Panels carrying the active Time
-- range (see CONTEXT.md). A Panel is one card: a single Metric rendered as a
-- chart. Both are owned by exactly one Account (ADR 0007); ON DELETE CASCADE
-- drops an Account's dashboards, and a dashboard's deletion drops its panels.

-- A Dashboard holds the Time range applied to all its Panels. The range is
-- either a preset token (7d/30d/3m/1y/all) or 'custom', in which case
-- range_from/range_to carry the day-granularity bounds (YYYY-MM-DD, ADR 0013 —
-- no time-of-day in v1). position orders dashboards in the switcher.
CREATE TABLE dashboards (
    id            INTEGER PRIMARY KEY,
    account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    position      INTEGER NOT NULL DEFAULT 0,
    range_preset  TEXT NOT NULL DEFAULT '30d',
    range_from    TEXT,
    range_to      TEXT,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

-- List an Account's dashboards in switcher order.
CREATE INDEX dashboards_account ON dashboards (account_id, position);

-- A Panel renders one Catalog Metric as a chart. chart_type is one of
-- bar/line/area/band/stacked_bar (the default is derived from the Metric's
-- aggregation rule; the user may switch among compatible types). bucket is
-- NULL to auto-derive from the Dashboard's span, or an override (day/week/
-- month). width is the column span (1-3). position orders panels in the grid.
-- account_id is denormalized from the dashboard so every read can filter by the
-- owning Account directly (strict isolation, ADR 0007).
CREATE TABLE panels (
    id            INTEGER PRIMARY KEY,
    dashboard_id  INTEGER NOT NULL REFERENCES dashboards(id) ON DELETE CASCADE,
    account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    metric        TEXT NOT NULL,
    chart_type    TEXT NOT NULL,
    bucket        TEXT,
    width         INTEGER NOT NULL DEFAULT 1,
    position      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

-- Load a dashboard's panels in grid order.
CREATE INDEX panels_dashboard ON panels (dashboard_id, position);
