package data

import (
	"context"
	"errors"
	"testing"

	"github.com/gauthier-se/verve/internal/catalog"
)

// manual builds a Manual entry with a real content key, so the idempotence tests
// exercise the same identity function the import path uses (ADR 0006).
func manual(acc int64, metric string, value float64, unit, at string) Measurement {
	return Measurement{
		AccountID:    acc,
		Metric:       metric,
		Value:        value,
		OriginalUnit: unit,
		StartAt:      at,
		EndAt:        at,
		Source:       catalog.SourceManual,
		ContentKey:   ContentKey(metric, catalog.SourceManual, at, at, "raw", unit),
	}
}

func TestInsertOnePopulatesID(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	row := manual(acc, "body_mass", 91, "kg", "2026-07-25T08:00:00Z")
	if err := models.Measurements.InsertOne(ctx, &row); err != nil {
		t.Fatalf("InsertOne: %v", err)
	}
	if row.ID == 0 {
		t.Fatal("InsertOne left ID zero")
	}

	got, err := models.Measurements.GetByID(ctx, acc, row.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Value != 91 || got.Source != catalog.SourceManual {
		t.Fatalf("round-trip = %v %q, want 91 %q", got.Value, got.Source, catalog.SourceManual)
	}
}

// TestInsertOneIsIdempotent pins that typing the same value twice yields one row and
// the same id — the same guarantee re-importing an export gets (ADR 0006, ADR 0022).
func TestInsertOneIsIdempotent(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	first := manual(acc, "height", 184, "cm", "2026-07-25T08:00:00Z")
	if err := models.Measurements.InsertOne(ctx, &first); err != nil {
		t.Fatalf("InsertOne first: %v", err)
	}

	second := manual(acc, "height", 184, "cm", "2026-07-25T08:00:00Z")
	if err := models.Measurements.InsertOne(ctx, &second); err != nil {
		t.Fatalf("InsertOne second: %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("second id = %d, want the existing %d", second.ID, first.ID)
	}
	rows, err := models.Measurements.ListManual(ctx, acc, "", 10)
	if err != nil {
		t.Fatalf("ListManual: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 — the second insert duplicated", len(rows))
	}
}

// TestDeleteRefusesImportedRow is the load-bearing test of ADR 0022: deleting an
// imported row would drop its content key and the next Import would silently restore
// it, so the guard lives in the statement and cannot be bypassed by any caller.
func TestDeleteRefusesImportedRow(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	imported := Measurement{
		AccountID: acc, Metric: "body_mass", Value: 91, OriginalUnit: "kg",
		StartAt: "2026-07-25T08:00:00Z", EndAt: "2026-07-25T08:00:00Z",
		Source: "Zepp Life", ContentKey: "imported-1",
	}
	if _, err := models.Measurements.InsertBatch(ctx, []Measurement{imported}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	var id int64
	if err := models.Measurements.DB.QueryRowContext(ctx,
		`SELECT id FROM measurements WHERE content_key = 'imported-1'`).Scan(&id); err != nil {
		t.Fatalf("resolve imported id: %v", err)
	}

	err := models.Measurements.Delete(ctx, acc, id)
	if !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("Delete on imported row = %v, want ErrRecordNotFound", err)
	}
	if _, err := models.Measurements.GetByID(ctx, acc, id); err != nil {
		t.Fatalf("imported row was removed anyway: %v", err)
	}
}

func TestDeleteRemovesManualRow(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	row := manual(acc, "body_fat_percentage", 0.22, "%", "2026-07-25T08:00:00Z")
	if err := models.Measurements.InsertOne(ctx, &row); err != nil {
		t.Fatalf("InsertOne: %v", err)
	}
	if err := models.Measurements.Delete(ctx, acc, row.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := models.Measurements.GetByID(ctx, acc, row.ID); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("GetByID after delete = %v, want ErrRecordNotFound", err)
	}
}

// TestDeleteIsAccountScoped keeps the Account isolation of ADR 0007 on the first
// destructive path in the app.
func TestDeleteIsAccountScoped(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	owner := seedAccount(t, models)

	other := &Account{Email: "other@example.com"}
	if err := models.Accounts.Insert(ctx, other); err != nil {
		t.Fatalf("insert other account: %v", err)
	}

	row := manual(owner, "body_mass", 91, "kg", "2026-07-25T08:00:00Z")
	if err := models.Measurements.InsertOne(ctx, &row); err != nil {
		t.Fatalf("InsertOne: %v", err)
	}

	if err := models.Measurements.Delete(ctx, other.ID, row.ID); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("cross-account Delete = %v, want ErrRecordNotFound", err)
	}
	if _, err := models.Measurements.GetByID(ctx, owner, row.ID); err != nil {
		t.Fatalf("owner's row was deleted by another Account: %v", err)
	}
}

// TestListManualExcludesImportedAndFilters checks the listing serves only what the
// Account can actually delete, newest first, and can narrow to one Metric.
func TestListManualExcludesImportedAndFilters(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	if _, err := models.Measurements.InsertBatch(ctx, []Measurement{{
		AccountID: acc, Metric: "body_mass", Value: 91, OriginalUnit: "kg",
		StartAt: "2026-07-20T08:00:00Z", EndAt: "2026-07-20T08:00:00Z",
		Source: "Zepp Life", ContentKey: "imported-1",
	}}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}

	older := manual(acc, "height", 184, "cm", "2026-07-21T08:00:00Z")
	newer := manual(acc, "body_mass", 90, "kg", "2026-07-24T08:00:00Z")
	for _, row := range []*Measurement{&older, &newer} {
		if err := models.Measurements.InsertOne(ctx, row); err != nil {
			t.Fatalf("InsertOne: %v", err)
		}
	}

	all, err := models.Measurements.ListManual(ctx, acc, "", 10)
	if err != nil {
		t.Fatalf("ListManual: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d rows, want 2 (the imported row must not be listed)", len(all))
	}
	if all[0].Metric != "body_mass" || all[1].Metric != "height" {
		t.Fatalf("order = %q, %q; want newest first", all[0].Metric, all[1].Metric)
	}

	filtered, err := models.Measurements.ListManual(ctx, acc, "height", 10)
	if err != nil {
		t.Fatalf("ListManual filtered: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Metric != "height" {
		t.Fatalf("filtered = %+v, want one height row", filtered)
	}
}
