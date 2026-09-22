package query

import (
	"context"
	"strings"
	"testing"
)

// The loose index scan is the whole reason migration 0017 exists: without it the
// Source walk reads every row the Account holds, on the screen that opens first.
// This fails the day the index is dropped or the query stops being able to seek.
func TestSourceLastDaysSeeksTheSourceIndex(t *testing.T) {
	e, _, acc := setup(t)
	rows, err := e.DB.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+sourceLastStartQuery, acc, "Manual")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()

	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan: %v", err)
		}
		plan = append(plan, detail)
	}
	joined := strings.Join(plan, "\n")
	for _, line := range plan {
		if strings.HasPrefix(line, "SCAN measurements") {
			t.Fatalf("full scan of measurements:\n%s", joined)
		}
	}
	if !strings.Contains(joined, "measurements_account_source_start") {
		t.Fatalf("plan does not use measurements_account_source_start:\n%s", joined)
	}
}

func TestSourceLastDaysAcrossFamilies(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 1, "2026-09-01T08:00:00Z", "Watch"},
		{"steps", 1, "2026-09-05T08:00:00Z", "Watch"},
		{"steps", 1, "2026-08-01T08:00:00Z", "iPhone"},
		{"body_mass", 75, "2026-09-20T08:00:00Z", "Manual"},
	})
	seedSleep(t, models, acc, []interval{
		{"asleep_core", "2026-09-05T23:00:00Z", "2026-09-06T06:00:00Z", "Watch"},
		{"asleep_core", "2026-07-01T23:00:00Z", "2026-07-02T06:00:00Z", "Ring"},
	})

	got, err := e.SourceLastDays(context.Background(), acc)
	if err != nil {
		t.Fatalf("SourceLastDays: %v", err)
	}
	want := []SourceLastDay{{"Ring", "2026-07-02"}, {"Watch", "2026-09-06"}, {"iPhone", "2026-08-01"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
}
