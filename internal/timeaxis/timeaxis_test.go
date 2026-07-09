package timeaxis

import (
	"errors"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/query"
)

func ptr(s string) *string { return &s }

func day(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts.UTC()
}

// now carries a time-of-day to prove Resolve truncates to today's UTC midnight.
var now = time.Date(2026, 7, 9, 14, 30, 0, 0, time.UTC)

func TestResolvePresetWindows(t *testing.T) {
	cases := []struct {
		preset   string
		from, to string
		bucket   query.Bucket
	}{
		{"7d", "2026-07-02", "2026-07-09", query.Day},
		{"30d", "2026-06-09", "2026-07-09", query.Day},
		{"3m", "2026-04-09", "2026-07-09", query.Week},
		{"1y", "2025-07-09", "2026-07-09", query.Week},
		{"all", "2000-01-01", "2026-07-09", query.Month},
	}
	for _, c := range cases {
		got, err := Resolve(Tokens{RangePreset: c.preset}, now)
		if err != nil {
			t.Fatalf("%s: %v", c.preset, err)
		}
		if !got.Current.From.Equal(day(t, c.from)) || !got.Current.To.Equal(day(t, c.to)) {
			t.Errorf("%s: window = [%s,%s), want [%s,%s)", c.preset,
				got.Current.From.Format("2006-01-02"), got.Current.To.Format("2006-01-02"), c.from, c.to)
		}
		if got.Bucket != c.bucket {
			t.Errorf("%s: bucket = %s, want %s", c.preset, got.Bucket, c.bucket)
		}
		if got.Baseline != nil {
			t.Errorf("%s: baseline = %+v, want nil", c.preset, got.Baseline)
		}
	}
}

func TestResolveCustomWindow(t *testing.T) {
	got, err := Resolve(Tokens{RangePreset: "custom", RangeFrom: ptr("2024-01-01"), RangeTo: ptr("2024-02-01")}, now)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !got.Current.From.Equal(day(t, "2024-01-01")) || !got.Current.To.Equal(day(t, "2024-02-01")) {
		t.Errorf("window = [%s,%s)", got.Current.From, got.Current.To)
	}
	if got.Bucket != query.Day { // 31 days
		t.Errorf("bucket = %s, want day", got.Bucket)
	}
}

func TestResolveOverrideBucketWins(t *testing.T) {
	got, err := Resolve(Tokens{RangePreset: "30d", Bucket: ptr("month")}, now)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Bucket != query.Month {
		t.Errorf("bucket = %s, want month (override)", got.Bucket)
	}
}

func TestResolveBaselineWindows(t *testing.T) {
	cases := []struct {
		name             string
		preset, rule     string
		now              time.Time
		baseFrom, baseTo string
	}{
		{"previous 30d", "30d", "previous", now, "2026-05-10", "2026-06-09"},
		{"same period last year", "30d", "same_period_last_year", now, "2025-06-09", "2025-07-09"},
		// same_period_last_year off a Feb-29 start normalizes to Mar 1.
		{"leap normalize", "30d", "same_period_last_year", time.Date(2024, 3, 30, 0, 0, 0, 0, time.UTC), "2023-03-01", "2023-03-30"},
	}
	for _, c := range cases {
		got, err := Resolve(Tokens{RangePreset: c.preset, BaselineRule: c.rule}, c.now)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got.Baseline == nil {
			t.Fatalf("%s: baseline nil", c.name)
		}
		if !got.Baseline.From.Equal(day(t, c.baseFrom)) || !got.Baseline.To.Equal(day(t, c.baseTo)) {
			t.Errorf("%s: baseline = [%s,%s), want [%s,%s)", c.name,
				got.Baseline.From.Format("2006-01-02"), got.Baseline.To.Format("2006-01-02"), c.baseFrom, c.baseTo)
		}
	}
}

func TestResolveCustomBaseline(t *testing.T) {
	got, err := Resolve(Tokens{
		RangePreset: "30d", BaselineRule: "custom",
		BaselineFrom: ptr("2020-01-01"), BaselineTo: ptr("2020-02-01"),
	}, now)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Baseline == nil || !got.Baseline.From.Equal(day(t, "2020-01-01")) || !got.Baseline.To.Equal(day(t, "2020-02-01")) {
		t.Errorf("custom baseline = %+v", got.Baseline)
	}
}

func TestResolveBaselineOnAllIsError(t *testing.T) {
	_, err := Resolve(Tokens{RangePreset: "all", BaselineRule: "previous"}, now)
	var inv Invalid
	if !errors.As(err, &inv) || inv["baseline"] == "" {
		t.Fatalf("want Invalid on baseline, got %v", err)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name  string
		tok   Tokens
		field string // "" means expect valid
	}{
		{"ok preset", Tokens{RangePreset: "30d", BaselineRule: "none"}, ""},
		{"ok custom override", Tokens{RangePreset: "custom", RangeFrom: ptr("2024-01-01"), RangeTo: ptr("2024-02-01"), Bucket: ptr("week")}, ""},
		{"unknown preset", Tokens{RangePreset: "5d"}, "range_preset"},
		{"unknown rule", Tokens{RangePreset: "30d", BaselineRule: "prev"}, "baseline_rule"},
		{"bounds on non-custom rule", Tokens{RangePreset: "30d", BaselineRule: "previous", BaselineFrom: ptr("2024-01-01")}, "baseline_from"},
		{"custom range missing bound", Tokens{RangePreset: "custom", RangeFrom: ptr("2024-01-01")}, "range_from"},
		{"custom range unordered", Tokens{RangePreset: "custom", RangeFrom: ptr("2024-02-01"), RangeTo: ptr("2024-01-01")}, "range_to"},
		{"too-fine override", Tokens{RangePreset: "30d", Bucket: ptr("hour")}, "bucket"},
		{"unknown override", Tokens{RangePreset: "30d", Bucket: ptr("year")}, "bucket"},
		{"custom baseline ok", Tokens{RangePreset: "30d", BaselineRule: "custom", BaselineFrom: ptr("2024-01-01"), BaselineTo: ptr("2024-02-01")}, ""},
		{"custom baseline missing bounds", Tokens{RangePreset: "30d", BaselineRule: "custom"}, "baseline_from"},
		{"custom baseline unordered", Tokens{RangePreset: "30d", BaselineRule: "custom", BaselineFrom: ptr("2024-02-01"), BaselineTo: ptr("2024-02-01")}, "baseline_to"},
		{"custom baseline malformed to", Tokens{RangePreset: "30d", BaselineRule: "custom", BaselineFrom: ptr("2024-01-01"), BaselineTo: ptr("02/01/2024")}, "baseline_to"},
		{"malformed range from", Tokens{RangePreset: "custom", RangeFrom: ptr("Jan 1"), RangeTo: ptr("2024-02-01")}, "range_from"},
	}
	for _, c := range cases {
		err := Validate(c.tok)
		if c.field == "" {
			if err != nil {
				t.Errorf("%s: want valid, got %v", c.name, err)
			}
			continue
		}
		var inv Invalid
		if !errors.As(err, &inv) {
			t.Errorf("%s: want Invalid, got %v", c.name, err)
			continue
		}
		if inv[c.field] == "" {
			t.Errorf("%s: want error on %q, got %v", c.name, c.field, inv)
		}
	}
}
