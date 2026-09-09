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

// TestInsertWorkoutIsAtomic: a workout is one thing an Account did. If any part of
// the write fails, none of it stands, so nothing downstream can read a Session
// carrying half its figures and mistake it for one that recorded only those.
func TestInsertWorkoutIsAtomic(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	sess := Session{
		AccountID: acc, ActivityType: "running",
		StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T09:00:00Z",
		Source: "Watch", ContentKey: "w1",
	}
	// A Route naming an Account that does not exist: routes.account_id is a foreign
	// key, so the third of the three writes fails after the other two succeeded.
	// Set explicitly, because an unset one is filled in from the Session.
	routes := []Route{{
		AccountID: 999999, Artifact: "abc.gpx",
		StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T09:00:00Z",
		Source: "Watch", ContentKey: "r1",
	}}
	stats := []SessionStat{{Metric: "heart_rate", Stat: "average", Value: 142}}

	if _, err := models.Sessions.InsertWorkout(ctx, &sess, stats, routes); err == nil {
		t.Fatal("the route insert was accepted: the foreign key this test relies on is gone")
	}

	// Nothing stands: not the Session, not its stats.
	var sessions, statRows int
	if err := models.Sessions.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE account_id = ?`, acc).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if err := models.Sessions.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_stats`).Scan(&statRows); err != nil {
		t.Fatalf("count stats: %v", err)
	}
	if sessions != 0 || statRows != 0 {
		t.Errorf("after a failed workout write: %d sessions, %d stats; want none of it to stand", sessions, statRows)
	}
}
