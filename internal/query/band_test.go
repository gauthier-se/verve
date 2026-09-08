package query

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
)

func bandDay(t *testing.T, s string) time.Time {
	t.Helper()
	return mustTime(t, s+"T00:00:00Z")
}

// TestBandIsDenseAndNamesItsGaps is the point of the band: the grid is
// materialised, so an empty stretch is a bucket that says it is empty rather than
// an absence the caller would have to infer boundaries to notice (ADR 0032).
func TestBandIsDenseAndNamesItsGaps(t *testing.T) {
	e, models, acc := setup(t)
	// Two clusters a fortnight apart, inside one month, so the band comes out daily.
	days := []string{"2024-01-01", "2024-01-02", "2024-01-03", "2024-01-14", "2024-01-15"}
	rows := make([]data.Measurement, len(days))
	for i, d := range days {
		rows[i] = data.Measurement{
			Metric: "body_mass", Value: 80 + float64(i), OriginalUnit: "kg",
			StartAt: d + "T08:00:00Z", EndAt: d + "T08:00:00Z", Source: "Scale",
			ContentKey: fmt.Sprintf("mass-%s", d),
		}
	}
	seedRows(t, models, acc, rows)

	band, err := e.Band(context.Background(), BandRequest{
		AccountID: acc, Metric: "body_mass",
		First: bandDay(t, "2024-01-01"), Last: bandDay(t, "2024-01-15"),
	})
	if err != nil {
		t.Fatalf("Band: %v", err)
	}

	if band.Bucket != "day" {
		t.Errorf("bucket = %q, want day for a fortnight of history", band.Bucket)
	}
	if got := len(band.Points); got != 15 { // Jan 1 → Jan 15 inclusive
		t.Errorf("points = %d, want 15 — one per bucket, gaps included", got)
	}
	gaps := 0
	for _, p := range band.Points {
		if p.Gap {
			gaps++
		}
	}
	if gaps != 10 { // Jan 4 → Jan 13
		t.Errorf("gap buckets = %d, want 10", gaps)
	}
	if len(band.Gaps) != 1 {
		t.Fatalf("gap runs = %+v, want one", band.Gaps)
	}
	if band.Gaps[0].From != "2024-01-04" || band.Gaps[0].To != "2024-01-13" {
		t.Errorf("gap run = %+v, want 2024-01-04 → 2024-01-13", band.Gaps[0])
	}
	// The grid is what anything folded onto this band is placed against, so it must
	// name every bucket the band drew.
	if len(band.Grid) != len(band.Points) {
		t.Errorf("grid = %d keys, points = %d: they must agree", len(band.Grid), len(band.Points))
	}
}

// TestBandSnapsItsBounds: the bounds are the first and last row's own instants, so
// a history whose last reading is at 08:00 must not gain a bucket for the rest of
// that day and draw it as a gap.
func TestBandSnapsItsBounds(t *testing.T) {
	e, models, acc := setup(t)
	seedRows(t, models, acc, []data.Measurement{
		{Metric: "body_mass", Value: 80, OriginalUnit: "kg", StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T08:00:00Z", Source: "Scale", ContentKey: "m1"},
		{Metric: "body_mass", Value: 81, OriginalUnit: "kg", StartAt: "2024-01-15T08:00:00Z", EndAt: "2024-01-15T08:00:00Z", Source: "Scale", ContentKey: "m2"},
	})

	band, err := e.Band(context.Background(), BandRequest{
		AccountID: acc, Metric: "body_mass",
		First: mustTime(t, "2024-01-01T08:00:00Z"), Last: mustTime(t, "2024-01-15T08:00:00Z"),
	})
	if err != nil {
		t.Fatalf("Band: %v", err)
	}
	if got := len(band.Points); got != 15 {
		t.Errorf("points = %d, want 15: the bounds are snapped to whole days", got)
	}
	last := band.Points[len(band.Points)-1]
	if last.Bucket != "2024-01-15" || last.Gap {
		t.Errorf("last point = %+v, want the final reading, not a gap", last)
	}
}

// TestBandGrainFollowsTheSpan: the grain is the history's own, by the same rule a
// Dashboard uses — an Account three months old gets weekly, not a coarser grain
// borrowed from someone else's history.
func TestBandGrainFollowsTheSpan(t *testing.T) {
	e, models, acc := setup(t)
	seedRows(t, models, acc, []data.Measurement{
		{Metric: "body_mass", Value: 80, OriginalUnit: "kg", StartAt: "2020-01-01T08:00:00Z", EndAt: "2020-01-01T08:00:00Z", Source: "Scale", ContentKey: "m1"},
		{Metric: "body_mass", Value: 81, OriginalUnit: "kg", StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T08:00:00Z", Source: "Scale", ContentKey: "m2"},
	})

	band, err := e.Band(context.Background(), BandRequest{
		AccountID: acc, Metric: "body_mass",
		First: bandDay(t, "2020-01-01"), Last: bandDay(t, "2024-01-01"),
	})
	if err != nil {
		t.Fatalf("Band: %v", err)
	}
	if band.Bucket != "month" {
		t.Errorf("bucket = %q, want month for four years of history", band.Bucket)
	}
}
