package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
	"github.com/gauthier-se/verve/internal/timeaxis"
)

// maxNameLen bounds a Dashboard name so a single field can't grow unbounded.
const maxNameLen = 120

// panelView is one Panel as the API exposes it. Aggregation is not stored — it is
// the Metric's Catalog rule — so the client reads it from GET /v1/metrics.
type panelView struct {
	ID        int64   `json:"id"`
	Metric    string  `json:"metric"`
	ChartType string  `json:"chart_type"`
	Bucket    *string `json:"bucket"`
	Width     int     `json:"width"`
	Position  int     `json:"position"`
}

// dashboardView is one Dashboard with its ordered Panels, so a client loads a
// whole dashboard in one response. The Baseline mirrors the Time range (ADR
// 0015): a rule plus, for "custom" only, a frozen from/to window.
type dashboardView struct {
	ID           int64       `json:"id"`
	Name         string      `json:"name"`
	Position     int         `json:"position"`
	RangePreset  string      `json:"range_preset"`
	RangeFrom    *string     `json:"range_from"`
	RangeTo      *string     `json:"range_to"`
	BaselineRule string      `json:"baseline_rule"`
	BaselineFrom *string     `json:"baseline_from"`
	BaselineTo   *string     `json:"baseline_to"`
	Panels       []panelView `json:"panels"`
}

func panelToView(p data.Panel) panelView {
	return panelView{
		ID: p.ID, Metric: p.Metric, ChartType: p.ChartType,
		Bucket: p.Bucket, Width: p.Width, Position: p.Position,
	}
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
		Panels: views,
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
	if err := writeJSON(w, http.StatusOK, envelope{"dashboards": views}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
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
	validateName(v, input.Name)
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
	if err := writeJSON(w, http.StatusCreated, envelope{"dashboard": dashboardToView(*d, nil)}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
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
	if err := writeJSON(w, http.StatusOK, envelope{"dashboard": dashboardToView(*d, panels)}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
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

	v := NewValidator()
	if input.Name != nil {
		validateName(v, d.Name)
	}
	if inv, ok := timeaxis.Validate(timeaxis.Tokens{
		RangePreset: d.RangePreset, RangeFrom: d.RangeFrom, RangeTo: d.RangeTo,
		BaselineRule: d.BaselineRule, BaselineFrom: d.BaselineFrom, BaselineTo: d.BaselineTo,
	}).(timeaxis.Invalid); ok {
		for field, msg := range inv {
			v.AddError(field, msg)
		}
	}
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
	if err := writeJSON(w, http.StatusOK, envelope{"dashboard": dashboardToView(*d, panels)}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
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
		Metric    string  `json:"metric"`
		ChartType *string `json:"chart_type"`
		Bucket    *string `json:"bucket"`
		Width     *int    `json:"width"`
	}
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	v := NewValidator()
	metric, known := catalog.Lookup(input.Metric)
	v.Check(input.Metric != "", "metric", "must be provided")
	if input.Metric != "" {
		v.Check(known, "metric", unknownMetricMsg)
	}

	chartType := ""
	if known {
		chartType = defaultChartType(metric)
	}
	if input.ChartType != nil {
		chartType = *input.ChartType
	}
	if known {
		validateChartType(v, chartType, metric)
	}

	bucket := validatePanelBucket(v, input.Bucket)
	width := 1
	if input.Width != nil {
		width = *input.Width
	}
	validateWidth(v, width)

	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	p := &data.Panel{
		DashboardID: d.ID, AccountID: accountID, Metric: input.Metric,
		ChartType: chartType, Bucket: bucket, Width: width,
	}
	if err := s.models.Panels.Insert(r.Context(), p); err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	if err := writeJSON(w, http.StatusCreated, envelope{"panel": panelToView(*p)}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
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
		ChartType *string         `json:"chart_type"`
		Bucket    json.RawMessage `json:"bucket"`
		Width     *int            `json:"width"`
	}
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	if input.ChartType != nil {
		p.ChartType = *input.ChartType
	}
	if input.Width != nil {
		p.Width = *input.Width
	}

	v := NewValidator()
	// The Metric is known (the panel exists), so chart-type compatibility can be
	// enforced against its aggregation rule.
	if metric, known := catalog.Lookup(p.Metric); known && input.ChartType != nil {
		validateChartType(v, p.ChartType, metric)
	}
	if input.Bucket != nil { // key present in the body
		p.Bucket = parseBucketOverride(input.Bucket, v)
	}
	validateWidth(v, p.Width)
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := s.models.Panels.Update(r.Context(), p); err != nil {
		s.respondRecordError(w, r, err, "panel")
		return
	}
	if err := writeJSON(w, http.StatusOK, envelope{"panel": panelToView(*p)}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
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
	if err := writeJSON(w, http.StatusOK, envelope{"dashboard": dashboardToView(*d, panels)}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
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

// respondRecordError maps a data-layer record error to a 404 and anything else to
// a 500 — the shared tail of every by-id handler. noun names the resource.
func (s *Server) respondRecordError(w http.ResponseWriter, r *http.Request, err error, noun string) {
	if errors.Is(err, data.ErrRecordNotFound) {
		s.notFoundResponse(w, r, "the requested "+noun+" could not be found")
		return
	}
	s.serverErrorResponse(w, r, err)
}

// defaultChartType is the chart a Metric gets when a Panel specifies none: signed
// derived → diverging bar (ADR 0014); else by aggregation — sum→bar, average→band,
// duration_by_state→stacked bar, latest (and unsigned derived)→line.
func defaultChartType(m catalog.Metric) string {
	if m.Signed {
		return "diverging_bar"
	}
	switch m.Aggregation {
	case catalog.Sum:
		return "bar"
	case catalog.Average:
		return "band"
	case catalog.DurationByState:
		return "stacked_bar"
	default: // Latest, and unsigned derived Metrics
		return "line"
	}
}

// validateName checks a Dashboard name is present and within the length cap.
func validateName(v *Validator, name string) {
	v.Check(name != "", "name", "must be provided")
	v.Check(len(name) <= maxNameLen, "name", "must be at most 120 characters")
}

// validChartTypes is the closed set a Panel may take.
var validChartTypes = map[string]bool{
	"bar": true, "line": true, "area": true, "band": true, "stacked_bar": true, "diverging_bar": true,
}

// validateChartType checks a chart type is known and compatible: band→average,
// stacked_bar→duration_by_state, diverging_bar→signed; bar/line/area suit any Metric.
func validateChartType(v *Validator, chartType string, m catalog.Metric) {
	if !validChartTypes[chartType] {
		v.AddError("chart_type", "must be one of bar, line, area, band, stacked_bar, diverging_bar")
		return
	}
	switch chartType {
	case "band":
		v.Check(m.Aggregation == catalog.Average, "chart_type", "the band variant is only for average metrics")
	case "stacked_bar":
		v.Check(m.Aggregation == catalog.DurationByState, "chart_type", "the stacked-bar variant is only for duration-by-state metrics")
	case "diverging_bar":
		v.Check(m.Signed, "chart_type", "the diverging-bar variant is only for signed metrics")
	}
}

// parseBucketOverride resolves a present bucket field on a panel update: the
// literal null clears the override (auto-derive), a JSON string is validated as
// day/week/month, and anything else is a validation error.
func parseBucketOverride(raw json.RawMessage, v *Validator) *string {
	if string(raw) == "null" {
		return nil
	}
	var b string
	if err := json.Unmarshal(raw, &b); err != nil {
		v.AddError("bucket", "must be a string (day, week, month) or null")
		return nil
	}
	return validatePanelBucket(v, &b)
}

// validatePanelBucket resolves an optional bucket override: nil (or explicit
// null) means auto-derive; a value must be a known bucket (query.ParseBucket, the
// single bucket vocabulary shared with the read path).
func validatePanelBucket(v *Validator, raw *string) *string {
	if raw == nil {
		return nil
	}
	if _, err := query.ParseBucket(*raw); err != nil {
		v.AddError("bucket", "must be day, week, or month, or omitted to auto-derive")
		return nil
	}
	return raw
}

// validateWidth checks a Panel's column span is 1, 2, or 3.
func validateWidth(v *Validator, width int) {
	v.Check(width >= 1 && width <= 3, "width", "must be between 1 and 3")
}
