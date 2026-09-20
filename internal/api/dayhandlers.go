package api

import (
	"errors"
	"net/http"

	"github.com/gauthier-se/verve/internal/day"
	"github.com/gauthier-se/verve/internal/query"
)

// One date read as an index (ADR 0043). The Day names what has identity on a date
// and links to it: the Night that woke into it, the workouts that started on it,
// the notes and the Manual rows written on it, and one figure per Metric.
//
// It is one call rather than six because the composition rules are read-path
// decisions with one right answer each — which Night belongs to this date, which
// side of midnight a workout falls on, whether an absent row is a gap or a refusal.
// Assembled client-side, those answers live in a component and drift from the
// engine that made them. internal/day owns them; this decodes the date, asks for
// the page, and writes it.
//
// Nothing in this payload is finer than the day. The shape of a night and the curve
// of a ride are on the entities that own them, and the Day carries a link rather
// than an axis of its own (ADR 0041).

// dayView is the whole Day in one payload. Everything but the figures is rendered
// through the view the thing already has, so a workout here is byte-identical to
// the same workout in the list and no shape is defined twice.
type dayView struct {
	Date    string       `json:"date"`
	Metrics []day.Metric `json:"metrics"`
	// Night carries the figures and not the intervals: the hypnogram is a link to
	// /v1/nights/{date}, which is the entity that owns the axis.
	Night       *query.NightSummary   `json:"night,omitempty"`
	Sessions    []sessionView         `json:"sessions"`
	Annotations []annotationView      `json:"annotations"`
	Manual      []measurementResponse `json:"manual_entries"`
	Exclusions  []exclusionView       `json:"exclusions"`
	Phase       *phaseView            `json:"phase,omitempty"`
}

// handleDay answers one Day, keyed by its date.
func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	date := r.PathValue("date")
	read, err := s.day.Read(r.Context(), accountID, date)
	if err != nil {
		if errors.Is(err, day.ErrInvalidDate) {
			s.failedValidationResponse(w, r, map[string]string{"date": "must be a YYYY-MM-DD date"})
			return
		}
		s.serverErrorResponse(w, r, err)
		return
	}

	s.respond(w, r, http.StatusOK, envelope{"day": newDayView(read)})
}

// newDayView maps the read onto the wire. A date with nothing on it yields empty
// collections and a 200: a date exists whether or not anything happened on it,
// which is why this is never a 404 the way an unrecorded Night is.
func newDayView(read day.Read) dayView {
	out := dayView{
		Date:        read.Date,
		Metrics:     read.Metrics,
		Night:       read.Night,
		Sessions:    make([]sessionView, 0, len(read.Sessions)),
		Annotations: make([]annotationView, 0, len(read.Annotations)),
		Manual:      make([]measurementResponse, 0, len(read.Manual)),
		Exclusions:  make([]exclusionView, 0, len(read.Exclusions)),
	}
	for _, ref := range read.Sessions {
		out.Sessions = append(out.Sessions, newSessionView(ref.Session, ref.HasRoute))
	}
	for _, a := range read.Annotations {
		out.Annotations = append(out.Annotations, annotationToView(a))
	}
	for _, m := range read.Manual {
		out.Manual = append(out.Manual, newMeasurementResponse(m))
	}
	for _, x := range read.Exclusions {
		out.Exclusions = append(out.Exclusions, exclusionToView(x))
	}
	if read.Phase != nil {
		view := newPhaseView(*read.Phase)
		out.Phase = &view
	}
	return out
}
