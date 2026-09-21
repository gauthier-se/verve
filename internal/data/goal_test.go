package data

import (
	"context"
	"errors"
	"testing"
)

func TestOpenGoalClosesThePreviousOnSameMetric(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	first, err := models.Goals.Open(ctx, acc, "steps", GoalAtLeast, 7000, "2026-03-01")
	if err != nil {
		t.Fatalf("Open first: %v", err)
	}
	other, err := models.Goals.Open(ctx, acc, "dietary_protein", GoalAtLeast, 150, "2026-03-01")
	if err != nil {
		t.Fatalf("Open other metric: %v", err)
	}
	second, err := models.Goals.Open(ctx, acc, "steps", GoalAtLeast, 7500, "2026-04-01")
	if err != nil {
		t.Fatalf("Open second: %v", err)
	}

	history, err := models.Goals.ListByAccount(ctx, acc, "steps")
	if err != nil {
		t.Fatalf("ListByAccount: %v", err)
	}
	if len(history) != 2 || history[0].ID != second.ID {
		t.Fatalf("history = %+v, want both steps Goals newest first", history)
	}

	// Contiguous: the closed Goal ends exactly where the new one begins, so no day is
	// judged twice and none is left out.
	closed, err := models.Goals.GetByID(ctx, acc, first.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if closed.EndedOn == nil || *closed.EndedOn != "2026-04-01" {
		t.Errorf("ended_on = %v, want the new Goal's start 2026-04-01", closed.EndedOn)
	}

	untouched, err := models.Goals.GetByID(ctx, acc, other.ID)
	if err != nil {
		t.Fatalf("GetByID other: %v", err)
	}
	if untouched.EndedOn != nil {
		t.Error("setting a steps Goal closed the protein Goal")
	}
}

// TestOnlyOneGoalOpenPerMetric pins the partial unique index, for the reason
// TestOnlyOnePhaseOpenPerAccount pins its own.
func TestOnlyOneGoalOpenPerMetric(t *testing.T) {
	db, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	if _, err := models.Goals.Open(ctx, acc, "steps", GoalAtLeast, 7000, "2026-03-01"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO goals (account_id, metric, direction, value, started_on) VALUES (?, ?, ?, ?, ?)`,
		acc, "steps", GoalAtLeast, 8000, "2026-04-01")
	if err == nil {
		t.Fatal("a second open Goal on one Metric was accepted; the partial unique index is missing")
	}
}

func TestOpenGoalRefusesToStartInsideAnEarlierOne(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	open, err := models.Goals.Open(ctx, acc, "steps", GoalAtLeast, 7000, "2026-03-10")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// On or before the open Goal's start would leave it an empty or inverted span.
	for _, day := range []string{"2026-03-01", "2026-03-10"} {
		if _, err := models.Goals.Open(ctx, acc, "steps", GoalAtLeast, 7500, day); !errors.Is(err, ErrGoalOverlap) {
			t.Errorf("start %s: err = %v, want ErrGoalOverlap", day, err)
		}
	}

	if err := models.Goals.Close(ctx, acc, open.ID, "2026-04-01"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Before a closed Goal's end would judge those days twice.
	if _, err := models.Goals.Open(ctx, acc, "steps", GoalAtLeast, 7500, "2026-03-20"); !errors.Is(err, ErrGoalOverlap) {
		t.Errorf("start inside a closed Goal: err = %v, want ErrGoalOverlap", err)
	}
	if _, err := models.Goals.Open(ctx, acc, "steps", GoalAtLeast, 7500, "2026-04-01"); err != nil {
		t.Errorf("start on a closed Goal's end: %v, want accepted", err)
	}
}

func TestGoalsInForceOverlapTheWindow(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	for _, g := range []struct {
		value float64
		start string
	}{{6000, "2026-01-01"}, {7000, "2026-03-01"}, {7500, "2026-05-01"}} {
		if _, err := models.Goals.Open(ctx, acc, "steps", GoalAtLeast, g.value, g.start); err != nil {
			t.Fatalf("Open %s: %v", g.start, err)
		}
	}

	cases := []struct {
		from, to string
		want     []float64
	}{
		{"2026-03-15", "2026-04-15", []float64{7000}},
		{"2026-02-15", "2026-03-15", []float64{6000, 7000}},
		// A window starting on a Goal's end does not see it: ended_on is exclusive.
		{"2026-03-01", "2026-03-02", []float64{7000}},
		// An open Goal runs to the end of any window.
		{"2027-01-01", "2027-02-01", []float64{7500}},
		{"2025-01-01", "2026-01-01", nil},
	}
	for _, c := range cases {
		got, err := models.Goals.InForce(ctx, acc, "steps", c.from, c.to)
		if err != nil {
			t.Fatalf("InForce: %v", err)
		}
		if len(got) != len(c.want) {
			t.Errorf("[%s, %s): %d Goals, want %v", c.from, c.to, len(got), c.want)
			continue
		}
		for i, g := range got {
			if g.Value != c.want[i] {
				t.Errorf("[%s, %s)[%d] = %v, want %v", c.from, c.to, i, g.Value, c.want[i])
			}
		}
	}
}

func TestCloseGoalTwiceIsRefused(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	g, err := models.Goals.Open(ctx, acc, "steps", GoalAtLeast, 7000, "2026-03-01")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := models.Goals.Close(ctx, acc, g.ID, "2026-04-01"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := models.Goals.Close(ctx, acc, g.ID, "2026-05-01"); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("second close: err = %v, want ErrRecordNotFound (moving the end rewrites history)", err)
	}
}

func TestGoalsAreAccountScoped(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	owner := seedAccount(t, models)

	other := &Account{Email: "other@example.com"}
	if err := models.Accounts.Insert(ctx, other); err != nil {
		t.Fatalf("insert other: %v", err)
	}
	g, err := models.Goals.Open(ctx, other.ID, "steps", GoalAtLeast, 7000, "2026-03-01")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// The owner may set its own Goal on the same Metric: the index is per Account.
	if _, err := models.Goals.Open(ctx, owner, "steps", GoalAtLeast, 9000, "2026-03-01"); err != nil {
		t.Fatalf("Open owner: %v", err)
	}
	if _, err := models.Goals.GetByID(ctx, owner, g.ID); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("GetByID across accounts: err = %v, want ErrRecordNotFound", err)
	}
	if err := models.Goals.Delete(ctx, owner, g.ID); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("Delete across accounts: err = %v, want ErrRecordNotFound", err)
	}
	listed, err := models.Goals.ListByAccount(ctx, owner, "")
	if err != nil {
		t.Fatalf("ListByAccount: %v", err)
	}
	if len(listed) != 1 || listed[0].Value != 9000 {
		t.Errorf("owner sees %+v, want only its own Goal", listed)
	}
}
