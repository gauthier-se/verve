package api

import (
	"net/http"

	"github.com/gauthier-se/verve/internal/dashtemplate"
	"github.com/gauthier-se/verve/internal/query"
)

// dashboardTemplateView is one offered Dashboard template: what it is, which
// Metrics it draws, and how many of them this Account holds data for. The count
// is a fact beside the name and never a filter (ADR 0047).
type dashboardTemplateView struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Metrics     []string `json:"metrics"`
	WithData    int      `json:"with_data"`
	Panels      int      `json:"panels"`
}

// handleListDashboardTemplates returns the templates a Dashboard can be started
// from, in roster order. The seeded one is left out: every Account already has
// it, or deleted it on purpose.
func (s *Server) handleListDashboardTemplates(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	withData, err := s.engine.MetricsWithData(r.Context(), accountID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	present := make(map[string]bool, len(withData))
	for _, slug := range withData {
		present[slug] = true
	}

	views := []dashboardTemplateView{}
	for _, f := range dashtemplate.All() {
		if f.Slug == dashtemplate.Seeded {
			continue
		}
		v := dashboardTemplateView{
			Slug: f.Slug, Name: f.Name, Description: f.Description,
			Metrics: f.Metrics(), Panels: len(f.Panels),
		}
		for _, slug := range v.Metrics {
			if query.HasData(present, slug) {
				v.WithData++
			}
		}
		views = append(views, v)
	}
	s.respond(w, r, http.StatusOK, envelope{"templates": views})
}
