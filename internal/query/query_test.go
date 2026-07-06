package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
)

// setup opens a fresh migrated DB, an Engine over it, and a seeded account.
func setup(t *testing.T) (Engine, data.Models, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "verve.db")
	db, err := data.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := data.Migrate(context.Background(), db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	models := data.NewModels(db)
	acc := &data.Account{Email: "owner@example.com"}
	if err := models.Accounts.Insert(context.Background(), acc); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	return Engine{DB: db}, models, acc.ID
}

// meas is a compact test measurement.
type meas struct {
	metric string
	value  float64
	at     string // RFC 3339
	source string
}

func seed(t *testing.T, db *sql.DB, models data.Models, acc int64, ms []meas) {
	t.Helper()
	batch := make([]data.Measurement, len(ms))
	for i, m := range ms {
		batch[i] = data.Measurement{
			AccountID: acc, Metric: m.metric, Value: m.value,
			OriginalUnit: "u", StartAt: m.at, EndAt: m.at, Source: m.source,
			ContentKey: fmt.Sprintf("k-%d-%s-%s-%v", i, m.metric, m.source, m.value),
		}
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), batch); err != nil {
		t.Fatalf("seed measurements: %v", err)
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return ts
}

func TestSeriesSumPerDay(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 100, "2024-01-01T08:00:00Z", "Watch"},
		{"steps", 200, "2024-01-01T18:00:00Z", "Watch"},
		{"steps", 50, "2024-01-02T09:00:00Z", "Watch"},
	})

	s, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-01-01T00:00:00Z"), To: mustTime(t, "2024-01-03T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if s.Aggregation != "sum" || s.Unit != "count" {
		t.Errorf("metadata = %q/%q, want sum/count", s.Aggregation, s.Unit)
	}
	want := []Point{{Bucket: "2024-01-01", Value: 300}, {Bucket: "2024-01-02", Value: 50}}
	if len(s.Points) != 2 || s.Points[0] != want[0] || s.Points[1] != want[1] {
		t.Errorf("points = %+v, want %+v", s.Points, want)
	}
}

func TestSeriesAverageBand(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"heart_rate", 60, "2024-01-01T08:00:00Z", "Watch"},
		{"heart_rate", 80, "2024-01-01T09:00:00Z", "Watch"},
		{"heart_rate", 100, "2024-01-01T10:00:00Z", "Watch"},
	})
	s, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "heart_rate", Bucket: Day,
		From: mustTime(t, "2024-01-01T00:00:00Z"), To: mustTime(t, "2024-01-02T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(s.Points) != 1 {
		t.Fatalf("points = %+v, want 1", s.Points)
	}
	p := s.Points[0]
	if p.Value != 80 || p.Min == nil || *p.Min != 60 || p.Max == nil || *p.Max != 100 {
		t.Errorf("avg point = %+v (min/max %v/%v), want 80 band 60..100", p, p.Min, p.Max)
	}
}

func TestSeriesLatestPerBucket(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"body_mass", 80.0, "2024-01-01T08:00:00Z", "Scale"},
		{"body_mass", 79.5, "2024-01-01T20:00:00Z", "Scale"}, // later same day → wins
		{"body_mass", 79.0, "2024-01-05T07:00:00Z", "Scale"},
	})
	s, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "body_mass", Bucket: Day,
		From: mustTime(t, "2024-01-01T00:00:00Z"), To: mustTime(t, "2024-01-06T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	want := []Point{{Bucket: "2024-01-01", Value: 79.5}, {Bucket: "2024-01-05", Value: 79.0}}
	if len(s.Points) != 2 || s.Points[0] != want[0] || s.Points[1] != want[1] {
		t.Errorf("points = %+v, want %+v", s.Points, want)
	}
}

// TestSeriesSourceResolutionNoDoubleCount is the ADR 0003 guard: steps recorded
// by both Watch and iPhone must resolve to the Watch and never sum both.
func TestSeriesSourceResolutionNoDoubleCount(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 300, "2024-01-01T08:00:00Z", "Gauthier's Apple Watch"},
		{"steps", 280, "2024-01-01T08:00:00Z", "Gauthier's iPhone"},
	})
	s, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-01-01T00:00:00Z"), To: mustTime(t, "2024-01-02T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if s.Source != "Gauthier's Apple Watch" {
		t.Errorf("resolved source = %q, want the Apple Watch", s.Source)
	}
	if len(s.Points) != 1 || s.Points[0].Value != 300 {
		t.Errorf("points = %+v, want single bucket of 300 (Watch only, no double-count)", s.Points)
	}
}

func TestSeriesAccountScoped(t *testing.T) {
	e, models, acc := setup(t)
	other := &data.Account{Email: "other@example.com"}
	if err := models.Accounts.Insert(context.Background(), other); err != nil {
		t.Fatalf("insert other: %v", err)
	}
	seed(t, e.DB, models, acc, []meas{{"steps", 100, "2024-01-01T08:00:00Z", "Watch"}})
	seed(t, e.DB, models, other.ID, []meas{{"steps", 9999, "2024-01-01T09:00:00Z", "Watch"}})

	s, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-01-01T00:00:00Z"), To: mustTime(t, "2024-01-02T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(s.Points) != 1 || s.Points[0].Value != 100 {
		t.Errorf("points = %+v, want only this account's 100", s.Points)
	}
}

func TestSeriesWeekAndMonthBuckets(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 10, "2024-01-01T08:00:00Z", "Watch"}, // Mon, ISO week starts 2024-01-01
		{"steps", 20, "2024-01-07T08:00:00Z", "Watch"}, // Sun, same ISO week
		{"steps", 30, "2024-01-08T08:00:00Z", "Watch"}, // Mon, next week
	})
	week, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Week,
		From: mustTime(t, "2024-01-01T00:00:00Z"), To: mustTime(t, "2024-02-01T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("Series week: %v", err)
	}
	wantWeek := []Point{{Bucket: "2024-01-01", Value: 30}, {Bucket: "2024-01-08", Value: 30}}
	if len(week.Points) != 2 || week.Points[0] != wantWeek[0] || week.Points[1] != wantWeek[1] {
		t.Errorf("week points = %+v, want %+v", week.Points, wantWeek)
	}

	month, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Month,
		From: mustTime(t, "2024-01-01T00:00:00Z"), To: mustTime(t, "2024-02-01T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("Series month: %v", err)
	}
	if len(month.Points) != 1 || month.Points[0] != (Point{Bucket: "2024-01-01", Value: 60}) {
		t.Errorf("month points = %+v, want one bucket of 60", month.Points)
	}
}

// TestSeriesOneYearBounded is the ADR 0012 payload guard: a full year of dense
// data still returns roughly a point per day, never the raw series.
func TestSeriesOneYearBounded(t *testing.T) {
	e, models, acc := setup(t)
	// One measurement per day for 365 days, each split into 4 raw points.
	var ms []meas
	day := mustTime(t, "2024-01-01T00:00:00Z")
	for d := 0; d < 365; d++ {
		ts := day.AddDate(0, 0, d)
		for h := 0; h < 4; h++ {
			ms = append(ms, meas{"steps", 10, ts.Add(time.Duration(h) * time.Hour).Format(time.RFC3339), "Watch"})
		}
	}
	seed(t, e.DB, models, acc, ms)

	s, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: day, To: day.AddDate(1, 0, 0),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(s.Points) != 365 {
		t.Errorf("points = %d, want 365 (one per day) despite 1460 raw rows", len(s.Points))
	}
	if s.Points[0].Value != 40 {
		t.Errorf("first bucket = %v, want 40 (4×10 summed)", s.Points[0].Value)
	}
}

func TestSeriesNoData(t *testing.T) {
	e, _, acc := setup(t)
	s, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-01-01T00:00:00Z"), To: mustTime(t, "2024-01-02T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if s.Source != "" || s.Points == nil || len(s.Points) != 0 {
		t.Errorf("empty range = %+v, want empty source and empty (non-nil) points", s)
	}
}

func TestSeriesErrors(t *testing.T) {
	e, _, acc := setup(t)
	base := Request{AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-01-01T00:00:00Z"), To: mustTime(t, "2024-01-02T00:00:00Z")}

	t.Run("unknown metric", func(t *testing.T) {
		r := base
		r.Metric = "not_a_metric"
		if _, err := e.Series(context.Background(), r); !errors.Is(err, ErrUnknownMetric) {
			t.Errorf("err = %v, want ErrUnknownMetric", err)
		}
	})
	t.Run("inverted range", func(t *testing.T) {
		r := base
		r.From, r.To = r.To, r.From
		if _, err := e.Series(context.Background(), r); !errors.Is(err, ErrInvalidRange) {
			t.Errorf("err = %v, want ErrInvalidRange", err)
		}
	})
	t.Run("range too large", func(t *testing.T) {
		r := base
		r.To = r.From.AddDate(10, 0, 0) // 3650 days > maxPoints with a day bucket
		if _, err := e.Series(context.Background(), r); !errors.Is(err, ErrRangeTooLarge) {
			t.Errorf("err = %v, want ErrRangeTooLarge", err)
		}
	})
}

func TestParseBucket(t *testing.T) {
	tests := map[string]struct {
		want Bucket
		err  error
	}{
		"day":    {Day, nil},
		"week":   {Week, nil},
		"month":  {Month, nil},
		"hour":   {"", ErrBucketTooFine},
		"minute": {"", ErrBucketTooFine},
		"year":   {"", ErrUnknownBucket},
		"":       {"", ErrUnknownBucket},
	}
	for in, tc := range tests {
		t.Run(in, func(t *testing.T) {
			got, err := ParseBucket(in)
			if got != tc.want || !errors.Is(err, tc.err) {
				t.Errorf("ParseBucket(%q) = %q, %v; want %q, %v", in, got, err, tc.want, tc.err)
			}
		})
	}
}
