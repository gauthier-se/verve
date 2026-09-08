package query

import (
	"testing"

	"github.com/gauthier-se/verve/internal/timeaxis"
)

// TestBucketBoundaryGoSQLAgree is the executable contract between the two bucket
// implementations: for a sweep of instants across day / ISO-week / month / year /
// leap-day boundaries, the label bucketSQL emits (real SQLite) must equal the Go
// label timeaxis.Bucket.Start computes. A divergence here silently turns baseline
// buckets into gaps.
//
// It lives in this package rather than in timeaxis because it is the one assertion
// that needs both sides: timeaxis is DB-free by design, and the SQL half of the
// boundary rule is this engine's private translation of it.
func TestBucketBoundaryGoSQLAgree(t *testing.T) {
	e, _, _ := setup(t)

	instants := []string{
		"2024-01-01T00:00:00Z", // Monday, month + year start
		"2024-01-01T23:59:59Z",
		"2024-01-07T12:00:00Z", // Sunday, last day of the week starting Jan 1
		"2024-01-08T00:00:00Z", // next Monday
		"2024-02-29T15:00:00Z", // leap day
		"2024-03-01T00:00:00Z",
		"2023-12-31T18:00:00Z", // Sunday, year end
		"2024-06-15T09:30:00Z",
		"2025-02-28T00:00:00Z",
	}

	for _, b := range []timeaxis.Bucket{timeaxis.Day, timeaxis.Week, timeaxis.Month} {
		q := "SELECT " + bucketSQL(b) + " FROM (SELECT ? AS start_at)"
		for _, s := range instants {
			ts := mustTime(t, s)
			var sqlLabel string
			if err := e.DB.QueryRow(q, rfc3339(ts)).Scan(&sqlLabel); err != nil {
				t.Fatalf("%s @ %s: sql label: %v", b, s, err)
			}
			if goLabel := b.Start(ts); sqlLabel != goLabel {
				t.Errorf("%s @ %s: SQL=%s Go=%s", b, s, sqlLabel, goLabel)
			}
		}
	}
}
