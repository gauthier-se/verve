package data

import (
	"context"
	"errors"
	"testing"
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
	if dashboards[0].Name != "Aperçu" {
		t.Errorf("dashboard name = %q, want %q", dashboards[0].Name, "Aperçu")
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
		if panels[i].Metric != w.metric || panels[i].ChartType != w.chartType {
			t.Errorf("panel %d = %s/%s, want %s/%s",
				i, panels[i].Metric, panels[i].ChartType, w.metric, w.chartType)
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
