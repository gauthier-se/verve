// Package query is Verve's read engine: it turns a request for one Metric over
// a time range into server-side aggregated buckets, never a raw series (ADR
// 0012). A single Metric can hold hundreds of thousands of points, so the
// per-bucket aggregation SQL here — not the client — is what bounds every graph
// to a few hundred points.
//
// Each request is resolved by the Metric's aggregation rule (sum / average with
// a min–max band / latest), scoped to the owning Account (ADR 0007), and pinned
// to a single winning Source so overlapping Sources never double-count (ADR
// 0003). The bucket resolution is capped so the raw series can never be shipped.
package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
)

// Sentinel errors let the HTTP layer map a failed query to the right status
// without depending on message text.
var (
	// ErrUnknownMetric is returned when the requested slug is not in the Catalog.
	ErrUnknownMetric = errors.New("query: unknown metric")
	// ErrUnknownBucket is returned when the bucket name is not recognized.
	ErrUnknownBucket = errors.New("query: unknown bucket")
	// ErrBucketTooFine is returned when the bucket is below the resolution cap,
	// which would risk shipping the raw series (ADR 0012).
	ErrBucketTooFine = errors.New("query: bucket below the resolution cap")
	// ErrInvalidRange is returned when the range is empty or inverted (from ≥ to).
	ErrInvalidRange = errors.New("query: invalid time range")
	// ErrRangeTooLarge is returned when range ÷ bucket would exceed maxPoints.
	ErrRangeTooLarge = errors.New("query: range too large for bucket")
	// ErrUnsupportedAggregation is returned for a Metric whose aggregation rule
	// the engine does not serve yet (duration_by_state, derived Metrics).
	ErrUnsupportedAggregation = errors.New("query: unsupported aggregation")
)

// maxPoints caps how many buckets a single query may span. A finer bucket over
// a range that would exceed it is rejected, forcing the caller to a coarser
// bucket — the guarantee that keeps a payload bounded regardless of history.
const maxPoints = 1000

// Bucket is a supported time-bucket granularity. The set is deliberately coarse:
// day is the finest resolution the API exposes, so the raw series is never
// returned (ADR 0012). Finer names (hour, minute) parse to ErrBucketTooFine.
type Bucket string

const (
	// Day buckets by calendar day (UTC) — the finest resolution the API exposes.
	Day Bucket = "day"
	// Week buckets by ISO week, keyed on its Monday.
	Week Bucket = "week"
	// Month buckets by calendar month, keyed on its first day.
	Month Bucket = "month"
)

// ParseBucket maps a query-string bucket name to a Bucket. An empty string is
// not defaulted here — the caller decides the default. Recognized-but-too-fine
// names yield ErrBucketTooFine so the API can distinguish "you asked below the
// cap" from "that is not a bucket".
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

// sqlExpr is the SQLite expression that maps a measurement's start_at to its
// bucket-start date (YYYY-MM-DD). start_at is stored as RFC 3339 UTC, which
// SQLite's date functions parse directly, so buckets fall on UTC boundaries.
func (b Bucket) sqlExpr() string {
	switch b {
	case Week:
		// Back up into the week then snap forward to Monday: the ISO week start.
		return "date(start_at, '-6 days', 'weekday 1')"
	case Month:
		return "date(start_at, 'start of month')"
	default: // Day
		return "date(start_at)"
	}
}

// approxDuration is a lower-bound bucket width used only for the point-count
// guard (a month is at least 28 days). It never affects the SQL bucketing.
func (b Bucket) approxDuration() time.Duration {
	switch b {
	case Week:
		return 7 * 24 * time.Hour
	case Month:
		return 28 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// Request is one aggregated-series query: a Metric over [From, To) collapsed
// into Bucket-sized buckets, scoped to AccountID.
type Request struct {
	AccountID int64
	Metric    string
	From      time.Time
	To        time.Time
	Bucket    Bucket
}

// Point is one aggregated bucket. Bucket is the bucket-start date (YYYY-MM-DD).
// Value is the aggregate under the Metric's rule (sum, average, or latest). Min
// and Max carry the band for average Metrics and are nil otherwise.
type Point struct {
	Bucket string   `json:"bucket"`
	Value  float64  `json:"value"`
	Min    *float64 `json:"min,omitempty"`
	Max    *float64 `json:"max,omitempty"`
}

// Series is the result of a query: the resolved Metric metadata, the single
// winning Source (empty when the range holds no data), and the ordered buckets.
type Series struct {
	Metric      string              `json:"metric"`
	Unit        string              `json:"unit"`
	Aggregation catalog.Aggregation `json:"aggregation"`
	Bucket      Bucket              `json:"bucket"`
	Source      string              `json:"source"`
	Points      []Point             `json:"points"`
}

// Engine answers aggregated-series queries against the measurements table.
type Engine struct {
	DB *sql.DB
}

// Series runs one aggregated query. It validates the request, resolves the
// single winning Source for the range, and returns the Metric's rule applied
// per bucket in SQL. A range with no data yields an empty (non-nil) Points slice
// and an empty Source rather than an error.
func (e Engine) Series(ctx context.Context, req Request) (Series, error) {
	metric, ok := catalog.Lookup(req.Metric)
	if !ok {
		return Series{}, fmt.Errorf("%w: %q", ErrUnknownMetric, req.Metric)
	}
	if !req.To.After(req.From) {
		return Series{}, ErrInvalidRange
	}
	if req.To.Sub(req.From)/req.Bucket.approxDuration() > maxPoints {
		return Series{}, ErrRangeTooLarge
	}

	out := Series{
		Metric:      metric.Slug,
		Unit:        metric.Unit,
		Aggregation: metric.Aggregation,
		Bucket:      req.Bucket,
		Points:      []Point{},
	}

	source, ok, err := e.resolveSource(ctx, req)
	if err != nil {
		return Series{}, err
	}
	if !ok {
		return out, nil // no data in range: empty series, no Source
	}
	out.Source = source

	points, err := e.aggregate(ctx, req, metric.Aggregation, source)
	if err != nil {
		return Series{}, err
	}
	out.Points = points
	return out, nil
}

// resolveSource finds the Sources with data for the Metric in the range and
// picks the single winner by the Metric's Source priority (ADR 0003).
func (e Engine) resolveSource(ctx context.Context, req Request) (string, bool, error) {
	const q = `
		SELECT DISTINCT source
		FROM measurements
		WHERE account_id = ? AND metric = ? AND start_at >= ? AND start_at < ?`
	rows, err := e.DB.QueryContext(ctx, q, req.AccountID, req.Metric, rfc3339(req.From), rfc3339(req.To))
	if err != nil {
		return "", false, fmt.Errorf("query: distinct sources: %w", err)
	}
	defer rows.Close()

	var available []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return "", false, fmt.Errorf("query: scan source: %w", err)
		}
		available = append(available, s)
	}
	if err := rows.Err(); err != nil {
		return "", false, fmt.Errorf("query: iterate sources: %w", err)
	}

	source, ok := catalog.ResolveSource(req.Metric, available)
	return source, ok, nil
}

// aggregate runs the per-bucket SQL for the Metric's rule against the resolved
// Source and returns the ordered buckets.
func (e Engine) aggregate(ctx context.Context, req Request, agg catalog.Aggregation, source string) ([]Point, error) {
	bucket := req.Bucket.sqlExpr()
	args := []any{req.AccountID, req.Metric, source, rfc3339(req.From), rfc3339(req.To)}

	switch agg {
	case catalog.Sum:
		return e.scanScalar(ctx, fmt.Sprintf(`
			SELECT %s AS b, SUM(value) AS v
			FROM measurements
			WHERE account_id = ? AND metric = ? AND source = ? AND start_at >= ? AND start_at < ?
			GROUP BY b ORDER BY b`, bucket), args)

	case catalog.Latest:
		// The most recent point in each bucket; ties broken by row id so the
		// pick is stable.
		return e.scanScalar(ctx, fmt.Sprintf(`
			SELECT b, value FROM (
				SELECT %s AS b, value,
				       ROW_NUMBER() OVER (PARTITION BY %s ORDER BY start_at DESC, id DESC) AS rn
				FROM measurements
				WHERE account_id = ? AND metric = ? AND source = ? AND start_at >= ? AND start_at < ?
			) WHERE rn = 1 ORDER BY b`, bucket, bucket), args)

	case catalog.Average:
		return e.scanBand(ctx, fmt.Sprintf(`
			SELECT %s AS b, AVG(value) AS v, MIN(value) AS lo, MAX(value) AS hi
			FROM measurements
			WHERE account_id = ? AND metric = ? AND source = ? AND start_at >= ? AND start_at < ?
			GROUP BY b ORDER BY b`, bucket), args)

	default:
		// duration_by_state aggregates the States family (sleep, stand hours),
		// which lives in its own table with a state dimension and a different
		// result shape — not the scalar measurements this engine serves. No
		// Catalog Metric uses it (catalog.go), so it is unreachable via a metric
		// slug; it is deferred to the slice that graphs sleep. Derived Metrics
		// (v2) land here too. Guarded rather than assumed unreachable.
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedAggregation, agg)
	}
}

// scanScalar reads (bucket, value) rows for the sum and latest rules.
func (e Engine) scanScalar(ctx context.Context, q string, args []any) ([]Point, error) {
	rows, err := e.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query: aggregate: %w", err)
	}
	defer rows.Close()

	points := []Point{}
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.Bucket, &p.Value); err != nil {
			return nil, fmt.Errorf("query: scan point: %w", err)
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: iterate points: %w", err)
	}
	return points, nil
}

// scanBand reads (bucket, avg, min, max) rows for the average rule, attaching
// the min–max band to each Point.
func (e Engine) scanBand(ctx context.Context, q string, args []any) ([]Point, error) {
	rows, err := e.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query: aggregate: %w", err)
	}
	defer rows.Close()

	points := []Point{}
	for rows.Next() {
		var p Point
		var lo, hi float64
		if err := rows.Scan(&p.Bucket, &p.Value, &lo, &hi); err != nil {
			return nil, fmt.Errorf("query: scan band point: %w", err)
		}
		p.Min, p.Max = &lo, &hi
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: iterate points: %w", err)
	}
	return points, nil
}

// rfc3339 formats a time as the UTC RFC 3339 string measurements are stored in,
// so range bounds compare correctly against start_at.
func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }
