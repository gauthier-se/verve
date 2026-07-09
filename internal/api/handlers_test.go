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
	res, body := do(t, srv, "/v1/series?metric=calorie_balance&range_preset=custom&range_from=2024-01-01&range_to=2024-01-03&bucket=day", cookie)
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
	res, body := do(t, srv, "/v1/series?metric=steps&range_preset=custom&range_from=2024-01-01&range_to=2024-01-02&bucket=day", cookie)
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
	_, body := do(t, srv, "/v1/series?metric=heart_rate&range_preset=custom&range_from=2024-01-01&range_to=2024-01-02&bucket=day", cookie)
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

// TestSeriesBaselinePrevious is the issue-03 acceptance: a baseline param returns
// a current series and a baseline series of equal length, index-aligned, each
// baseline bucket carrying its own real date. Without the param the response is
// today's single-series shape (TestSeriesNoBaselineSingleSeries).
func TestSeriesBaselinePrevious(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		// Current window Feb 1–3.
		{Metric: "steps", Value: 100, OriginalUnit: "count", StartAt: "2024-02-01T08:00:00Z", EndAt: "2024-02-01T08:00:00Z", Source: "Watch", ContentKey: "c1"},
		{Metric: "steps", Value: 200, OriginalUnit: "count", StartAt: "2024-02-02T08:00:00Z", EndAt: "2024-02-02T08:00:00Z", Source: "Watch", ContentKey: "c2"},
		{Metric: "steps", Value: 300, OriginalUnit: "count", StartAt: "2024-02-03T08:00:00Z", EndAt: "2024-02-03T08:00:00Z", Source: "Watch", ContentKey: "c3"},
		// Previous window (prior 3 days) Jan 29–31.
		{Metric: "steps", Value: 10, OriginalUnit: "count", StartAt: "2024-01-29T08:00:00Z", EndAt: "2024-01-29T08:00:00Z", Source: "Watch", ContentKey: "b1"},
		{Metric: "steps", Value: 20, OriginalUnit: "count", StartAt: "2024-01-30T08:00:00Z", EndAt: "2024-01-30T08:00:00Z", Source: "Watch", ContentKey: "b2"},
		{Metric: "steps", Value: 30, OriginalUnit: "count", StartAt: "2024-01-31T08:00:00Z", EndAt: "2024-01-31T08:00:00Z", Source: "Watch", ContentKey: "b3"},
	})
	res, body := do(t, srv, "/v1/series?metric=steps&range_preset=custom&range_from=2024-02-01&range_to=2024-02-04&bucket=day&baseline_rule=previous", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", res.StatusCode, body["error"])
	}
	var current, baseline query.Series
	if err := json.Unmarshal(body["series"], &current); err != nil {
		t.Fatalf("decode series: %v", err)
	}
	if _, ok := body["baseline"]; !ok {
		t.Fatalf("response has no baseline key, want a baseline series")
	}
	if err := json.Unmarshal(body["baseline"], &baseline); err != nil {
		t.Fatalf("decode baseline: %v", err)
	}
	if len(current.Points) != 3 || len(baseline.Points) != 3 {
		t.Fatalf("lengths = current %d / baseline %d, want 3/3", len(current.Points), len(baseline.Points))
	}
	if current.Points[0].Bucket != "2024-02-01" || baseline.Points[0].Bucket != "2024-01-29" {
		t.Errorf("first buckets = %q / %q, want 2024-02-01 / 2024-01-29 (baseline keeps its own date)",
			current.Points[0].Bucket, baseline.Points[0].Bucket)
	}
	if baseline.Points[2].Value != 30 {
		t.Errorf("baseline last value = %v, want 30", baseline.Points[2].Value)
	}
}

// TestSeriesNoBaselineSingleSeries proves the response carries no baseline key
// when the param is absent — the pre-comparison shape, unchanged.
func TestSeriesNoBaselineSingleSeries(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 100, OriginalUnit: "count", StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T08:00:00Z", Source: "Watch", ContentKey: "a"},
	})
	res, body := do(t, srv, "/v1/series?metric=steps&range_preset=custom&range_from=2024-01-01&range_to=2024-01-02&bucket=day", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if _, ok := body["series"]; !ok {
		t.Errorf("response = %v, want a series key", body)
	}
	if _, ok := body["baseline"]; ok {
		t.Errorf("response carries a baseline key without the param, want none")
	}
}

// TestSeriesBaselineNoneIsOff proves the explicit "none" rule the Dashboard
// persists for comparison-off is treated like an absent param: a single series,
// no baseline key — so the SPA can forward the stored rule verbatim.
func TestSeriesBaselineNoneIsOff(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 100, OriginalUnit: "count", StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T08:00:00Z", Source: "Watch", ContentKey: "a"},
	})
	res, body := do(t, srv, "/v1/series?metric=steps&range_preset=custom&range_from=2024-01-01&range_to=2024-01-02&bucket=day&baseline_rule=none", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", res.StatusCode, body["error"])
	}
	if _, ok := body["baseline"]; ok {
		t.Errorf("response carries a baseline key for baseline=none, want none")
	}
}

// TestSeriesBaselineCustomRequiresBounds rejects a custom baseline missing bounds.
func TestSeriesBaselineCustomRequiresBounds(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, body := do(t, srv, "/v1/series?metric=steps&range_preset=custom&range_from=2024-02-01&range_to=2024-02-04&bucket=day&baseline_rule=custom", cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.StatusCode)
	}
	var fields map[string]string
	_ = json.Unmarshal(body["error"], &fields)
	if _, ok := fields["baseline_from"]; !ok {
		t.Errorf("error = %v, want a baseline_from error", fields)
	}
}

// TestSeriesBaselineCustomWindow serves a custom baseline over absolute bounds.
func TestSeriesBaselineCustomWindow(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 100, OriginalUnit: "count", StartAt: "2024-02-01T08:00:00Z", EndAt: "2024-02-01T08:00:00Z", Source: "Watch", ContentKey: "c1"},
		{Metric: "steps", Value: 200, OriginalUnit: "count", StartAt: "2024-02-02T08:00:00Z", EndAt: "2024-02-02T08:00:00Z", Source: "Watch", ContentKey: "c2"},
		{Metric: "steps", Value: 42, OriginalUnit: "count", StartAt: "2023-07-01T08:00:00Z", EndAt: "2023-07-01T08:00:00Z", Source: "Watch", ContentKey: "b1"},
		{Metric: "steps", Value: 43, OriginalUnit: "count", StartAt: "2023-07-02T08:00:00Z", EndAt: "2023-07-02T08:00:00Z", Source: "Watch", ContentKey: "b2"},
	})
	res, body := do(t, srv, "/v1/series?metric=steps&range_preset=custom&range_from=2024-02-01&range_to=2024-02-03&bucket=day&baseline_rule=custom&baseline_from=2023-07-01&baseline_to=2023-07-03", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", res.StatusCode, body["error"])
	}
	var baseline query.Series
	if err := json.Unmarshal(body["baseline"], &baseline); err != nil {
		t.Fatalf("decode baseline: %v", err)
	}
	if len(baseline.Points) != 2 || baseline.Points[0].Bucket != "2023-07-01" || baseline.Points[0].Value != 42 {
		t.Errorf("baseline = %+v, want the custom July 2023 window", baseline.Points)
	}
}

// TestSeriesBaselineAllRangeRejected enforces the ADR 0015 rule: nothing precedes
// the `all` range, so a baseline param with it is a validation error.
func TestSeriesBaselineAllRangeRejected(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, body := do(t, srv, "/v1/series?metric=steps&range_preset=all&bucket=month&baseline_rule=previous", cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.StatusCode)
	}
	var fields map[string]string
	_ = json.Unmarshal(body["error"], &fields)
	if _, ok := fields["baseline"]; !ok {
		t.Errorf("error = %v, want a baseline error for the all range", fields)
	}
}

// TestSeriesBaselineUnknownRule rejects a baseline rule outside the active set.
func TestSeriesBaselineUnknownRule(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, body := do(t, srv, "/v1/series?metric=steps&range_preset=custom&range_from=2024-02-01&range_to=2024-02-04&bucket=day&baseline_rule=weekly", cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.StatusCode)
	}
	var fields map[string]string
	_ = json.Unmarshal(body["error"], &fields)
	if _, ok := fields["baseline_rule"]; !ok {
		t.Errorf("error = %v, want a baseline_rule error", fields)
	}
}

// TestSeriesBaselineDerived proves a derived Metric serves a baseline series too,
// with an empty Source (each operand resolves its own, ADR 0003).
func TestSeriesBaselineDerived(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		// Current day Feb 2.
		{Metric: "dietary_energy", Value: 1800, OriginalUnit: "kcal", StartAt: "2024-02-02T12:00:00Z", EndAt: "2024-02-02T12:00:00Z", Source: "Food", ContentKey: "d1"},
		{Metric: "active_energy", Value: 400, OriginalUnit: "kcal", StartAt: "2024-02-02T18:00:00Z", EndAt: "2024-02-02T18:00:00Z", Source: "Watch", ContentKey: "a1"},
		{Metric: "basal_energy", Value: 1600, OriginalUnit: "kcal", StartAt: "2024-02-02T23:00:00Z", EndAt: "2024-02-02T23:00:00Z", Source: "Watch", ContentKey: "e1"},
		// Previous day Feb 1.
		{Metric: "dietary_energy", Value: 2200, OriginalUnit: "kcal", StartAt: "2024-02-01T12:00:00Z", EndAt: "2024-02-01T12:00:00Z", Source: "Food", ContentKey: "d0"},
		{Metric: "active_energy", Value: 300, OriginalUnit: "kcal", StartAt: "2024-02-01T18:00:00Z", EndAt: "2024-02-01T18:00:00Z", Source: "Watch", ContentKey: "a0"},
		{Metric: "basal_energy", Value: 1500, OriginalUnit: "kcal", StartAt: "2024-02-01T23:00:00Z", EndAt: "2024-02-01T23:00:00Z", Source: "Watch", ContentKey: "e0"},
	})
	res, body := do(t, srv, "/v1/series?metric=calorie_balance&range_preset=custom&range_from=2024-02-02&range_to=2024-02-03&bucket=day&baseline_rule=previous", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", res.StatusCode, body["error"])
	}
	var baseline query.Series
	if err := json.Unmarshal(body["baseline"], &baseline); err != nil {
		t.Fatalf("decode baseline: %v", err)
	}
	if baseline.Source != "" {
		t.Errorf("derived baseline Source = %q, want empty", baseline.Source)
	}
	if len(baseline.Points) != 1 || baseline.Points[0].Value != 400 { // 2200 − 300 − 1500
		t.Errorf("baseline = %+v, want Feb 1 recomputed to 400", baseline.Points)
	}
}

func TestSeriesRelativePreset(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	// No data, but a valid "1y" preset should resolve and return an empty series.
	res, body := do(t, srv, "/v1/series?metric=steps&range_preset=1y&bucket=day", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", res.StatusCode, body["error"])
	}
}

func TestSeriesBucketBelowCapRejected(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, body := do(t, srv, "/v1/series?metric=steps&range_preset=7d&bucket=hour", cookie)
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
		"missing metric":   {"/v1/series?range_preset=7d", "metric"},
		"unknown metric":   {"/v1/series?metric=nope&range_preset=7d", "metric"},
		"missing range":    {"/v1/series?metric=steps", "range_preset"},
		"bad range":        {"/v1/series?metric=steps&range_preset=xyz", "range_preset"},
		"bad custom bound": {"/v1/series?metric=steps&range_preset=custom&range_from=nonsense&range_to=2024-01-02", "range_from"},
		"range too large":  {"/v1/series?metric=steps&range_preset=custom&range_from=2000-01-01&range_to=2024-01-01&bucket=day", "bucket"},
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
