package api

import (
	"errors"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/query"
	"github.com/gauthier-se/verve/internal/timeaxis"
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

// metricView is one Catalog entry as exposed by the API. An imported Metric
// carries its aggregation rule; a derived Metric instead reports its Formula and
// a signed flag and omits aggregation — it has no rule of its own (ADR 0014), so
// the field is dropped rather than faked.
type metricView struct {
	Slug        string       `json:"slug"`
	Unit        string       `json:"unit"`
	Aggregation string       `json:"aggregation,omitempty"`
	Nature      string       `json:"nature"`
	Signed      bool         `json:"signed,omitempty"`
	Formula     *formulaView `json:"formula,omitempty"`
}

// formulaView renders a derived Metric's Formula in a readable, structured form
// for a tooltip: the operand terms of the numerator and denominator weighted sums
// plus the constant scale (ADR 0014). An empty denominator means 1.
type formulaView struct {
	Scale       float64    `json:"scale"`
	Numerator   []termView `json:"numerator"`
	Denominator []termView `json:"denominator,omitempty"`
}

// termView is one Formula operand: a Catalog slug and its coefficient.
type termView struct {
	Metric      string  `json:"metric"`
	Coefficient float64 `json:"coefficient"`
}

// handleMetrics exposes the Catalog: every canonical Metric with its unit and
// nature, imported entries carrying their aggregation rule and derived entries
// their Formula and signed flag, sorted by slug for a stable listing.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	all := catalog.All()
	views := make([]metricView, 0, len(all))
	for _, m := range all {
		views = append(views, metricToView(m))
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Slug < views[j].Slug })

	if err := writeJSON(w, http.StatusOK, envelope{"metrics": views}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// metricToView projects a Catalog Metric to its API shape. Aggregation is empty
// for a derived Metric (omitted by the JSON tag), which instead carries a Formula.
func metricToView(m catalog.Metric) metricView {
	v := metricView{
		Slug: m.Slug, Unit: m.Unit,
		Aggregation: string(m.Aggregation), Nature: string(m.Nature),
		Signed: m.Signed,
	}
	if m.Formula != nil {
		v.Formula = formulaToView(*m.Formula)
	}
	return v
}

// formulaToView projects a Catalog Formula to its API shape. The denominator is
// omitted when empty, mirroring the "denominator = 1" convention (ADR 0014).
func formulaToView(f catalog.Formula) *formulaView {
	fv := &formulaView{Scale: f.Scale, Numerator: termsToView(f.Numerator)}
	if len(f.Denominator) > 0 {
		fv.Denominator = termsToView(f.Denominator)
	}
	return fv
}

// termsToView projects a Formula's weighted-sum Terms to their API shape.
func termsToView(terms []catalog.Term) []termView {
	out := make([]termView, len(terms))
	for i, t := range terms {
		out[i] = termView{Metric: t.Metric, Coefficient: t.Coefficient}
	}
	return out
}

// handleSeries answers the aggregated-bucket query: metric + the Dashboard's time
// axis tokens → one point per bucket under the Metric's rule (ADR 0012), scoped to
// the request's Account. timeaxis resolves the tokens into the concrete current
// window, the optional Baseline window, and the bucket.
func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	qs := r.URL.Query()
	v := NewValidator()

	metric := qs.Get("metric")
	v.Check(metric != "", "metric", "must be provided")
	if metric != "" {
		if _, ok := catalog.Lookup(metric); !ok {
			v.AddError("metric", unknownMetricMsg)
		}
	}

	resolved, err := timeaxis.Resolve(timeaxis.Tokens{
		RangePreset:  qs.Get("range_preset"),
		RangeFrom:    optionalParam(qs, "range_from"),
		RangeTo:      optionalParam(qs, "range_to"),
		BaselineRule: qs.Get("baseline_rule"),
		BaselineFrom: optionalParam(qs, "baseline_from"),
		BaselineTo:   optionalParam(qs, "baseline_to"),
		Bucket:       optionalParam(qs, "bucket"),
	}, time.Now())
	if inv, ok := err.(timeaxis.Invalid); ok {
		for field, msg := range inv {
			v.AddError(field, msg)
		}
	} else if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	req := query.Request{
		AccountID: accountID, Metric: metric,
		From: resolved.Current.From, To: resolved.Current.To, Bucket: resolved.Bucket,
	}

	// Without a baseline the response keeps its pre-comparison single-series shape.
	if resolved.Baseline == nil {
		series, err := s.engine.Series(r.Context(), req)
		if err != nil {
			s.respondSeriesError(w, r, err)
			return
		}
		if err := writeJSON(w, http.StatusOK, envelope{"series": series}, nil); err != nil {
			s.serverErrorResponse(w, r, err)
		}
		return
	}

	// Comparison mode: the current series plus a baseline series over the resolved
	// baseline window, aligned and equal length by the engine (ADR 0015).
	cmp, err := s.engine.Compare(r.Context(), req, resolved.Baseline.From, resolved.Baseline.To)
	if err != nil {
		s.respondSeriesError(w, r, err)
		return
	}
	if err := writeJSON(w, http.StatusOK, envelope{"series": cmp.Current, "baseline": cmp.Baseline}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// optionalParam returns the query value for key, or nil when it is absent or
// empty — the shape timeaxis.Tokens uses to tell an omitted bound from a set one.
func optionalParam(qs url.Values, key string) *string {
	if val := qs.Get(key); val != "" {
		return &val
	}
	return nil
}

// respondSeriesError maps query-engine errors to HTTP responses. The input
// errors are semantic (422) rather than parse failures; genuine faults are 500.
func (s *Server) respondSeriesError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, query.ErrUnknownMetric):
		s.failedValidationResponse(w, r, map[string]string{"metric": unknownMetricMsg})
	case errors.Is(err, query.ErrInvalidRange):
		s.failedValidationResponse(w, r, map[string]string{"range_preset": "the range is empty or inverted"})
	case errors.Is(err, query.ErrRangeTooLarge):
		s.failedValidationResponse(w, r, map[string]string{"bucket": "too many buckets for this range; use a coarser bucket"})
	case errors.Is(err, query.ErrUnsupportedAggregation):
		s.errorResponse(w, r, http.StatusNotImplemented, "this metric's aggregation is not served yet")
	default:
		s.serverErrorResponse(w, r, err)
	}
}
