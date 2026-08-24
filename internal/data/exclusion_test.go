package data

import (
	"context"
	"fmt"
	"testing"
)

func TestExclusionSetExcludes(t *testing.T) {
	set := ExclusionSet{
		"body_fat_percentage": {{Metric: "body_fat_percentage", StartsOn: "2026-01-01"}},
		"steps":               {{Metric: "steps", StartsOn: "2026-03-01", EndsOn: "2026-03-31"}},
		"height":              {{Metric: "height"}},
		"weight_pair": {
			{Metric: "weight_pair", EndsOn: "2020-12-31"},
			{Metric: "weight_pair", StartsOn: "2026-01-01"},
		},
	}

	for name, tc := range map[string]struct {
		metric, startAt string
		want            bool
	}{
		"metric never excluded":    {"heart_rate", "2026-06-01T10:00:00Z", false},
		"open end, before":         {"body_fat_percentage", "2025-12-31T23:00:00Z", false},
		"open end, on the day":     {"body_fat_percentage", "2026-01-01T00:00:00Z", true},
		"open end, after":          {"body_fat_percentage", "2030-01-01T00:00:00Z", true},
		"both bounds, before":      {"steps", "2026-02-28T23:59:59Z", false},
		"both bounds, first day":   {"steps", "2026-03-01T00:00:00Z", true},
		"both bounds, last day":    {"steps", "2026-03-31T23:00:00Z", true},
		"both bounds, after":       {"steps", "2026-04-01T00:00:00Z", false},
		"unbounded takes anything": {"height", "1999-05-04T12:00:00Z", true},
		"two spans, in the first":  {"weight_pair", "2019-07-01T12:00:00Z", true},
		"two spans, in neither":    {"weight_pair", "2023-07-01T12:00:00Z", false},
		"two spans, in the second": {"weight_pair", "2027-07-01T12:00:00Z", true},
		// normalizeTime keeps an unparseable Apple timestamp verbatim, so a Record can
		// reach the check with no day in it. An unbounded Exclusion still takes it,
		// because it makes no comparison, and a bounded one must not: SQLite's date()
		// is NULL there and the purge leaves such a row standing. "not-a-date" sorts
		// after "2026-01-01", so a naive lexical compare would refuse at import what
		// the purge kept.
		"unreadable timestamp, unbounded": {"height", "not-a-date", true},
		"unreadable timestamp, bounded":   {"body_fat_percentage", "not-a-date", false},
		"empty timestamp, unbounded":      {"height", "", true},
		"empty timestamp, bounded":        {"body_fat_percentage", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := set.Excludes(tc.metric, tc.startAt); got != tc.want {
				t.Errorf("Excludes(%q, %q) = %v, want %v", tc.metric, tc.startAt, got, tc.want)
			}
		})
	}
}

// The empty set is what every Account that has never excluded anything imports
// under, so it must answer no without touching anything.
func TestEmptyExclusionSetExcludesNothing(t *testing.T) {
	var set ExclusionSet
	if set.Excludes("steps", "2026-01-01T00:00:00Z") {
		t.Error("the zero ExclusionSet excluded a Record")
	}
}

// TestExclusionGoSQLAgree pins the two halves of the feature to each other. The
// purge is SQL (`date(start_at)` against the bounds) and the import refusal is Go
// (a day prefix compared lexically, guarded by a parse), and they must answer
// identically for every row: a day one removes and the other re-admits is the
// feature lying, in whichever direction.
func TestExclusionGoSQLAgree(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()

	days := []string{
		"2025-12-31T23:59:59Z",
		"2026-01-01T00:00:00Z",
		"2026-01-01T12:00:00Z",
		"2026-02-15T06:30:00Z",
		"2026-03-31T23:00:00Z",
		"2026-04-01T00:00:00Z",
		"2026-04-01T00:00:01Z",
		// A timestamp normalizeTime could not parse and kept verbatim: an unbounded
		// span takes it, a bounded one must leave it, and both sides must say so.
		"not-a-date",
	}

	for _, span := range []Exclusion{
		{StartsOn: "", EndsOn: ""},
		{StartsOn: "2026-01-01", EndsOn: ""},
		{StartsOn: "", EndsOn: "2026-03-31"},
		{StartsOn: "2026-01-01", EndsOn: "2026-03-31"},
		{StartsOn: "2026-04-01", EndsOn: "2026-04-01"},
		{StartsOn: "2027-01-01", EndsOn: ""},
	} {
		name := fmt.Sprintf("%q..%q", span.StartsOn, span.EndsOn)
		t.Run(name, func(t *testing.T) {
			// A fresh Account per span, so each purge starts from the same rows.
			acc := &Account{Email: fmt.Sprintf("owner%s%s@example.com", span.StartsOn, span.EndsOn)}
			if err := models.Accounts.Insert(ctx, acc); err != nil {
				t.Fatalf("seed account: %v", err)
			}
			rows := make([]Measurement, len(days))
			for i, at := range days {
				rows[i] = Measurement{
					AccountID: acc.ID, Metric: "steps", Value: float64(i), OriginalUnit: "count",
					StartAt: at, EndAt: at, Source: "Watch", ContentKey: fmt.Sprintf("k-%d", i),
				}
			}
			if _, err := models.Measurements.InsertBatch(ctx, rows); err != nil {
				t.Fatalf("seed measurements: %v", err)
			}

			e := Exclusion{AccountID: acc.ID, Metric: "steps", StartsOn: span.StartsOn, EndsOn: span.EndsOn}
			if _, err := models.Exclusions.Insert(ctx, &e); err != nil {
				t.Fatalf("insert exclusion: %v", err)
			}

			set, err := models.Exclusions.Set(ctx, acc.ID)
			if err != nil {
				t.Fatalf("load set: %v", err)
			}
			wantPurged := int64(0)
			for _, at := range days {
				if set.Excludes("steps", at) {
					wantPurged++
				}
			}
			if e.Purged != wantPurged {
				t.Errorf("SQL purged %d rows, Go would refuse %d", e.Purged, wantPurged)
			}
			left, err := models.Measurements.CountInSpan(ctx, acc.ID, "steps", "", "")
			if err != nil {
				t.Fatalf("count: %v", err)
			}
			if left != int64(len(days))-wantPurged {
				t.Errorf("%d rows left, want %d", left, int64(len(days))-wantPurged)
			}
		})
	}
}
