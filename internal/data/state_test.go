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

// TestHasStates: the one question the Ledger asks of this family, scoped to the
// owning Account and to one kind.
func TestHasStates(t *testing.T) {
	_, models := openTestDB(t)
	acc := seedAccount(t, models)

	has, err := models.States.HasStates(context.Background(), acc, "sleep")
	if err != nil {
		t.Fatalf("HasStates: %v", err)
	}
	if has {
		t.Error("HasStates = true on an empty table, want false")
	}

	if _, err := models.States.InsertStateBatch(context.Background(), []State{{
		AccountID: acc, Kind: "sleep", StateValue: "asleep_core",
		StartAt: "2024-01-01T23:00:00Z", EndAt: "2024-01-02T06:00:00Z",
		Source: "Apple Watch", ContentKey: "k1",
	}}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	has, err = models.States.HasStates(context.Background(), acc, "sleep")
	if err != nil || !has {
		t.Errorf("HasStates(sleep) = %v (%v), want true", has, err)
	}
	// Another kind, and another Account, are separate questions.
	if has, _ := models.States.HasStates(context.Background(), acc, "stand"); has {
		t.Error("HasStates(stand) = true, want false")
	}
	if has, _ := models.States.HasStates(context.Background(), acc+1, "sleep"); has {
		t.Error("HasStates for another Account = true, want false")
	}
}
