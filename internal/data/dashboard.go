package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Dashboard is a named grid of Panels carrying the active Time range and Baseline
// (CONTEXT.md), owned by one Account (ADR 0007). Range/Baseline bounds are set
// only for the "custom" preset/rule (ADR 0015).
type Dashboard struct {
	ID           int64
	AccountID    int64
	Name         string
	Position     int
	RangePreset  string
	RangeFrom    *string
	RangeTo      *string
	BaselineRule string
	BaselineFrom *string
	BaselineTo   *string
	CreatedAt    string
	UpdatedAt    string
}

// PanelMetric is one Metric on a Panel with its own chart type (ADR 0020). The
// Panel's Metrics slice order is its display order, persisted as position.
type PanelMetric struct {
	Metric    string
	ChartType string
}

// Panel is one card in a Dashboard: one to four Catalog Metrics, each with its
// own chart type, rendered as one combo chart (ADR 0020). Bucket is nil to
// auto-derive or an override (day/week/month). AccountID is denormalized for
// direct Account filtering (strict isolation, ADR 0007). The metric/unit caps
// are enforced at the save boundary, not here — the units live in the Catalog.
type Panel struct {
	ID          int64
	DashboardID int64
	AccountID   int64
	Metrics     []PanelMetric
	Bucket      *string
	Width       int
	Position    int
	CreatedAt   string
	UpdatedAt   string
}

// DashboardModel is the DAO for dashboards.
type DashboardModel struct {
	DB *sql.DB
}

// Insert appends a dashboard at the end of the Account's list (position computed
// in-statement so concurrent inserts can't collide); populates ID, Position, timestamps.
func (m DashboardModel) Insert(ctx context.Context, d *Dashboard) error {
	return insertDashboard(ctx, m.DB, d)
}

// insertDashboard inserts a dashboard through any querier.
func insertDashboard(ctx context.Context, q querier, d *Dashboard) error {
	// Default the Baseline to comparison-off so a zero value never hits the NOT NULL column.
	if d.BaselineRule == "" {
		d.BaselineRule = "none"
	}
	const query = `
		INSERT INTO dashboards (account_id, name, position, range_preset, range_from, range_to, baseline_rule, baseline_from, baseline_to)
		VALUES (?, ?, (SELECT COALESCE(MAX(position)+1, 0) FROM dashboards WHERE account_id = ?), ?, ?, ?, ?, ?, ?)
		RETURNING id, position, created_at, updated_at`
	args := []any{d.AccountID, d.Name, d.AccountID, d.RangePreset, d.RangeFrom, d.RangeTo,
		d.BaselineRule, d.BaselineFrom, d.BaselineTo}
	return q.QueryRowContext(ctx, query, args...).
		Scan(&d.ID, &d.Position, &d.CreatedAt, &d.UpdatedAt)
}

// ListByAccount returns the Account's dashboards in switcher (position) order.
func (m DashboardModel) ListByAccount(ctx context.Context, accountID int64) ([]Dashboard, error) {
	const query = `
		SELECT id, account_id, name, position, range_preset, range_from, range_to,
		       baseline_rule, baseline_from, baseline_to, created_at, updated_at
		FROM dashboards
		WHERE account_id = ?
		ORDER BY position, id`
	rows, err := m.DB.QueryContext(ctx, query, accountID)
	if err != nil {
		return nil, fmt.Errorf("data: list dashboards: %w", err)
	}
	defer rows.Close()

	dashboards := []Dashboard{}
	for rows.Next() {
		var d Dashboard
		if err := rows.Scan(&d.ID, &d.AccountID, &d.Name, &d.Position,
			&d.RangePreset, &d.RangeFrom, &d.RangeTo,
			&d.BaselineRule, &d.BaselineFrom, &d.BaselineTo, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("data: scan dashboard: %w", err)
		}
		dashboards = append(dashboards, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: iterate dashboards: %w", err)
	}
	return dashboards, nil
}

// GetByID returns the Account's dashboard, or ErrRecordNotFound (also for another
// Account's id, so a probe can't tell missing from forbidden).
func (m DashboardModel) GetByID(ctx context.Context, accountID, id int64) (*Dashboard, error) {
	const query = `
		SELECT id, account_id, name, position, range_preset, range_from, range_to,
		       baseline_rule, baseline_from, baseline_to, created_at, updated_at
		FROM dashboards
		WHERE id = ? AND account_id = ?`
	var d Dashboard
	err := m.DB.QueryRowContext(ctx, query, id, accountID).Scan(
		&d.ID, &d.AccountID, &d.Name, &d.Position,
		&d.RangePreset, &d.RangeFrom, &d.RangeTo,
		&d.BaselineRule, &d.BaselineFrom, &d.BaselineTo, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &d, nil
}

// Update saves a dashboard's name, range, and Baseline, scoped to d.AccountID;
// returns ErrRecordNotFound if none belongs to the Account.
func (m DashboardModel) Update(ctx context.Context, d *Dashboard) error {
	const query = `
		UPDATE dashboards
		SET name = ?, range_preset = ?, range_from = ?, range_to = ?,
		    baseline_rule = ?, baseline_from = ?, baseline_to = ?,
		    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND account_id = ?`
	args := []any{d.Name, d.RangePreset, d.RangeFrom, d.RangeTo,
		d.BaselineRule, d.BaselineFrom, d.BaselineTo, d.ID, d.AccountID}
	return execExpectingRow(ctx, m.DB, query, args...)
}

// Delete removes the Account's dashboard (its panels cascade), scoped by Account.
// It returns ErrRecordNotFound if no such dashboard belongs to the Account.
func (m DashboardModel) Delete(ctx context.Context, accountID, id int64) error {
	return execExpectingRow(ctx, m.DB,
		`DELETE FROM dashboards WHERE id = ? AND account_id = ?`, id, accountID)
}

// PanelModel is the DAO for panels.
type PanelModel struct {
	DB *sql.DB
}

// Insert appends a panel at the end of its dashboard's grid (position computed
// in-statement); populates ID, Position, timestamps. AccountID comes from the
// already-authorized owning dashboard. The panel row and its metric rows are
// written in one transaction.
func (m PanelModel) Insert(ctx context.Context, p *Panel) error {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("data: begin insert panel: %w", err)
	}
	defer tx.Rollback() // no-op after Commit

	if err := insertPanel(ctx, tx, p); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("data: commit insert panel: %w", err)
	}
	return nil
}

// insertPanel inserts a panel and its metric rows through any querier. Callers
// own the transaction boundary: multiple statements are only atomic when q is
// a *sql.Tx.
func insertPanel(ctx context.Context, q querier, p *Panel) error {
	const query = `
		INSERT INTO panels (dashboard_id, account_id, bucket, width, position)
		VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(position)+1, 0) FROM panels WHERE dashboard_id = ?))
		RETURNING id, position, created_at, updated_at`
	args := []any{p.DashboardID, p.AccountID, p.Bucket, p.Width, p.DashboardID}
	if err := q.QueryRowContext(ctx, query, args...).
		Scan(&p.ID, &p.Position, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return err
	}
	return insertPanelMetrics(ctx, q, p)
}

// insertPanelMetrics writes p.Metrics in slice order; position is the index.
func insertPanelMetrics(ctx context.Context, q querier, p *Panel) error {
	const query = `
		INSERT INTO panel_metrics (panel_id, account_id, metric, chart_type, position)
		VALUES (?, ?, ?, ?, ?)`
	for i, pm := range p.Metrics {
		if _, err := q.ExecContext(ctx, query, p.ID, p.AccountID, pm.Metric, pm.ChartType, i); err != nil {
			return fmt.Errorf("data: insert panel metric %s: %w", pm.Metric, err)
		}
	}
	return nil
}

// ListByDashboard returns the dashboard's panels in grid (position) order, each
// with its Metrics in display order, scoped to the Account so it never returns
// another Account's panels.
func (m PanelModel) ListByDashboard(ctx context.Context, accountID, dashboardID int64) ([]Panel, error) {
	const query = `
		SELECT id, dashboard_id, account_id, bucket, width, position, created_at, updated_at
		FROM panels
		WHERE dashboard_id = ? AND account_id = ?
		ORDER BY position, id`
	rows, err := m.DB.QueryContext(ctx, query, dashboardID, accountID)
	if err != nil {
		return nil, fmt.Errorf("data: list panels: %w", err)
	}
	defer rows.Close()

	panels := []Panel{}
	for rows.Next() {
		var p Panel
		if err := rows.Scan(&p.ID, &p.DashboardID, &p.AccountID,
			&p.Bucket, &p.Width, &p.Position, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("data: scan panel: %w", err)
		}
		panels = append(panels, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: iterate panels: %w", err)
	}

	metrics, err := m.metricsByPanel(ctx, accountID, dashboardID)
	if err != nil {
		return nil, err
	}
	for i := range panels {
		panels[i].Metrics = metrics[panels[i].ID]
	}
	return panels, nil
}

// metricsByPanel loads every metric row of a dashboard's panels in one query,
// grouped by panel id, each group in display order.
func (m PanelModel) metricsByPanel(ctx context.Context, accountID, dashboardID int64) (map[int64][]PanelMetric, error) {
	const query = `
		SELECT pm.panel_id, pm.metric, pm.chart_type
		FROM panel_metrics pm
		JOIN panels p ON p.id = pm.panel_id
		WHERE p.dashboard_id = ? AND pm.account_id = ?
		ORDER BY pm.panel_id, pm.position, pm.id`
	rows, err := m.DB.QueryContext(ctx, query, dashboardID, accountID)
	if err != nil {
		return nil, fmt.Errorf("data: list panel metrics: %w", err)
	}
	defer rows.Close()

	metrics := make(map[int64][]PanelMetric)
	for rows.Next() {
		var panelID int64
		var pm PanelMetric
		if err := rows.Scan(&panelID, &pm.Metric, &pm.ChartType); err != nil {
			return nil, fmt.Errorf("data: scan panel metric: %w", err)
		}
		metrics[panelID] = append(metrics[panelID], pm)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: iterate panel metrics: %w", err)
	}
	return metrics, nil
}

// GetByID returns the Account's panel with its Metrics in display order, or
// ErrRecordNotFound (also for another Account's id, so a probe can't
// distinguish missing from forbidden).
func (m PanelModel) GetByID(ctx context.Context, accountID, id int64) (*Panel, error) {
	const query = `
		SELECT id, dashboard_id, account_id, bucket, width, position, created_at, updated_at
		FROM panels
		WHERE id = ? AND account_id = ?`
	var p Panel
	err := m.DB.QueryRowContext(ctx, query, id, accountID).Scan(
		&p.ID, &p.DashboardID, &p.AccountID,
		&p.Bucket, &p.Width, &p.Position, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}

	if p.Metrics, err = m.loadPanelMetrics(ctx, accountID, p.ID); err != nil {
		return nil, err
	}
	return &p, nil
}

// loadPanelMetrics returns one panel's metric rows in display order.
func (m PanelModel) loadPanelMetrics(ctx context.Context, accountID, panelID int64) ([]PanelMetric, error) {
	const query = `
		SELECT metric, chart_type
		FROM panel_metrics
		WHERE panel_id = ? AND account_id = ?
		ORDER BY position, id`
	rows, err := m.DB.QueryContext(ctx, query, panelID, accountID)
	if err != nil {
		return nil, fmt.Errorf("data: get panel metrics: %w", err)
	}
	defer rows.Close()

	var metrics []PanelMetric
	for rows.Next() {
		var pm PanelMetric
		if err := rows.Scan(&pm.Metric, &pm.ChartType); err != nil {
			return nil, fmt.Errorf("data: scan panel metric: %w", err)
		}
		metrics = append(metrics, pm)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: iterate panel metrics: %w", err)
	}
	return metrics, nil
}

// Update saves a panel's presentation (its Metrics with their chart types,
// bucket, width), replacing the metric rows wholesale in one transaction so no
// orphan row can survive an edit. Scoped to p.AccountID; ErrRecordNotFound if
// no such panel belongs to the Account. Dashboard membership is fixed at
// creation.
func (m PanelModel) Update(ctx context.Context, p *Panel) error {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("data: begin update panel: %w", err)
	}
	defer tx.Rollback() // no-op after Commit

	const query = `
		UPDATE panels
		SET bucket = ?, width = ?,
		    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND account_id = ?`
	if err := execExpectingRow(ctx, tx, query, p.Bucket, p.Width, p.ID, p.AccountID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM panel_metrics WHERE panel_id = ? AND account_id = ?`, p.ID, p.AccountID); err != nil {
		return fmt.Errorf("data: clear panel metrics: %w", err)
	}
	if err := insertPanelMetrics(ctx, tx, p); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("data: commit update panel: %w", err)
	}
	return nil
}

// Delete removes the Account's panel, scoped by Account. It returns
// ErrRecordNotFound if no such panel belongs to the Account.
func (m PanelModel) Delete(ctx context.Context, accountID, id int64) error {
	return execExpectingRow(ctx, m.DB,
		`DELETE FROM panels WHERE id = ? AND account_id = ?`, id, accountID)
}

// Reorder rewrites panel positions in one transaction (each row set to its index in
// orderedIDs). Scoped to (dashboard, account), so a foreign id matches nothing.
func (m PanelModel) Reorder(ctx context.Context, accountID, dashboardID int64, orderedIDs []int64) error {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("data: begin reorder: %w", err)
	}
	defer tx.Rollback() // no-op after Commit

	const query = `
		UPDATE panels
		SET position = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND dashboard_id = ? AND account_id = ?`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("data: prepare reorder: %w", err)
	}
	defer stmt.Close()

	for pos, id := range orderedIDs {
		if _, err := stmt.ExecContext(ctx, pos, id, dashboardID, accountID); err != nil {
			return fmt.Errorf("data: reorder panel %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("data: commit reorder: %w", err)
	}
	return nil
}

// execExpectingRow runs an Account-scoped UPDATE/DELETE that must affect one row,
// mapping "no row affected" to ErrRecordNotFound.
func execExpectingRow(ctx context.Context, q querier, query string, args ...any) error {
	res, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrRecordNotFound
	}
	return nil
}
