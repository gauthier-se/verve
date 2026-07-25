package api

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/estimate"
)

// The Plan endpoints (ADR 0023). `GET /v1/plan` answers everything the page renders in
// one call, including the targets a given rate implies — the client renders figures, it
// never re-derives them. That is the same rule as the Panel summary (ADR 0019), and for
// the same reason: two implementations of one calculation eventually disagree, and the
// one on the client is the one nobody tests.

// maxRatePctPerWeek bounds a Target rate. Well past anything advisable — the guardrails,
// not the validator, are what say "this is a bad idea". The validator only refuses input
// that is not a plan at all.
const maxRatePctPerWeek = 3.0

type basalView struct {
	Equation string           `json:"equation"`
	Name     string           `json:"name"`
	Kcal     *float64         `json:"kcal,omitempty"`
	Missing  []estimate.Input `json:"missing,omitempty"`
}

type phaseView struct {
	ID             int64   `json:"id"`
	RatePctPerWeek float64 `json:"rate_pct_per_week"`
	StartedAt      string  `json:"started_at"`
	EndedAt        *string `json:"ended_at,omitempty"`
}

func newPhaseView(p data.Phase) phaseView {
	return phaseView{ID: p.ID, RatePctPerWeek: p.RatePctPerWeek, StartedAt: p.StartedAt, EndedAt: p.EndedAt}
}

// planView is the whole Plan page in one payload.
type planView struct {
	Basal []basalView `json:"basal"`
	// PreselectedEquation is the equation the UI should open on — the best one the
	// Account's *trusted* data supports. Server-side because it depends on the
	// body-composition trust setting, which the client should not have to interpret.
	PreselectedEquation string                `json:"preselected_equation,omitempty"`
	Expenditure         *estimate.Expenditure `json:"expenditure,omitempty"`
	ActualRate          *estimate.Rate        `json:"actual_rate,omitempty"`
	Phase               *phaseView            `json:"phase,omitempty"`
	Rate                float64               `json:"rate_pct_per_week"`
	Targets             *estimate.Targets     `json:"targets,omitempty"`
	Adherence           *estimate.Adherence   `json:"adherence,omitempty"`
	Guardrails          []estimate.Guardrail  `json:"guardrails"`
	// Insufficient reports that no basis could produce an Expenditure estimate, so the
	// page shows what to do about it rather than a zero.
	Insufficient bool `json:"insufficient,omitempty"`
}

// handlePlan answers the Plan page. The `rate` parameter previews a Target rate without
// opening a Phase, which is what the slider does while it is being dragged; absent, the
// rate is the open Phase's, or the Account's measured actual rate, or zero.
func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	ctx := r.Context()
	now := time.Now().UTC()

	account, err := s.models.Accounts.GetByID(ctx, accountID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	v := NewValidator()
	var rateOverride *float64
	if raw := r.URL.Query().Get("rate"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		v.Check(err == nil && !math.IsNaN(parsed) && math.Abs(parsed) <= maxRatePctPerWeek,
			"rate", "must be a number within ±"+strconv.FormatFloat(maxRatePctPerWeek, 'g', -1, 64)+" percent of body mass per week")
		if err == nil {
			rateOverride = &parsed
		}
	}
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	profile := profileFromAccount(account)
	inputs, err := s.estimates.ResolveInputs(ctx, accountID, profile, now)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	basals := estimate.Basal(inputs)
	out := planView{Basal: make([]basalView, 0, len(basals)), Guardrails: []estimate.Guardrail{}}
	for _, b := range basals {
		out.Basal = append(out.Basal, basalView{Equation: b.Equation, Name: b.Name, Kcal: b.Kcal, Missing: b.Missing})
	}

	preselected := estimate.Preselect(basals, trustFromAccount(account))
	out.PreselectedEquation = preselected
	basalKcal := basalKcalFor(basals, preselected)

	exp, err := s.estimates.Expenditure(ctx, accountID, inputs, basalKcal, now)
	if err != nil {
		if !errors.Is(err, estimate.ErrInsufficientData) {
			s.serverErrorResponse(w, r, err)
			return
		}
		out.Insufficient = true
		if err := writeJSON(w, http.StatusOK, envelope{"plan": out}, nil); err != nil {
			s.serverErrorResponse(w, r, err)
		}
		return
	}
	out.Expenditure = &exp

	actual, err := s.estimates.ActualRate(ctx, accountID, now)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	out.ActualRate = actual

	phase, err := s.models.Phases.Current(ctx, accountID)
	if err != nil && !errors.Is(err, data.ErrRecordNotFound) {
		s.serverErrorResponse(w, r, err)
		return
	}
	if phase != nil {
		view := newPhaseView(*phase)
		out.Phase = &view
	}

	// The rate in force, in precedence order: an explicit preview, else the open Phase's
	// commitment, else what the Account is measurably already doing — so the page opens
	// by stating the current reality rather than presenting an empty form.
	out.Rate = resolveRate(rateOverride, phase, actual)

	targets, ok := estimate.DeriveTargets(exp.Kcal, out.Rate, inputs)
	if ok {
		out.Targets = &targets
	}

	var adherence *estimate.Adherence
	if phase != nil && ok {
		startedAt, err := time.Parse(time.RFC3339, phase.StartedAt)
		if err == nil {
			a, err := s.estimates.Adherence(ctx, accountID, startedAt, targets, phase.RatePctPerWeek, now)
			if err != nil {
				s.serverErrorResponse(w, r, err)
				return
			}
			adherence = &a
			out.Adherence = adherence
		}
	}

	if ok {
		out.Guardrails = estimate.Guardrails(targets, out.Rate, basalKcal, adherence)
	}

	if err := writeJSON(w, http.StatusOK, envelope{"plan": out}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// resolveRate picks the rate the page renders: an explicit preview, the open Phase's
// commitment, or the measured actual rate.
func resolveRate(override *float64, phase *data.Phase, actual *estimate.Rate) float64 {
	switch {
	case override != nil:
		return *override
	case phase != nil:
		return phase.RatePctPerWeek
	case actual != nil:
		return actual.PctPerWeek
	default:
		return 0
	}
}

func basalKcalFor(basals []estimate.BasalEstimate, equation string) *float64 {
	for _, b := range basals {
		if b.Equation == equation {
			return b.Kcal
		}
	}
	return nil
}

// handleListPhases returns the Account's Phase history, newest first.
func (s *Server) handleListPhases(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	phases, err := s.models.Phases.ListByAccount(r.Context(), accountID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	out := make([]phaseView, 0, len(phases))
	for _, p := range phases {
		out = append(out, newPhaseView(p))
	}
	if err := writeJSON(w, http.StatusOK, envelope{"phases": out}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// handleOpenPhase starts a Phase, closing whatever was open. It never edits the previous
// one's rate: a Phase is a record of what was intended over a stretch, and rewriting it
// would make every past adherence figure meaningless.
func (s *Server) handleOpenPhase(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	var input struct {
		RatePctPerWeek *float64 `json:"rate_pct_per_week"`
	}
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	v := NewValidator()
	if input.RatePctPerWeek == nil {
		v.AddError("rate_pct_per_week", "must be provided")
	} else {
		v.Check(!math.IsNaN(*input.RatePctPerWeek) && !math.IsInf(*input.RatePctPerWeek, 0) &&
			math.Abs(*input.RatePctPerWeek) <= maxRatePctPerWeek,
			"rate_pct_per_week", "must be a number within ±3 percent of body mass per week")
	}
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	phase, err := s.models.Phases.Open(r.Context(), accountID, *input.RatePctPerWeek,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	if err := writeJSON(w, http.StatusCreated, envelope{"phase": newPhaseView(*phase)}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// handleClosePhase ends an open Phase without starting another — stepping off a plan
// rather than switching to a new one.
func (s *Server) handleClosePhase(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	if err := s.models.Phases.Close(r.Context(), accountID, id, time.Now().UTC().Format(time.RFC3339)); err != nil {
		s.respondRecordError(w, r, err, "open phase")
		return
	}
	phase, err := s.models.Phases.GetByID(r.Context(), accountID, id)
	if err != nil {
		s.respondRecordError(w, r, err, "phase")
		return
	}
	if err := writeJSON(w, http.StatusOK, envelope{"phase": newPhaseView(*phase)}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// handleDeletePhase removes a Phase outright — for a mis-typed rate, where closing it
// would leave a meaningless stretch in the history instead of correcting it.
func (s *Server) handleDeletePhase(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	if err := s.models.Phases.Delete(r.Context(), accountID, id); err != nil {
		s.respondRecordError(w, r, err, "phase")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
