-- 0007_panel_metrics: a Panel's Metrics become a one-to-many relation (ADR 0020).
--
-- Cross-metric overlays let one Panel carry up to four Metrics, each with its
-- own chart type; panel_metrics holds them in display order (position). The
-- axis side is never stored — it derives from each Metric's canonical unit.
-- account_id is denormalized like panels.account_id so every read can filter by
-- the owning Account directly (strict isolation, ADR 0007).
CREATE TABLE panel_metrics (
    id          INTEGER PRIMARY KEY,
    panel_id    INTEGER NOT NULL REFERENCES panels(id) ON DELETE CASCADE,
    account_id  INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    metric      TEXT NOT NULL,
    chart_type  TEXT NOT NULL,
    position    INTEGER NOT NULL DEFAULT 0
) STRICT;

-- Load a panel's metrics in display order.
CREATE INDEX panel_metrics_panel ON panel_metrics (panel_id, position);

-- Every existing single-Metric Panel becomes one row at position 0, then the
-- scalar columns are dropped so the relation has exactly one representation.
INSERT INTO panel_metrics (panel_id, account_id, metric, chart_type, position)
SELECT id, account_id, metric, chart_type, 0 FROM panels;

ALTER TABLE panels DROP COLUMN metric;
ALTER TABLE panels DROP COLUMN chart_type;
