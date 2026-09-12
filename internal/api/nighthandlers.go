package api

import (
	"net/http"
	"time"

	"github.com/gauthier-se/verve/internal/query"
)

// A Night addressed as an entity (ADR 0041). It is the sleep Metric's own rows,
// resolved by the sleep Metric's own rule, described rather than folded: the
// intervals as they were recorded, and the figures that exist only because there
// is an axis to compute them on.
//
// It is deliberately not `/v1/series?metric=sleep&day=...`. Once a Series can be
// asked for one day at a finer grain, every caller holding a Series can, and
// ADR 0012's cap is enforced by nothing but discipline. Addressing the entity is
// what makes the boundary structural: this URL cannot be pointed at a range, a
// Dashboard or a Panel, because it takes no range at all.

// handleNight answers one Night, keyed by the morning it woke on.
func (s *Server) handleNight(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	label := r.PathValue("date")
	if _, err := time.Parse(dayLayout, label); err != nil {
		s.failedValidationResponse(w, r, map[string]string{"date": "must be a YYYY-MM-DD night, the morning it woke on"})
		return
	}

	night, ok, err := s.engine.Night(r.Context(), accountID, label)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	if !ok {
		// Nothing was recorded for that night, which is a fact about the data and
		// not an error. It is a 404 rather than an empty body for the same reason
		// another Account's workout is: the resource does not exist.
		s.notFoundResponse(w, r, "no night was recorded on that morning")
		return
	}

	s.respond(w, r, http.StatusOK, envelope{"night": nightView(night)})
}

// nightView is the payload shape. It is query.NightDetail as it stands, named
// here so the contract test has a type to pin against types.ts and so the read
// module can change its internals without changing the wire.
type nightView = query.NightDetail
