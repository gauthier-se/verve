package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/dashtemplate"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/timeaxis"
)

// panelMetricView is one Metric of a Panel with its chart type, in display order.
type panelMetricView struct {
	Metric    string `json:"metric"`
	ChartType string `json:"chart_type"`
}

// panelView is one Panel as the API exposes it. Aggregation is not stored — it is
// the Metric's Catalog rule — so the client reads it from GET /v1/metrics. The
// scalar metric/chart_type mirror the first Metrics entry for the pre-ADR-0020
// client; they go away with the SPA cutover (issue 03).
type panelView struct {
	ID       int64             `json:"id"`
	Metrics  []panelMetricView `json:"metrics"`
	Bucket   *string           `json:"bucket"`
	Width    int               `json:"width"`
	Position int               `json:"position"`
}

// dashboardView is one Dashboard with its ordered Panels, so a client loads a
// whole dashboard in one response. The Baseline mirrors the Time range (ADR
// 0015): a rule plus, for "custom" only, a frozen from/to window.
type dashboardView struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	Position     int     `json:"position"`
	RangePreset  string  `json:"range_preset"`
	RangeFrom    *string `json:"range_from"`
	RangeTo      *string `json:"range_to"`
	BaselineRule string  `json:"baseline_rule"`
	BaselineFrom *string `json:"baseline_from"`
	BaselineTo   *string `json:"baseline_to"`
	// Annotations is whether this Dashboard draws the Account's Annotation markers.
	// The notes are Account data; only their showing is a Dashboard property, because
	// the Dashboard owns the time axis they sit on (ADR 0030).
	Annotations bool        `json:"annotations"`
	Panels      []panelView `json:"panels"`
}

func panelToView(p data.Panel) panelView {
	view := panelView{
		ID: p.ID, Metrics: make([]panelMetricView, 0, len(p.Metrics)),
		Bucket: p.Bucket, Width: p.Width, Position: p.Position,
	}
	for _, pm := range p.Metrics {
		view.Metrics = append(view.Metrics, panelMetricView{Metric: pm.Metric, ChartType: pm.ChartType})
	}
	return view
}

func dashboardToView(d data.Dashboard, panels []data.Panel) dashboardView {
	views := make([]panelView, 0, len(panels))
	for _, p := range panels {
		views = append(views, panelToView(p))
	}
	return dashboardView{
		ID: d.ID, Name: d.Name, Position: d.Position,
		RangePreset: d.RangePreset, RangeFrom: d.RangeFrom, RangeTo: d.RangeTo,
		BaselineRule: d.BaselineRule, BaselineFrom: d.BaselineFrom, BaselineTo: d.BaselineTo,
		Annotations: d.Annotations,
		Panels:      views,
	}
}

// handleListDashboards returns the Account's dashboards, each with its panels.
func (s *Server) handleListDashboards(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	dashboards, err := s.models.Dashboards.ListByAccount(r.Context(), accountID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	views := make([]dashboardView, 0, len(dashboards))
	for _, d := range dashboards {
		panels, err := s.models.Panels.ListByDashboard(r.Context(), accountID, d.ID)
		if err != nil {
			s.serverErrorResponse(w, r, err)
			return
		}
		views = append(views, dashboardToView(d, panels))
	}
	s.respond(w, r, http.StatusOK, envelope{"dashboards": views})
}

// handleCreateDashboard creates an empty dashboard for the Account and returns it.
func (s *Server) handleCreateDashboard(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	var input struct {
		Name string `json:"name"`
	}
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	v := NewValidator()
	dashtemplate.ValidateName(v.invalid(), input.Name)
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	// A new dashboard defaults to the last-30-days preset; the client can widen
	// or narrow it immediately via PATCH.
	d := &data.Dashboard{AccountID: accountID, Name: input.Name, RangePreset: "30d"}
	if err := s.models.Dashboards.Insert(r.Context(), d); err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	s.respond(w, r, http.StatusCreated, envelope{"dashboard": dashboardToView(*d, nil)})
}

// handleGetDashboard returns one of the Account's dashboards with its panels.
func (s *Server) handleGetDashboard(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	d, ok := s.lookupDashboard(w, r, accountID)
	if !ok {
		return
	}
	panels, err := s.models.Panels.ListByDashboard(r.Context(), accountID, d.ID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	s.respond(w, r, http.StatusOK, envelope{"dashboard": dashboardToView(*d, panels)})
}

// handleUpdateDashboard patches a dashboard's name and/or Time range. Absent
// fields are left unchanged (pointer inputs distinguish "omitted" from "empty").
func (s *Server) handleUpdateDashboard(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	d, ok := s.lookupDashboard(w, r, accountID)
	if !ok {
		return
	}

	var input struct {
		Name         *string `json:"name"`
		RangePreset  *string `json:"range_preset"`
		RangeFrom    *string `json:"range_from"`
		RangeTo      *string `json:"range_to"`
		BaselineRule *string `json:"baseline_rule"`
		BaselineFrom *string `json:"baseline_from"`
		BaselineTo   *string `json:"baseline_to"`
		Annotations  *bool   `json:"annotations"`
	}
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	if input.Name != nil {
		d.Name = *input.Name
	}
	if input.RangePreset != nil {
		d.RangePreset = *input.RangePreset
	}
	// Range bounds carry meaning only for a custom preset; a preset clears them.
	if d.RangePreset == "custom" {
		if input.RangeFrom != nil {
			d.RangeFrom = input.RangeFrom
		}
		if input.RangeTo != nil {
			d.RangeTo = input.RangeTo
		}
	} else {
		d.RangeFrom, d.RangeTo = nil, nil
	}

	if input.BaselineRule != nil {
		d.BaselineRule = *input.BaselineRule
	}
	// Baseline bounds carry meaning only for the custom rule; a relative rule clears
	// any stale window, but bounds sent with a non-custom rule are kept so
	// timeaxis.Validate can reject them.
	switch {
	case d.BaselineRule == "custom":
		if input.BaselineFrom != nil {
			d.BaselineFrom = input.BaselineFrom
		}
		if input.BaselineTo != nil {
			d.BaselineTo = input.BaselineTo
		}
	case input.BaselineFrom != nil || input.BaselineTo != nil:
		d.BaselineFrom, d.BaselineTo = input.BaselineFrom, input.BaselineTo
	default:
		d.BaselineFrom, d.BaselineTo = nil, nil
	}

	if input.Annotations != nil {
		d.Annotations = *input.Annotations
	}

	v := NewValidator()
	if input.Name != nil {
		dashtemplate.ValidateName(v.invalid(), d.Name)
	}
	mergeInvalid(v, timeaxis.Validate(timeaxis.Tokens{
		RangePreset: d.RangePreset, RangeFrom: d.RangeFrom, RangeTo: d.RangeTo,
		BaselineRule: d.BaselineRule, BaselineFrom: d.BaselineFrom, BaselineTo: d.BaselineTo,
	}))
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := s.models.Dashboards.Update(r.Context(), d); err != nil {
		s.respondRecordError(w, r, err, "dashboard")
		return
	}
	panels, err := s.models.Panels.ListByDashboard(r.Context(), accountID, d.ID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	s.respond(w, r, http.StatusOK, envelope{"dashboard": dashboardToView(*d, panels)})
}

// handleDeleteDashboard removes a dashboard (its panels cascade in SQL).
func (s *Server) handleDeleteDashboard(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	if err := s.models.Dashboards.Delete(r.Context(), accountID, id); err != nil {
		s.respondRecordError(w, r, err, "dashboard")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleCreatePanel adds a panel to one of the Account's dashboards. chart_type
// defaults to the Metric's aggregation-derived type; bucket and width default to
// auto and single-column.
func (s *Server) handleCreatePanel(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	d, ok := s.lookupDashboard(w, r, accountID)
	if !ok {
		return
	}

	var input struct {
		Metric    string                     `json:"metric"`
		ChartType *string                    `json:"chart_type"`
		Metrics   []dashtemplate.MetricInput `json:"metrics"`
		Bucket    *string                    `json:"bucket"`
		Width     *int                       `json:"width"`
	}
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	v := NewValidator()
	// A nil Metrics means the key was absent — the legacy scalar shape, one entry
	// under its historical error key (dropped with the SPA cutover, issue 03). An
	// explicit empty list is the list shape, and errors as such.
	field, entries := "metrics", input.Metrics
	if input.Metrics == nil {
		field = "metric"
		if input.Metric != "" {
			entries = []dashtemplate.MetricInput{{Metric: input.Metric, ChartType: input.ChartType}}
		}
	}
	metrics := toPanelMetrics(dashtemplate.ValidatePanelMetrics(v.invalid(), field, entries))

	bucket := dashtemplate.ValidateBucket(v.invalid(), input.Bucket)
	width := 1
	if input.Width != nil {
		width = *input.Width
	}
	dashtemplate.ValidateWidth(v.invalid(), width)

	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	p := &data.Panel{
		DashboardID: d.ID, AccountID: accountID,
		Metrics: metrics,
		Bucket:  bucket, Width: width,
	}
	if err := s.models.Panels.Insert(r.Context(), p); err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	s.respond(w, r, http.StatusCreated, envelope{"panel": panelToView(*p)})
}

// handleUpdatePanel patches a panel's presentation (chart type, bucket, width).
// The Metric and dashboard membership are fixed at creation.
func (s *Server) handleUpdatePanel(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	p, err := s.models.Panels.GetByID(r.Context(), accountID, id)
	if err != nil {
		s.respondRecordError(w, r, err, "panel")
		return
	}

	// Bucket is json.RawMessage to tell an omitted key (leave unchanged) from an
	// explicit null (clear to auto-derive) — a *string collapses both to nil.
	var input struct {
		ChartType *string                    `json:"chart_type"`
		Metrics   []dashtemplate.MetricInput `json:"metrics"`
		Bucket    json.RawMessage            `json:"bucket"`
		Width     *int                       `json:"width"`
	}
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	if input.Width != nil {
		p.Width = *input.Width
	}

	v := NewValidator()
	switch {
	case input.Metrics != nil:
		// A metrics list replaces the Panel's whole list (ADR 0020); an explicit
		// empty list is a validation error, not a fall-through to the legacy shape.
		p.Metrics = toPanelMetrics(dashtemplate.ValidatePanelMetrics(v.invalid(), "metrics", input.Metrics))
	case input.ChartType != nil && len(p.Metrics) > 0:
		// Legacy scalar shape: the chart type applies to the first (only) Metric,
		// whose slug is known (the panel exists), so compatibility is enforced
		// against its aggregation rule.
		p.Metrics[0].ChartType = *input.ChartType
		if metric, known := catalog.Lookup(p.Metrics[0].Metric); known {
			dashtemplate.ValidateChartType(v.invalid(), p.Metrics[0].ChartType, metric)
		}
	}
	if input.Bucket != nil { // key present in the body
		p.Bucket = dashtemplate.ParseBucketOverride(input.Bucket, v.invalid())
	}
	dashtemplate.ValidateWidth(v.invalid(), p.Width)
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := s.models.Panels.Update(r.Context(), p); err != nil {
		s.respondRecordError(w, r, err, "panel")
		return
	}
	s.respond(w, r, http.StatusOK, envelope{"panel": panelToView(*p)})
}

// handleDeletePanel removes one of the Account's panels.
func (s *Server) handleDeletePanel(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	if err := s.models.Panels.Delete(r.Context(), accountID, id); err != nil {
		s.respondRecordError(w, r, err, "panel")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleReorderPanels persists a drag-reordered grid: the body lists the
// dashboard's panel ids in their new order. Ids that don't belong to the
// dashboard are simply ignored by the scoped update.
func (s *Server) handleReorderPanels(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)
	d, ok := s.lookupDashboard(w, r, accountID)
	if !ok {
		return
	}

	var input struct {
		PanelIDs []int64 `json:"panel_ids"`
	}
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	if err := s.models.Panels.Reorder(r.Context(), accountID, d.ID, input.PanelIDs); err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	panels, err := s.models.Panels.ListByDashboard(r.Context(), accountID, d.ID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	s.respond(w, r, http.StatusOK, envelope{"dashboard": dashboardToView(*d, panels)})
}

// lookupDashboard resolves the {id} path value to one of the Account's
// dashboards, writing the appropriate error response (400 for a bad id, 404 for
// a missing/foreign one) and returning ok=false when it can't.
func (s *Server) lookupDashboard(w http.ResponseWriter, r *http.Request, accountID int64) (*data.Dashboard, bool) {
	id, ok := s.pathID(w, r)
	if !ok {
		return nil, false
	}
	d, err := s.models.Dashboards.GetByID(r.Context(), accountID, id)
	if err != nil {
		s.respondRecordError(w, r, err, "dashboard")
		return nil, false
	}
	return d, true
}

// pathID parses the {id} path wildcard as a positive int64, writing a 404 for a
// malformed value (an unparseable id can never name a real record).
func (s *Server) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.notFoundResponse(w, r, "the requested resource could not be found")
		return 0, false
	}
	return id, true
}

// toPanelMetrics carries a validated Panel's Metrics into the storage type.
func toPanelMetrics(ms []dashtemplate.PanelMetric) []data.PanelMetric {
	out := make([]data.PanelMetric, len(ms))
	for i, m := range ms {
		out[i] = data.PanelMetric{Metric: m.Metric, ChartType: m.ChartType}
	}
	return out
}
