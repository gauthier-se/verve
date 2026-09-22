package data

import (
	"context"
	"fmt"

	"github.com/gauthier-se/verve/internal/dashtemplate"
)

// CreateAccount is the one account-creation path every caller shares (the CLI
// today, the web bootstrap next): it inserts the Account and seeds its default
// "Overview" Dashboard in a single transaction, so no Account is ever created
// without a starting board and seeding cannot be forgotten by a caller (ADR
// 0018). A taken email yields ErrDuplicateEmail and nothing is written.
func (m Models) CreateAccount(ctx context.Context, a *Account) error {
	return m.Tx(ctx, func(tx Models) error {
		if err := insertAccount(ctx, tx.Accounts.DB, a); err != nil {
			return err
		}
		return seedDefaultDashboard(ctx, tx.Accounts.DB, a.ID)
	})
}

// seedDefaultDashboard inserts the seeded template, "Overview", for accountID
// (ADR 0018), through the same path any template takes (ADR 0047), so a seeded
// board is an ordinary, editable Dashboard afterward.
func seedDefaultDashboard(ctx context.Context, q Handle, accountID int64) error {
	f, ok := dashtemplate.Get(dashtemplate.Seeded)
	if !ok {
		return fmt.Errorf("data: seed default dashboard: no %q template", dashtemplate.Seeded)
	}
	if _, err := insertFromFile(ctx, q, accountID, f, ""); err != nil {
		return fmt.Errorf("data: seed default dashboard: %w", err)
	}
	return nil
}

// CreateDashboardFromFile writes one Dashboard file as an ordinary Dashboard of
// the Account, appended to its list, in one transaction (ADR 0038). The result is
// a copy: it keeps no link to the file, so a later change to a template never
// reaches it (ADR 0047). name overrides the file's own when not empty. The file
// is taken as validated; the caller holds it to dashtemplate.Validate.
func (m Models) CreateDashboardFromFile(ctx context.Context, accountID int64, f dashtemplate.File, name string) (*Dashboard, error) {
	var d *Dashboard
	err := m.Tx(ctx, func(tx Models) error {
		var err error
		d, err = insertFromFile(ctx, tx.Dashboards.DB, accountID, f, name)
		return err
	})
	if err != nil {
		return nil, err
	}
	return d, nil
}

// insertFromFile inserts a Dashboard and its Panels, in file order, through the
// ordinary Dashboard/Panel insert path, so what it writes is exactly what the
// CRUD would have written.
func insertFromFile(ctx context.Context, q Handle, accountID int64, f dashtemplate.File, name string) (*Dashboard, error) {
	if name == "" {
		name = f.Name
	}
	d := &Dashboard{AccountID: accountID, Name: name, RangePreset: f.RangePreset, BaselineRule: f.BaselineRule}
	if err := insertDashboard(ctx, q, d); err != nil {
		return nil, fmt.Errorf("data: dashboard from file: %w", err)
	}
	for i, fp := range f.Panels {
		p := &Panel{DashboardID: d.ID, AccountID: accountID, Bucket: fp.Bucket, Width: fp.Width}
		for _, m := range fp.Metrics {
			p.Metrics = append(p.Metrics, PanelMetric{Metric: m.Metric, ChartType: m.ChartType})
		}
		if err := insertPanel(ctx, q, p); err != nil {
			return nil, fmt.Errorf("data: panel %d from file: %w", i, err)
		}
	}
	return d, nil
}
