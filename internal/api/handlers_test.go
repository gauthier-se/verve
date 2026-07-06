package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

// newTestServer builds a Server over a fresh migrated DB, seeded with a dev
// account, and returns it with the account email and models for seeding data.
func newTestServer(t *testing.T) (*Server, data.Models) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "verve.db")
	db, err := data.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := data.Migrate(context.Background(), db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	models := data.NewModels(db)
	acc := &data.Account{Email: "dev@example.com"}
	if err := models.Accounts.Insert(context.Background(), acc); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	srv := New(slog.New(slog.NewTextHandler(io.Discard, nil)), models, query.Engine{DB: db}, "dev@example.com")
	return srv, models
}

func seedSteps(t *testing.T, models data.Models, email string, ms []data.Measurement) {
	t.Helper()
	acc, err := models.Accounts.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	for i := range ms {
		ms[i].AccountID = acc.ID
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), ms); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// do sends a GET to the server's handler and returns the response and decoded body.
func do(t *testing.T, srv *Server, target string) (*http.Response, map[string]json.RawMessage) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	res := rec.Result()
	var body map[string]json.RawMessage
	if b, _ := io.ReadAll(res.Body); len(b) > 0 {
		if err := json.Unmarshal(b, &body); err != nil {
			t.Fatalf("decode body %q: %v", b, err)
		}
	}
	return res, body
}

func TestHealthz(t *testing.T) {
	srv, _ := newTestServer(t)
	res, body := do(t, srv, "/v1/healthz")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if string(body["status"]) != `"ok"` {
		t.Errorf("status field = %s, want \"ok\"", body["status"])
	}
}

func TestMetricsListsCatalog(t *testing.T) {
	srv, _ := newTestServer(t)
	res, body := do(t, srv, "/v1/metrics")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var metrics []metricView
	if err := json.Unmarshal(body["metrics"], &metrics); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	if len(metrics) == 0 {
		t.Fatal("metrics list is empty")
	}
	// Sorted by slug and carrying rule metadata.
	var steps *metricView
	for i := range metrics {
		if i > 0 && metrics[i-1].Slug > metrics[i].Slug {
			t.Errorf("metrics not sorted: %q before %q", metrics[i-1].Slug, metrics[i].Slug)
		}
		if metrics[i].Slug == "steps" {
			steps = &metrics[i]
		}
	}
	if steps == nil || steps.Aggregation != "sum" || steps.Unit != "count" {
		t.Errorf("steps view = %+v, want sum/count", steps)
	}
}

func TestSeriesStepsSummedPerDay(t *testing.T) {
	srv, models := newTestServer(t)
	seedSteps(t, models, "dev@example.com", []data.Measurement{
		{Metric: "steps", Value: 100, OriginalUnit: "count", StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T08:00:00Z", Source: "Watch", ContentKey: "a"},
		{Metric: "steps", Value: 200, OriginalUnit: "count", StartAt: "2024-01-01T18:00:00Z", EndAt: "2024-01-01T18:00:00Z", Source: "Watch", ContentKey: "b"},
	})
	res, body := do(t, srv, "/v1/series?metric=steps&from=2024-01-01&to=2024-01-02&bucket=day")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", res.StatusCode, body["error"])
	}
	var series query.Series
	if err := json.Unmarshal(body["series"], &series); err != nil {
		t.Fatalf("decode series: %v", err)
	}
	if series.Aggregation != "sum" || len(series.Points) != 1 || series.Points[0].Value != 300 {
		t.Errorf("series = %+v, want one summed bucket of 300", series)
	}
}

func TestSeriesHeartRateBand(t *testing.T) {
	srv, models := newTestServer(t)
	seedSteps(t, models, "dev@example.com", []data.Measurement{
		{Metric: "heart_rate", Value: 60, OriginalUnit: "count/min", StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T08:00:00Z", Source: "Watch", ContentKey: "a"},
		{Metric: "heart_rate", Value: 100, OriginalUnit: "count/min", StartAt: "2024-01-01T09:00:00Z", EndAt: "2024-01-01T09:00:00Z", Source: "Watch", ContentKey: "b"},
	})
	_, body := do(t, srv, "/v1/series?metric=heart_rate&from=2024-01-01&to=2024-01-02&bucket=day")
	var series query.Series
	if err := json.Unmarshal(body["series"], &series); err != nil {
		t.Fatalf("decode series: %v", err)
	}
	if len(series.Points) != 1 {
		t.Fatalf("points = %+v, want 1", series.Points)
	}
	p := series.Points[0]
	if p.Value != 80 || p.Min == nil || *p.Min != 60 || p.Max == nil || *p.Max != 100 {
		t.Errorf("band point = %+v (%v/%v), want avg 80 min 60 max 100", p, p.Min, p.Max)
	}
}

func TestSeriesRangeShorthand(t *testing.T) {
	srv, _ := newTestServer(t)
	// No data, but a valid "1y" range should resolve and return an empty series.
	res, body := do(t, srv, "/v1/series?metric=steps&range=1y&bucket=day")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", res.StatusCode, body["error"])
	}
}

func TestSeriesBucketBelowCapRejected(t *testing.T) {
	srv, _ := newTestServer(t)
	res, body := do(t, srv, "/v1/series?metric=steps&range=7d&bucket=hour")
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.StatusCode)
	}
	var fields map[string]string
	if err := json.Unmarshal(body["error"], &fields); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if _, ok := fields["bucket"]; !ok {
		t.Errorf("error fields = %v, want a bucket error", fields)
	}
}

func TestSeriesValidationErrors(t *testing.T) {
	srv, _ := newTestServer(t)
	tests := map[string]struct {
		target string
		field  string
	}{
		"missing metric":  {"/v1/series?range=7d", "metric"},
		"unknown metric":  {"/v1/series?metric=nope&range=7d", "metric"},
		"missing range":   {"/v1/series?metric=steps", "range"},
		"bad range":       {"/v1/series?metric=steps&range=xyz", "range"},
		"bad from":        {"/v1/series?metric=steps&from=nonsense&to=2024-01-02", "from"},
		"range too large": {"/v1/series?metric=steps&from=2000-01-01&to=2024-01-01&bucket=day", "bucket"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			res, body := do(t, srv, tc.target)
			if res.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", res.StatusCode)
			}
			var fields map[string]string
			if err := json.Unmarshal(body["error"], &fields); err != nil {
				t.Fatalf("decode error %s: %v", body["error"], err)
			}
			if _, ok := fields[tc.field]; !ok {
				t.Errorf("error fields = %v, want key %q", fields, tc.field)
			}
		})
	}
}

func TestSeriesAccountScopingViaHeader(t *testing.T) {
	srv, models := newTestServer(t)
	// A second account with its own steps; the dev account has none.
	other := &data.Account{Email: "other@example.com"}
	if err := models.Accounts.Insert(context.Background(), other); err != nil {
		t.Fatalf("insert other: %v", err)
	}
	seedSteps(t, models, "other@example.com", []data.Measurement{
		{Metric: "steps", Value: 500, OriginalUnit: "count", StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T08:00:00Z", Source: "Watch", ContentKey: "a"},
	})

	// Default (dev) account sees nothing.
	req := httptest.NewRequest(http.MethodGet, "/v1/series?metric=steps&from=2024-01-01&to=2024-01-02", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var devBody map[string]json.RawMessage
	json.NewDecoder(rec.Body).Decode(&devBody)
	var devSeries query.Series
	json.Unmarshal(devBody["series"], &devSeries)
	if len(devSeries.Points) != 0 {
		t.Errorf("dev account points = %+v, want none", devSeries.Points)
	}

	// The header scopes the request to the other account, which has data.
	req = httptest.NewRequest(http.MethodGet, "/v1/series?metric=steps&from=2024-01-01&to=2024-01-02", nil)
	req.Header.Set(accountHeader, "other@example.com")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var otherBody map[string]json.RawMessage
	json.NewDecoder(rec.Body).Decode(&otherBody)
	var otherSeries query.Series
	json.Unmarshal(otherBody["series"], &otherSeries)
	if len(otherSeries.Points) != 1 || otherSeries.Points[0].Value != 500 {
		t.Errorf("other account points = %+v, want one bucket of 500", otherSeries.Points)
	}
}

func TestSeriesUnknownAccount(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/series?metric=steps&range=7d", nil)
	req.Header.Set(accountHeader, "ghost@example.com")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Result().StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for unknown account", rec.Result().StatusCode)
	}
}

// TestUnknownRouteAndMethod checks the mux's method+pattern routing: an unknown
// path 404s and a known path with the wrong method 405s.
func TestUnknownRouteAndMethod(t *testing.T) {
	srv, _ := newTestServer(t)
	get := func(method, target string) int {
		req := httptest.NewRequest(method, target, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec.Result().StatusCode
	}
	if code := get(http.MethodGet, "/v1/nope"); code != http.StatusNotFound {
		t.Errorf("unknown path status = %d, want 404", code)
	}
	if code := get(http.MethodPost, "/v1/metrics"); code != http.StatusMethodNotAllowed {
		t.Errorf("wrong method status = %d, want 405", code)
	}
}
