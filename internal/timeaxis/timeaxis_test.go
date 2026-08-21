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

// TestFoldOntoBucketGrid pins the one arithmetic a marker depends on. A Panel's X
// axis is categorical, keyed on the bucket-start dates the server emitted, so a
// folded day that is off by one boundary rule does not raise anything: the marker
// simply is not drawn. Every case below is a day whose bucket is not itself.
func TestFoldOntoBucketGrid(t *testing.T) {
	// A March window, wide enough that the caller has to choose the bucket.
	window := Window{day(t, "2026-03-01"), day(t, "2026-04-01")}

	tests := []struct {
		name        string
		bucket      query.Bucket
		from, to    string
		start, end  string
		wantOK      bool
		wantIsRange bool
	}{
		{name: "a day is its own bucket at the day grain", bucket: query.Day,
			from: "2026-03-12", to: "2026-03-12", start: "2026-03-12", end: "2026-03-12", wantOK: true},
		{name: "a Thursday folds back to its Monday", bucket: query.Week,
			from: "2026-03-12", to: "2026-03-12", start: "2026-03-09", end: "2026-03-09", wantOK: true},
		{name: "a Monday is its own week", bucket: query.Week,
			from: "2026-03-09", to: "2026-03-09", start: "2026-03-09", end: "2026-03-09", wantOK: true},
		{name: "a Sunday folds back six days", bucket: query.Week,
			from: "2026-03-15", to: "2026-03-15", start: "2026-03-09", end: "2026-03-09", wantOK: true},
		{name: "any day folds to the first of its month", bucket: query.Month,
			from: "2026-03-12", to: "2026-03-31", start: "2026-03-01", end: "2026-03-01", wantOK: true},
		{name: "a span keeps both ends at the day grain", bucket: query.Day,
			from: "2026-03-12", to: "2026-03-19", start: "2026-03-12", end: "2026-03-19", wantOK: true,
			wantIsRange: true},
		{name: "a span crossing a Monday spans two weeks", bucket: query.Week,
			from: "2026-03-12", to: "2026-03-19", start: "2026-03-09", end: "2026-03-16", wantOK: true,
			wantIsRange: true},
		// A span that started before the range is still on screen for the days it
		// overlaps, and must be drawn there rather than dropped.
		{name: "a span starting before the window is clamped to its first bucket", bucket: query.Day,
			from: "2026-02-20", to: "2026-03-03", start: "2026-03-01", end: "2026-03-03", wantOK: true,
			wantIsRange: true},
		{name: "a span running past the window is clamped to its last day", bucket: query.Day,
			from: "2026-03-28", to: "2026-04-15", start: "2026-03-28", end: "2026-03-31", wantOK: true,
			wantIsRange: true},
		// The window is half-open: 2026-04-01 is the first day of the next window.
		{name: "the last day of the window is inside it", bucket: query.Day,
			from: "2026-03-31", to: "2026-03-31", start: "2026-03-31", end: "2026-03-31", wantOK: true},
		{name: "the day the window ends on is outside it", bucket: query.Day,
			from: "2026-04-01", to: "2026-04-01", wantOK: false},
		{name: "a day before the window is outside it", bucket: query.Day,
			from: "2026-02-28", to: "2026-02-28", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := Resolved{Current: window, Bucket: tc.bucket}
			start, end, ok := r.Fold(day(t, tc.from), day(t, tc.to))
			if ok != tc.wantOK {
				t.Fatalf("Fold(%s, %s) ok = %v, want %v", tc.from, tc.to, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if start != tc.start || end != tc.end {
				t.Errorf("Fold(%s, %s) = (%s, %s), want (%s, %s)",
					tc.from, tc.to, start, end, tc.start, tc.end)
			}
			// A span that folds into one bucket is one bucket wide and draws as a
			// marker, not a band: the client's test is start != end, so it has to
			// hold for the right reason.
			if (start != end) != tc.wantIsRange {
				t.Errorf("Fold(%s, %s) spans more than one bucket = %v, want %v",
					tc.from, tc.to, start != end, tc.wantIsRange)
			}
		})
	}
}

// TestFoldNormalizesAnInvertedSpan: the API rejects an inverted span, so this is
// belt and braces, but a silent inversion here would mean a ReferenceArea drawn
// from right to left, which Recharts renders as nothing at all.
func TestFoldNormalizesAnInvertedSpan(t *testing.T) {
	r := Resolved{Current: Window{day(t, "2026-03-01"), day(t, "2026-04-01")}, Bucket: query.Day}
	start, end, ok := r.Fold(day(t, "2026-03-19"), day(t, "2026-03-12"))
	if !ok || start != "2026-03-12" || end != "2026-03-19" {
		t.Errorf("Fold(19th, 12th) = (%s, %s, %v), want (2026-03-12, 2026-03-19, true)", start, end, ok)
	}
}
