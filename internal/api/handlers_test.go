package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

const (
	testEmail    = "dev@example.com"
	testPassword = "correct horse battery staple"
)

// newTestServer builds a Server over a fresh migrated DB, seeded with a dev
// account whose password is testPassword, and returns it with the models and a
// session cookie already authenticating that account. Cookies are non-Secure so
// the httptest (plain-HTTP) client keeps them.
func newTestServer(t *testing.T) (*Server, data.Models, *http.Cookie) {
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
	seedAccountWithPassword(t, models, testEmail, testPassword)

	srv := New(slog.New(slog.NewTextHandler(io.Discard, nil)), models, query.Engine{DB: db}, Config{SecureCookies: false})
	cookie := login(t, srv, testEmail, testPassword)
	return srv, models, cookie
}

// seedAccountWithPassword inserts an account and sets its argon2id password hash.
func seedAccountWithPassword(t *testing.T, models data.Models, email, password string) {
	t.Helper()
	acc := &data.Account{Email: email}
	if err := models.Accounts.Insert(context.Background(), acc); err != nil {
		t.Fatalf("insert account %s: %v", email, err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := models.Accounts.SetPassword(context.Background(), acc.ID, hash); err != nil {
		t.Fatalf("set password: %v", err)
	}
}

// login POSTs credentials and returns the session cookie the server set.
func login(t *testing.T, srv *Server, email, password string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	res := rec.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", res.StatusCode)
	}
	for _, c := range res.Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	t.Fatalf("login set no %s cookie", sessionCookieName)
	return nil
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

// do sends a GET to the server's handler, attaching any cookies, and returns the
// response and decoded body.
func do(t *testing.T, srv *Server, target string, cookies ...*http.Cookie) (*http.Response, map[string]json.RawMessage) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for _, c := range cookies {
		if c != nil {
			req.AddCookie(c)
		}
	}
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
	srv, _, _ := newTestServer(t)
	res, body := do(t, srv, "/v1/healthz")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if string(body["status"]) != `"ok"` {
		t.Errorf("status field = %s, want \"ok\"", body["status"])
	}
}

func TestMetricsListsCatalog(t *testing.T) {
	srv, _, _ := newTestServer(t)
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

func TestMetricsExposesDerivedFormula(t *testing.T) {
	srv, _, _ := newTestServer(t)
	res, body := do(t, srv, "/v1/metrics")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var metrics []metricView
	if err := json.Unmarshal(body["metrics"], &metrics); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	var cb *metricView
	for i := range metrics {
		if metrics[i].Slug == "calorie_balance" {
			cb = &metrics[i]
		}
	}
	if cb == nil {
		t.Fatal("calorie_balance not listed")
	}
	if cb.Nature != "derived" || cb.Unit != "kcal" || !cb.Signed {
		t.Errorf("calorie_balance view = %+v, want derived/kcal/signed", cb)
	}
	if cb.Aggregation != "" {
		t.Errorf("derived Aggregation = %q, want empty", cb.Aggregation)
	}
	if cb.Formula == nil || cb.Formula.Scale != 1 || len(cb.Formula.Numerator) != 3 || cb.Formula.Denominator != nil {
		t.Fatalf("calorie_balance formula = %+v, want scale 1, 3 numerator terms, no denominator", cb.Formula)
	}
	want := map[string]float64{"dietary_energy": 1, "active_energy": -1, "basal_energy": -1}
	for _, term := range cb.Formula.Numerator {
		if c, ok := want[term.Metric]; !ok || c != term.Coefficient {
			t.Errorf("numerator term %+v not in expected set %v", term, want)
		}
	}
}

// TestMetricsAggregationOmittedForDerived asserts the raw JSON drops the
// aggregation key on a derived Metric rather than emitting an empty string.
func TestMetricsAggregationOmittedForDerived(t *testing.T) {
	srv, _, _ := newTestServer(t)
	_, body := do(t, srv, "/v1/metrics")
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(body["metrics"], &raw); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	for _, m := range raw {
		if string(m["slug"]) != `"protein_per_kg"` {
			continue
		}
		if _, present := m["aggregation"]; present {
			t.Errorf("derived metric carries an aggregation key: %v", m)
		}
		return
	}
	t.Fatal("protein_per_kg not listed")
}

func TestSeriesDerivedCalorieBalanceSigned(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	// Jan 1 has intake and expenditure → a signed balance; Jan 2 has only
	// expenditure and no dietary_energy → a gap, not a fake deficit.
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "dietary_energy", Value: 1800, OriginalUnit: "kcal", StartAt: "2024-01-01T12:00:00Z", EndAt: "2024-01-01T12:00:00Z", Source: "Food", ContentKey: "d1"},
		{Metric: "active_energy", Value: 500, OriginalUnit: "kcal", StartAt: "2024-01-01T18:00:00Z", EndAt: "2024-01-01T18:00:00Z", Source: "Watch", ContentKey: "a1"},
		{Metric: "basal_energy", Value: 1600, OriginalUnit: "kcal", StartAt: "2024-01-01T23:00:00Z", EndAt: "2024-01-01T23:00:00Z", Source: "Watch", ContentKey: "b1"},
		{Metric: "active_energy", Value: 400, OriginalUnit: "kcal", StartAt: "2024-01-02T18:00:00Z", EndAt: "2024-01-02T18:00:00Z", Source: "Watch", ContentKey: "a2"},
		{Metric: "basal_energy", Value: 1600, OriginalUnit: "kcal", StartAt: "2024-01-02T23:00:00Z", EndAt: "2024-01-02T23:00:00Z", Source: "Watch", ContentKey: "b2"},
	})
	res, body := do(t, srv, "/v1/series?metric=calorie_balance&from=2024-01-01&to=2024-01-03&bucket=day", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", res.StatusCode, body["error"])
	}
	var series query.Series
	if err := json.Unmarshal(body["series"], &series); err != nil {
		t.Fatalf("decode series: %v", err)
	}
	// A derived Series reports no rule and no single Source.
	if series.Aggregation != "" || series.Source != "" {
		t.Errorf("series = %+v, want empty aggregation and source", series)
	}
	// Only Jan 1 is complete: 1800 − 500 − 1600 = −300. Jan 2 is a gap.
	if len(series.Points) != 1 {
		t.Fatalf("points = %+v, want a single non-gap bucket", series.Points)
	}
	p := series.Points[0]
	if p.Bucket != "2024-01-01" || p.Value != -300 {
		t.Errorf("point = %+v, want 2024-01-01 = -300", p)
	}
	if p.Min != nil || p.Max != nil {
		t.Errorf("derived point carries a band: %+v", p)
	}
}

func TestSeriesStepsSummedPerDay(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 100, OriginalUnit: "count", StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T08:00:00Z", Source: "Watch", ContentKey: "a"},
		{Metric: "steps", Value: 200, OriginalUnit: "count", StartAt: "2024-01-01T18:00:00Z", EndAt: "2024-01-01T18:00:00Z", Source: "Watch", ContentKey: "b"},
	})
	res, body := do(t, srv, "/v1/series?metric=steps&from=2024-01-01&to=2024-01-02&bucket=day", cookie)
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
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "heart_rate", Value: 60, OriginalUnit: "count/min", StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T08:00:00Z", Source: "Watch", ContentKey: "a"},
		{Metric: "heart_rate", Value: 100, OriginalUnit: "count/min", StartAt: "2024-01-01T09:00:00Z", EndAt: "2024-01-01T09:00:00Z", Source: "Watch", ContentKey: "b"},
	})
	_, body := do(t, srv, "/v1/series?metric=heart_rate&from=2024-01-01&to=2024-01-02&bucket=day", cookie)
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
	srv, _, cookie := newTestServer(t)
	// No data, but a valid "1y" range should resolve and return an empty series.
	res, body := do(t, srv, "/v1/series?metric=steps&range=1y&bucket=day", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", res.StatusCode, body["error"])
	}
}

func TestSeriesBucketBelowCapRejected(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, body := do(t, srv, "/v1/series?metric=steps&range=7d&bucket=hour", cookie)
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
	srv, _, cookie := newTestServer(t)
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
			res, body := do(t, srv, tc.target, cookie)
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

// TestUnknownRouteAndMethod checks the mux's method+pattern routing: an unknown
// path 404s and a known path with the wrong method 405s.
func TestUnknownRouteAndMethod(t *testing.T) {
	srv, _, _ := newTestServer(t)
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
