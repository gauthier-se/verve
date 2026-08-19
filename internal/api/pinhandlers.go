package api

import (
	"net/http"

	"github.com/gauthier-se/verve/internal/catalog"
)

// pinView is one Pin as the API exposes it. It is deliberately thin: a Pin is a
// Catalog slug and its place in the sidebar, nothing else (CONTEXT.md). Anything
// richer here would be the first step toward a one-Panel Dashboard.
type pinView struct {
	Metric   string `json:"metric"`
	Position int    `json:"position"`
}

// handleListPins returns the Account's Pins in sidebar order.
func (s *Server) handleListPins(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	pins, err := s.models.Pins.ListByAccount(r.Context(), accountID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	views := make([]pinView, 0, len(pins))
	for _, p := range pins {
		views = append(views, pinView{Metric: p.Metric, Position: p.Position})
	}
	if err := writeJSON(w, http.StatusOK, envelope{"pins": views}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// handleCreatePin pins a Catalog Metric. It answers 200 for an already-pinned
// Metric rather than a conflict: the client's toggle asks for a state, and a
// caller that gets the state it asked for has not made an error.
func (s *Server) handleCreatePin(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	var input struct {
		Metric string `json:"metric"`
	}
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	v := NewValidator()
	if input.Metric == "" {
		v.AddError("metric", "must be provided")
	} else if _, known := catalog.Lookup(input.Metric); !known {
		v.AddError("metric", unknownMetricMsg)
	}
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	p, err := s.models.Pins.Add(r.Context(), accountID, input.Metric)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	if err := writeJSON(w, http.StatusOK, envelope{"pin": pinView{Metric: p.Metric, Position: p.Position}}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// handleDeletePin unpins a Metric. It answers 204 whether or not the Pin existed,
// for the same reason the create path is idempotent: the requested state holds.
func (s *Server) handleDeletePin(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	metric := r.PathValue("metric")
	if err := s.models.Pins.Delete(r.Context(), accountID, metric); err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
