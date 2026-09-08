package timeaxis

import (
	"errors"
	"fmt"
	"time"
)

// Sentinel errors for a bucket token that does not name a grain this module
// serves. They live here rather than in the read engine because the bucket is
// part of the time axis, and the HTTP layer maps them to a 422 either way.
var (
	ErrUnknownBucket = errors.New("timeaxis: unknown bucket")
	// ErrBucketTooFine is a recognized bucket below the resolution cap (ADR 0012).
	ErrBucketTooFine = errors.New("timeaxis: bucket below the resolution cap")
)

// Bucket is a supported time-bucket granularity. Day is the finest the API
// exposes (ADR 0012); finer names parse to ErrBucketTooFine.
//
// It lives in this module because a bucket is a property of the time axis, not of
// the read that happens to be drawn on it: CONTEXT.md puts preset→window,
// rule→window and span→bucket in one place, and the boundaries a Bucket computes
// are the same boundaries a folded Annotation and an ordinal-aligned Baseline are
// placed against. The read engine translates a Bucket into SQL (bucketSQL) and
// keeps that translation to itself, so this module stays pure and DB-free.
type Bucket string

const (
	Day   Bucket = "day"   // calendar day (UTC)
	Week  Bucket = "week"  // ISO week, keyed on its Monday
	Month Bucket = "month" // calendar month, keyed on its first day
)

// ParseBucket maps a query-string bucket name to a Bucket. An empty string is
// not defaulted (the caller decides); too-fine names yield ErrBucketTooFine.
func ParseBucket(s string) (Bucket, error) {
	switch s {
	case "day":
		return Day, nil
	case "week":
		return Week, nil
	case "month":
		return Month, nil
	case "minute", "second", "hour":
		return "", ErrBucketTooFine
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownBucket, s)
	}
}

// ApproxDuration is a lower-bound bucket width, used only for a point-count guard
// (a month is at least 28 days). It never decides a boundary.
func (b Bucket) ApproxDuration() time.Duration {
	switch b {
	case Week:
		return 7 * 24 * time.Hour
	case Month:
		return 28 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// Start is the bucket-start date (YYYY-MM-DD) of the bucket holding t: the key
// a Point carries and the category a chart's X axis is drawn on. It is snap's
// exported face, so anything that has to name a position on the grid (a folded
// Annotation, say) asks the module that owns the boundaries instead of
// re-deriving them.
func (b Bucket) Start(t time.Time) string {
	return b.snap(t).Format(dayLayout)
}

// snap rounds t down to the start of its bucket, in UTC.
func (b Bucket) snap(t time.Time) time.Time {
	t = t.UTC()
	y, m, d := t.Date()
	switch b {
	case Week:
		day := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		offset := (int(day.Weekday()) + 6) % 7 // days since Monday (the ISO week start)
		return day.AddDate(0, 0, -offset)
	case Month:
		return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	default: // Day
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
}

// next advances a bucket start to the following bucket start (calendar-aware).
func (b Bucket) next(t time.Time) time.Time {
	switch b {
	case Week:
		return t.AddDate(0, 0, 7)
	case Month:
		return t.AddDate(0, 1, 0)
	default: // Day
		return t.AddDate(0, 0, 1)
	}
}

// Starts enumerates the bucket-start dates covering [from, to), in order — the
// ordinal sequence used to align a Baseline by position, not date (ADR 0015), and
// the grid a dense series is drawn on when the gaps themselves are the subject
// (ADR 0032).
func (b Bucket) Starts(from, to time.Time) []string {
	out := []string{}
	for cur := b.snap(from); cur.Before(to.UTC()); cur = b.next(cur) {
		out = append(out, cur.Format(dayLayout))
	}
	return out
}

// AutoBucket derives the bucket from a span: ≤31d→day, ≤366d→week, else month,
// keeping the point count bounded without a per-Panel choice.
//
// Exported because it is not only the Dashboard's rule: the History band picks its
// grain from the Account's whole extent by the same thresholds (ADR 0032), and an
// unexported copy is how the two silently drift apart.
func AutoBucket(w Window) Bucket {
	days := int(w.To.Sub(w.From) / (24 * time.Hour))
	switch {
	case days <= 31:
		return Day
	case days <= 366:
		return Week
	default:
		return Month
	}
}

// Shift advances a bucket key (YYYY-MM-DD) by n buckets and returns the key it
// lands on. ok is false for a key this module did not emit.
//
// It exists so a Lag (CONTEXT.md: Lag) is expressed in buckets rather than in
// days: "the week after" is seven days at a day grain and one step at a week one,
// and a caller doing that arithmetic itself would be a second boundary rule.
func (b Bucket) Shift(key string, n int) (string, bool) {
	t, err := time.Parse(dayLayout, key)
	if err != nil {
		return "", false
	}
	t = t.UTC()
	for i := 0; i < n; i++ {
		t = b.next(t)
	}
	return t.Format(dayLayout), true
}
