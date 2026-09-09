package api

import (
	"net/http"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/data"
)

// Exclusion endpoints (ADR 0033). An Exclusion is the one object serving both
// "delete everything this Metric holds" and "remember what not to import": it
// purges when it is created and it refuses at every import after, which is what
// makes the deletion stick where a plain row delete could not (ADR 0022).
//
// There is no PATCH. Widening a span has to purge again, which is what creating one
// already does, so editing is delete then create and exactly one code path removes
// data.

// exclusionView is one Exclusion as the API exposes it. Empty bounds are unbounded
// on that side, sent as "" rather than omitted so a client never has to tell an
// absent key from an open end.
type exclusionView struct {
	ID        int64  `json:"id"`
	Metric    string `json:"metric"`
	StartsOn  string `json:"starts_on"`
	EndsOn    string `json:"ends_on"`
	Purged    int64  `json:"purged"`
	CreatedAt string `json:"created_at"`
}

func exclusionToView(e data.Exclusion) exclusionView {
	return exclusionView{
		ID: e.ID, Metric: e.Metric, StartsOn: e.StartsOn, EndsOn: e.EndsOn,
		Purged: e.Purged, CreatedAt: e.CreatedAt,
	}
}

// handleListExclusions returns the Account's whole set: what the next import will
// refuse, which is also the whole of "propose the same import when I come back".
func (s *Server) handleListExclusions(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	exclusions, err := s.models.Exclusions.ListByAccount(r.Context(), accountID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	views := make([]exclusionView, 0, len(exclusions))
	for _, e := range exclusions {
		views = append(views, exclusionToView(e))
	}
	s.respond(w, r, http.StatusOK, envelope{"exclusions": views})
}

// exclusionInput is the create body. Absent bounds mean unbounded on that side, so
// {"metric":"body_fat_percentage"} is "everything this Metric holds", the common
// case, and the one that must not require typing two dates to express.
type exclusionInput struct {
	Metric   string  `json:"metric"`
	StartsOn *string `json:"starts_on"`
	EndsOn   *string `json:"ends_on"`
}

// handleCreateExclusion writes an Exclusion and purges what it covers, in one
// transaction (see data.ExclusionModel.Insert). It answers 201 with the purge count
// the first time and 200 with the standing row after, for the same reason a re-typed
// Measurement answers 200: the state asked for holds, and nothing went wrong.
func (s *Server) handleCreateExclusion(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	var input exclusionInput
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	v := NewValidator()
	metric := validateExclusionMetric(v, input.Metric)
	startsOn, endsOn := validateExclusionSpan(v, input.StartsOn, input.EndsOn)
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	e := data.Exclusion{AccountID: accountID, Metric: metric, StartsOn: startsOn, EndsOn: endsOn}
	created, err := s.models.Exclusions.Insert(r.Context(), &e)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	status := http.StatusOK // it already stood, and its purge already ran
	if created {
		status = http.StatusCreated
	}
	s.respond(w, r, status, envelope{"exclusion": exclusionToView(e)})
}

// handleDeleteExclusion removes one of the Account's Exclusions, and only the rule:
// the purged Measurements are not restored here, and the imported ones come back
// with the next import, which this call has just stopped refusing.
func (s *Server) handleDeleteExclusion(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	if err := s.models.Exclusions.Delete(r.Context(), accountID, id); err != nil {
		s.respondRecordError(w, r, err, "exclusion")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePreviewExclusion counts what an Exclusion over this Metric and span would
// purge, without writing anything. It exists for the confirmation: the difference
// between "refuse this from now on" and "delete 431 readings" has to be legible in
// the half-second before the click, and only a number makes it so.
//
// It validates exactly what the create path validates, so a preview that answers
// cannot be followed by a create that refuses.
func (s *Server) handlePreviewExclusion(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	qs := r.URL.Query()

	v := NewValidator()
	metric := validateExclusionMetric(v, qs.Get("metric"))
	startsOn, endsOn := validateExclusionSpan(v, optionalParam(qs, "starts_on"), optionalParam(qs, "ends_on"))
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	n, err := s.models.Measurements.CountInSpan(r.Context(), accountID, metric, startsOn, endsOn)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	body := envelope{"metric": metric, "starts_on": startsOn, "ends_on": endsOn, "measurements": n}
	s.respond(w, r, http.StatusOK, body)
}

// validateExclusionMetric refuses the slugs an Exclusion cannot name and returns the
// one it can. The three refusals are the same shape validateManualMetric applies to
// a Manual entry, and the messages are deliberately not shared: each says what to do
// instead, and what to do instead is different.
//
// The `sleep` refusal is the one that earns its 422. Every other unsupported slug is
// either absent from the Catalog or visibly derived; `sleep` is a real, familiar,
// imported-looking Metric whose rows live in `states`, so accepting it would write a
// rule that reports success and does nothing, forever.
func validateExclusionMetric(v *Validator, slug string) string {
	if slug == "" {
		v.AddError("metric", "must be provided")
		return ""
	}
	metric, ok := catalog.Lookup(slug)
	if !ok {
		v.AddError("metric", unknownMetricMsg)
		return ""
	}
	if metric.Nature == catalog.Derived {
		v.AddError("metric", "is a derived Metric and owns no stored rows: exclude the operands it is computed from")
		return slug
	}
	if metric.Aggregation == catalog.DurationByState {
		v.AddError("metric", "is stored as States rather than Measurements: excluding it is not supported yet")
		return slug
	}
	return slug
}

// validateExclusionSpan normalizes the optional bounds: absent, empty and blank all
// become "", which is how the schema spells unbounded. Bounds are inclusive days,
// the shape and the words an Annotation uses, because both are a period a person
// typed rather than a window the server resolved.
func validateExclusionSpan(v *Validator, from, to *string) (string, string) {
	startsOn, endsOn := valueOrEmpty(from), valueOrEmpty(to)

	start, startOK := parseDayValue(startsOn)
	if startsOn != "" && !startOK {
		v.AddError("starts_on", "must be YYYY-MM-DD")
	}
	if endsOn == "" {
		return startsOn, endsOn
	}
	end, endOK := parseDayValue(endsOn)
	if !endOK {
		v.AddError("ends_on", "must be YYYY-MM-DD")
		return startsOn, endsOn
	}
	if startsOn != "" && startOK && end.Before(start) {
		v.AddError("ends_on", "must not be before starts_on")
	}
	return startsOn, endsOn
}
