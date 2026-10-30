package sqlite

import (
	"database/sql"
	"time"
)

// Timestamps are stored as INTEGER Unix milliseconds, UTC.

// toMillis returns t in Unix milliseconds. Sub-millisecond digits are dropped.
func toMillis(t time.Time) int64 {
	return t.UnixMilli()
}

// nullMillis returns t in Unix milliseconds for a nullable column.
func nullMillis(t time.Time) sql.NullInt64 {
	return sql.NullInt64{Int64: toMillis(t), Valid: true}
}

// fromMillis returns the UTC time of ms Unix milliseconds.
func fromMillis(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}

// fromNullMillis returns the UTC time of a nullable column, or the zero time when it is NULL.
func fromNullMillis(ms sql.NullInt64) time.Time {
	if !ms.Valid {
		return time.Time{}
	}

	return fromMillis(ms.Int64)
}
