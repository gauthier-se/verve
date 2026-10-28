package sqlite

import "time"

// Timestamps are stored as INTEGER Unix milliseconds, UTC.

// toMillis returns t in Unix milliseconds. Sub-millisecond digits are dropped.
func toMillis(t time.Time) int64 {
	return t.UnixMilli()
}

// fromMillis returns the UTC time of ms Unix milliseconds.
func fromMillis(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}
