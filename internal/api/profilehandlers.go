package api

import (
	"net/http"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/estimate"
	"github.com/gauthier-se/verve/internal/query"
)

// Profile endpoints: the Account attributes that are not Measurements. Date of birth and
// biological sex are columns, filled by Apple's `Me` record on import and left empty when
// it carried none — which is the reference Account's actual state, and the reason two of
// the four Basal equations cannot run for it.
//
// Height, body mass and body fat are deliberately **not** here. They are Metrics, written
// through POST /v1/measurements as Manual entries (ADR 0022). Mirroring them as columns
// would create a second height that silently diverges from the graphed one.

const (
	minAgeYears = 13
	maxAgeYears = 120
)

type profileView struct {
	DateOfBirth   *string `json:"date_of_birth,omitempty"`
	BiologicalSex *string `json:"biological_sex,omitempty"`
	// BodyCompositionTrust is what the Account declared, absent when it has not.
	BodyCompositionTrust *string `json:"body_composition_trust,omitempty"`
	// DerivedTrust is the suggestion to show when nothing is declared — a hint the UI
	// should present as a default, not as a stored choice.
	DerivedTrust estimate.Trust `json:"derived_trust"`
}

// handleGetProfile returns the Account's profile attributes plus the trust suggestion.
func (s *Server) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	account, ok := s.currentAccount(w, r)
	if !ok {
		return
	}
	view, err := s.profileView(r, account)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	if err := writeJSON(w, http.StatusOK, envelope{"profile": view}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// handleUpdateProfile applies a partial update. Fields absent from the body are left
// alone; a field sent as null is cleared.
func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	account, ok := s.currentAccount(w, r)
	if !ok {
		return
	}

	// Double pointers distinguish the three cases a partial update needs: absent
	// (leave alone), null (clear), and a value (set).
	var input struct {
		DateOfBirth          **string `json:"date_of_birth"`
		BiologicalSex        **string `json:"biological_sex"`
		BodyCompositionTrust **string `json:"body_composition_trust"`
	}
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	v := NewValidator()
	if input.DateOfBirth != nil && *input.DateOfBirth != nil {
		validateDateOfBirth(v, **input.DateOfBirth, time.Now().UTC())
	}
	if input.BiologicalSex != nil && *input.BiologicalSex != nil {
		sex := estimate.Sex(**input.BiologicalSex)
		v.Check(sex == estimate.SexMale || sex == estimate.SexFemale,
			"biological_sex", `must be "male" or "female" — it is an input to two of the basal equations, nothing more`)
	}
	if input.BodyCompositionTrust != nil && *input.BodyCompositionTrust != nil {
		t := estimate.Trust(**input.BodyCompositionTrust)
		v.Check(t == estimate.TrustMeasured || t == estimate.TrustEstimated || t == estimate.TrustUnknown,
			"body_composition_trust", `must be "measured", "estimated" or "unknown"`)
	}
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	patch := data.ProfilePatch{
		DateOfBirth:          input.DateOfBirth,
		BiologicalSex:        input.BiologicalSex,
		BodyCompositionTrust: input.BodyCompositionTrust,
	}
	if err := s.models.Accounts.UpdateProfile(r.Context(), account.ID, patch); err != nil {
		s.respondRecordError(w, r, err, "account")
		return
	}

	updated, err := s.models.Accounts.GetByID(r.Context(), account.ID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	view, err := s.profileView(r, updated)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	if err := writeJSON(w, http.StatusOK, envelope{"profile": view}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// profileView renders an Account's profile, resolving the trust suggestion from where its
// lean-mass data actually comes from.
func (s *Server) profileView(r *http.Request, account *data.Account) (profileView, error) {
	source, err := s.leanMassSource(r, account.ID)
	if err != nil {
		return profileView{}, err
	}
	return profileView{
		DateOfBirth:          account.DateOfBirth,
		BiologicalSex:        account.BiologicalSex,
		BodyCompositionTrust: account.BodyCompositionTrust,
		DerivedTrust:         estimate.DerivedTrust(source),
	}, nil
}

// compositionMetrics are the Metrics whose trustworthiness the setting is about.
var compositionMetrics = []string{"lean_body_mass", "body_fat_percentage"}

// leanMassSource reports where the Account's body-composition data comes from, so the
// trust suggestion can tell a hand-entered figure from a scale's.
//
// It asks the measurement store directly rather than reading Series.Source, which cannot
// answer this: the Manual overlay deliberately keeps reporting the *imported* Source, so
// that one corrected day does not rename a whole curve (ADR 0022). A Series over an
// Account with both a scale and a manual correction therefore says "Zepp Life" — true for
// the curve, and exactly the wrong answer for this question.
func (s *Server) leanMassSource(r *http.Request, accountID int64) (string, error) {
	for _, metric := range compositionMetrics {
		manual, err := s.models.Measurements.ListManual(r.Context(), accountID, metric, 1)
		if err != nil {
			return "", err
		}
		if len(manual) > 0 {
			return catalog.SourceManual, nil
		}
	}
	now := time.Now().UTC()
	for _, metric := range compositionMetrics {
		series, err := s.engine.Series(r.Context(), seriesOverYears(accountID, metric, now))
		if err != nil {
			return "", err
		}
		if series.Source != "" {
			return series.Source, nil
		}
	}
	return "", nil
}

// validateDateOfBirth requires an ISO date in the past yielding a plausible adult age.
func validateDateOfBirth(v *Validator, raw string, now time.Time) {
	dob, err := time.Parse("2006-01-02", raw)
	if err != nil {
		v.AddError("date_of_birth", "must be an ISO date (YYYY-MM-DD)")
		return
	}
	years := now.Sub(dob).Hours() / 24 / 365.25
	v.Check(years >= minAgeYears && years <= maxAgeYears,
		"date_of_birth", "must give an age between 13 and 120")
}

// currentAccount resolves the authenticated Account, answering 500 on a lookup failure.
func (s *Server) currentAccount(w http.ResponseWriter, r *http.Request) (*data.Account, bool) {
	accountID, _ := s.accountID(r)
	account, err := s.models.Accounts.GetByID(r.Context(), accountID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return nil, false
	}
	return account, true
}

// profileFromAccount maps the stored columns onto the estimate package's Profile.
func profileFromAccount(a *data.Account) estimate.Profile {
	p := estimate.Profile{}
	if a.DateOfBirth != nil {
		if dob, err := time.Parse("2006-01-02", *a.DateOfBirth); err == nil {
			p.DateOfBirth = &dob
		}
	}
	if a.BiologicalSex != nil {
		p.Sex = estimate.Sex(*a.BiologicalSex)
	}
	return p
}

// trustFromAccount reads the declared trust, falling back to unknown. The *derived*
// suggestion is only ever a UI default; preselection uses what was actually declared, so
// an Account that never answered does not get lean-mass equations preferred on its behalf.
func trustFromAccount(a *data.Account) estimate.Trust {
	if a.BodyCompositionTrust == nil {
		return estimate.TrustUnknown
	}
	return estimate.Trust(*a.BodyCompositionTrust)
}

// seriesOverYears is a multi-year, month-bucketed request — the shape needed to find a
// Metric measured once and then never again. A day bucket cannot span this: the engine
// caps a request at 1000 points.
func seriesOverYears(accountID int64, metric string, now time.Time) query.Request {
	return query.Request{
		AccountID: accountID, Metric: metric,
		From: now.AddDate(-30, 0, 0), To: now, Bucket: query.Month,
	}
}
