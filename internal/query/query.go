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
//
// A derived Metric has no rule of its own: it is recomputed per bucket from its
// Formula operands, each resolved as its own aggregated series (own Source, own
// rule) and combined in Go (seriesDerived, ADR 0014).
package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
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
	// the engine does not serve yet (duration_by_state). Derived Metrics are
	// served via the seriesDerived path (ADR 0014), not this rule.
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

// sqlExpr maps a row's RFC 3339 start_at to its bucket-start date (YYYY-MM-DD)
// for GROUP BY. snap is its Go twin; TestBucketBoundaryGoSQLAgree pins that the
// two produce the same boundary.
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

// starts enumerates the bucket-start dates covering [from, to), in order — the
// ordinal sequence used to align a Baseline by position, not date (ADR 0015).
func (b Bucket) starts(from, to time.Time) []string {
	out := []string{}
	for cur := b.snap(from); cur.Before(to.UTC()); cur = b.next(cur) {
		out = append(out, cur.Format("2006-01-02"))
	}
	return out
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
//
// Gap marks an ordinal slot a comparison had to hold open: a Baseline bucket with
// no data that still needs a position so the baseline stays index-aligned with
// the current series (ADR 0015). It appears only in an aligned Baseline series —
// a normal query never emits gaps, it omits empty buckets — and carries the real
// bucket date (for a tooltip) but no value.
type Point struct {
	Bucket string   `json:"bucket"`
	Value  float64  `json:"value"`
	Min    *float64 `json:"min,omitempty"`
	Max    *float64 `json:"max,omitempty"`
	Gap    bool     `json:"gap,omitempty"`
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

	if metric.Nature == catalog.Derived {
		return e.seriesDerived(ctx, req, metric)
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

// seriesDerived answers a request for a derived Metric by recomputing it per
// bucket from its Formula operands (ADR 0014). Each operand is resolved as its
// own aggregated series — its own winning Source, its own rule — then the Formula
// combines the operands bucket-by-bucket in Go. A bucket is emitted only when
// every operand has a value and the denominator is non-zero; otherwise it is a
// gap, never a zero. The derived Series carries no single Source (each operand
// resolves independently) and its Points carry no min/max band.
func (e Engine) seriesDerived(ctx context.Context, req Request, metric catalog.Metric) (Series, error) {
	if metric.Formula == nil {
		// A derived Metric always carries a Formula (validated at build time,
		// formula_test); guard rather than deref-panic on a mislabeled entry.
		return Series{}, fmt.Errorf("%w: derived %q has no Formula", ErrUnsupportedAggregation, metric.Slug)
	}

	out := Series{
		Metric:      metric.Slug,
		Unit:        metric.Unit,
		Aggregation: metric.Aggregation, // empty: a derived Metric has no rule
		Bucket:      req.Bucket,
		Points:      []Point{},
	}

	// Resolve each distinct operand into its own per-bucket aggregated values.
	operands := metric.Formula.Operands()
	byOperand := make(map[string]map[string]float64, len(operands))
	for _, slug := range operands {
		vals, err := e.resolveOperand(ctx, req, slug)
		if err != nil {
			return Series{}, err
		}
		byOperand[slug] = vals
	}

	// Combine per bucket over the union of buckets any operand produced; the
	// Formula decides which are complete (all operands present, denominator
	// non-zero) and which are gaps.
	for _, bucket := range unionBuckets(byOperand) {
		values := make(map[string]float64, len(operands))
		for slug, vals := range byOperand {
			if v, ok := vals[bucket]; ok {
				values[slug] = v
			}
		}
		if v, ok := metric.Formula.Evaluate(values); ok {
			out.Points = append(out.Points, Point{Bucket: bucket, Value: v})
		}
	}
	return out, nil
}

// resolveOperand aggregates one Formula operand as its own series — its own
// winning Source (ADR 0003), its own Catalog rule — and returns its value keyed
// by bucket. An operand with no data anywhere in the range yields an empty map,
// so every bucket is a gap for it. Only the aggregated value is kept; an average
// operand's min/max band is dropped, so no band leaks into the derived Series.
func (e Engine) resolveOperand(ctx context.Context, req Request, slug string) (map[string]float64, error) {
	m, ok := catalog.Lookup(slug)
	if !ok {
		// Operands are validated against the Catalog at build time (formula_test);
		// guard rather than assume the invariant holds at runtime.
		return nil, fmt.Errorf("%w: derived operand %q", ErrUnknownMetric, slug)
	}

	opReq := req
	opReq.Metric = slug
	source, ok, err := e.resolveSource(ctx, opReq)
	if err != nil {
		return nil, err
	}
	if !ok {
		return map[string]float64{}, nil // no data for this operand in the range
	}

	points, err := e.aggregate(ctx, opReq, m.Aggregation, source)
	if err != nil {
		return nil, err
	}
	vals := make(map[string]float64, len(points))
	for _, p := range points {
		vals[p.Bucket] = p.Value
	}
	return vals, nil
}

// unionBuckets returns the sorted set of bucket keys present across every
// operand's values. Bucket keys are YYYY-MM-DD, so lexical order is chronological.
func unionBuckets(byOperand map[string]map[string]float64) []string {
	seen := map[string]struct{}{}
	for _, vals := range byOperand {
		for b := range vals {
			seen[b] = struct{}{}
		}
	}
	buckets := make([]string, 0, len(seen))
	for b := range seen {
		buckets = append(buckets, b)
	}
	sort.Strings(buckets)
	return buckets
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
		// take the seriesDerived path (ADR 0014) and never reach here — an empty
		// agg would be a mislabeled operand. Guarded rather than assumed
		// unreachable.
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
