package query

import (
	"testing"
)

// TestBucketBoundaryGoSQLAgree is the executable contract between the two bucket
// implementations: for a sweep of instants across day / ISO-week / month / year /
// leap-day boundaries, the label sqlExpr emits (real SQLite) must equal snap's Go
// label. A divergence here silently turns baseline buckets into gaps.
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

	for _, b := range []Bucket{Day, Week, Month} {
		q := "SELECT " + b.sqlExpr() + " FROM (SELECT ? AS start_at)"
		for _, s := range instants {
			ts := mustTime(t, s)
			var sqlLabel string
			if err := e.DB.QueryRow(q, rfc3339(ts)).Scan(&sqlLabel); err != nil {
				t.Fatalf("%s @ %s: sql label: %v", b, s, err)
			}
			if goLabel := b.snap(ts).Format("2006-01-02"); sqlLabel != goLabel {
				t.Errorf("%s @ %s: SQL=%s Go=%s", b, s, sqlLabel, goLabel)
			}
		}
	}
}

func TestBucketStarts(t *testing.T) {
	cases := []struct {
		b        Bucket
		from, to string
		want     []string
	}{
		{Day, "2024-01-01T00:00:00Z", "2024-01-04T00:00:00Z", []string{"2024-01-01", "2024-01-02", "2024-01-03"}},
		{Day, "2024-02-28T00:00:00Z", "2024-03-01T00:00:00Z", []string{"2024-02-28", "2024-02-29"}}, // leap
		{Week, "2024-01-03T00:00:00Z", "2024-01-20T00:00:00Z", []string{"2024-01-01", "2024-01-08", "2024-01-15"}},
		{Month, "2024-01-15T00:00:00Z", "2024-04-01T00:00:00Z", []string{"2024-01-01", "2024-02-01", "2024-03-01"}},
	}
	for _, c := range cases {
		got := c.b.starts(mustTime(t, c.from), mustTime(t, c.to))
		if len(got) != len(c.want) {
			t.Errorf("%s [%s,%s): got %v, want %v", c.b, c.from, c.to, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s [%s,%s): got %v, want %v", c.b, c.from, c.to, got, c.want)
				break
			}
		}
	}
}

func TestBucketNext(t *testing.T) {
	cases := []struct {
		b        Bucket
		in, want string
	}{
		{Day, "2024-02-28T00:00:00Z", "2024-02-29T00:00:00Z"}, // leap rollover
		{Week, "2024-01-01T00:00:00Z", "2024-01-08T00:00:00Z"},
		{Month, "2024-01-01T00:00:00Z", "2024-02-01T00:00:00Z"},
		{Month, "2024-12-01T00:00:00Z", "2025-01-01T00:00:00Z"}, // year rollover
	}
	for _, c := range cases {
		if got := c.b.next(mustTime(t, c.in)); !got.Equal(mustTime(t, c.want)) {
			t.Errorf("%s next(%s) = %s, want %s", c.b, c.in, got.Format("2006-01-02"), c.want)
		}
	}
}
