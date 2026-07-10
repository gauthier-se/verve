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

// Panel is one card in a Dashboard: a Catalog Metric as a chart. Bucket is nil to
// auto-derive or an override (day/week/month). AccountID is denormalized for direct
// Account filtering (strict isolation, ADR 0007).
type Panel struct {
	ID          int64
	DashboardID int64
	AccountID   int64
	Metric      string
	ChartType   string
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
// already-authorized owning dashboard.
func (m PanelModel) Insert(ctx context.Context, p *Panel) error {
	return insertPanel(ctx, m.DB, p)
}

// insertPanel inserts a panel through any querier.
func insertPanel(ctx context.Context, q querier, p *Panel) error {
	const query = `
		INSERT INTO panels (dashboard_id, account_id, metric, chart_type, bucket, width, position)
		VALUES (?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(position)+1, 0) FROM panels WHERE dashboard_id = ?))
		RETURNING id, position, created_at, updated_at`
	args := []any{p.DashboardID, p.AccountID, p.Metric, p.ChartType, p.Bucket, p.Width, p.DashboardID}
	return q.QueryRowContext(ctx, query, args...).
		Scan(&p.ID, &p.Position, &p.CreatedAt, &p.UpdatedAt)
}

// ListByDashboard returns the dashboard's panels in grid (position) order, scoped
// to the Account so it never returns another Account's panels.
func (m PanelModel) ListByDashboard(ctx context.Context, accountID, dashboardID int64) ([]Panel, error) {
	const query = `
		SELECT id, dashboard_id, account_id, metric, chart_type, bucket, width, position, created_at, updated_at
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
		if err := rows.Scan(&p.ID, &p.DashboardID, &p.AccountID, &p.Metric, &p.ChartType,
			&p.Bucket, &p.Width, &p.Position, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("data: scan panel: %w", err)
		}
		panels = append(panels, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: iterate panels: %w", err)
	}
	return panels, nil
}

// GetByID returns the Account's panel, or ErrRecordNotFound (also for another
// Account's id, so a probe can't distinguish missing from forbidden).
func (m PanelModel) GetByID(ctx context.Context, accountID, id int64) (*Panel, error) {
	const query = `
		SELECT id, dashboard_id, account_id, metric, chart_type, bucket, width, position, created_at, updated_at
		FROM panels
		WHERE id = ? AND account_id = ?`
	var p Panel
	err := m.DB.QueryRowContext(ctx, query, id, accountID).Scan(
		&p.ID, &p.DashboardID, &p.AccountID, &p.Metric, &p.ChartType,
		&p.Bucket, &p.Width, &p.Position, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &p, nil
}

// Update saves a panel's presentation (chart type, bucket, width), scoped to
// p.AccountID. It returns ErrRecordNotFound if no such panel belongs to the
// Account. Metric and dashboard membership are fixed at creation.
func (m PanelModel) Update(ctx context.Context, p *Panel) error {
	const query = `
		UPDATE panels
		SET chart_type = ?, bucket = ?, width = ?,
		    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND account_id = ?`
	args := []any{p.ChartType, p.Bucket, p.Width, p.ID, p.AccountID}
	return execExpectingRow(ctx, m.DB, query, args...)
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
func execExpectingRow(ctx context.Context, db *sql.DB, query string, args ...any) error {
	res, err := db.ExecContext(ctx, query, args...)
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
