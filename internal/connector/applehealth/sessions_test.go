package applehealth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeOpener serves route GPX bytes by their FileReference path, standing in for
// a directory or zip during tests.
type fakeOpener map[string]string

func (f fakeOpener) open(ref string) (io.ReadCloser, error) {
	c, ok := f[ref]
	if !ok {
		return nil, fmt.Errorf("fakeOpener: no route %s", ref)
	}
	return io.NopCloser(strings.NewReader(c)), nil
}

// workoutXML is two workouts: a run with distance, active energy and a GPS
// route, and a strength session with neither distance nor route.
const workoutXML = `<HealthData locale="en_US">
 <Workout workoutActivityType="HKWorkoutActivityTypeRunning" duration="30" durationUnit="min" sourceName="Nike Run Club" startDate="2023-03-15 14:19:08 +0000" endDate="2023-03-15 14:49:08 +0000">
  <WorkoutStatistics type="HKQuantityTypeIdentifierActiveEnergyBurned" sum="499.977" unit="kcal"/>
  <WorkoutStatistics type="HKQuantityTypeIdentifierDistanceWalkingRunning" sum="5.67128" unit="km"/>
  <WorkoutRoute sourceName="Apple Watch" startDate="2023-03-15 14:19:08 +0000" endDate="2023-03-15 14:49:08 +0000">
   <MetadataEntry key="HKMetadataKeySyncVersion" value="2"/>
   <FileReference path="/workout-routes/route_2023-03-15_2.49pm.gpx"/>
  </WorkoutRoute>
 </Workout>
 <Workout workoutActivityType="HKWorkoutActivityTypeTraditionalStrengthTraining" duration="45" durationUnit="min" sourceName="Fitbod" startDate="2023-03-16 09:00:00 +0000" endDate="2023-03-16 09:45:00 +0000">
  <WorkoutStatistics type="HKQuantityTypeIdentifierActiveEnergyBurned" sum="300" unit="kcal"/>
 </Workout>
</HealthData>`

const gpxBody = `<?xml version="1.0"?><gpx><trk><trkpt lat="48.1" lon="2.3"/></trk></gpx>`

const routeRef1 = "/workout-routes/route_2023-03-15_2.49pm.gpx"

func TestImportStreamSessionsAndRoutes(t *testing.T) {
	store, db, acc := openStore(t)
	ctx := context.Background()
	artifacts := t.TempDir()
	opener := fakeOpener{routeRef1: gpxBody}

	report, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(workoutXML), artifacts, opener)
	if err != nil {
		t.Fatalf("importStream: %v", err)
	}

	if report.SessionsAdded != 2 {
		t.Errorf("SessionsAdded = %d, want 2", report.SessionsAdded)
	}
	if report.RoutesAdded != 1 {
		t.Errorf("RoutesAdded = %d, want 1", report.RoutesAdded)
	}
	if got := report.PerActivity["running"].Added; got != 1 {
		t.Errorf("running sessions = %d, want 1", got)
	}
	if got := report.PerActivity["traditional_strength_training"].Added; got != 1 {
		t.Errorf("strength sessions = %d, want 1", got)
	}

	// The run: duration normalized to seconds, distance km, active energy kcal.
	var activity, start string
	var duration float64
	var distance, energy *float64
	err = db.QueryRowContext(ctx,
		`SELECT activity_type, start_at, duration, total_distance, total_energy
		 FROM sessions WHERE account_id = ? AND activity_type = 'running'`, acc).
		Scan(&activity, &start, &duration, &distance, &energy)
	if err != nil {
		t.Fatalf("select running session: %v", err)
	}
	if duration != 1800 {
		t.Errorf("duration = %v, want 1800 s", duration)
	}
	if start != "2023-03-15T14:19:08Z" {
		t.Errorf("start_at = %q, want RFC3339 UTC", start)
	}
	if distance == nil || *distance != 5.67128 {
		t.Errorf("total_distance = %v, want 5.67128 km", distance)
	}
	if energy == nil || *energy != 499.977 {
		t.Errorf("total_energy = %v, want 499.977 kcal", energy)
	}

	// The strength session has no distance: total_distance is NULL, not 0.
	if err := db.QueryRowContext(ctx,
		`SELECT total_distance FROM sessions WHERE account_id = ? AND activity_type = 'traditional_strength_training'`, acc).
		Scan(&distance); err != nil {
		t.Fatalf("select strength session: %v", err)
	}
	if distance != nil {
		t.Errorf("strength total_distance = %v, want NULL", *distance)
	}

	// The route: artifact is content-addressed, the file was copied to disk, and
	// the row points at the run's session.
	wantKey := sha256.Sum256([]byte(gpxBody))
	wantArtifact := hex.EncodeToString(wantKey[:]) + ".gpx"
	var artifact string
	var sessionID, runSessionID int64
	if err := db.QueryRowContext(ctx,
		`SELECT artifact, session_id FROM routes WHERE account_id = ?`, acc).
		Scan(&artifact, &sessionID); err != nil {
		t.Fatalf("select route: %v", err)
	}
	if artifact != wantArtifact {
		t.Errorf("artifact = %q, want %q", artifact, wantArtifact)
	}
	if _, err := os.Stat(filepath.Join(artifacts, artifact)); err != nil {
		t.Errorf("route artifact not on disk: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM sessions WHERE account_id = ? AND activity_type = 'running'`, acc).
		Scan(&runSessionID); err != nil {
		t.Fatalf("select run id: %v", err)
	}
	if sessionID != runSessionID {
		t.Errorf("route session_id = %d, want run session %d", sessionID, runSessionID)
	}
}

// TestImportStreamSessionsIdempotent is the acceptance guard for re-import:
// sessions, states and routes all add nothing the second time (ADR 0006).
func TestImportStreamSessionsIdempotent(t *testing.T) {
	store, db, acc := openStore(t)
	ctx := context.Background()
	artifacts := t.TempDir()
	opener := fakeOpener{routeRef1: gpxBody}

	if _, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(workoutXML), artifacts, opener); err != nil {
		t.Fatalf("first import: %v", err)
	}
	report, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(workoutXML), artifacts, opener)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if report.SessionsAdded != 0 || report.SessionsSkipped != 2 {
		t.Errorf("re-import sessions = %d added, %d skipped, want 0/2", report.SessionsAdded, report.SessionsSkipped)
	}
	if report.RoutesAdded != 0 || report.RoutesSkipped != 1 {
		t.Errorf("re-import routes = %d added, %d skipped, want 0/1", report.RoutesAdded, report.RoutesSkipped)
	}

	for _, tbl := range []struct {
		name string
		want int
	}{{"sessions", 2}, {"routes", 1}} {
		var n int
		if err := db.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT count(*) FROM %s WHERE account_id = ?`, tbl.name), acc).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", tbl.name, err)
		}
		if n != tbl.want {
			t.Errorf("%s rows after re-import = %d, want %d", tbl.name, n, tbl.want)
		}
	}
}
