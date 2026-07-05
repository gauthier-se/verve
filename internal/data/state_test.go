package data

import (
	"context"
	"testing"
)

func TestInsertStateBatchIdempotent(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	batch := []State{
		{AccountID: acc, Kind: "sleep", StateValue: "asleep_core", StartAt: "2024-01-01T23:00:00Z", EndAt: "2024-01-02T00:00:00Z", Source: "Watch", ContentKey: "s1"},
		{AccountID: acc, Kind: "stand", StateValue: "stood", StartAt: "2024-01-01T13:00:00Z", EndAt: "2024-01-01T14:00:00Z", Source: "Watch", ContentKey: "s2"},
	}
	inserted, err := models.States.InsertStateBatch(ctx, batch)
	if err != nil {
		t.Fatalf("first InsertStateBatch: %v", err)
	}
	if !inserted[0] || !inserted[1] {
		t.Fatalf("inserted = %v, want [true true]", inserted)
	}

	inserted, err = models.States.InsertStateBatch(ctx, batch)
	if err != nil {
		t.Fatalf("second InsertStateBatch: %v", err)
	}
	if inserted[0] || inserted[1] {
		t.Errorf("re-import inserted = %v, want [false false]", inserted)
	}

	var count int
	if err := models.States.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM states WHERE account_id = ?`, acc).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("state rows = %d, want 2 (no duplicates)", count)
	}
}
