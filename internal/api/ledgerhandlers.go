package api

import (
	"net/http"
	"time"
)

// handleLedger answers the Ledger overview (ADR 0021): one row per Metric the
// Account has data for, each folded server-side over fixed recent windows by the
// Metric's own rule, so the figures match the graphs. The per-Metric detail table
// uses GET /v1/series, not this endpoint.
func (s *Server) handleLedger(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	rows, err := s.engine.Ledger(r.Context(), accountID, time.Now().UTC())
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	s.respond(w, r, http.StatusOK, envelope{"rows": rows})
}
