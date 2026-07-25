package data

import (
	"context"
	"errors"
	"testing"
)

func TestOpenClosesThePreviousPhase(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	first, err := models.Phases.Open(ctx, acc, -0.5, "2026-06-01T00:00:00Z")
	if err != nil {
		t.Fatalf("Open first: %v", err)
	}
	second, err := models.Phases.Open(ctx, acc, 0.25, "2026-07-01T00:00:00Z")
	if err != nil {
		t.Fatalf("Open second: %v", err)
	}

	history, err := models.Phases.ListByAccount(ctx, acc)
	if err != nil {
		t.Fatalf("ListByAccount: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history has %d phases, want 2 — the first was overwritten", len(history))
	}
	if history[0].ID != second.ID {
		t.Error("history is not newest-first")
	}

	// The windows must be contiguous: the closed Phase ends exactly where the new one
	// begins, so adherence over the past has no gap and no overlap.
	closed, err := models.Phases.GetByID(ctx, acc, first.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if closed.EndedAt == nil {
		t.Fatal("the previous phase was left open")
	}
	if *closed.EndedAt != second.StartedAt {
		t.Errorf("ended_at = %q, want the new phase's start %q", *closed.EndedAt, second.StartedAt)
	}
	if second.EndedAt != nil {
		t.Error("the new phase is not open")
	}
}

// TestOnlyOnePhaseOpenPerAccount pins the partial unique index. It is the database, not
// application logic, that must refuse a second open Phase — otherwise a race leaves two
// open and every adherence figure afterwards is ambiguous.
func TestOnlyOnePhaseOpenPerAccount(t *testing.T) {
	db, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	if _, err := models.Phases.Open(ctx, acc, -0.5, "2026-06-01T00:00:00Z"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Bypass Open() (which closes the current one) to hit the index directly.
	_, err := db.ExecContext(ctx,
		`INSERT INTO phases (account_id, rate_pct_per_week, started_at) VALUES (?, ?, ?)`,
		acc, 0.25, "2026-07-01T00:00:00Z")
	if err == nil {
		t.Fatal("a second open phase was accepted; the partial unique index is missing")
	}
}

func TestPhasesAreAccountScoped(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	owner := seedAccount(t, models)

	other := &Account{Email: "other@example.com"}
	if err := models.Accounts.Insert(ctx, other); err != nil {
		t.Fatalf("insert other: %v", err)
	}

	// Both Accounts may hold an open Phase: the index is per account_id.
	mine, err := models.Phases.Open(ctx, owner, -0.5, "2026-07-01T00:00:00Z")
	if err != nil {
		t.Fatalf("Open owner: %v", err)
	}
	if _, err := models.Phases.Open(ctx, other.ID, 0.25, "2026-07-01T00:00:00Z"); err != nil {
		t.Fatalf("Open other: %v", err)
	}

	if _, err := models.Phases.GetByID(ctx, other.ID, mine.ID); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("cross-account GetByID = %v, want ErrRecordNotFound", err)
	}
	if err := models.Phases.Delete(ctx, other.ID, mine.ID); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("cross-account Delete = %v, want ErrRecordNotFound", err)
	}
	if _, err := models.Phases.GetByID(ctx, owner, mine.ID); err != nil {
		t.Errorf("owner's phase was removed by another Account: %v", err)
	}
}

func TestCurrentReturnsOnlyTheOpenPhase(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	if _, err := models.Phases.Current(ctx, acc); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("Current with no phase = %v, want ErrRecordNotFound", err)
	}

	opened, err := models.Phases.Open(ctx, acc, -0.5, "2026-07-01T00:00:00Z")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	current, err := models.Phases.Current(ctx, acc)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current.ID != opened.ID {
		t.Errorf("Current = %d, want %d", current.ID, opened.ID)
	}

	if err := models.Phases.Close(ctx, acc, opened.ID, "2026-07-20T00:00:00Z"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := models.Phases.Current(ctx, acc); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("Current after close = %v, want ErrRecordNotFound", err)
	}
}

// TestCloseIsNotIdempotent: re-closing would move the end date and rewrite history, so
// it must fail rather than silently succeed.
func TestCloseIsNotIdempotent(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	p, err := models.Phases.Open(ctx, acc, -0.5, "2026-07-01T00:00:00Z")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := models.Phases.Close(ctx, acc, p.ID, "2026-07-20T00:00:00Z"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := models.Phases.Close(ctx, acc, p.ID, "2026-07-25T00:00:00Z"); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("second Close = %v, want ErrRecordNotFound", err)
	}
	closed, err := models.Phases.GetByID(ctx, acc, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if *closed.EndedAt != "2026-07-20T00:00:00Z" {
		t.Errorf("ended_at = %q; the second close rewrote history", *closed.EndedAt)
	}
}

func TestDeletePhaseFreesTheOpenSlot(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	p, err := models.Phases.Open(ctx, acc, -9, "2026-07-01T00:00:00Z") // a mis-typed rate
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := models.Phases.Delete(ctx, acc, p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := models.Phases.Open(ctx, acc, -0.5, "2026-07-02T00:00:00Z"); err != nil {
		t.Fatalf("Open after delete: %v", err)
	}
}

func TestUpdateProfilePatchesNamedFieldsOnly(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	dob, sex := "1996-03-15", "male"
	if err := models.Accounts.UpdateProfile(ctx, acc, ProfilePatch{
		DateOfBirth: ptrTo(&dob), BiologicalSex: ptrTo(&sex),
	}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	trust := "estimated"
	if err := models.Accounts.UpdateProfile(ctx, acc, ProfilePatch{
		BodyCompositionTrust: ptrTo(&trust),
	}); err != nil {
		t.Fatalf("UpdateProfile trust: %v", err)
	}

	got, err := models.Accounts.GetByID(ctx, acc)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.DateOfBirth == nil || *got.DateOfBirth != dob {
		t.Errorf("date_of_birth = %v; the trust update blanked it", got.DateOfBirth)
	}
	if got.BiologicalSex == nil || *got.BiologicalSex != sex {
		t.Errorf("biological_sex = %v; the trust update blanked it", got.BiologicalSex)
	}
	if got.BodyCompositionTrust == nil || *got.BodyCompositionTrust != trust {
		t.Errorf("trust = %v, want %q", got.BodyCompositionTrust, trust)
	}

	// An explicit null clears one column and leaves the rest.
	var null *string
	if err := models.Accounts.UpdateProfile(ctx, acc, ProfilePatch{BiologicalSex: ptrTo(null)}); err != nil {
		t.Fatalf("UpdateProfile clear: %v", err)
	}
	got, _ = models.Accounts.GetByID(ctx, acc)
	if got.BiologicalSex != nil {
		t.Errorf("biological_sex = %v, want cleared", *got.BiologicalSex)
	}
	if got.DateOfBirth == nil {
		t.Error("clearing sex also cleared the date of birth")
	}
}

func ptrTo[T any](v T) *T { return &v }
