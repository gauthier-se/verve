package api

import (
	"net/http"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/history"
)

// handleHistory answers the History page: the long view of everything the Account
// holds, and the context that explains it (CONTEXT.md: History).
//
// It is one call rather than five because the page is one reading — the band's
// grain, the Phase spans folded onto it, the gaps and the events all have to agree
// about the same axis. internal/history owns that agreement; this decodes the
// Metric, asks for the page, and writes it.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	slug := r.URL.Query().Get("metric")
	if slug == "" {
		slug = history.DefaultMetric
	}
	if _, ok := catalog.Lookup(slug); !ok {
		s.failedValidationResponse(w, r, map[string]string{"metric": unknownMetricMsg})
		return
	}

	view, err := s.history.Read(r.Context(), accountID, slug)
	if err != nil {
		s.respondSeriesError(w, r, err)
		return
	}

	if err := writeJSON(w, http.StatusOK, envelope{"history": view}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}
