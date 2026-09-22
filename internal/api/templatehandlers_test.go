package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type templateView struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Metrics     []string `json:"metrics"`
	WithData    int      `json:"with_data"`
	Panels      int      `json:"panels"`
}

func listTemplates(t *testing.T, srv *Server, cookie *http.Cookie) []templateView {
	t.Helper()
	res, body := do(t, srv, "/v1/dashboard-templates", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", res.StatusCode, body["error"])
	}
	var list []templateView
	if err := json.Unmarshal(body["templates"], &list); err != nil {
		t.Fatalf("decode templates: %v", err)
	}
	return list
}

// TestTemplatesAreListedWithoutTheOverview: every Account already has the Overview
// or deleted it on purpose, so the listing offers the others, by name, each with
// how many of its Metrics this Account holds data for (ADR 0047).
func TestTemplatesAreListedWithoutTheOverview(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedDaily(t, models, "resting_heart_rate", 3, false)

	list := listTemplates(t, srv, cookie)
	var slugs []string
	for _, tv := range list {
		slugs = append(slugs, tv.Slug)
	}
	if want := []string{"cut", "endurance", "sleep"}; len(slugs) != 3 ||
		slugs[0] != want[0] || slugs[1] != want[1] || slugs[2] != want[2] {
		t.Fatalf("slugs = %v, want %v", slugs, want)
	}

	sleep := list[2]
	if sleep.Name != "Sleep" || sleep.Description == "" || sleep.Panels != 5 {
		t.Errorf("sleep = %+v", sleep)
	}
	wantMetrics := []string{"sleep", "heart_rate_variability_sdnn", "resting_heart_rate",
		"respiratory_rate", "apple_sleeping_wrist_temperature"}
	if len(sleep.Metrics) != len(wantMetrics) {
		t.Fatalf("sleep metrics = %v, want %v", sleep.Metrics, wantMetrics)
	}
	for i := range wantMetrics {
		if sleep.Metrics[i] != wantMetrics[i] {
			t.Errorf("sleep metrics = %v, want %v", sleep.Metrics, wantMetrics)
			break
		}
	}
	if sleep.WithData != 1 {
		t.Errorf("sleep with_data = %d, want 1 (resting heart rate only)", sleep.WithData)
	}
	// Endurance names resting heart rate once, on a Panel beside HRV: counted once.
	if endurance := list[1]; endurance.WithData != 1 || len(endurance.Metrics) != 6 {
		t.Errorf("endurance = %+v, want 6 distinct metrics, 1 with data", endurance)
	}
	if cut := list[0]; cut.WithData != 0 {
		t.Errorf("cut with_data = %d, want 0", cut.WithData)
	}
}

// TestADerivedMetricHasDataWhenEveryOperandDoes: a derived Metric has no rows of
// its own, so it counts once each term of its Formula has data, and not before.
func TestADerivedMetricHasDataWhenEveryOperandDoes(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	for _, m := range []string{"dietary_energy", "active_energy", "basal_energy", "body_mass"} {
		seedDaily(t, models, m, 3, false)
	}

	// Cut: calorie_balance, body_mass, dietary_energy and total_energy_expenditure
	// have data; body_fat_percentage does not, nor protein_per_kg, whose protein
	// term is missing although its body_mass term is present.
	if cut := listTemplates(t, srv, cookie)[0]; cut.Slug != "cut" || cut.WithData != 4 || len(cut.Metrics) != 6 {
		t.Errorf("cut = %+v, want 4 of 6 with data", cut)
	}
}

func createFromTemplate(t *testing.T, srv *Server, cookie *http.Cookie, body string) dashboardView {
	t.Helper()
	res, decoded := doReq(t, srv, http.MethodPost, "/v1/dashboards", body, cookie)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d body=%s, want 201", res.StatusCode, decoded["error"])
	}
	var d dashboardView
	if err := json.Unmarshal(decoded["dashboard"], &d); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	return d
}

// TestADashboardIsCreatedFromATemplate: the response is the whole Dashboard, with
// the template's Panels in order, so the client lands on it without a second read.
func TestADashboardIsCreatedFromATemplate(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	d := createFromTemplate(t, srv, cookie, `{"template":"endurance"}`)
	if d.Name != "Endurance" || d.RangePreset != "3m" || d.BaselineRule != "none" {
		t.Errorf("dashboard = %+v, want Endurance over 3m", d)
	}
	if len(d.Panels) != 5 {
		t.Fatalf("panels = %d, want 5", len(d.Panels))
	}
	first := d.Panels[0]
	if first.Width != 2 || first.Bucket == nil || *first.Bucket != "week" || first.Position != 0 ||
		len(first.Metrics) != 1 || first.Metrics[0] != (panelMetricView{"training_time", "stacked_bar"}) {
		t.Errorf("panel 0 = %+v", first)
	}
	if pair := d.Panels[3]; len(pair.Metrics) != 2 || pair.Metrics[1].Metric != "heart_rate_variability_sdnn" {
		t.Errorf("panel 3 = %+v, want resting heart rate beside HRV", pair)
	}

	// The same Dashboard as a later read returns it.
	res, body := do(t, srv, fmt.Sprintf("/v1/dashboards/%d", d.ID), cookie)
	var got dashboardView
	if res.StatusCode != http.StatusOK || json.Unmarshal(body["dashboard"], &got) != nil || len(got.Panels) != 5 {
		t.Errorf("GET after create = %d %+v", res.StatusCode, got)
	}
}

// TestATemplateIsCopiedEachTime: instantiating twice makes two ordinary
// Dashboards, and a name given replaces the template's own.
func TestATemplateIsCopiedEachTime(t *testing.T) {
	srv, models, cookie := newTestServer(t)

	a := createFromTemplate(t, srv, cookie, `{"template":"sleep"}`)
	b := createFromTemplate(t, srv, cookie, `{"template":"sleep","name":"Sleep, travel weeks"}`)
	if a.ID == b.ID || a.Name != "Sleep" || b.Name != "Sleep, travel weeks" {
		t.Errorf("created %d %q and %d %q, want two Dashboards, the second renamed", a.ID, a.Name, b.ID, b.Name)
	}

	// The other Account sees none of them.
	seedAccountWithPassword(t, models, "other@example.com", testPassword)
	other := login(t, srv, "other@example.com", testPassword)
	res, body := do(t, srv, "/v1/dashboards", other)
	var list []dashboardView
	if res.StatusCode != http.StatusOK || json.Unmarshal(body["dashboards"], &list) != nil {
		t.Fatalf("list = %d", res.StatusCode)
	}
	for _, d := range list {
		if d.ID == a.ID || d.ID == b.ID {
			t.Errorf("another Account sees dashboard %d", d.ID)
		}
	}
}

func TestCreatingFromATemplateRefuses(t *testing.T) {
	cases := map[string]struct{ body, field string }{
		"an unknown template": {`{"template":"bulk"}`, "template"},
		"a name past the cap": {`{"template":"sleep","name":"` + strings.Repeat("z", 121) + `"}`, "name"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv, models, cookie := newTestServer(t)
			res, body := doReq(t, srv, http.MethodPost, "/v1/dashboards", tc.body, cookie)
			if res.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", res.StatusCode)
			}
			var errs map[string]string
			if err := json.Unmarshal(body["error"], &errs); err != nil || errs[tc.field] == "" {
				t.Errorf("error = %s, want a fault on %q", body["error"], tc.field)
			}
			acc, _ := models.Accounts.GetByEmail(context.Background(), testEmail)
			// The test Account is inserted without the seed, so a refusal leaves none.
			if list, _ := models.Dashboards.ListByAccount(context.Background(), acc.ID); len(list) != 0 {
				t.Errorf("dashboards = %d, want none written", len(list))
			}
		})
	}
}
