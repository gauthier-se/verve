package data

import (
	"context"
	"testing"
)

// seedAccount inserts an account and returns its ID for owning test rows.
func seedAccount(t *testing.T, models Models) int64 {
	t.Helper()
	acc := &Account{Email: "owner@example.com"}
	if err := models.Accounts.Insert(context.Background(), acc); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	return acc.ID
}

func TestInsertBatchReportsNewRows(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	batch := []Measurement{
		{AccountID: acc, Metric: "heart_rate", Value: 62, OriginalUnit: "count/min", StartAt: "2024-01-01T00:00:00Z", EndAt: "2024-01-01T00:00:00Z", Source: "Watch", ContentKey: "k1"},
		{AccountID: acc, Metric: "steps", Value: 100, OriginalUnit: "count", StartAt: "2024-01-01T00:00:00Z", EndAt: "2024-01-01T01:00:00Z", Source: "Watch", ContentKey: "k2"},
	}
	inserted, err := models.Measurements.InsertBatch(ctx, batch)
	if err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	if len(inserted) != 2 || !inserted[0] || !inserted[1] {
		t.Fatalf("inserted = %v, want [true true]", inserted)
	}
}

// TestInsertBatchIsIdempotent is the core acceptance guard: re-inserting the
// same content keys adds nothing and reports every row as skipped.
func TestInsertBatchIsIdempotent(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	batch := []Measurement{
		{AccountID: acc, Metric: "heart_rate", Value: 62, OriginalUnit: "count/min", StartAt: "2024-01-01T00:00:00Z", EndAt: "2024-01-01T00:00:00Z", Source: "Watch", ContentKey: "k1"},
		{AccountID: acc, Metric: "steps", Value: 100, OriginalUnit: "count", StartAt: "2024-01-01T00:00:00Z", EndAt: "2024-01-01T01:00:00Z", Source: "Watch", ContentKey: "k2"},
	}
	if _, err := models.Measurements.InsertBatch(ctx, batch); err != nil {
		t.Fatalf("first InsertBatch: %v", err)
	}

	inserted, err := models.Measurements.InsertBatch(ctx, batch)
	if err != nil {
		t.Fatalf("second InsertBatch: %v", err)
	}
	for i, ins := range inserted {
		if ins {
			t.Errorf("row %d reported inserted on re-import, want skipped", i)
		}
	}

	var count int
	if err := models.Measurements.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM measurements WHERE account_id = ?`, acc).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("measurement rows = %d, want 2 (no duplicates)", count)
	}
}

// TestContentKeyScopedPerAccount verifies the dedup identity is per-account: an
// identical content key under a different Account is a distinct row.
func TestContentKeyScopedPerAccount(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc1 := seedAccount(t, models)
	acc2 := &Account{Email: "other@example.com"}
	if err := models.Accounts.Insert(ctx, acc2); err != nil {
		t.Fatalf("second account: %v", err)
	}

	mk := func(owner int64) []Measurement {
		return []Measurement{{AccountID: owner, Metric: "steps", Value: 1, OriginalUnit: "count", StartAt: "t", EndAt: "t", Source: "s", ContentKey: "shared"}}
	}
	if _, err := models.Measurements.InsertBatch(ctx, mk(acc1)); err != nil {
		t.Fatalf("insert acc1: %v", err)
	}
	inserted, err := models.Measurements.InsertBatch(ctx, mk(acc2.ID))
	if err != nil {
		t.Fatalf("insert acc2: %v", err)
	}
	if !inserted[0] {
		t.Error("same content key under a different account should insert, not skip")
	}
}

func TestInsertUnmappedBatchIdempotent(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	batch := []UnmappedRecord{
		{AccountID: acc, SourceType: "HKCategoryTypeIdentifierSleepAnalysis", Value: "HKCategoryValueSleepAnalysisAsleep", Unit: "", StartAt: "2024-01-01T00:00:00Z", EndAt: "2024-01-01T08:00:00Z", Source: "Watch", ContentKey: "u1"},
	}
	if _, err := models.Measurements.InsertUnmappedBatch(ctx, batch); err != nil {
		t.Fatalf("first InsertUnmappedBatch: %v", err)
	}
	inserted, err := models.Measurements.InsertUnmappedBatch(ctx, batch)
	if err != nil {
		t.Fatalf("second InsertUnmappedBatch: %v", err)
	}
	if inserted[0] {
		t.Error("re-inserting the same unmapped record should skip")
	}
}

func TestRecordImport(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	imp := &Import{AccountID: acc, SourceFile: "export.xml", AddedCount: 10, SkippedCount: 2, UnmappedCount: 3}
	if err := models.Measurements.RecordImport(ctx, imp); err != nil {
		t.Fatalf("RecordImport: %v", err)
	}
	if imp.ID == 0 {
		t.Error("RecordImport did not populate ID")
	}
	if imp.ImportedAt == "" {
		t.Error("RecordImport did not populate ImportedAt")
	}
}
