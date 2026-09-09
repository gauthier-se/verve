package data

import (
	"context"
	"errors"
	"testing"
)

// TestTxRollsBackEverything: the point of the seam is that a caller outside this
// package can compose two writes across two families and have them stand or fall
// together. Before it existed, that needed a bespoke Models method each time.
func TestTxRollsBackEverything(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	boom := errors.New("boom")
	err := models.Tx(ctx, func(tx Models) error {
		if _, err := tx.Phases.Open(ctx, acc, -0.5, "2024-01-10"); err != nil {
			return err
		}
		note := &Annotation{AccountID: acc, Label: "Flu", StartsOn: "2024-01-15"}
		if err := tx.Annotations.Insert(ctx, note); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Tx error = %v, want the callback's", err)
	}

	phases, err := models.Phases.ListByAccount(ctx, acc)
	if err != nil {
		t.Fatalf("list phases: %v", err)
	}
	notes, err := models.Annotations.ListAll(ctx, acc)
	if err != nil {
		t.Fatalf("list annotations: %v", err)
	}
	if len(phases) != 0 || len(notes) != 0 {
		t.Errorf("after a rolled-back Tx: %d phases, %d notes; want neither to stand", len(phases), len(notes))
	}
}

// TestTxCommitsEverything is the other half: on a nil return both writes stand.
func TestTxCommitsEverything(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	if err := models.Tx(ctx, func(tx Models) error {
		if _, err := tx.Phases.Open(ctx, acc, 0.25, "2024-01-10"); err != nil {
			return err
		}
		return tx.Annotations.Insert(ctx, &Annotation{AccountID: acc, Label: "Trip", StartsOn: "2024-01-15"})
	}); err != nil {
		t.Fatalf("Tx: %v", err)
	}

	phases, _ := models.Phases.ListByAccount(ctx, acc)
	notes, _ := models.Annotations.ListAll(ctx, acc)
	if len(phases) != 1 || len(notes) != 1 {
		t.Errorf("after a committed Tx: %d phases, %d notes; want one of each", len(phases), len(notes))
	}
}

// TestTxIsReentrant: a model that keeps its own atomicity must still compose. If
// the inner write began a second transaction it would deadlock on the single
// connection rather than fail, so this test would hang rather than report.
func TestTxIsReentrant(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	if err := models.Tx(ctx, func(tx Models) error {
		// InsertBatch wraps itself in a transaction when called directly.
		_, err := tx.Measurements.InsertBatch(ctx, []Measurement{{
			AccountID: acc, Metric: "steps", Value: 900, OriginalUnit: "count",
			StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T08:00:00Z",
			Source: "Watch", ContentKey: "k1",
		}})
		return err
	}); err != nil {
		t.Fatalf("Tx: %v", err)
	}

	var n int
	if err := models.Measurements.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM measurements WHERE account_id = ?`, acc).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("measurements = %d, want the nested batch to have committed once", n)
	}
}
