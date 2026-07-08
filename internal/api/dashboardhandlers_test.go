package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// itoa renders an id for a request path.
func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// doReq sends a request with an optional JSON body and cookies, returning the
// response and decoded envelope. It is the write-path companion to `do`.
func doReq(t *testing.T, srv *Server, method, target, body string, cookies ...*http.Cookie) (*http.Response, map[string]json.RawMessage) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, target, r)
	for _, c := range cookies {
		if c != nil {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	res := rec.Result()
	var decoded map[string]json.RawMessage
	if b, _ := io.ReadAll(res.Body); len(b) > 0 {
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("decode body %q: %v", b, err)
		}
	}
	return res, decoded
}

// createDashboard is a test helper that creates a dashboard and returns its view.
func createDashboard(t *testing.T, srv *Server, cookie *http.Cookie, name string) dashboardView {
	t.Helper()
	res, body := doReq(t, srv, http.MethodPost, "/v1/dashboards", `{"name":"`+name+`"}`, cookie)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create dashboard status = %d, want 201 (%s)", res.StatusCode, body["error"])
	}
	var d dashboardView
	if err := json.Unmarshal(body["dashboard"], &d); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	return d
}

func TestDashboardsRequireAuth(t *testing.T) {
	srv, _, _ := newTestServer(t)
	res, _ := doReq(t, srv, http.MethodGet, "/v1/dashboards", "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated list status = %d, want 401", res.StatusCode)
	}
}

func TestCreateAndListDashboards(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	created := createDashboard(t, srv, cookie, "Training")
	if created.Name != "Training" || created.ID == 0 {
		t.Fatalf("created = %+v, want named Training with an id", created)
	}

	res, body := do(t, srv, "/v1/dashboards", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", res.StatusCode)
	}
	var list []dashboardView
	if err := json.Unmarshal(body["dashboards"], &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Errorf("list = %+v, want the one created dashboard", list)
	}
}

func TestCreateDashboardValidation(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, body := doReq(t, srv, http.MethodPost, "/v1/dashboards", `{"name":""}`, cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.StatusCode)
	}
	var fields map[string]string
	_ = json.Unmarshal(body["error"], &fields)
	if _, ok := fields["name"]; !ok {
		t.Errorf("error = %v, want a name error", fields)
	}
}

func TestCreatePanelDefaultsChartTypeFromAggregation(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	d := createDashboard(t, srv, cookie, "D")

	// steps aggregates by sum → default chart type bar.
	res, body := doReq(t, srv, http.MethodPost, "/v1/dashboards/"+itoa(d.ID)+"/panels", `{"metric":"steps"}`, cookie)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create panel status = %d, want 201 (%s)", res.StatusCode, body["error"])
	}
	var p panelView
	if err := json.Unmarshal(body["panel"], &p); err != nil {
		t.Fatalf("decode panel: %v", err)
	}
	if p.Metric != "steps" || p.ChartType != "bar" || p.Width != 1 || p.Bucket != nil {
		t.Errorf("panel = %+v, want steps/bar/width1/auto-bucket", p)
	}

	// heart_rate aggregates by average → default chart type band.
	_, body2 := doReq(t, srv, http.MethodPost, "/v1/dashboards/"+itoa(d.ID)+"/panels", `{"metric":"heart_rate"}`, cookie)
	var hr panelView
	_ = json.Unmarshal(body2["panel"], &hr)
	if hr.ChartType != "band" {
		t.Errorf("heart_rate default chart = %q, want band", hr.ChartType)
	}
}

func TestCreatePanelDefaultsDivergingBarForSignedMetric(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	d := createDashboard(t, srv, cookie, "D")

	// calorie_balance is a signed derived Metric → default chart diverging_bar.
	res, body := doReq(t, srv, http.MethodPost, "/v1/dashboards/"+itoa(d.ID)+"/panels", `{"metric":"calorie_balance"}`, cookie)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create panel status = %d, want 201 (%s)", res.StatusCode, body["error"])
	}
	var p panelView
	if err := json.Unmarshal(body["panel"], &p); err != nil {
		t.Fatalf("decode panel: %v", err)
	}
	if p.Metric != "calorie_balance" || p.ChartType != "diverging_bar" {
		t.Errorf("panel = %+v, want calorie_balance/diverging_bar", p)
	}
}

func TestDivergingBarRejectedForUnsignedMetric(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	d := createDashboard(t, srv, cookie, "D")
	// steps is not signed, so it may not take the diverging-bar variant.
	res, body := doReq(t, srv, http.MethodPost, "/v1/dashboards/"+itoa(d.ID)+"/panels", `{"metric":"steps","chart_type":"diverging_bar"}`, cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.StatusCode)
	}
	var fields map[string]string
	_ = json.Unmarshal(body["error"], &fields)
	if _, ok := fields["chart_type"]; !ok {
		t.Errorf("error = %v, want a chart_type error", fields)
	}
}

func TestCreatePanelRejectsUnknownMetricAndBadChart(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	d := createDashboard(t, srv, cookie, "D")
	base := "/v1/dashboards/" + itoa(d.ID) + "/panels"

	tests := map[string]struct {
		body  string
		field string
	}{
		"unknown metric":       {`{"metric":"nope"}`, "metric"},
		"band on non-average":  {`{"metric":"steps","chart_type":"band"}`, "chart_type"},
		"stacked on non-sleep": {`{"metric":"steps","chart_type":"stacked_bar"}`, "chart_type"},
		"bad bucket":           {`{"metric":"steps","bucket":"hour"}`, "bucket"},
		"width too wide":       {`{"metric":"steps","width":9}`, "width"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			res, body := doReq(t, srv, http.MethodPost, base, tc.body, cookie)
			if res.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", res.StatusCode)
			}
			var fields map[string]string
			_ = json.Unmarshal(body["error"], &fields)
			if _, ok := fields[tc.field]; !ok {
				t.Errorf("error = %v, want field %q", fields, tc.field)
			}
		})
	}
}

func TestUpdateDashboardCustomRange(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	d := createDashboard(t, srv, cookie, "D")

	res, body := doReq(t, srv, http.MethodPatch, "/v1/dashboards/"+itoa(d.ID),
		`{"range_preset":"custom","range_from":"2024-01-01","range_to":"2024-02-01"}`, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", res.StatusCode, body["error"])
	}
	var got dashboardView
	_ = json.Unmarshal(body["dashboard"], &got)
	if got.RangePreset != "custom" || got.RangeFrom == nil || *got.RangeFrom != "2024-01-01" {
		t.Errorf("dashboard = %+v, want custom range 2024-01-01..02-01", got)
	}
}

func TestUpdateDashboardRejectsInvertedCustomRange(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	d := createDashboard(t, srv, cookie, "D")
	res, body := doReq(t, srv, http.MethodPatch, "/v1/dashboards/"+itoa(d.ID),
		`{"range_preset":"custom","range_from":"2024-02-01","range_to":"2024-01-01"}`, cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.StatusCode)
	}
	var fields map[string]string
	_ = json.Unmarshal(body["error"], &fields)
	if _, ok := fields["range_to"]; !ok {
		t.Errorf("error = %v, want a range_to error", fields)
	}
}

func TestSwitchingToPresetClearsCustomBounds(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	d := createDashboard(t, srv, cookie, "D")
	_, _ = doReq(t, srv, http.MethodPatch, "/v1/dashboards/"+itoa(d.ID),
		`{"range_preset":"custom","range_from":"2024-01-01","range_to":"2024-02-01"}`, cookie)

	_, body := doReq(t, srv, http.MethodPatch, "/v1/dashboards/"+itoa(d.ID), `{"range_preset":"7d"}`, cookie)
	var got dashboardView
	_ = json.Unmarshal(body["dashboard"], &got)
	if got.RangePreset != "7d" || got.RangeFrom != nil || got.RangeTo != nil {
		t.Errorf("dashboard = %+v, want 7d preset with cleared bounds", got)
	}
}

func TestReorderPanels(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	d := createDashboard(t, srv, cookie, "D")
	base := "/v1/dashboards/" + itoa(d.ID) + "/panels"

	var ids []int64
	for _, m := range []string{"steps", "heart_rate", "body_mass"} {
		_, body := doReq(t, srv, http.MethodPost, base, `{"metric":"`+m+`"}`, cookie)
		var p panelView
		_ = json.Unmarshal(body["panel"], &p)
		ids = append(ids, p.ID)
	}

	order := itoa(ids[2]) + "," + itoa(ids[1]) + "," + itoa(ids[0])
	res, body := doReq(t, srv, http.MethodPatch, base+"/order", `{"panel_ids":[`+order+`]}`, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reorder status = %d, want 200 (%s)", res.StatusCode, body["error"])
	}
	var got dashboardView
	_ = json.Unmarshal(body["dashboard"], &got)
	if got.Panels[0].ID != ids[2] || got.Panels[2].ID != ids[0] {
		t.Errorf("panel order = %d..%d, want reversed", got.Panels[0].ID, got.Panels[2].ID)
	}
}

func TestDeletePanelAndDashboard(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	d := createDashboard(t, srv, cookie, "D")
	_, body := doReq(t, srv, http.MethodPost, "/v1/dashboards/"+itoa(d.ID)+"/panels", `{"metric":"steps"}`, cookie)
	var p panelView
	_ = json.Unmarshal(body["panel"], &p)

	res, _ := doReq(t, srv, http.MethodDelete, "/v1/panels/"+itoa(p.ID), "", cookie)
	if res.StatusCode != http.StatusNoContent {
		t.Errorf("delete panel status = %d, want 204", res.StatusCode)
	}
	res, _ = doReq(t, srv, http.MethodDelete, "/v1/dashboards/"+itoa(d.ID), "", cookie)
	if res.StatusCode != http.StatusNoContent {
		t.Errorf("delete dashboard status = %d, want 204", res.StatusCode)
	}
	// Deleting a now-missing dashboard is a 404.
	res, _ = doReq(t, srv, http.MethodDelete, "/v1/dashboards/"+itoa(d.ID), "", cookie)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("re-delete status = %d, want 404", res.StatusCode)
	}
}

func TestDashboardIsolationAcrossAccounts(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	alice := createDashboard(t, srv, cookie, "Alice")

	// Seed a second account and log in as them.
	seedAccountWithPassword(t, models, "bob@example.com", testPassword)
	bobCookie := login(t, srv, "bob@example.com", testPassword)

	// Bob cannot see or fetch Alice's dashboard.
	res, _ := doReq(t, srv, http.MethodGet, "/v1/dashboards/"+itoa(alice.ID), "", bobCookie)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("cross-account GET status = %d, want 404", res.StatusCode)
	}
	_, body := do(t, srv, "/v1/dashboards", bobCookie)
	var list []dashboardView
	_ = json.Unmarshal(body["dashboards"], &list)
	if len(list) != 0 {
		t.Errorf("bob sees %d dashboards, want 0", len(list))
	}
}

func TestUpdatePanelBucketOverrideAndClear(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	d := createDashboard(t, srv, cookie, "D")
	_, body := doReq(t, srv, http.MethodPost, "/v1/dashboards/"+itoa(d.ID)+"/panels", `{"metric":"steps"}`, cookie)
	var p panelView
	_ = json.Unmarshal(body["panel"], &p)

	// Set an override.
	_, body = doReq(t, srv, http.MethodPatch, "/v1/panels/"+itoa(p.ID), `{"bucket":"week","chart_type":"line"}`, cookie)
	var got panelView
	_ = json.Unmarshal(body["panel"], &got)
	if got.Bucket == nil || *got.Bucket != "week" || got.ChartType != "line" {
		t.Errorf("panel = %+v, want week/line", got)
	}
	// Clear it back to auto.
	_, body = doReq(t, srv, http.MethodPatch, "/v1/panels/"+itoa(p.ID), `{"bucket":null}`, cookie)
	got = panelView{}
	_ = json.Unmarshal(body["panel"], &got)
	if got.Bucket != nil {
		t.Errorf("panel bucket = %v, want nil (auto)", got.Bucket)
	}
}

// TestValidateBaseline exercises the Baseline rules directly (ADR 0015): bounds
// are required and well-formed only under the custom rule, and forbidden
// otherwise. The payload wiring that feeds it is issue 03.
func TestValidateBaseline(t *testing.T) {
	ptr := func(s string) *string { return &s }

	tests := map[string]struct {
		rule     string
		from, to *string
		wantKey  string // "" means valid
	}{
		"none is valid":                 {rule: "none"},
		"previous is valid":             {rule: "previous"},
		"same period last year":         {rule: "same_period_last_year"},
		"custom with ordered bounds":    {rule: "custom", from: ptr("2024-01-01"), to: ptr("2024-02-01")},
		"unknown rule":                  {rule: "weekly", wantKey: "baseline_rule"},
		"custom missing both bounds":    {rule: "custom", wantKey: "baseline_from"},
		"custom missing to":             {rule: "custom", from: ptr("2024-01-01"), wantKey: "baseline_from"},
		"custom malformed from":         {rule: "custom", from: ptr("Jan 1"), to: ptr("2024-02-01"), wantKey: "baseline_from"},
		"custom malformed to":           {rule: "custom", from: ptr("2024-01-01"), to: ptr("02/01/2024"), wantKey: "baseline_to"},
		"custom to not after from":      {rule: "custom", from: ptr("2024-02-01"), to: ptr("2024-02-01"), wantKey: "baseline_to"},
		"bounds under none":             {rule: "none", from: ptr("2024-01-01"), to: ptr("2024-02-01"), wantKey: "baseline_from"},
		"stray bound under previous":    {rule: "previous", to: ptr("2024-02-01"), wantKey: "baseline_from"},
		"bounds under last year's rule": {rule: "same_period_last_year", from: ptr("2024-01-01"), wantKey: "baseline_from"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			v := NewValidator()
			validateBaseline(v, tc.rule, tc.from, tc.to)
			if tc.wantKey == "" {
				if !v.Valid() {
					t.Errorf("errors = %v, want none", v.Errors)
				}
				return
			}
			if _, ok := v.Errors[tc.wantKey]; !ok {
				t.Errorf("errors = %v, want key %q", v.Errors, tc.wantKey)
			}
		})
	}
}
