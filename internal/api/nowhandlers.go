package api

import (
	"net/http"
	"time"

	"github.com/gauthier-se/verve/internal/now"
)

// Now answers where the Account stands (ADR 0045): how old its data is, measured
// on the last datum and never on the last Import. It is one call because the page
// is one reading, and internal/now owns the rules that make it one: which day the
// data stops on, which Source is late and which is retired.

// nowView is the Now screen in one payload.
type nowView struct {
	Freshness now.Freshness `json:"freshness"`
}

// handleNow answers the Now screen. The server's clock fixes today, the same day
// Goals and resolved windows are dated by, so the browser's clock never moves an age.
func (s *Server) handleNow(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	freshness, err := s.now.Freshness(r.Context(), accountID, time.Now())
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	s.respond(w, r, http.StatusOK, envelope{"now": nowView{Freshness: freshness}})
}
