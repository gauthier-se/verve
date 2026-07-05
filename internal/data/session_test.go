package data

import (
	"context"
	"testing"
)

func floatPtr(v float64) *float64 { return &v }

// TestInsertSessionReturnsExistingID guards the behavior routes depend on: a
// re-inserted workout is reported as not-new but still yields its existing id so
// its routes can be attached.
func TestInsertSessionReturnsExistingID(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	s := &Session{
		AccountID: acc, ActivityType: "running", StartAt: "2024-01-01T10:00:00Z",
		EndAt: "2024-01-01T10:30:00Z", Duration: 1800, TotalDistance: floatPtr(5.6),
		TotalEnergy: floatPtr(500), Source: "Nike Run Club", ContentKey: "w1",
	}
	inserted, err := models.Sessions.InsertSession(ctx, s)
	if err != nil {
		t.Fatalf("first InsertSession: %v", err)
	}
	if !inserted || s.ID == 0 {
		t.Fatalf("first insert = (%v, id=%d), want (true, non-zero)", inserted, s.ID)
	}
	firstID := s.ID

	again := &Session{
		AccountID: acc, ActivityType: "running", StartAt: "2024-01-01T10:00:00Z",
		EndAt: "2024-01-01T10:30:00Z", Duration: 1800, Source: "Nike Run Club", ContentKey: "w1",
	}
	inserted, err = models.Sessions.InsertSession(ctx, again)
	if err != nil {
		t.Fatalf("second InsertSession: %v", err)
	}
	if inserted {
		t.Error("re-inserting the same workout should report not-new")
	}
	if again.ID != firstID {
		t.Errorf("existing session id = %d, want %d", again.ID, firstID)
	}
}

// TestInsertSessionNullableTotals verifies a session with no distance/energy
// stores SQL NULL, not zero — so a strength workout is distinguishable from a
// zero-distance one.
func TestInsertSessionNullableTotals(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	s := &Session{
		AccountID: acc, ActivityType: "strength_training", StartAt: "2024-01-01T10:00:00Z",
		EndAt: "2024-01-01T10:45:00Z", Duration: 2700, Source: "Fitbod", ContentKey: "w2",
	}
	if _, err := models.Sessions.InsertSession(ctx, s); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}

	var distance, energy *float64
	if err := models.Sessions.DB.QueryRowContext(ctx,
		`SELECT total_distance, total_energy FROM sessions WHERE id = ?`, s.ID).
		Scan(&distance, &energy); err != nil {
		t.Fatalf("select: %v", err)
	}
	if distance != nil || energy != nil {
		t.Errorf("totals = (%v, %v), want (NULL, NULL)", distance, energy)
	}
}

func TestInsertRouteIdempotent(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	s := &Session{
		AccountID: acc, ActivityType: "running", StartAt: "2024-01-01T10:00:00Z",
		EndAt: "2024-01-01T10:30:00Z", Duration: 1800, Source: "Nike Run Club", ContentKey: "w1",
	}
	if _, err := models.Sessions.InsertSession(ctx, s); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}

	r := &Route{
		AccountID: acc, SessionID: s.ID, Artifact: "abc123.gpx",
		StartAt: "2024-01-01T10:00:00Z", EndAt: "2024-01-01T10:30:00Z",
		Source: "Apple Watch", ContentKey: "abc123",
	}
	inserted, err := models.Sessions.InsertRoute(ctx, r)
	if err != nil {
		t.Fatalf("first InsertRoute: %v", err)
	}
	if !inserted {
		t.Fatal("first route insert should report new")
	}
	inserted, err = models.Sessions.InsertRoute(ctx, r)
	if err != nil {
		t.Fatalf("second InsertRoute: %v", err)
	}
	if inserted {
		t.Error("re-inserting the same route should skip")
	}
}
