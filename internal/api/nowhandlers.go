package api

import (
	"net/http"
	"time"

	"github.com/gauthier-se/verve/internal/now"
)

// Now answers where the Account stands (ADR 0045): how old its data is, measured
// on the last datum and never on the last Import, and each Pin at its Latest value.
// It is one call because the page is one reading, and internal/now owns the rules
// that make it one: which day the data stops on, which Source is late and which is
// retired, and which week a Goal is counted over.

// nowView is the Now screen in one payload. Goals is always present, empty when
// none is in force; the last Night and workout are absent for an Account that never
// recorded one (ADR 0049).
type nowView struct {
	Freshness   now.Freshness   `json:"freshness"`
	Cards       []now.Card      `json:"cards"`
	LastNight   *now.Night      `json:"last_night,omitempty"`
	LastWorkout *nowWorkoutView `json:"last_workout,omitempty"`
	Goals       []now.GoalRow   `json:"goals"`
}

// nowWorkoutView is the last workout as the workout list shows it, with its age.
type nowWorkoutView struct {
	sessionView
	AgeDays int `json:"age_days"`
}

// handleNow answers the Now screen. The server's clock fixes today, the same day
// Goals and resolved windows are dated by, so the browser's clock never moves an age.
func (s *Server) handleNow(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	read, err := s.now.Read(r.Context(), accountID, time.Now())
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	view := nowView{
		Freshness: read.Freshness,
		Cards:     read.Cards,
		LastNight: read.LastNight,
		Goals:     read.Goals,
	}
	if wk := read.LastWorkout; wk != nil {
		view.LastWorkout = &nowWorkoutView{sessionView: newSessionView(wk.Session, wk.HasRoute), AgeDays: wk.AgeDays}
	}
	s.respond(w, r, http.StatusOK, envelope{"now": view})
}

// handleNowUnusual answers the followed Metrics outside their Usual (ADR 0049). It
// is its own call because it reads a Usual per followed Metric, which is the slow
// half of Now, and the page renders its Pins without waiting for it.
func (s *Server) handleNowUnusual(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	cards, err := s.now.Unusual(r.Context(), accountID, time.Now())
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	s.respond(w, r, http.StatusOK, envelope{"unusual": cards})
}
