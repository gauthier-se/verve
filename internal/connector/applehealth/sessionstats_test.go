package applehealth

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/gauthier-se/verve/internal/data"
)

// statsXML is one run carrying the four shapes a WorkoutStatistics takes: a sum
// in a non-canonical unit (metres, canonically km), a statistic with average,
// minimum and maximum but no sum at all (which the pre-milestone import dropped
// whole), a plain count sum, and a type Verve does not model.
const statsXML = `<HealthData locale="en_US">
 <Workout workoutActivityType="HKWorkoutActivityTypeRunning" duration="30" durationUnit="min" sourceName="Apple Watch" startDate="2024-05-01 06:00:00 +0000" endDate="2024-05-01 06:30:00 +0000">
  <WorkoutStatistics type="HKQuantityTypeIdentifierDistanceWalkingRunning" sum="5670" unit="m"/>
  <WorkoutStatistics type="HKQuantityTypeIdentifierHeartRate" average="148.2" minimum="96" maximum="178" unit="count/min"/>
  <WorkoutStatistics type="HKQuantityTypeIdentifierStepCount" sum="5120" unit="count"/>
  <WorkoutStatistics type="HKQuantityTypeIdentifierNotAThingVerveModels" sum="42" unit="count"/>
 </Workout>
</HealthData>`

// readStats returns a Session's stats keyed "metric/stat".
func readStats(t *testing.T, db *sql.DB, accountID int64) map[string]float64 {
	t.Helper()
	rows, err := db.Query(
		`SELECT s.metric, s.stat, s.value
		   FROM session_stats s JOIN sessions w ON w.id = s.session_id
		  WHERE w.account_id = ?`, accountID)
	if err != nil {
		t.Fatalf("select session stats: %v", err)
	}
	defer rows.Close()
	got := map[string]float64{}
	for rows.Next() {
		var metric, stat string
		var value float64
		if err := rows.Scan(&metric, &stat, &value); err != nil {
			t.Fatalf("scan stat: %v", err)
		}
		got[metric+"/"+stat] = value
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("stat rows: %v", err)
	}
	return got
}

// TestWorkoutStatisticsAreKeptWhole guards the widening: every aggregate of every
// mapped type is stored, in the Metric's canonical unit, and an unmapped type is
// skipped rather than landing in a second unmapped bin.
func TestWorkoutStatisticsAreKeptWhole(t *testing.T) {
	store, db, acc := openStore(t)
	ctx := context.Background()

	if _, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(statsXML), t.TempDir(), fakeOpener{}); err != nil {
		t.Fatalf("importStream: %v", err)
	}

	got := readStats(t, db, acc)
	want := map[string]float64{
		"distance_walking_running/sum": 5.67, // metres in, canonical km out
		"heart_rate/average":           148.2,
		"heart_rate/min":               96,
		"heart_rate/max":               178,
		"steps/sum":                    5120,
	}
	if len(got) != len(want) {
		t.Errorf("stored %d stats, want %d: %v", len(got), len(want), got)
	}
	for key, wantValue := range want {
		v, ok := got[key]
		if !ok {
			t.Errorf("stat %s missing", key)
			continue
		}
		if diff := v - wantValue; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("stat %s = %v, want %v", key, v, wantValue)
		}
	}
	if _, ok := got["not_a_thing_verve_models/sum"]; ok {
		t.Error("an unmapped statistic type was stored")
	}
}

// TestWorkoutStatisticsWithoutSum is the case the pre-milestone import dropped
// entirely: heart rate reports an average, a minimum and a maximum and never a
// sum, so a reader that only looked at sum saw nothing at all.
func TestWorkoutStatisticsWithoutSum(t *testing.T) {
	store, db, acc := openStore(t)
	ctx := context.Background()

	if _, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(statsXML), t.TempDir(), fakeOpener{}); err != nil {
		t.Fatalf("importStream: %v", err)
	}

	got := readStats(t, db, acc)
	for _, stat := range []string{data.StatAverage, data.StatMin, data.StatMax} {
		if _, ok := got["heart_rate/"+stat]; !ok {
			t.Errorf("heart_rate/%s missing", stat)
		}
	}
	if _, ok := got["heart_rate/sum"]; ok {
		t.Error("heart_rate/sum stored, but the export reports no sum")
	}
}

// TestPromotedColumnsUseTheirOwnUnit guards the disagreement between a stat's
// canonical unit and the promoted column's: swimming distance is canonically
// metres while sessions.total_distance is documented in km, so promoting the
// converted stat would silently store metres in a km column.
func TestPromotedColumnsUseTheirOwnUnit(t *testing.T) {
	const swimXML = `<HealthData locale="en_US">
 <Workout workoutActivityType="HKWorkoutActivityTypeSwimming" duration="40" durationUnit="min" sourceName="Apple Watch" startDate="2024-05-02 07:00:00 +0000" endDate="2024-05-02 07:40:00 +0000">
  <WorkoutStatistics type="HKQuantityTypeIdentifierDistanceSwimming" sum="1500" unit="m"/>
 </Workout>
</HealthData>`

	store, db, acc := openStore(t)
	ctx := context.Background()

	if _, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(swimXML), t.TempDir(), fakeOpener{}); err != nil {
		t.Fatalf("importStream: %v", err)
	}

	var distance *float64
	if err := db.QueryRowContext(ctx,
		`SELECT total_distance FROM sessions WHERE account_id = ? AND activity_type = 'swimming'`, acc).
		Scan(&distance); err != nil {
		t.Fatalf("select swim: %v", err)
	}
	if distance == nil || *distance != 1.5 {
		t.Errorf("total_distance = %v, want 1.5 (km)", distance)
	}

	// The stat itself keeps the Catalog's unit for the Metric, which is metres.
	if got := readStats(t, db, acc)["distance_swimming/sum"]; got != 1500 {
		t.Errorf("distance_swimming/sum = %v, want 1500 (m)", got)
	}
}

// TestReimportConverges is the point of this issue. Idempotent, for a Session,
// must mean convergent and not inert: a workout already imported before the
// statistics were captured has to gain them when the same export is dropped
// again. Attaching stats only on the newly-inserted branch would leave every
// database that already exists stat-less forever, which is every database.
func TestReimportConverges(t *testing.T) {
	store, db, acc := openStore(t)
	ctx := context.Background()

	// First import: the workout, with no statistics at all.
	const bare = `<HealthData locale="en_US">
 <Workout workoutActivityType="HKWorkoutActivityTypeRunning" duration="30" durationUnit="min" sourceName="Apple Watch" startDate="2024-05-01 06:00:00 +0000" endDate="2024-05-01 06:30:00 +0000">
 </Workout>
</HealthData>`
	if _, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(bare), t.TempDir(), fakeOpener{}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if got := len(readStats(t, db, acc)); got != 0 {
		t.Fatalf("stats after bare import = %d, want 0", got)
	}

	// Second import: the same workout, now carrying its statistics.
	report, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(statsXML), t.TempDir(), fakeOpener{})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if report.SessionsAdded != 0 || report.SessionsSkipped != 1 {
		t.Errorf("second import added %d skipped %d, want 0 added 1 skipped",
			report.SessionsAdded, report.SessionsSkipped)
	}

	var sessions int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE account_id = ?`, acc).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 1 {
		t.Errorf("sessions = %d, want 1 (the workout must not be duplicated)", sessions)
	}
	if got := readStats(t, db, acc); len(got) != 5 {
		t.Errorf("stats after re-import = %d, want 5: %v", len(got), got)
	}
}

// TestReimportCorrectsAValue is the other half of convergence: a corrected
// export must correct the stored figure rather than duplicate the row, which is
// why the insert conflicts into an update and not into an ignore.
func TestReimportCorrectsAValue(t *testing.T) {
	store, db, acc := openStore(t)
	ctx := context.Background()

	if _, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(statsXML), t.TempDir(), fakeOpener{}); err != nil {
		t.Fatalf("first import: %v", err)
	}

	corrected := strings.Replace(statsXML, `maximum="178"`, `maximum="181"`, 1)
	if _, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(corrected), t.TempDir(), fakeOpener{}); err != nil {
		t.Fatalf("second import: %v", err)
	}

	got := readStats(t, db, acc)
	if got["heart_rate/max"] != 181 {
		t.Errorf("heart_rate/max = %v, want 181", got["heart_rate/max"])
	}
	if len(got) != 5 {
		t.Errorf("stats = %d, want 5 (a correction must not duplicate): %v", len(got), got)
	}
}

// TestSessionStatsCascade: a stat is reachable only through its Session and
// cannot outlive it.
func TestSessionStatsCascade(t *testing.T) {
	store, db, acc := openStore(t)
	ctx := context.Background()

	if _, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(statsXML), t.TempDir(), fakeOpener{}); err != nil {
		t.Fatalf("importStream: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE account_id = ?`, acc); err != nil {
		t.Fatalf("delete sessions: %v", err)
	}

	var stats int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM session_stats`).Scan(&stats); err != nil {
		t.Fatalf("count stats: %v", err)
	}
	if stats != 0 {
		t.Errorf("session_stats rows after deleting the sessions = %d, want 0", stats)
	}
}
