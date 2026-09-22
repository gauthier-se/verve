package data

import (
	"context"
	"errors"
	"testing"

	"github.com/gauthier-se/verve/internal/dashtemplate"
)

func TestCreateAccountSeedsDefaultDashboard(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()

	acc := &Account{Email: "new@example.com"}
	if err := models.CreateAccount(ctx, acc); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if acc.ID == 0 {
		t.Fatalf("CreateAccount did not populate the account ID")
	}

	dashboards, err := models.Dashboards.ListByAccount(ctx, acc.ID)
	if err != nil {
		t.Fatalf("ListByAccount: %v", err)
	}
	if len(dashboards) != 1 {
		t.Fatalf("seeded dashboards = %d, want 1", len(dashboards))
	}
	if dashboards[0].Name != "Overview" {
		t.Errorf("dashboard name = %q, want %q", dashboards[0].Name, "Overview")
	}

	panels, err := models.Panels.ListByDashboard(ctx, acc.ID, dashboards[0].ID)
	if err != nil {
		t.Fatalf("ListByDashboard: %v", err)
	}
	want := []struct {
		metric    string
		chartType string
	}{
		{"body_mass", "line"},
		{"active_energy", "bar"},
		{"steps", "bar"},
		{"resting_heart_rate", "line"},
		{"apple_exercise_time", "bar"},
	}
	if len(panels) != len(want) {
		t.Fatalf("seeded panels = %d, want %d", len(panels), len(want))
	}
	for i, w := range want {
		if len(panels[i].Metrics) != 1 {
			t.Fatalf("panel %d metrics = %d, want 1", i, len(panels[i].Metrics))
		}
		if panels[i].Metrics[0].Metric != w.metric || panels[i].Metrics[0].ChartType != w.chartType {
			t.Errorf("panel %d = %s/%s, want %s/%s",
				i, panels[i].Metrics[0].Metric, panels[i].Metrics[0].ChartType, w.metric, w.chartType)
		}
		if panels[i].Position != i {
			t.Errorf("panel %d position = %d, want %d", i, panels[i].Position, i)
		}
	}
}

func TestCreateAccountDuplicateEmailSeedsNothing(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()

	if err := models.CreateAccount(ctx, &Account{Email: "dup@example.com"}); err != nil {
		t.Fatalf("first CreateAccount: %v", err)
	}

	err := models.CreateAccount(ctx, &Account{Email: "dup@example.com"})
	if !errors.Is(err, ErrDuplicateEmail) {
		t.Fatalf("second CreateAccount error = %v, want ErrDuplicateEmail", err)
	}

	// The failed creation must leave no orphan dashboard behind (rolled back).
	var dashboards int
	if err := models.db.QueryRowContext(ctx, `SELECT count(*) FROM dashboards`).Scan(&dashboards); err != nil {
		t.Fatalf("count dashboards: %v", err)
	}
	if dashboards != 1 {
		t.Errorf("dashboards after duplicate = %d, want 1 (rollback failed?)", dashboards)
	}
}

func TestCreateDashboardFromFileWritesItsArrangement(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := &Account{Email: "file@example.com"}
	if err := models.CreateAccount(ctx, acc); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	week := "week"
	file := dashtemplate.File{
		Format: dashtemplate.Format, Name: "Endurance", RangePreset: "3m", BaselineRule: "previous",
		Panels: []dashtemplate.Panel{
			{Metrics: []dashtemplate.PanelMetric{{Metric: "training_time", ChartType: "stacked_bar"}}, Bucket: &week, Width: 2},
			{Metrics: []dashtemplate.PanelMetric{
				{Metric: "resting_heart_rate", ChartType: "line"},
				{Metric: "heart_rate_variability_sdnn", ChartType: "line"},
			}, Width: 1},
		},
	}
	d, err := models.CreateDashboardFromFile(ctx, acc.ID, file, "")
	if err != nil {
		t.Fatalf("CreateDashboardFromFile: %v", err)
	}
	if d.Name != "Endurance" || d.RangePreset != "3m" || d.BaselineRule != "previous" || d.Position != 1 {
		t.Errorf("dashboard = %+v, want Endurance, 3m, previous, after the Overview", d)
	}

	panels, err := models.Panels.ListByDashboard(ctx, acc.ID, d.ID)
	if err != nil {
		t.Fatalf("ListByDashboard: %v", err)
	}
	if len(panels) != 2 {
		t.Fatalf("panels = %d, want 2", len(panels))
	}
	if panels[0].Width != 2 || panels[0].Bucket == nil || *panels[0].Bucket != "week" ||
		panels[0].Metrics[0] != (PanelMetric{Metric: "training_time", ChartType: "stacked_bar"}) {
		t.Errorf("panel 0 = %+v", panels[0])
	}
	if panels[1].Width != 1 || panels[1].Bucket != nil || len(panels[1].Metrics) != 2 ||
		panels[1].Metrics[1] != (PanelMetric{Metric: "heart_rate_variability_sdnn", ChartType: "line"}) {
		t.Errorf("panel 1 = %+v", panels[1])
	}

	renamed, err := models.CreateDashboardFromFile(ctx, acc.ID, file, "My block")
	if err != nil {
		t.Fatalf("CreateDashboardFromFile with a name: %v", err)
	}
	if renamed.Name != "My block" {
		t.Errorf("name = %q, want the override", renamed.Name)
	}
}
