package data

import (
	"context"
	"fmt"
)

// defaultDashboardName is the seeded starter board (ADR 0018): "Aperçu" ("Overview").
const defaultDashboardName = "Aperçu"

// defaultPanels is the curated template seeded into every new Account's starter
// board (ADR 0018), in this order. The Metrics are iPhone-universal (no Watch
// required) and each chart type is valid for the Metric's Catalog aggregation.
// This is a product decision defined here, never derived from user input.
var defaultPanels = []struct {
	metric    string
	chartType string
}{
	{"body_mass", "line"},          // latest
	{"active_energy", "bar"},       // sum
	{"steps", "bar"},               // sum
	{"resting_heart_rate", "line"}, // average, shown as a plain line
	{"apple_exercise_time", "bar"}, // sum
}

// CreateAccount is the one account-creation path every caller shares (the CLI
// today, the web bootstrap next): it inserts the Account and seeds its default
// "Aperçu" Dashboard in a single transaction, so no Account is ever created
// without a starting board and seeding cannot be forgotten by a caller (ADR
// 0018). A taken email yields ErrDuplicateEmail and nothing is written.
func (m Models) CreateAccount(ctx context.Context, a *Account) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("data: begin create account: %w", err)
	}
	defer tx.Rollback() // no-op after Commit

	if err := insertAccount(ctx, tx, a); err != nil {
		return err
	}
	if err := seedDefaultDashboard(ctx, tx, a.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("data: commit create account: %w", err)
	}
	return nil
}

// seedDefaultDashboard inserts the "Aperçu" Dashboard and its template Panels for
// accountID, in template order, reusing the ordinary Dashboard/Panel insert path
// (ADR 0012) so a seeded board is an ordinary, editable Dashboard afterward.
func seedDefaultDashboard(ctx context.Context, q querier, accountID int64) error {
	d := &Dashboard{AccountID: accountID, Name: defaultDashboardName, RangePreset: "30d"}
	if err := insertDashboard(ctx, q, d); err != nil {
		return fmt.Errorf("data: seed default dashboard: %w", err)
	}
	for _, tp := range defaultPanels {
		p := &Panel{
			DashboardID: d.ID, AccountID: accountID,
			Metrics: []PanelMetric{{Metric: tp.metric, ChartType: tp.chartType}},
			Width:   1,
		}
		if err := insertPanel(ctx, q, p); err != nil {
			return fmt.Errorf("data: seed panel %s: %w", tp.metric, err)
		}
	}
	return nil
}
