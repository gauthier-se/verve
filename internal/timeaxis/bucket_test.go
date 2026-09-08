package timeaxis

import (
	"errors"
	"testing"
	"time"
)

func mustInstant(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts.UTC()
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
		got := c.b.Starts(mustInstant(t, c.from), mustInstant(t, c.to))
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
		if got := c.b.next(mustInstant(t, c.in)); !got.Equal(mustInstant(t, c.want)) {
			t.Errorf("%s next(%s) = %s, want %s", c.b, c.in, got.Format(dayLayout), c.want)
		}
	}
}

// TestBucketShift pins that a Lag is counted in buckets, not in days: shifting a
// week key by one lands a week later, and shifting a month key across a year
// boundary stays on the calendar rather than on a fixed 30 days.
func TestBucketShift(t *testing.T) {
	cases := []struct {
		b    Bucket
		key  string
		n    int
		want string
	}{
		{Day, "2024-02-28", 1, "2024-02-29"}, // leap
		{Day, "2024-01-01", 0, "2024-01-01"},
		{Week, "2024-01-01", 2, "2024-01-15"},
		{Month, "2024-11-01", 2, "2025-01-01"}, // year rollover
	}
	for _, c := range cases {
		got, ok := c.b.Shift(c.key, c.n)
		if !ok {
			t.Errorf("%s Shift(%q, %d): ok = false", c.b, c.key, c.n)
			continue
		}
		if got != c.want {
			t.Errorf("%s Shift(%q, %d) = %q, want %q", c.b, c.key, c.n, got, c.want)
		}
	}

	if _, ok := Day.Shift("not-a-bucket", 1); ok {
		t.Error("Shift of an unparseable key reported ok, want false")
	}
}

// TestAutoBucketThresholds pins the span→grain rule the History band and the
// Dashboard now share. It used to exist twice, in two packages, on two copies of
// these numbers.
func TestAutoBucketThresholds(t *testing.T) {
	day := func(s string) time.Time { return mustInstant(t, s+"T00:00:00Z") }
	cases := []struct {
		from, to string
		want     Bucket
	}{
		{"2024-01-01", "2024-01-08", Day},   // a week
		{"2024-01-01", "2024-02-01", Day},   // 31 days, the last day bucket
		{"2024-01-01", "2024-02-02", Week},  // 32 days
		{"2024-01-01", "2025-01-01", Week},  // 366 days, the last week bucket
		{"2024-01-01", "2025-01-02", Month}, // 367 days
	}
	for _, c := range cases {
		if got := AutoBucket(Window{From: day(c.from), To: day(c.to)}); got != c.want {
			t.Errorf("AutoBucket(%s → %s) = %s, want %s", c.from, c.to, got, c.want)
		}
	}
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
