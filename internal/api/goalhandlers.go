package api

import (
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/data"
)

// goalView is one Goal as the API exposes it. EndedOn absent means open.
type goalView struct {
	ID        int64   `json:"id"`
	Metric    string  `json:"metric"`
	Direction string  `json:"direction"`
	Value     float64 `json:"value"`
	StartedOn string  `json:"started_on"`
	EndedOn   *string `json:"ended_on,omitempty"`
}

func newGoalView(g data.Goal) goalView {
	return goalView{
		ID: g.ID, Metric: g.Metric, Direction: g.Direction, Value: g.Value,
		StartedOn: g.StartedOn, EndedOn: g.EndedOn,
	}
}

// today is the day a Goal cannot start or end after, at UTC midnight like every
// window the time axis resolves (ADR 0012).
func today() string {
	return time.Now().UTC().Format(dayLayout)
}

// handleListGoals returns the Account's Goal history newest first, on one Metric
// when ?metric= is given.
func (s *Server) handleListGoals(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	metric := r.URL.Query().Get("metric")
	if metric != "" {
		if _, ok := catalog.Lookup(metric); !ok {
			s.failedValidationResponse(w, r, map[string]string{"metric": unknownMetricMsg})
			return
		}
	}

	goals, err := s.models.Goals.ListByAccount(r.Context(), accountID, metric)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	out := make([]goalView, 0, len(goals))
	for _, g := range goals {
		out = append(out, newGoalView(g))
	}
	s.respond(w, r, http.StatusOK, envelope{"goals": out})
}

// handleOpenGoal sets a Goal on a Metric, closing the one open on it. It never edits
// the previous Goal's value: each day is judged against the Goal in force on it, and
// rewriting one would change counts about days that have not changed (ADR 0044).
func (s *Server) handleOpenGoal(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	var input struct {
		Metric    string   `json:"metric"`
		Direction string   `json:"direction"`
		Value     *float64 `json:"value"`
		StartedOn *string  `json:"started_on"`
	}
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	v := NewValidator()
	validateGoalMetric(v, input.Metric)
	v.Check(input.Direction == data.GoalAtLeast || input.Direction == data.GoalAtMost,
		"direction", `must be "at_least" or "at_most"`)
	// Any finite value: a derived Metric can be meaningfully negative.
	if input.Value == nil {
		v.AddError("value", "must be provided")
	} else {
		v.Check(!math.IsNaN(*input.Value) && !math.IsInf(*input.Value, 0), "value", "must be a number")
	}
	startedOn := today()
	if input.StartedOn != nil {
		startedOn = *input.StartedOn
		if _, ok := parseDayValue(startedOn); !ok {
			v.AddError("started_on", "must be YYYY-MM-DD")
		} else {
			v.Check(startedOn <= today(), "started_on", "must not be in the future")
		}
	}
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	goal, err := s.models.Goals.Open(r.Context(), accountID, input.Metric, input.Direction, *input.Value, startedOn)
	if err != nil {
		if errors.Is(err, data.ErrGoalOverlap) {
			s.failedValidationResponse(w, r, map[string]string{
				"started_on": "must be after the start of the Goal in force and not before the end of the last one: delete the entry to correct the past",
			})
			return
		}
		s.serverErrorResponse(w, r, err)
		return
	}
	s.respond(w, r, http.StatusCreated, envelope{"goal": newGoalView(*goal)})
}

// validateGoalMetric accepts every Catalog Metric but a `latest` one, asking the rule
// rather than naming Metrics so a new Catalog entry needs no decision. "75 kg" is a
// destination reached once, not a bound held daily: that question is a Phase's.
func validateGoalMetric(v *Validator, slug string) {
	if slug == "" {
		v.AddError("metric", "must be provided")
		return
	}
	metric, ok := catalog.Lookup(slug)
	if !ok {
		v.AddError("metric", unknownMetricMsg)
		return
	}
	if metric.Aggregation == catalog.Latest {
		v.AddError("metric", "is a latest-reading Metric: a Goal is a daily bound, and a target body figure is a Phase's question")
	}
}

// handleCloseGoal ends an open Goal without setting another, on ended_on (today by
// default). A Goal that started on that very day has no span to keep and is removed
// by deleting it.
func (s *Server) handleCloseGoal(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}

	var input struct {
		EndedOn *string `json:"ended_on"`
	}
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	goal, err := s.models.Goals.GetByID(r.Context(), accountID, id)
	if err != nil {
		s.respondRecordError(w, r, err, "goal")
		return
	}
	if goal.EndedOn != nil {
		s.notFoundResponse(w, r, "the requested open goal could not be found")
		return
	}

	endedOn := today()
	if input.EndedOn != nil {
		endedOn = *input.EndedOn
	}
	v := NewValidator()
	if _, ok := parseDayValue(endedOn); !ok {
		v.AddError("ended_on", "must be YYYY-MM-DD")
	} else {
		v.Check(endedOn <= today(), "ended_on", "must not be in the future")
		v.Check(endedOn > goal.StartedOn, "ended_on", "must be after the Goal's start: delete a Goal that never held a day")
	}
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := s.models.Goals.Close(r.Context(), accountID, id, endedOn); err != nil {
		s.respondRecordError(w, r, err, "open goal")
		return
	}
	goal.EndedOn = &endedOn
	s.respond(w, r, http.StatusOK, envelope{"goal": newGoalView(*goal)})
}

// handleDeleteGoal removes a Goal outright: for a mistyped value, where closing it
// would leave a meaningless stretch in the history instead of correcting it.
func (s *Server) handleDeleteGoal(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	if err := s.models.Goals.Delete(r.Context(), accountID, id); err != nil {
		s.respondRecordError(w, r, err, "goal")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
