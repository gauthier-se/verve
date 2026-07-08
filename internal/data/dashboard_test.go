package data

import (
	"context"
	"errors"
	"testing"
)

// seedNamedAccount inserts a bare account with the given email and returns its
// id, for the cross-Account isolation tests that need two distinct owners.
func seedNamedAccount(t *testing.T, models Models, email string) int64 {
	t.Helper()
	acc := &Account{Email: email}
	if err := models.Accounts.Insert(context.Background(), acc); err != nil {
		t.Fatalf("seed account %s: %v", email, err)
	}
	return acc.ID
}

func TestDashboardInsertAppendsAndPopulates(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedNamedAccount(t, models, "a@example.com")

	first := &Dashboard{AccountID: acc, Name: "Training", RangePreset: "30d"}
	if err := models.Dashboards.Insert(ctx, first); err != nil {
		t.Fatalf("Insert first: %v", err)
	}
	if first.ID == 0 || first.CreatedAt == "" {
		t.Errorf("Insert did not populate ID/CreatedAt: %+v", first)
	}
	if first.Position != 0 {
		t.Errorf("first dashboard position = %d, want 0", first.Position)
	}

	second := &Dashboard{AccountID: acc, Name: "Sleep", RangePreset: "7d"}
	if err := models.Dashboards.Insert(ctx, second); err != nil {
		t.Fatalf("Insert second: %v", err)
	}
	if second.Position != 1 {
		t.Errorf("second dashboard position = %d, want 1", second.Position)
	}
}

func TestDashboardListScopedAndOrdered(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	alice := seedNamedAccount(t, models, "alice@example.com")
	bob := seedNamedAccount(t, models, "bob@example.com")

	for _, name := range []string{"One", "Two"} {
		if err := models.Dashboards.Insert(ctx, &Dashboard{AccountID: alice, Name: name, RangePreset: "30d"}); err != nil {
			t.Fatalf("insert alice %s: %v", name, err)
		}
	}
	if err := models.Dashboards.Insert(ctx, &Dashboard{AccountID: bob, Name: "Bob", RangePreset: "30d"}); err != nil {
		t.Fatalf("insert bob: %v", err)
	}

	got, err := models.Dashboards.ListByAccount(ctx, alice)
	if err != nil {
		t.Fatalf("ListByAccount: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("alice dashboards = %d, want 2 (isolation leak?)", len(got))
	}
	if got[0].Name != "One" || got[1].Name != "Two" {
		t.Errorf("dashboards out of order: %q, %q", got[0].Name, got[1].Name)
	}
}

func TestDashboardGetByIDIsAccountScoped(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	alice := seedNamedAccount(t, models, "alice@example.com")
	bob := seedNamedAccount(t, models, "bob@example.com")

	d := &Dashboard{AccountID: alice, Name: "Private", RangePreset: "30d"}
	if err := models.Dashboards.Insert(ctx, d); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Alice can read her own.
	if _, err := models.Dashboards.GetByID(ctx, alice, d.ID); err != nil {
		t.Errorf("owner GetByID: %v", err)
	}
	// Bob cannot — cross-account access is a not-found, never a leak.
	if _, err := models.Dashboards.GetByID(ctx, bob, d.ID); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("cross-account GetByID error = %v, want ErrRecordNotFound", err)
	}
}

func TestDashboardUpdateNameAndRange(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedNamedAccount(t, models, "a@example.com")

	d := &Dashboard{AccountID: acc, Name: "Old", RangePreset: "30d"}
	if err := models.Dashboards.Insert(ctx, d); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	d.Name = "New"
	d.RangePreset = "custom"
	d.RangeFrom = ptr("2024-01-01")
	d.RangeTo = ptr("2024-02-01")
	if err := models.Dashboards.Update(ctx, d); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := models.Dashboards.GetByID(ctx, acc, d.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "New" || got.RangePreset != "custom" {
		t.Errorf("update not persisted: %+v", got)
	}
	if got.RangeFrom == nil || *got.RangeFrom != "2024-01-01" || got.RangeTo == nil || *got.RangeTo != "2024-02-01" {
		t.Errorf("custom range not persisted: from=%v to=%v", got.RangeFrom, got.RangeTo)
	}
}

func TestDashboardBaselineDefaultsToNone(t *testing.T) {
	db, models := openTestDB(t)
	ctx := context.Background()
	acc := seedNamedAccount(t, models, "a@example.com")

	d := &Dashboard{AccountID: acc, Name: "Plain", RangePreset: "30d"}
	if err := models.Dashboards.Insert(ctx, d); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := models.Dashboards.GetByID(ctx, acc, d.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.BaselineRule != "none" || got.BaselineFrom != nil || got.BaselineTo != nil {
		t.Errorf("new dashboard baseline = %q from=%v to=%v, want none with nil bounds",
			got.BaselineRule, got.BaselineFrom, got.BaselineTo)
	}

	// A row written without the baseline columns — like every dashboard that
	// predates the 0006 migration — must also read back as rule 'none'.
	res, err := db.ExecContext(ctx,
		`INSERT INTO dashboards (account_id, name, range_preset) VALUES (?, 'Legacy', '7d')`, acc)
	if err != nil {
		t.Fatalf("raw insert: %v", err)
	}
	legacyID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	legacy, err := models.Dashboards.GetByID(ctx, acc, legacyID)
	if err != nil {
		t.Fatalf("GetByID legacy: %v", err)
	}
	if legacy.BaselineRule != "none" || legacy.BaselineFrom != nil || legacy.BaselineTo != nil {
		t.Errorf("legacy dashboard baseline = %q from=%v to=%v, want none with nil bounds",
			legacy.BaselineRule, legacy.BaselineFrom, legacy.BaselineTo)
	}
}

func TestDashboardBaselineRoundTrip(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedNamedAccount(t, models, "a@example.com")

	d := &Dashboard{
		AccountID: acc, Name: "Compare", RangePreset: "30d",
		BaselineRule: "custom", BaselineFrom: ptr("2024-01-01"), BaselineTo: ptr("2024-02-01"),
	}
	if err := models.Dashboards.Insert(ctx, d); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := models.Dashboards.GetByID(ctx, acc, d.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.BaselineRule != "custom" {
		t.Errorf("baseline rule = %q, want custom", got.BaselineRule)
	}
	if got.BaselineFrom == nil || *got.BaselineFrom != "2024-01-01" || got.BaselineTo == nil || *got.BaselineTo != "2024-02-01" {
		t.Errorf("custom baseline not persisted: from=%v to=%v", got.BaselineFrom, got.BaselineTo)
	}

	// A relative rule carries no bounds; Update must persist the switch.
	got.BaselineRule = "previous"
	got.BaselineFrom, got.BaselineTo = nil, nil
	if err := models.Dashboards.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	list, err := models.Dashboards.ListByAccount(ctx, acc)
	if err != nil {
		t.Fatalf("ListByAccount: %v", err)
	}
	if len(list) != 1 || list[0].BaselineRule != "previous" || list[0].BaselineFrom != nil || list[0].BaselineTo != nil {
		t.Errorf("updated baseline = %+v, want previous with nil bounds", list)
	}
}

func TestDashboardUpdateIsAccountScoped(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	alice := seedNamedAccount(t, models, "alice@example.com")
	bob := seedNamedAccount(t, models, "bob@example.com")

	d := &Dashboard{AccountID: alice, Name: "Alice", RangePreset: "30d"}
	if err := models.Dashboards.Insert(ctx, d); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Bob tries to update Alice's dashboard by claiming her id.
	intruder := &Dashboard{ID: d.ID, AccountID: bob, Name: "Hijacked", RangePreset: "7d"}
	if err := models.Dashboards.Update(ctx, intruder); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("cross-account Update error = %v, want ErrRecordNotFound", err)
	}
}

func TestDashboardDeleteCascadesPanels(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedNamedAccount(t, models, "a@example.com")

	d := &Dashboard{AccountID: acc, Name: "Doomed", RangePreset: "30d"}
	if err := models.Dashboards.Insert(ctx, d); err != nil {
		t.Fatalf("Insert dashboard: %v", err)
	}
	p := &Panel{DashboardID: d.ID, AccountID: acc, Metric: "steps", ChartType: "bar", Width: 1}
	if err := models.Panels.Insert(ctx, p); err != nil {
		t.Fatalf("Insert panel: %v", err)
	}

	if err := models.Dashboards.Delete(ctx, acc, d.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := models.Dashboards.GetByID(ctx, acc, d.ID); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("dashboard still present after delete: %v", err)
	}
	panels, err := models.Panels.ListByDashboard(ctx, acc, d.ID)
	if err != nil {
		t.Fatalf("ListByDashboard: %v", err)
	}
	if len(panels) != 0 {
		t.Errorf("panels = %d after cascade, want 0", len(panels))
	}
}

func TestDashboardDeleteIsAccountScoped(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	alice := seedNamedAccount(t, models, "alice@example.com")
	bob := seedNamedAccount(t, models, "bob@example.com")

	d := &Dashboard{AccountID: alice, Name: "Alice", RangePreset: "30d"}
	if err := models.Dashboards.Insert(ctx, d); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := models.Dashboards.Delete(ctx, bob, d.ID); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("cross-account Delete error = %v, want ErrRecordNotFound", err)
	}
}

func TestPanelInsertAppendsWithinDashboard(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedNamedAccount(t, models, "a@example.com")
	d := &Dashboard{AccountID: acc, Name: "D", RangePreset: "30d"}
	if err := models.Dashboards.Insert(ctx, d); err != nil {
		t.Fatalf("Insert dashboard: %v", err)
	}

	first := &Panel{DashboardID: d.ID, AccountID: acc, Metric: "steps", ChartType: "bar", Width: 1}
	if err := models.Panels.Insert(ctx, first); err != nil {
		t.Fatalf("Insert first panel: %v", err)
	}
	if first.ID == 0 || first.Position != 0 {
		t.Errorf("first panel = %+v, want id set, position 0", first)
	}
	second := &Panel{DashboardID: d.ID, AccountID: acc, Metric: "heart_rate", ChartType: "line", Width: 2, Bucket: ptr("week")}
	if err := models.Panels.Insert(ctx, second); err != nil {
		t.Fatalf("Insert second panel: %v", err)
	}
	if second.Position != 1 {
		t.Errorf("second panel position = %d, want 1", second.Position)
	}

	panels, err := models.Panels.ListByDashboard(ctx, acc, d.ID)
	if err != nil {
		t.Fatalf("ListByDashboard: %v", err)
	}
	if len(panels) != 2 || panels[0].Metric != "steps" || panels[1].Metric != "heart_rate" {
		t.Errorf("panels = %+v, want steps then heart_rate", panels)
	}
	if panels[1].Bucket == nil || *panels[1].Bucket != "week" {
		t.Errorf("panel bucket = %v, want week", panels[1].Bucket)
	}
}

func TestPanelUpdateAndScoping(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	alice := seedNamedAccount(t, models, "alice@example.com")
	bob := seedNamedAccount(t, models, "bob@example.com")
	d := &Dashboard{AccountID: alice, Name: "D", RangePreset: "30d"}
	if err := models.Dashboards.Insert(ctx, d); err != nil {
		t.Fatalf("Insert dashboard: %v", err)
	}
	p := &Panel{DashboardID: d.ID, AccountID: alice, Metric: "steps", ChartType: "bar", Width: 1}
	if err := models.Panels.Insert(ctx, p); err != nil {
		t.Fatalf("Insert panel: %v", err)
	}

	p.ChartType = "area"
	p.Width = 3
	p.Bucket = ptr("month")
	if err := models.Panels.Update(ctx, p); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := models.Panels.GetByID(ctx, alice, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ChartType != "area" || got.Width != 3 || got.Bucket == nil || *got.Bucket != "month" {
		t.Errorf("panel update not persisted: %+v", got)
	}

	// Bob cannot update Alice's panel.
	intruder := &Panel{ID: p.ID, AccountID: bob, Metric: "steps", ChartType: "line", Width: 1}
	if err := models.Panels.Update(ctx, intruder); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("cross-account panel Update = %v, want ErrRecordNotFound", err)
	}
}

func TestPanelReorderSetsPositions(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedNamedAccount(t, models, "a@example.com")
	d := &Dashboard{AccountID: acc, Name: "D", RangePreset: "30d"}
	if err := models.Dashboards.Insert(ctx, d); err != nil {
		t.Fatalf("Insert dashboard: %v", err)
	}
	var ids []int64
	for _, metric := range []string{"steps", "heart_rate", "body_mass"} {
		p := &Panel{DashboardID: d.ID, AccountID: acc, Metric: metric, ChartType: "bar", Width: 1}
		if err := models.Panels.Insert(ctx, p); err != nil {
			t.Fatalf("Insert panel %s: %v", metric, err)
		}
		ids = append(ids, p.ID)
	}

	// Reverse the order.
	reversed := []int64{ids[2], ids[1], ids[0]}
	if err := models.Panels.Reorder(ctx, acc, d.ID, reversed); err != nil {
		t.Fatalf("Reorder: %v", err)
	}

	panels, err := models.Panels.ListByDashboard(ctx, acc, d.ID)
	if err != nil {
		t.Fatalf("ListByDashboard: %v", err)
	}
	if panels[0].Metric != "body_mass" || panels[2].Metric != "steps" {
		t.Errorf("reorder not applied: %q .. %q", panels[0].Metric, panels[2].Metric)
	}
}

func TestPanelDeleteScoped(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	alice := seedNamedAccount(t, models, "alice@example.com")
	bob := seedNamedAccount(t, models, "bob@example.com")
	d := &Dashboard{AccountID: alice, Name: "D", RangePreset: "30d"}
	if err := models.Dashboards.Insert(ctx, d); err != nil {
		t.Fatalf("Insert dashboard: %v", err)
	}
	p := &Panel{DashboardID: d.ID, AccountID: alice, Metric: "steps", ChartType: "bar", Width: 1}
	if err := models.Panels.Insert(ctx, p); err != nil {
		t.Fatalf("Insert panel: %v", err)
	}

	if err := models.Panels.Delete(ctx, bob, p.ID); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("cross-account Delete = %v, want ErrRecordNotFound", err)
	}
	if err := models.Panels.Delete(ctx, alice, p.ID); err != nil {
		t.Errorf("owner Delete: %v", err)
	}
}
