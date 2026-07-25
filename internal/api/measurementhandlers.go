package api

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/data"
)

// Manual entry endpoints (ADR 0022): the Account's own write path into the measurement
// store, which until now had exactly one writer — an idempotent Connector import.
//
// The values here are the *canonical* stored ones, never a friendlier display scale.
// That matters most for `%` Metrics, which Apple stores as fractions: a 27% body fat is
// `0.27`, and an oxygen saturation of 96.9% is `0.969`. Rescaling belongs in the client,
// where the unit is shown next to the field; doing it here would leave the API unable to
// say what a stored row actually holds.

// manualClockSkew is how far into the future a measuredAt may sit before it is refused.
// A client clock a little ahead of the server's is ordinary; a date next week is a typo.
const manualClockSkew = 5 * time.Minute

// maxManualList caps the manual-entry listing, and is also its default.
const maxManualList = 200

// measurementResponse is a Manual entry as the API returns it. Unit echoes the Metric's
// canonical unit so a client can label the value without consulting the Catalog, and ID
// is what a later DELETE addresses — the Ledger's aggregated rows (ADR 0021) carry none.
type measurementResponse struct {
	ID         int64   `json:"id"`
	Metric     string  `json:"metric"`
	Value      float64 `json:"value"`
	Unit       string  `json:"unit"`
	MeasuredAt string  `json:"measured_at"`
	Source     string  `json:"source"`
}

func newMeasurementResponse(row data.Measurement) measurementResponse {
	return measurementResponse{
		ID:         row.ID,
		Metric:     row.Metric,
		Value:      row.Value,
		Unit:       row.OriginalUnit,
		MeasuredAt: row.StartAt,
		Source:     row.Source,
	}
}

// handleCreateMeasurement records one Manual entry. It is idempotent by content key
// (ADR 0006), exactly as a re-import is: submitting the same value at the same instant
// twice yields one row, 201 the first time and 200 the second. That distinction is the
// only signal a client gets, and it is more useful than a 409 — nothing went wrong.
func (s *Server) handleCreateMeasurement(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	var input struct {
		Metric     string   `json:"metric"`
		Value      *float64 `json:"value"`
		MeasuredAt *string  `json:"measured_at"`
	}
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	v := NewValidator()
	metric, unit := s.validateManualMetric(v, input.Metric)
	value := validateManualValue(v, input.Value)
	measuredAt := validateManualTime(v, input.MeasuredAt, time.Now())
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	// The raw value string feeds the content key so an identical resubmission hashes
	// identically, and it is formatted the same way for every caller — the key must not
	// depend on how a client happened to spell the number.
	raw := strconv.FormatFloat(value, 'f', -1, 64)
	at := measuredAt.UTC().Format(time.RFC3339)
	row := data.Measurement{
		AccountID:    accountID,
		Metric:       metric.Slug,
		Value:        value,
		OriginalUnit: unit,
		StartAt:      at,
		EndAt:        at,
		Source:       catalog.SourceManual,
		ContentKey:   data.ContentKey(metric.Slug, catalog.SourceManual, at, at, raw, unit),
	}

	created, err := s.models.Measurements.InsertOne(r.Context(), &row)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	status := http.StatusOK // the value was already stored: a no-op, not a failure
	if created {
		status = http.StatusCreated
	}
	if err := writeJSON(w, status, envelope{"measurement": newMeasurementResponse(row)}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// handleListMeasurements lists the Account's Manual entries, newest first. Only
// `source=manual` is accepted: the imported rows are served aggregated (ADR 0012,
// ADR 0021), and silently listing everything for an unrecognized filter would hand a
// client a different resource than it asked for.
func (s *Server) handleListMeasurements(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	qs := r.URL.Query()
	v := NewValidator()

	v.Check(qs.Get("source") == "manual", "source", "must be \"manual\" — imported Measurements are served aggregated, via /v1/series")

	metric := qs.Get("metric")
	if metric != "" {
		if _, ok := catalog.Lookup(metric); !ok {
			v.AddError("metric", unknownMetricMsg)
		}
	}

	limit := maxManualList
	if raw := qs.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		v.Check(err == nil && n > 0 && n <= maxManualList, "limit",
			"must be a positive integer no greater than "+strconv.Itoa(maxManualList))
		if err == nil && n > 0 && n <= maxManualList {
			limit = n
		}
	}

	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	rows, err := s.models.Measurements.ListManual(r.Context(), accountID, metric, limit)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	out := make([]measurementResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, newMeasurementResponse(row))
	}
	if err := writeJSON(w, http.StatusOK, envelope{"measurements": out}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// handleDeleteMeasurement removes one Manual entry. It reads the row first so the three
// outcomes stay distinguishable: absent or another Account's is 404 (never leaking that
// the id exists), the Account's own but imported is 403, and only a Manual row is
// deleted. The model refuses imported rows in SQL regardless (ADR 0022) — this handler
// exists to say *why*, which a zero-rows-affected delete cannot.
func (s *Server) handleDeleteMeasurement(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}

	row, err := s.models.Measurements.GetByID(r.Context(), accountID, id)
	if err != nil {
		s.respondRecordError(w, r, err, "measurement")
		return
	}
	if row.Source != catalog.SourceManual {
		s.forbiddenResponse(w, r,
			"only manually entered Measurements can be deleted — imported data is removed by re-importing its Source")
		return
	}

	if err := s.models.Measurements.Delete(r.Context(), accountID, id); err != nil {
		s.respondRecordError(w, r, err, "measurement")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateManualMetric resolves the slug and refuses the Metrics a Manual entry cannot
// be: an unknown one, a derived one (computed from a Formula, never entered — ADR 0014),
// and one whose family is not Measurement. It returns the Metric and its canonical unit.
func (s *Server) validateManualMetric(v *Validator, slug string) (catalog.Metric, string) {
	if slug == "" {
		v.AddError("metric", "must be provided")
		return catalog.Metric{}, ""
	}
	metric, ok := catalog.Lookup(slug)
	if !ok {
		v.AddError("metric", unknownMetricMsg)
		return catalog.Metric{}, ""
	}
	if metric.Nature == catalog.Derived {
		v.AddError("metric", "is a derived Metric, computed from other Metrics — enter its operands instead")
		return metric, metric.Unit
	}
	if metric.Aggregation == catalog.DurationByState {
		v.AddError("metric", "is not a Measurement — states and sessions cannot be entered by hand yet")
		return metric, metric.Unit
	}
	return metric, metric.Unit
}

// validateManualValue requires a present, finite value. Absence is distinct from zero,
// which is why the input field is a pointer: a zero step count is a legitimate entry.
func validateManualValue(v *Validator, value *float64) float64 {
	if value == nil {
		v.AddError("value", "must be provided")
		return 0
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) {
		v.AddError("value", "must be a finite number")
		return 0
	}
	return *value
}

// validateManualTime defaults an absent measuredAt to now and refuses a future one
// beyond a small clock skew — a health measurement cannot be taken next week.
func validateManualTime(v *Validator, measuredAt *string, now time.Time) time.Time {
	if measuredAt == nil || *measuredAt == "" {
		return now
	}
	at, err := time.Parse(time.RFC3339, *measuredAt)
	if err != nil {
		v.AddError("measured_at", "must be an RFC 3339 timestamp")
		return now
	}
	if at.After(now.Add(manualClockSkew)) {
		v.AddError("measured_at", "cannot be in the future")
	}
	return at
}
