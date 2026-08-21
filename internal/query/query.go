// Package query is Verve's read engine: it turns a request for one Metric over a
// time range into server-side aggregated buckets, never a raw series (ADR 0012),
// scoped to the owning Account (ADR 0007) and pinned to one winning Source (ADR
// 0003). A derived Metric is recomputed per bucket from its Formula operands
// (seriesDerived, ADR 0014).
package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
)

// Sentinel errors let the HTTP layer map a failed query to a status without
// depending on message text.
var (
	ErrUnknownMetric = errors.New("query: unknown metric")
	ErrUnknownBucket = errors.New("query: unknown bucket")
	// ErrBucketTooFine is a recognized bucket below the resolution cap (ADR 0012).
	ErrBucketTooFine = errors.New("query: bucket below the resolution cap")
	// ErrInvalidRange is an empty or inverted range (from ≥ to).
	ErrInvalidRange = errors.New("query: invalid time range")
	// ErrRangeTooLarge is range ÷ bucket exceeding maxPoints.
	ErrRangeTooLarge = errors.New("query: range too large for bucket")
	// ErrUnsupportedAggregation is a rule the engine does not serve (duration_by_state).
	ErrUnsupportedAggregation = errors.New("query: unsupported aggregation")
)

// maxPoints caps how many buckets one query may span; a finer bucket over a
// larger range is rejected, keeping the payload bounded regardless of history.
const maxPoints = 1000

// Bucket is a supported time-bucket granularity. Day is the finest the API
// exposes (ADR 0012); finer names parse to ErrBucketTooFine.
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

// Start is the bucket-start date (YYYY-MM-DD) of the bucket holding t: the key
// a Point carries and the category a chart's X axis is drawn on. It is snap's
// exported face, so anything that has to name a position on the grid (a folded
// Annotation, say) asks the module that owns the boundaries instead of
// re-deriving them.
func (b Bucket) Start(t time.Time) string {
	return b.snap(t).Format("2006-01-02")
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
// the grid a dense series is drawn on when the gaps themselves are the subject.
func (b Bucket) Starts(from, to time.Time) []string {
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

// Point is one aggregated bucket: Bucket is the start date (YYYY-MM-DD), Value the
// aggregate under the Metric's rule, Min/Max the band for average Metrics (nil
// otherwise). Gap marks a dated Baseline slot held open for ordinal alignment
// (ADR 0015) — no value, and only ever set in an aligned Baseline series.
type Point struct {
	Bucket string   `json:"bucket"`
	Value  float64  `json:"value"`
	Min    *float64 `json:"min,omitempty"`
	Max    *float64 `json:"max,omitempty"`
	Gap    bool     `json:"gap,omitempty"`
	// States is the bucket's minutes per Stage, set only for a duration_by_state
	// Metric (ADR 0027): the decomposition the stacked bar renders. Value stays the
	// one scalar every other consumer reads — for sleep, time asleep — so `awake`
	// appears here and is never counted there. Omitted for every other Metric, whose
	// payload is therefore byte-identical to what it was before sleep was served.
	States map[string]float64 `json:"states,omitempty"`
	// Count is how many rows the bucket was folded from — Measurements for an
	// observed Metric, Nights for a duration_by_state one (ADR 0027). It is the
	// evidence behind the value, which the Ledger prints beside it: a weekly average
	// of 52 bpm reads differently at 300 readings than at two. Zero (omitted) for a
	// derived Metric, whose operands each have their own count and whose combined one
	// would mean nothing.
	Count int `json:"count,omitempty"`
}

// Series is the result of a query: the resolved Metric metadata, the single
// winning Source (empty when the range holds no data), the ordered buckets, and the
// Panel summary — the whole window folded into one value (ADR 0019).
type Series struct {
	Metric      string              `json:"metric"`
	Unit        string              `json:"unit"`
	Aggregation catalog.Aggregation `json:"aggregation"`
	Bucket      Bucket              `json:"bucket"`
	Source      string              `json:"source"`
	Points      []Point             `json:"points"`
	// Summary is the Panel summary: the Metric aggregated over the whole window as a
	// single bucket (ADR 0019), so an average is a true count-weighted mean and not a
	// mean of per-bucket means. Nil is a gap ("—"): no data, or a derived Metric
	// missing a required operand over the window. Never re-derived client-side.
	Summary *Point `json:"summary,omitempty"`
	// Days is the whole-day span of the query window [From, To). It is the honest
	// denominator for a per-day average of a `sum` summary (total ÷ Days) — including
	// a Baseline window of a different length — so the client divides a server total
	// by a server day-count rather than guessing from the (gap-pruned) points.
	Days int `json:"days"`
	// Mean is the arithmetic mean of the Metric's values over the window, set only for
	// a `latest` Metric — its "average over the period" (e.g. mean body mass). The
	// summary of a `latest` Metric is its last reading; Mean is what the client shows
	// instead when summaries are rendered as period averages, so a trend (this period's
	// mean vs the compared period's mean) is legible. Nil is a gap (no data).
	Mean *float64 `json:"mean,omitempty"`
	// Nights is the number of Nights holding data in the window, set only for a
	// duration_by_state Metric (ADR 0027). It is to a per-night figure what Days is to
	// a per-day one: the honest denominator. A 30-day window over 21 recorded nights
	// must divide by 21 — dividing by Days would report a shortfall the Account never
	// had, with the confidence of a computed number.
	Nights int `json:"nights,omitempty"`
}

// windowDays is the whole-day span of a query window [from, to), rounded to the
// nearest day (windows are day-aligned by timeaxis, so this is exact in practice).
// It is the per-day denominator carried on the Series.
func windowDays(from, to time.Time) int {
	return int(math.Round(to.Sub(from).Hours() / 24))
}

// Engine answers aggregated-series queries against the measurements table.
type Engine struct {
	DB *sql.DB
}

// Series runs one aggregated query: it validates the request, resolves the
// winning Source, and applies the Metric's rule per bucket in SQL. A range with no
// data yields an empty (non-nil) Points slice and an empty Source, not an error.
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
	// A duration_by_state Metric reads intervals from the States family, not points
	// from measurements, and buckets them by Night (sleep.go, ADR 0027). Everything
	// below this line is measurement-shaped — the Source filter, the Manual overlay,
	// the per-bucket SQL — and stays untouched by it.
	if metric.Aggregation == catalog.DurationByState {
		return e.seriesSleep(ctx, req, metric)
	}

	out := Series{
		Metric:      metric.Slug,
		Unit:        metric.Unit,
		Aggregation: metric.Aggregation,
		Bucket:      req.Bucket,
		Points:      []Point{},
		Days:        windowDays(req.From, req.To),
	}

	filter, err := e.resolveSource(ctx, req)
	if err != nil {
		return Series{}, err
	}
	if !filter.any() {
		return out, nil // no data in range: empty series, no Source
	}
	out.Source = filter.reported()

	points, err := e.aggregate(ctx, req, metric.Aggregation, filter)
	if err != nil {
		return Series{}, err
	}
	out.Points = points

	summary, err := e.summarize(ctx, req, metric.Aggregation, filter)
	if err != nil {
		return Series{}, err
	}
	out.Summary = summary

	// A `latest` Metric also carries its window mean, so it can be shown as a period
	// average (mean body mass) rather than the last reading — the better trend view.
	if metric.Aggregation == catalog.Latest && summary != nil {
		mean, err := e.summaryMean(ctx, req, filter)
		if err != nil {
			return Series{}, err
		}
		out.Mean = mean
	}
	return out, nil
}

// summaryMean is the arithmetic mean of a Metric's values over the window, from the
// winning Source — the period average behind a `latest` Metric's trend. A NULL mean
// (empty window) is a nil gap.
func (e Engine) summaryMean(ctx context.Context, req Request, f sourceFilter) (*float64, error) {
	pred, args := f.where(req)
	q := `SELECT AVG(value) FROM measurements WHERE ` + pred
	var v sql.NullFloat64
	err := e.DB.QueryRowContext(ctx, q, args...).Scan(&v)
	if err != nil {
		return nil, fmt.Errorf("query: summary mean: %w", err)
	}
	if !v.Valid {
		return nil, nil
	}
	return &v.Float64, nil
}

// seriesDerived recomputes a derived Metric per bucket from its Formula operands
// (ADR 0014): each operand resolves as its own aggregated series, then the Formula
// combines them per bucket. A bucket with any operand missing (or a zero
// denominator) is a gap, never a zero. No single Source, no min/max band.
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
		Days:        windowDays(req.From, req.To),
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

	summary, err := e.summarizeDerived(ctx, req, metric)
	if err != nil {
		return Series{}, err
	}
	out.Summary = summary
	return out, nil
}

// resolveOperand aggregates one Formula operand as its own series (own Source per
// ADR 0003, own rule) keyed by bucket; no data yields an empty map. The band is dropped.
func (e Engine) resolveOperand(ctx context.Context, req Request, slug string) (map[string]float64, error) {
	m, ok := catalog.Lookup(slug)
	if !ok {
		// Operands are validated against the Catalog at build time (formula_test);
		// guard rather than assume the invariant holds at runtime.
		return nil, fmt.Errorf("%w: derived operand %q", ErrUnknownMetric, slug)
	}

	opReq := req
	opReq.Metric = slug
	filter, err := e.resolveSource(ctx, opReq)
	if err != nil {
		return nil, err
	}
	if !filter.any() {
		return map[string]float64{}, nil // no data for this operand in the range
	}

	points, err := e.aggregate(ctx, opReq, m.Aggregation, filter)
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

// sourceFilter is the resolved row set for one Metric over one window: the winning
// imported Source (ADR 0003) plus, when the Account has typed values for this Metric,
// the Manual overlay (ADR 0022).
//
// The two mechanics are deliberately different shapes. Source priority elects one
// winner for the *whole range* and every read then filters on it, which suits devices
// producing continuous streams. A human corrects isolated days, so a Manual entry
// cannot compete as a Source — ranking it first would make one typed value the winner
// of the entire window and hide every device reading around it. It overlays at **day**
// grain instead: on a day the Account typed a value, that day's Manual rows replace
// the winner's rows; every other day is untouched.
//
// Day grain, rather than the caller's bucket grain, is what keeps the resolved row set
// independent of how the caller happens to be bucketing — so a daily chart, a monthly
// chart and a window summary can never disagree about which rows are in play.
type sourceFilter struct {
	source    string // winning imported Source; "" when only Manual rows exist
	hasManual bool   // the Account has Manual rows for this Metric in the window
}

// any reports whether the filter selects anything at all: an imported winner, Manual
// rows, or both. False means no data in the range.
func (f sourceFilter) any() bool { return f.source != "" || f.hasManual }

// reported is the Source name carried on the Series. An active overlay does not change
// it: the imported Source is still what the bulk of the curve comes from, and calling
// the whole series "Manual" because one day was corrected would misdescribe it.
func (f sourceFilter) reported() string {
	if f.source != "" {
		return f.source
	}
	return catalog.SourceManual
}

// where renders the row-set predicate shared by every read path, and its args. It is a
// drop-in replacement for the plain `source = ?` filter this engine used before the
// overlay existed.
//
// With no Manual rows — every Metric of every Account that has never typed one — it
// emits exactly that original predicate, so nothing existing can change behaviour.
func (f sourceFilter) where(req Request) (string, []any) {
	const base = `account_id = ? AND metric = ? AND source = ? AND start_at >= ? AND start_at < ?`
	from, to := rfc3339(req.From), rfc3339(req.To)

	if !f.hasManual {
		return base, []any{req.AccountID, req.Metric, f.source, from, to}
	}
	if f.source == "" {
		// Manual rows only: the base predicate, pinned to the Manual Source.
		return base, []any{req.AccountID, req.Metric, catalog.SourceManual, from, to}
	}

	// The overlay. The manual-days subquery is deliberately *not* range-filtered: a
	// window boundary that splits a day would otherwise let the device's rows for that
	// day survive alongside the correction. Manual rows are few by nature (a person
	// types them), so scanning them all is cheaper than getting this subtly wrong.
	const overlay = `account_id = ? AND metric = ? AND start_at >= ? AND start_at < ?
		AND (source = ? OR source = ?)
		AND (source = ? OR date(start_at) NOT IN (
			SELECT date(mo.start_at) FROM measurements mo
			WHERE mo.account_id = ? AND mo.metric = ? AND mo.source = ?))`
	return overlay, []any{
		req.AccountID, req.Metric, from, to,
		f.source, catalog.SourceManual,
		catalog.SourceManual,
		req.AccountID, req.Metric, catalog.SourceManual,
	}
}

// resolveSource finds the Sources with data for the Metric in the range, elects the
// single imported winner by the Metric's Source priority (ADR 0003), and notes
// separately whether the Account has Manual rows to overlay (ADR 0022). Manual is
// split out *before* the election so it never competes as a Source.
func (e Engine) resolveSource(ctx context.Context, req Request) (sourceFilter, error) {
	const q = `
		SELECT DISTINCT source
		FROM measurements
		WHERE account_id = ? AND metric = ? AND start_at >= ? AND start_at < ?`
	rows, err := e.DB.QueryContext(ctx, q, req.AccountID, req.Metric, rfc3339(req.From), rfc3339(req.To))
	if err != nil {
		return sourceFilter{}, fmt.Errorf("query: distinct sources: %w", err)
	}
	defer rows.Close()

	var f sourceFilter
	var available []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return sourceFilter{}, fmt.Errorf("query: scan source: %w", err)
		}
		if s == catalog.SourceManual {
			f.hasManual = true
			continue
		}
		available = append(available, s)
	}
	if err := rows.Err(); err != nil {
		return sourceFilter{}, fmt.Errorf("query: iterate sources: %w", err)
	}

	if source, ok := catalog.ResolveSource(req.Metric, available); ok {
		f.source = source
	}
	return f, nil
}

// aggregate runs the per-bucket SQL for the Metric's rule against the resolved row
// set and returns the ordered buckets. The filter has already settled which rows are
// in play (winning Source, Manual overlay), so every rule below is unchanged by the
// overlay's existence — that separation is the point of resolving at day grain.
func (e Engine) aggregate(ctx context.Context, req Request, agg catalog.Aggregation, f sourceFilter) ([]Point, error) {
	bucket := req.Bucket.sqlExpr()
	where, args := f.where(req)

	switch agg {
	case catalog.Sum:
		return e.scanScalar(ctx, fmt.Sprintf(`
			SELECT %s AS b, SUM(value) AS v, COUNT(*) AS n
			FROM measurements
			WHERE %s
			GROUP BY b ORDER BY b`, bucket, where), args)

	case catalog.Latest:
		// Most recent point per bucket; ties broken by row id for a stable pick. The
		// count is the bucket's whole row set, not the one row that won: it says how
		// much evidence the bucket held, which is the question the Ledger asks.
		return e.scanScalar(ctx, fmt.Sprintf(`
			SELECT b, value, n FROM (
				SELECT %s AS b, value,
				       COUNT(*) OVER (PARTITION BY %s) AS n,
				       ROW_NUMBER() OVER (PARTITION BY %s ORDER BY start_at DESC, id DESC) AS rn
				FROM measurements
				WHERE %s
			) WHERE rn = 1 ORDER BY b`, bucket, bucket, bucket, where), args)

	case catalog.Average:
		return e.scanBand(ctx, fmt.Sprintf(`
			SELECT %s AS b, AVG(value) AS v, MIN(value) AS lo, MAX(value) AS hi, COUNT(*) AS n
			FROM measurements
			WHERE %s
			GROUP BY b ORDER BY b`, bucket, where), args)

	default:
		// duration_by_state never reaches here: Series routes it to seriesSleep, which
		// reads the states table, and derived Metrics take seriesDerived. Guarded rather
		// than assumed, so a future rule that forgets to route itself fails loudly.
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedAggregation, agg)
	}
}

// scanScalar reads (bucket, value, count) rows for the sum and latest rules.
func (e Engine) scanScalar(ctx context.Context, q string, args []any) ([]Point, error) {
	rows, err := e.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query: aggregate: %w", err)
	}
	defer rows.Close()

	points := []Point{}
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.Bucket, &p.Value, &p.Count); err != nil {
			return nil, fmt.Errorf("query: scan point: %w", err)
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: iterate points: %w", err)
	}
	return points, nil
}

// scanBand reads (bucket, avg, min, max, count) rows for the average rule, attaching
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
		if err := rows.Scan(&p.Bucket, &p.Value, &lo, &hi, &p.Count); err != nil {
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

// summarize folds the whole window into one value under the Metric's rule — the
// Panel summary, "a single bucket spanning the range" (ADR 0019). It is the same
// aggregation as a bucket but with no GROUP BY, so an average runs over the raw rows
// and is a true count-weighted mean, never a mean of per-bucket means. Returns nil
// when the window holds no value (a gap: "—", never a zero). The Point's Bucket
// carries the window start for completeness; the client reads only the value/band.
func (e Engine) summarize(ctx context.Context, req Request, agg catalog.Aggregation, f sourceFilter) (*Point, error) {
	pred, args := f.where(req)
	label := req.From.UTC().Format("2006-01-02")
	where := `WHERE ` + pred
	switch agg {
	case catalog.Sum:
		return e.scanSummaryScalar(ctx, `SELECT SUM(value), COUNT(*) FROM measurements `+where, args, label)
	case catalog.Latest:
		return e.scanSummaryScalar(ctx, `SELECT value, (SELECT COUNT(*) FROM measurements `+where+`) FROM measurements `+where+
			` ORDER BY start_at DESC, id DESC LIMIT 1`, append(append([]any{}, args...), args...), label)
	case catalog.Average:
		return e.scanSummaryBand(ctx, `SELECT AVG(value), MIN(value), MAX(value), COUNT(*) FROM measurements `+where, args, label)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedAggregation, agg)
	}
}

// scanSummaryScalar reads a single window value for the sum and latest rules. A
// NULL aggregate or no row means an empty window: a nil summary (gap).
func (e Engine) scanSummaryScalar(ctx context.Context, q string, args []any, label string) (*Point, error) {
	var v sql.NullFloat64
	var n int
	err := e.DB.QueryRowContext(ctx, q, args...).Scan(&v, &n)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query: summarize: %w", err)
	}
	if !v.Valid {
		return nil, nil
	}
	return &Point{Bucket: label, Value: v.Float64, Count: n}, nil
}

// scanSummaryBand reads the window mean with its min–max band for the average rule.
func (e Engine) scanSummaryBand(ctx context.Context, q string, args []any, label string) (*Point, error) {
	var avg, lo, hi sql.NullFloat64
	var n int
	err := e.DB.QueryRowContext(ctx, q, args...).Scan(&avg, &lo, &hi, &n)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query: summarize band: %w", err)
	}
	if !avg.Valid {
		return nil, nil
	}
	l, h := lo.Float64, hi.Float64
	return &Point{Bucket: label, Value: avg.Float64, Min: &l, Max: &h, Count: n}, nil
}

// summarizeDerived folds a derived Metric over the whole window: each operand is
// aggregated over the window as a single value (its own rule), then the Formula is
// applied once (ADR 0019) — so a ratio is the period's real ratio, not a mean of
// per-bucket ratios. A missing required operand (or a zero denominator) yields nil,
// the ADR 0014 gap rule at window scope.
func (e Engine) summarizeDerived(ctx context.Context, req Request, metric catalog.Metric) (*Point, error) {
	values := make(map[string]float64, len(metric.Formula.Operands()))
	for _, slug := range metric.Formula.Operands() {
		v, ok, err := e.summarizeOperand(ctx, req, slug)
		if err != nil {
			return nil, err
		}
		if ok {
			values[slug] = v
		}
	}
	v, ok := metric.Formula.Evaluate(values)
	if !ok {
		return nil, nil // a required operand absent over the window: a gap
	}
	return &Point{Bucket: req.From.UTC().Format("2006-01-02"), Value: v}, nil
}

// summarizeOperand folds one Formula operand over the whole window (its own Source
// per ADR 0003, its own rule) into a single value; no data yields ok=false.
func (e Engine) summarizeOperand(ctx context.Context, req Request, slug string) (float64, bool, error) {
	m, ok := catalog.Lookup(slug)
	if !ok {
		return 0, false, fmt.Errorf("%w: derived operand %q", ErrUnknownMetric, slug)
	}
	opReq := req
	opReq.Metric = slug
	filter, err := e.resolveSource(ctx, opReq)
	if err != nil {
		return 0, false, err
	}
	if !filter.any() {
		return 0, false, nil // no data for this operand in the window
	}
	p, err := e.summarize(ctx, opReq, m.Aggregation, filter)
	if err != nil {
		return 0, false, err
	}
	if p == nil {
		return 0, false, nil
	}
	return p.Value, true, nil
}

// rfc3339 formats a time as the UTC RFC 3339 string measurements are stored in,
// so range bounds compare correctly against start_at.
func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }
