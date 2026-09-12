package api

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gauthier-se/verve/internal/archive"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
	"github.com/gauthier-se/verve/internal/timeaxis"
)

// doRaw is do() for a response that is a file rather than an envelope: the two
// endpoints here answer a zip and a CSV, so nothing is decoded.
func doRaw(t *testing.T, srv *Server, target string, cookies ...*http.Cookie) (*http.Response, []byte) {
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
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return res, body
}

func TestArchiveEndpointStreamsAZip(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 1200, OriginalUnit: "count",
			StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T09:00:00Z", Source: "Watch", ContentKey: "k1"},
		{Metric: "steps", Value: 800, OriginalUnit: "count",
			StartAt: "2024-01-02T08:00:00Z", EndAt: "2024-01-02T09:00:00Z", Source: "Watch", ContentKey: "k2"},
	})

	res, body := doRaw(t, srv, "/v1/export/archive", cookie)

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); got != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", got)
	}
	disposition := res.Header.Get("Content-Disposition")
	if !strings.HasPrefix(disposition, `attachment; filename="verve-`) || !strings.HasSuffix(disposition, `.zip"`) {
		t.Errorf("Content-Disposition = %q, want a dated attachment filename", disposition)
	}

	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("the response is not a readable zip: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{archive.ManifestName, archive.MeasurementsName, archive.StatesName, archive.SessionsName, archive.UnmappedName, archive.ImportsName} {
		if !names[want] {
			t.Errorf("archive is missing %s", want)
		}
	}
	if n := strings.Count(entryBody(t, zr, archive.MeasurementsName), "\n"); n != 2 {
		t.Errorf("measurements.ndjson holds %d rows, want the 2 seeded", n)
	}
}

func TestArchiveEndpointRequiresAuth(t *testing.T) {
	srv, _ := newEmptyServer(t)
	res, _ := doRaw(t, srv, "/v1/export/archive")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without a session", res.StatusCode)
	}
}

// An export is the one read that touches every table at once, so the isolation
// every other handler test asserts (ADR 0007) is worth asserting here on bytes.
func TestArchiveEndpointCarriesOnlyThisAccount(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedAccountWithPassword(t, models, "other@example.com", testPassword)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 1, OriginalUnit: "count",
			StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T09:00:00Z", Source: "Watch", ContentKey: "mine"},
	})
	seedSteps(t, models, "other@example.com", []data.Measurement{
		{Metric: "steps", Value: 2, OriginalUnit: "count",
			StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T09:00:00Z", Source: "Phone", ContentKey: "theirs"},
	})

	_, body := doRaw(t, srv, "/v1/export/archive", cookie)

	if bytes.Contains(body, []byte("theirs")) {
		t.Fatal("the archive carries another Account's row")
	}
	if !bytes.Contains(body, []byte("mine")) {
		// Compressed entries would hide both; assert the fixture is visible so the
		// check above is meaningful rather than vacuously true.
		zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatalf("open archive: %v", err)
		}
		if !strings.Contains(entryBody(t, zr, archive.MeasurementsName), "mine") {
			t.Fatal("the Account's own row is missing from its archive")
		}
	}
}

// The CSV's bytes are its contract: header, order, separators and all. It is
// pinned rather than described, because everything a reader depends on is in
// the exact text.
func TestSeriesCSVIsPinnedToItsBytes(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 1200, OriginalUnit: "count",
			StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T09:00:00Z", Source: "Watch", ContentKey: "k1"},
		{Metric: "steps", Value: 800, OriginalUnit: "count",
			StartAt: "2024-01-01T18:00:00Z", EndAt: "2024-01-01T19:00:00Z", Source: "Watch", ContentKey: "k2"},
		{Metric: "steps", Value: 500, OriginalUnit: "count",
			StartAt: "2024-01-02T08:00:00Z", EndAt: "2024-01-02T09:00:00Z", Source: "Watch", ContentKey: "k3"},
	})

	res, body := doRaw(t, srv,
		"/v1/series.csv?metric=steps&range_preset=custom&range_from=2024-01-01&range_to=2024-01-03&bucket=day", cookie)

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", res.StatusCode, body)
	}
	if got := res.Header.Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	// The window is half-open, so the name ends on the last day the file covers.
	if got := res.Header.Get("Content-Disposition"); got != `attachment; filename="verve-steps-2024-01-01-2024-01-02.csv"` {
		t.Errorf("Content-Disposition = %q", got)
	}

	want := "bucket_start,bucket_end,metric,unit,aggregation,value,count\r\n" +
		"2024-01-01,2024-01-02,steps,count,sum,2000,2\r\n" +
		"2024-01-02,2024-01-03,steps,count,sum,500,1\r\n"
	if string(body) != want {
		t.Errorf("csv =\n%q\nwant\n%q", body, want)
	}
}

// A day with nothing recorded is a day with no row, and above all not a zero:
// in a spreadsheet a zero is a day you did not move, and a gap is a day the
// scale was not stepped on (ADR 0014).
func TestSeriesCSVLeavesAnEmptyDayOutRatherThanZeroing(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "body_mass", Value: 71.5, OriginalUnit: "kg",
			StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T08:00:00Z", Source: "Scale", ContentKey: "k1"},
		{Metric: "body_mass", Value: 71, OriginalUnit: "kg",
			StartAt: "2024-01-03T08:00:00Z", EndAt: "2024-01-03T08:00:00Z", Source: "Scale", ContentKey: "k2"},
	})

	_, body := doRaw(t, srv,
		"/v1/series.csv?metric=body_mass&range_preset=custom&range_from=2024-01-01&range_to=2024-01-04&bucket=day", cookie)

	want := "bucket_start,bucket_end,metric,unit,aggregation,value,count\r\n" +
		"2024-01-01,2024-01-02,body_mass,kg,latest,71.5,1\r\n" +
		"2024-01-03,2024-01-04,body_mass,kg,latest,71,1\r\n"
	if string(body) != want {
		t.Errorf("csv =\n%q\nwant\n%q", body, want)
	}
	if strings.Contains(string(body), "2024-01-02") && !strings.Contains(string(body), "2024-01-01,2024-01-02") {
		t.Error("the empty day got a row of its own")
	}
}

// The empty cell is what a gap Point renders as, wherever one reaches this
// writer. Series is sparse today, so the case is asserted on the function that
// makes the row rather than through an endpoint that cannot produce one.
func TestCSVRowWritesAGapAsAnEmptyCell(t *testing.T) {
	row := csvRows(
		query.Series{Metric: "body_mass", Unit: "kg", Aggregation: "latest"},
		query.Point{Bucket: "2024-01-02", Gap: true},
		timeaxis.Day,
	)
	if len(row) != 1 {
		t.Fatalf("a gap produced %d rows, want 1", len(row))
	}
	want := []string{"2024-01-02", "2024-01-03", "body_mass", "kg", "latest", "", "0"}
	for i := range want {
		if row[0][i] != want[i] {
			t.Fatalf("gap row = %v, want %v", row[0], want)
		}
	}
}

// A Metric read as durations per state has no single figure, so it travels as
// one row per state rather than as a second header shape (ADR 0027).
func TestSeriesCSVSplitsADurationByStateMetric(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	acc, err := models.Accounts.GetByEmail(context.Background(), testEmail)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if _, err := models.States.InsertStateBatch(context.Background(), []data.State{
		{AccountID: acc.ID, Kind: "sleep", StateValue: "asleep_core",
			StartAt: "2024-01-01T23:00:00Z", EndAt: "2024-01-02T02:00:00Z", Source: "Apple Watch", ContentKey: "n1"},
		{AccountID: acc.ID, Kind: "sleep", StateValue: "asleep_deep",
			StartAt: "2024-01-02T02:00:00Z", EndAt: "2024-01-02T04:00:00Z", Source: "Apple Watch", ContentKey: "n2"},
	}); err != nil {
		t.Fatalf("seed states: %v", err)
	}

	_, body := doRaw(t, srv,
		"/v1/series.csv?metric=sleep&range_preset=custom&range_from=2024-01-01&range_to=2024-01-03&bucket=day", cookie)

	got := string(body)
	for _, want := range []string{
		"2024-01-02,2024-01-03,sleep.asleep_core,min,duration_by_state,180",
		"2024-01-02,2024-01-03,sleep.asleep_deep,min,duration_by_state,120",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("csv does not hold %q:\n%s", want, got)
		}
	}
}

func TestSeriesCSVCarriesEveryRequestedMetric(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 1200, OriginalUnit: "count",
			StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T09:00:00Z", Source: "Watch", ContentKey: "k1"},
		{Metric: "body_mass", Value: 71.5, OriginalUnit: "kg",
			StartAt: "2024-01-01T08:00:00Z", EndAt: "2024-01-01T08:00:00Z", Source: "Scale", ContentKey: "k2"},
	})

	res, body := doRaw(t, srv,
		"/v1/series.csv?metric=steps&metric=body_mass&range_preset=custom&range_from=2024-01-01&range_to=2024-01-02&bucket=day", cookie)

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if got := res.Header.Get("Content-Disposition"); !strings.Contains(got, "verve-series-") {
		t.Errorf("Content-Disposition = %q, want the multi-metric name", got)
	}
	want := "bucket_start,bucket_end,metric,unit,aggregation,value,count\r\n" +
		"2024-01-01,2024-01-02,steps,count,sum,1200,1\r\n" +
		"2024-01-01,2024-01-02,body_mass,kg,latest,71.5,1\r\n"
	if string(body) != want {
		t.Errorf("csv =\n%q\nwant\n%q", body, want)
	}
}

// A caller who asked for a comparison and received one window would believe the
// file holds two, so the CSV refuses rather than quietly dropping the baseline.
func TestSeriesCSVRefusesABaseline(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, body := doRaw(t, srv,
		"/v1/series.csv?metric=steps&range_preset=30d&baseline_rule=previous", cookie)

	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.StatusCode)
	}
	if !strings.Contains(string(body), "baseline_rule") {
		t.Errorf("body = %s, want the refusal to name the field", body)
	}
}

// The CSV validates exactly what /v1/series validates, because it parses its
// parameters with the same code.
func TestSeriesCSVValidatesLikeSeries(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	tests := map[string]string{
		"an unknown metric": "/v1/series.csv?metric=not_a_metric&range_preset=30d",
		"no metric at all":  "/v1/series.csv?range_preset=30d",
		"an inverted range": "/v1/series.csv?metric=steps&range_preset=custom&range_from=2024-03-01&range_to=2024-01-01",
	}
	for name, target := range tests {
		t.Run(name, func(t *testing.T) {
			res, _ := doRaw(t, srv, target, cookie)
			if res.StatusCode != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, want 422", res.StatusCode)
			}
		})
	}
}

func TestSeriesCSVRequiresAuth(t *testing.T) {
	srv, _ := newEmptyServer(t)
	res, _ := doRaw(t, srv, "/v1/series.csv?metric=steps&range_preset=30d")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without a session", res.StatusCode)
	}
}

func entryBody(t *testing.T, zr *zip.Reader, name string) string {
	t.Helper()
	f, err := zr.Open(name)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

// A Metric with a breakdown downloads one row per key, which the CSV has done
// since it was written and nothing asserted. Training volume is the second such
// Metric (ADR 0040) and the first one whose keys are an open set, so the
// generalization is worth pinning rather than rediscovering.
func TestSeriesCSVSplitsTrainingByActivity(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	acc, err := models.Accounts.GetByEmail(context.Background(), testEmail)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	distance := 9.0
	for i, w := range []struct {
		activity string
		start    string
		seconds  float64
	}{
		{"running", "2024-01-01T06:00:00Z", 2700},
		{"cycling", "2024-01-01T18:00:00Z", 5400},
	} {
		s := &data.Session{
			AccountID: acc.ID, ActivityType: w.activity, StartAt: w.start, EndAt: w.start,
			Duration: w.seconds, TotalDistance: &distance, Source: "Watch",
			ContentKey: fmt.Sprintf("w%d", i),
		}
		if _, err := models.Sessions.InsertWorkout(context.Background(), s, nil, nil); err != nil {
			t.Fatalf("seed workout: %v", err)
		}
	}

	_, body := doRaw(t, srv,
		"/v1/series.csv?metric=training_time&range_preset=custom&range_from=2024-01-01&range_to=2024-01-02&bucket=day", cookie)

	want := "bucket_start,bucket_end,metric,unit,aggregation,value,count\r\n" +
		"2024-01-01,2024-01-02,training_time.cycling,min,sum_by_state,90,2\r\n" +
		"2024-01-01,2024-01-02,training_time.running,min,sum_by_state,45,2\r\n"
	if string(body) != want {
		t.Errorf("csv =\n%q\nwant\n%q", body, want)
	}
}
