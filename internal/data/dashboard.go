package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Dashboard is a named, user-arranged grid of Panels carrying the active Time
// range (see CONTEXT.md), owned by exactly one Account (ADR 0007). The range is
// a preset token (7d/30d/3m/1y/all) or "custom", in which case RangeFrom and
// RangeTo carry day-granularity bounds (YYYY-MM-DD); both are nil for a preset.
// The Baseline (ADR 0015) is the second window comparison reads against:
// BaselineRule is none/previous/same_period_last_year/custom, and only "custom"
// carries BaselineFrom/BaselineTo, shaped exactly like the range bounds.
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

// Panel is one card in a Dashboard: a single Catalog Metric rendered as a chart.
// ChartType is bar/line/area/band/stacked_bar. Bucket is nil to auto-derive from
// the Dashboard's span, or an override (day/week/month). Width is the column span
// (1-3). AccountID is denormalized from the owning dashboard so every read filters
// by the Account directly (strict isolation, ADR 0007).
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

// Insert appends a new dashboard at the end of the Account's list — its position
// is the next after the Account's current maximum, computed in the same statement
// so concurrent inserts can't collide. It populates ID, Position, and timestamps.
func (m DashboardModel) Insert(ctx context.Context, d *Dashboard) error {
	// A caller that says nothing about the Baseline gets comparison off, so a
	// zero-valued rule never reaches the NOT NULL column as ''.
	if d.BaselineRule == "" {
		d.BaselineRule = "none"
	}
	const query = `
		INSERT INTO dashboards (account_id, name, position, range_preset, range_from, range_to, baseline_rule, baseline_from, baseline_to)
		VALUES (?, ?, (SELECT COALESCE(MAX(position)+1, 0) FROM dashboards WHERE account_id = ?), ?, ?, ?, ?, ?, ?)
		RETURNING id, position, created_at, updated_at`
	args := []any{d.AccountID, d.Name, d.AccountID, d.RangePreset, d.RangeFrom, d.RangeTo,
		d.BaselineRule, d.BaselineFrom, d.BaselineTo}
	return m.DB.QueryRowContext(ctx, query, args...).
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

// GetByID returns the Account's dashboard with the given id, or ErrRecordNotFound
// — including when the id belongs to another Account, so a cross-Account probe is
// indistinguishable from a missing row.
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

// Update saves a dashboard's name, Time range, and Baseline, scoped to
// d.AccountID so one Account can never mutate another's row. It returns
// ErrRecordNotFound if no such dashboard belongs to the Account.
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

// Insert appends a new panel at the end of its dashboard's grid — position is the
// next after that dashboard's current maximum, computed in the same statement. It
// populates ID, Position, and timestamps. The caller supplies AccountID from the
// already-authorized owning dashboard.
func (m PanelModel) Insert(ctx context.Context, p *Panel) error {
	const query = `
		INSERT INTO panels (dashboard_id, account_id, metric, chart_type, bucket, width, position)
		VALUES (?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(position)+1, 0) FROM panels WHERE dashboard_id = ?))
		RETURNING id, position, created_at, updated_at`
	args := []any{p.DashboardID, p.AccountID, p.Metric, p.ChartType, p.Bucket, p.Width, p.DashboardID}
	return m.DB.QueryRowContext(ctx, query, args...).
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

// Reorder rewrites the panel positions in one transaction so the drag-reordered
// grid persists. orderedIDs lists the dashboard's panels in their new order;
// each row is set to its index. Every update is scoped to (dashboard, account),
// so an id from another dashboard or Account simply matches nothing.
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

// execExpectingRow runs a write that must affect exactly one row (an Account-
// scoped UPDATE or DELETE) and maps "no row affected" to ErrRecordNotFound, so a
// missing or cross-Account target is reported the same way everywhere.
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
