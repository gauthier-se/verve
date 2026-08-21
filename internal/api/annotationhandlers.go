package api

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/timeaxis"
)

// An Annotation's two text fields are bounded so a single row cannot grow without
// limit: a label is a chart tooltip's worth of words, a body a paragraph's.
const (
	maxLabelLen = 120
	maxBodyLen  = 2000
)

const dayLayout = "2006-01-02"

// annotationView is one Annotation as the API exposes it. StartsOn/EndsOn are the
// real dates (what the tooltip and the list show), while Bucket/EndBucket are
// those dates folded onto the resolved bucket grid, which is where a marker is
// drawn. Both are needed and neither is derivable from the other client-side: the
// dates cannot be snapped without duplicating the boundary rules, and the buckets
// no longer name the day the note is about.
type annotationView struct {
	ID       int64   `json:"id"`
	Label    string  `json:"label"`
	Body     *string `json:"body"`
	StartsOn string  `json:"starts_on"`
	EndsOn   *string `json:"ends_on"`
	// Bucket is the grid position of the first day on screen, absent when the
	// request carried no time axis (the Data page's full list).
	Bucket string `json:"bucket,omitempty"`
	// EndBucket is set only when the span covers more than one bucket, so its
	// presence is exactly the client's "band, not marker" test. A fortnight at the
	// month bucket is one bucket wide and has no band to draw.
	EndBucket string `json:"end_bucket,omitempty"`
}

func annotationToView(a data.Annotation) annotationView {
	return annotationView{
		ID: a.ID, Label: a.Label, Body: a.Body, StartsOn: a.StartsOn, EndsOn: a.EndsOn,
	}
}

// handleListAnnotations answers either of two questions, told apart by whether the
// request carries a time axis. With range_preset (and the usual bounds and bucket
// override) it returns the Annotations overlapping that window, each folded onto
// the bucket grid the Panels are drawn on. It takes the tokens /v1/series takes, so
// the markers and the curves are resolved by one module against one window.
// Without it, it returns the Account's whole history unfolded: no axis, no buckets.
func (s *Server) handleListAnnotations(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	qs := r.URL.Query()

	if qs.Get("range_preset") == "" {
		annotations, err := s.models.Annotations.ListAll(r.Context(), accountID)
		if err != nil {
			s.serverErrorResponse(w, r, err)
			return
		}
		views := make([]annotationView, 0, len(annotations))
		for _, a := range annotations {
			views = append(views, annotationToView(a))
		}
		s.writeAnnotations(w, r, views)
		return
	}

	resolved, err := timeaxis.Resolve(annotationTokens(qs), time.Now())
	if inv, ok := err.(timeaxis.Invalid); ok {
		s.failedValidationResponse(w, r, inv)
		return
	} else if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	from := resolved.Current.From.Format(dayLayout)
	to := resolved.Current.To.Format(dayLayout)
	annotations, err := s.models.Annotations.ListByWindow(r.Context(), accountID, from, to)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	views := make([]annotationView, 0, len(annotations))
	for _, a := range annotations {
		view := annotationToView(a)
		start, ok := parseDayValue(a.StartsOn)
		if !ok {
			continue
		}
		end := start
		if a.EndsOn != nil {
			if e, ok := parseDayValue(*a.EndsOn); ok {
				end = e
			}
		}
		first, last, ok := resolved.Fold(start, end)
		if !ok {
			continue
		}
		view.Bucket = first
		if last != first {
			view.EndBucket = last
		}
		views = append(views, view)
	}
	s.writeAnnotations(w, r, views)
}

// annotationTokens reads the time-axis tokens a list request carries. The Baseline
// is deliberately not among them: a Baseline series is drawn on the current
// window's ordinal axis, so a marker there would sit at a bucket whose date is not
// the date under it (ADR 0030).
func annotationTokens(qs url.Values) timeaxis.Tokens {
	return timeaxis.Tokens{
		RangePreset: qs.Get("range_preset"),
		RangeFrom:   optionalParam(qs, "range_from"),
		RangeTo:     optionalParam(qs, "range_to"),
		Bucket:      optionalParam(qs, "bucket"),
	}
}

func (s *Server) writeAnnotations(w http.ResponseWriter, r *http.Request, views []annotationView) {
	if err := writeJSON(w, http.StatusOK, envelope{"annotations": views}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// annotationInput is the create/update body. An absent field is left unchanged by a
// PATCH and an empty string clears it, which is how a span becomes a single day
// again and how a body is emptied. JSON null is not the clearing signal: it decodes
// to the same nil pointer as an absent field, here as everywhere else in this API.
type annotationInput struct {
	Label    *string `json:"label"`
	Body     *string `json:"body"`
	StartsOn *string `json:"starts_on"`
	EndsOn   *string `json:"ends_on"`
}

// handleCreateAnnotation writes a new Annotation for the Account.
func (s *Server) handleCreateAnnotation(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	var input annotationInput
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	a := data.Annotation{AccountID: accountID}
	if input.Label != nil {
		a.Label = strings.TrimSpace(*input.Label)
	}
	if input.StartsOn != nil {
		a.StartsOn = *input.StartsOn
	}
	a.Body = trimmedBody(input.Body)
	a.EndsOn = emptyToNil(input.EndsOn)

	v := NewValidator()
	validateAnnotation(v, a)
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := s.models.Annotations.Insert(r.Context(), &a); err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	if err := writeJSON(w, http.StatusCreated, envelope{"annotation": annotationToView(a)}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// handleUpdateAnnotation patches an Annotation's label, body or span. Absent fields
// are left unchanged; an empty string clears the body or the end day.
func (s *Server) handleUpdateAnnotation(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	a, err := s.models.Annotations.GetByID(r.Context(), accountID, id)
	if err != nil {
		s.respondRecordError(w, r, err, "annotation")
		return
	}

	var input annotationInput
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}
	if input.Label != nil {
		a.Label = strings.TrimSpace(*input.Label)
	}
	if input.Body != nil {
		a.Body = trimmedBody(input.Body)
	}
	if input.StartsOn != nil {
		a.StartsOn = *input.StartsOn
	}
	if input.EndsOn != nil {
		a.EndsOn = emptyToNil(input.EndsOn)
	}

	v := NewValidator()
	validateAnnotation(v, *a)
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := s.models.Annotations.Update(r.Context(), a); err != nil {
		s.respondRecordError(w, r, err, "annotation")
		return
	}
	if err := writeJSON(w, http.StatusOK, envelope{"annotation": annotationToView(*a)}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// handleDeleteAnnotation removes one of the Account's Annotations. Unlike a
// Measurement, which is deletable only when its Source is Manual (ADR 0022), every
// Annotation is deletable: they are all typed by their owner.
func (s *Server) handleDeleteAnnotation(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	if err := s.models.Annotations.Delete(r.Context(), accountID, id); err != nil {
		s.respondRecordError(w, r, err, "annotation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateAnnotation checks the shape the schema's CHECKs also hold: a non-empty
// bounded label, day-granular dates, and a span that ends on or after it starts.
func validateAnnotation(v *Validator, a data.Annotation) {
	switch {
	case a.Label == "":
		v.AddError("label", "must be provided")
	case len(a.Label) > maxLabelLen:
		v.AddError("label", "must not be longer than 120 characters")
	}
	if a.Body != nil && len(*a.Body) > maxBodyLen {
		v.AddError("body", "must not be longer than 2000 characters")
	}

	start, ok := parseDayValue(a.StartsOn)
	if !ok {
		v.AddError("starts_on", "must be YYYY-MM-DD")
	}
	if a.EndsOn == nil {
		return
	}
	end, endOK := parseDayValue(*a.EndsOn)
	if !endOK {
		v.AddError("ends_on", "must be YYYY-MM-DD")
		return
	}
	if ok && end.Before(start) {
		v.AddError("ends_on", "must not be before starts_on")
	}
}

// trimmedBody normalizes an optional prose field: absent, blank and whitespace all
// become nil, so "no body" has one representation in the database.
func trimmedBody(body *string) *string {
	if body == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*body)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// emptyToNil maps an empty string to nil, so a client clearing a span can send ""
// as readily as null.
func emptyToNil(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}

func parseDayValue(s string) (time.Time, bool) {
	t, err := time.Parse(dayLayout, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}
