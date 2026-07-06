package api

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/query"
)

// unknownMetricMsg is the single client-facing message for a slug outside the
// Catalog, shared by the up-front validation and the engine-error fallback.
const unknownMetricMsg = "unknown metric — see GET /v1/metrics"

// handleHealthz is an unauthenticated liveness+DB check for probes.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := s.models.Ping(r.Context()); err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	if err := writeJSON(w, http.StatusOK, envelope{"status": "ok"}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// metricView is one Catalog entry as exposed by the API.
type metricView struct {
	Slug        string `json:"slug"`
	Unit        string `json:"unit"`
	Aggregation string `json:"aggregation"`
	Nature      string `json:"nature"`
}

// handleMetrics exposes the Catalog: every canonical Metric with its unit,
// aggregation rule, and nature, sorted by slug for a stable listing.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	all := catalog.All()
	views := make([]metricView, 0, len(all))
	for _, m := range all {
		views = append(views, metricView{
			Slug: m.Slug, Unit: m.Unit,
			Aggregation: string(m.Aggregation), Nature: string(m.Nature),
		})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Slug < views[j].Slug })

	if err := writeJSON(w, http.StatusOK, envelope{"metrics": views}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// handleSeries answers the aggregated-bucket query: metric + time range +
// bucket → one point per bucket under the Metric's rule (ADR 0012), scoped to
// the request's Account.
func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	// requireAuth guarantees an authenticated Account on the context.
	accountID, _ := s.accountID(r)

	qs := r.URL.Query()
	v := NewValidator()

	metric := qs.Get("metric")
	v.Check(metric != "", "metric", "must be provided")
	if metric != "" {
		_, ok := catalog.Lookup(metric)
		v.Check(ok, "metric", unknownMetricMsg)
	}

	bucket := s.parseBucket(qs.Get("bucket"), v)
	from, to := s.parseTimeRange(qs, v, time.Now())

	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	series, err := s.engine.Series(r.Context(), query.Request{
		AccountID: accountID, Metric: metric, From: from, To: to, Bucket: bucket,
	})
	if err != nil {
		s.respondSeriesError(w, r, err)
		return
	}
	if err := writeJSON(w, http.StatusOK, envelope{"series": series}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// parseBucket resolves the bucket parameter, defaulting to day. A too-fine or
// unknown bucket records a validation error rather than reaching the engine.
func (s *Server) parseBucket(raw string, v *Validator) query.Bucket {
	if raw == "" {
		return query.Day
	}
	bucket, err := query.ParseBucket(raw)
	switch {
	case errors.Is(err, query.ErrBucketTooFine):
		v.AddError("bucket", "below the resolution cap; use day, week, or month")
	case err != nil:
		v.AddError("bucket", "unknown bucket; use day, week, or month")
	}
	return bucket
}

// parseTimeRange resolves the query window from either explicit from/to
// parameters (RFC 3339 or YYYY-MM-DD) or a range shorthand like "30d"/"1y",
// recording validation errors for anything malformed.
func (s *Server) parseTimeRange(qs map[string][]string, v *Validator, now time.Time) (from, to time.Time) {
	get := func(k string) string {
		if vs := qs[k]; len(vs) > 0 {
			return vs[0]
		}
		return ""
	}
	fromStr, toStr, rangeStr := get("from"), get("to"), get("range")

	switch {
	case fromStr != "" || toStr != "":
		from = parseTimeParam(fromStr, "from", v)
		to = parseTimeParam(toStr, "to", v)
	case rangeStr != "":
		f, t, ok := parseRange(rangeStr, now)
		v.Check(ok, "range", "must be <N>d, <N>w, <N>m, or <N>y (e.g. 30d, 1y)")
		from, to = f, t
	default:
		v.AddError("range", "provide range=<N>[dwmy], or both from and to")
	}
	return from, to
}

// parseTimeParam parses one time parameter as RFC 3339 or a bare date.
func parseTimeParam(s, field string, v *Validator) time.Time {
	if s == "" {
		v.AddError(field, "must be provided alongside the other bound")
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC()
	}
	v.AddError(field, "must be RFC 3339 or YYYY-MM-DD")
	return time.Time{}
}

// parseRange turns a shorthand like "30d" or "1y" into a [from, to=now] window.
// It reports ok=false for any malformed or non-positive value.
func parseRange(s string, now time.Time) (from, to time.Time, ok bool) {
	if len(s) < 2 {
		return time.Time{}, time.Time{}, false
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n <= 0 {
		return time.Time{}, time.Time{}, false
	}
	switch s[len(s)-1] {
	case 'd':
		from = now.AddDate(0, 0, -n)
	case 'w':
		from = now.AddDate(0, 0, -7*n)
	case 'm':
		from = now.AddDate(0, -n, 0)
	case 'y':
		from = now.AddDate(-n, 0, 0)
	default:
		return time.Time{}, time.Time{}, false
	}
	return from, now, true
}

// respondSeriesError maps query-engine errors to HTTP responses. The input
// errors are semantic (422) rather than parse failures; genuine faults are 500.
func (s *Server) respondSeriesError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, query.ErrUnknownMetric):
		s.failedValidationResponse(w, r, map[string]string{"metric": unknownMetricMsg})
	case errors.Is(err, query.ErrInvalidRange):
		s.failedValidationResponse(w, r, map[string]string{"range": "the range is empty or inverted (from must be before to)"})
	case errors.Is(err, query.ErrRangeTooLarge):
		s.failedValidationResponse(w, r, map[string]string{"bucket": "too many buckets for this range; use a coarser bucket"})
	case errors.Is(err, query.ErrUnsupportedAggregation):
		s.errorResponse(w, r, http.StatusNotImplemented, "this metric's aggregation is not served yet")
	default:
		s.serverErrorResponse(w, r, err)
	}
}
