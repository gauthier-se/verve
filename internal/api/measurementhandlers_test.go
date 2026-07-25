package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/data"
)

// send issues a request with an optional JSON body and returns the response with its
// decoded envelope. The read-only `do` helper cannot express POST/DELETE.
func send(t *testing.T, srv *Server, method, target string, body any, cookies ...*http.Cookie) (*http.Response, map[string]json.RawMessage) {
	t.Helper()
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(raw)
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

func decodeMeasurement(t *testing.T, body map[string]json.RawMessage) measurementResponse {
	t.Helper()
	var m measurementResponse
	if err := json.Unmarshal(body["measurement"], &m); err != nil {
		t.Fatalf("decode measurement: %v", err)
	}
	return m
}

func TestCreateMeasurementRoundTrips(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	res, body := send(t, srv, http.MethodPost, "/v1/measurements", map[string]any{
		"metric": "body_mass", "value": 91.2, "measured_at": "2026-07-25T08:00:00Z",
	}, cookie)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %v", res.StatusCode, body)
	}
	m := decodeMeasurement(t, body)
	if m.ID == 0 || m.Value != 91.2 || m.Unit != "kg" || m.Source != catalog.SourceManual {
		t.Fatalf("measurement = %+v, want an id, 91.2 kg, Manual", m)
	}

	res, body = send(t, srv, http.MethodGet, "/v1/measurements?source=manual", nil, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", res.StatusCode)
	}
	var list []measurementResponse
	if err := json.Unmarshal(body["measurements"], &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].ID != m.ID {
		t.Fatalf("list = %+v, want the created row", list)
	}
}

// TestCreateMeasurementIsIdempotent pins the 201-then-200 contract: resubmitting the
// same value is a no-op, not a conflict — nothing went wrong, so a 409 would misdescribe
// it and force clients to special-case a normal retry.
func TestCreateMeasurementIsIdempotent(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	payload := map[string]any{"metric": "height", "value": 184, "measured_at": "2026-07-25T08:00:00Z"}

	res, body := send(t, srv, http.MethodPost, "/v1/measurements", payload, cookie)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("first status = %d, want 201", res.StatusCode)
	}
	first := decodeMeasurement(t, body)

	res, body = send(t, srv, http.MethodPost, "/v1/measurements", payload, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("second status = %d, want 200 (idempotent no-op)", res.StatusCode)
	}
	if got := decodeMeasurement(t, body); got.ID != first.ID {
		t.Fatalf("second id = %d, want the existing %d", got.ID, first.ID)
	}
}

func TestCreateMeasurementRejectsBadInput(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	cases := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"unknown slug", map[string]any{"metric": "not_a_metric", "value": 1}, "metric"},
		{"derived metric", map[string]any{"metric": "calorie_balance", "value": 1}, "metric"},
		{"missing metric", map[string]any{"value": 1}, "metric"},
		{"missing value", map[string]any{"metric": "body_mass"}, "value"},
		{"future date", map[string]any{"metric": "body_mass", "value": 91, "measured_at": "2030-01-01T00:00:00Z"}, "measured_at"},
		{"unparseable date", map[string]any{"metric": "body_mass", "value": 91, "measured_at": "25/07/2026"}, "measured_at"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, body := send(t, srv, http.MethodPost, "/v1/measurements", tc.body, cookie)
			if res.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", res.StatusCode)
			}
			var fields map[string]string
			if err := json.Unmarshal(body["error"], &fields); err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if _, ok := fields[tc.field]; !ok {
				t.Errorf("errors = %v, want a %q entry", fields, tc.field)
			}
		})
	}
}

// TestDeleteMeasurementRefusesImported is the API face of ADR 0022. A 404 would be a
// lie the client could not act on: the row exists and is the Account's own — the
// operation is what is refused.
func TestDeleteMeasurementRefusesImported(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{{
		Metric: "body_mass", Value: 91, OriginalUnit: "kg",
		StartAt: "2026-07-25T08:00:00Z", EndAt: "2026-07-25T08:00:00Z",
		Source: "Zepp Life", ContentKey: "imported-1",
	}})

	acc, err := models.Accounts.GetByEmail(context.Background(), testEmail)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	var id int64
	if err := models.Measurements.DB.QueryRowContext(context.Background(),
		`SELECT id FROM measurements WHERE content_key = 'imported-1'`).Scan(&id); err != nil {
		t.Fatalf("resolve id: %v", err)
	}

	res, _ := send(t, srv, http.MethodDelete, "/v1/measurements/"+itoa(id), nil, cookie)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", res.StatusCode)
	}
	if _, err := models.Measurements.GetByID(context.Background(), acc.ID, id); err != nil {
		t.Fatalf("imported row was deleted anyway: %v", err)
	}
}

func TestDeleteMeasurementRemovesManual(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	_, body := send(t, srv, http.MethodPost, "/v1/measurements", map[string]any{
		"metric": "body_fat_percentage", "value": 0.22,
	}, cookie)
	m := decodeMeasurement(t, body)

	res, _ := send(t, srv, http.MethodDelete, "/v1/measurements/"+itoa(m.ID), nil, cookie)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", res.StatusCode)
	}
	res, _ = send(t, srv, http.MethodDelete, "/v1/measurements/"+itoa(m.ID), nil, cookie)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", res.StatusCode)
	}
}

// TestDeleteMeasurementIsAccountScoped checks another Account's row reads as absent
// rather than forbidden — a 403 would confirm the id exists (ADR 0007).
func TestDeleteMeasurementIsAccountScoped(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedAccountWithPassword(t, models, "other@example.com", testPassword)
	otherCookie := login(t, srv, "other@example.com", testPassword)

	_, body := send(t, srv, http.MethodPost, "/v1/measurements", map[string]any{
		"metric": "body_mass", "value": 70,
	}, otherCookie)
	theirs := decodeMeasurement(t, body)

	res, _ := send(t, srv, http.MethodDelete, "/v1/measurements/"+itoa(theirs.ID), nil, cookie)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (never 403 — that would confirm the id)", res.StatusCode)
	}
}

func TestListMeasurementsRequiresManualSource(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	for _, target := range []string{"/v1/measurements", "/v1/measurements?source=all"} {
		res, _ := send(t, srv, http.MethodGet, target, nil, cookie)
		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s status = %d, want 422", target, res.StatusCode)
		}
	}
}

func TestMeasurementEndpointsRequireAuth(t *testing.T) {
	srv, _, _ := newTestServer(t)

	cases := []struct{ method, target string }{
		{http.MethodPost, "/v1/measurements"},
		{http.MethodGet, "/v1/measurements?source=manual"},
		{http.MethodDelete, "/v1/measurements/1"},
	}
	for _, tc := range cases {
		res, _ := send(t, srv, tc.method, tc.target, nil)
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", tc.method, tc.target, res.StatusCode)
		}
	}
}

// TestManualEntryOverridesSeriesValue is the end-to-end payoff: a typed correction wins
// its own day in /v1/series without erasing the surrounding readings.
func TestManualEntryOverridesSeriesValue(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "body_mass", Value: 91.0, OriginalUnit: "kg", StartAt: "2026-07-23T08:00:00Z", EndAt: "2026-07-23T08:00:00Z", Source: "Zepp Life", ContentKey: "i1"},
		{Metric: "body_mass", Value: 99.9, OriginalUnit: "kg", StartAt: "2026-07-24T08:00:00Z", EndAt: "2026-07-24T08:00:00Z", Source: "Zepp Life", ContentKey: "i2"},
		{Metric: "body_mass", Value: 91.4, OriginalUnit: "kg", StartAt: "2026-07-25T08:00:00Z", EndAt: "2026-07-25T08:00:00Z", Source: "Zepp Life", ContentKey: "i3"},
	})

	res, _ := send(t, srv, http.MethodPost, "/v1/measurements", map[string]any{
		"metric": "body_mass", "value": 91.2, "measured_at": "2026-07-24T09:00:00Z",
	}, cookie)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", res.StatusCode)
	}

	res, body := do(t, srv, "/v1/series?metric=body_mass&range_preset=custom"+
		"&range_from=2026-07-23&range_to=2026-07-26&bucket=day", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("series status = %d, want 200", res.StatusCode)
	}

	// One metric with no Baseline keeps the single-series shape: an object, not an array.
	var series struct {
		Source string `json:"source"`
		Points []struct {
			Bucket string  `json:"bucket"`
			Value  float64 `json:"value"`
		} `json:"points"`
	}
	if err := json.Unmarshal(body["series"], &series); err != nil {
		t.Fatalf("decode series: %v", err)
	}
	// One corrected day does not rename the whole curve.
	if series.Source != "Zepp Life" {
		t.Errorf("source = %q, want %q", series.Source, "Zepp Life")
	}
	got := map[string]float64{}
	for _, p := range series.Points {
		got[p.Bucket] = p.Value
	}
	if len(got) != 3 {
		t.Fatalf("points = %v, want 3 days — the overlay swallowed the others", got)
	}
	if got["2026-07-24"] != 91.2 {
		t.Errorf("2026-07-24 = %v, want the manual 91.2", got["2026-07-24"])
	}
	if got["2026-07-23"] != 91.0 || got["2026-07-25"] != 91.4 {
		t.Errorf("neighbours = %v/%v, want the untouched 91.0/91.4", got["2026-07-23"], got["2026-07-25"])
	}
}
