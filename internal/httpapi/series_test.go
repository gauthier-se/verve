package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store"
)

// seriesPath returns the path of the series of the metric of key, with query.
func seriesPath(key, query string) string {
	return metricsPath + "/" + key + "/series?" + query
}

// mustWriteDailySums writes, as the daily rows of a cumulative type of user and metricType, the sum
// of sums at each start.
func (m metricsTest) mustWriteDailySums(t *testing.T, user store.User, source store.Source, metricType store.MetricType,
	sums map[time.Time]float64,
) {
	t.Helper()

	rows := make([]store.AggregateRow, 0, len(sums))
	for start, sum := range sums {
		rows = append(rows, store.AggregateRow{
			Start: start, Sum: sum, Count: 1, Selection: store.SelectedSource, SourceID: source.ID, Hours: 1,
		})
	}

	err := m.store.Aggregates().ReplaceDaily(t.Context(), user.ID, metricType.ID,
		time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC), rows)
	require.NoError(t, err)
}

// mustSetTimezone stores name as the timezone of user, without recomputing anything.
func (m metricsTest) mustSetTimezone(t *testing.T, user store.User, name string) {
	t.Helper()

	_, err := m.store.Preferences().SetTimezone(t.Context(), user.ID, name)
	require.NoError(t, err)
}

// seriesBody is the response of a series, as the tests read it.
type seriesBody struct {
	Bucket   string `json:"bucket"`
	Unit     string `json:"unit"`
	Updating bool   `json:"updating"`
	Points   []struct {
		Start string  `json:"start"`
		Value float64 `json:"value"`
	} `json:"points"`
}

// mustReadSeries requires 200 from rec and returns its series.
func mustReadSeries(t *testing.T, rec *httptest.ResponseRecorder) seriesBody {
	t.Helper()

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body seriesBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	return body
}

// Given a session or a token with the read scope, when it asks the series of a metric with from
// and to as local dates of its timezone, a bucket and a statistic, then it gets the bucket used, the
// canonical unit and the points of its days with data in the period, to included, whatever the data
// of another user.
func TestSeries_AnswersTheBucketTheUnitAndThePointsOfThePeriod(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	steps := m.mustTypeByKey(t, "step_count")
	m.mustSetTimezone(t, m.alice, "Europe/Paris")

	// The local midnights of 2026-11-09 to 2026-11-12 in Europe/Paris.
	day := func(n int) time.Time { return time.Date(2026, 11, 8, 23, 0, 0, 0, time.UTC).AddDate(0, 0, n) }
	m.mustWriteDailySums(t, m.alice, m.aliceSource, steps, map[time.Time]float64{
		day(-1): 50, day(0): 1200, day(1): 3400, day(3): 99,
	})
	m.mustWriteDailySums(t, m.bob, m.bobSource, steps, map[time.Time]float64{day(0): 7, day(2): 8})

	readToken, _ := m.mustCreateToken(t, m.alice, time.Time{}, store.ScopeRead)
	path := seriesPath("step_count", "from=2026-11-09&to=2026-11-11&bucket=day&statistic=sum")

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"session": getWith(t, m.handler, path, m.aliceCookie),
		"token":   getWithAuthorization(t, m.handler, path, "Bearer "+readToken),
	} {
		require.JSONEq(t, `{
			"bucket": "day",
			"unit": "count",
			"updating": false,
			"points": [
				{"start": "2026-11-08T23:00:00Z", "value": 1200},
				{"start": "2026-11-09T23:00:00Z", "value": 3400}
			]
		}`, mustReadSeries(t, rec).rawJSON(t), name)
	}
}

// rawJSON returns b as JSON, without the fields the tests ignore.
func (b seriesBody) rawJSON(t *testing.T) string {
	t.Helper()

	raw, err := json.Marshal(b)
	require.NoError(t, err)

	return string(raw)
}

// Given a user without a timezone, then the local dates are read in UTC.
func TestSeries_ReadsTheDatesInUTCWithoutATimezone(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	steps := m.mustTypeByKey(t, "step_count")
	m.mustWriteDailySums(t, m.alice, m.aliceSource, steps, map[time.Time]float64{
		time.Date(2026, 11, 8, 23, 0, 0, 0, time.UTC): 1,
		time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC):  2,
	})

	body := mustReadSeries(t, getWith(t, m.handler,
		seriesPath("step_count", "from=2026-11-09&to=2026-11-09&bucket=day&statistic=sum"), m.aliceCookie))

	require.Len(t, body.Points, 1)
	require.Equal(t, "2026-11-09T00:00:00Z", body.Points[0].Start)
	require.InDelta(t, 2, body.Points[0].Value, 0)
}

// Given auto, then the bucket used is the smallest that yields at most 400 points.
func TestSeries_AutoAnswersTheBucketUsed(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)

	body := mustReadSeries(t, getWith(t, m.handler,
		seriesPath("step_count", "from=2026-01-01&to=2026-12-31&bucket=auto&statistic=sum"), m.aliceCookie))

	require.Equal(t, "day", body.Bucket)
	require.NotNil(t, body.Points)
	require.Empty(t, body.Points)
}

// Given a statistic that the kind of the type does not accept, then 422 statistic_unsupported.
// Given an explicit bucket of more than 5,000 points, then 422 too_many_points. Given an unknown
// bucket or statistic, a malformed date, or to before from, then 422.
func TestSeries_RefusesInvalidQueriesWith422(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)

	for _, tc := range []struct {
		key, query string
		code       string
	}{
		{"heart_rate", "from=2026-11-01&to=2026-11-02&bucket=day&statistic=sum", "statistic_unsupported"},
		{"step_count", "from=2026-11-01&to=2026-11-02&bucket=day&statistic=mean", "statistic_unsupported"},
		{"step_count", "from=2000-01-01&to=2026-11-02&bucket=hour&statistic=sum", "too_many_points"},
		{"step_count", "from=2026-11-01&to=2026-11-02&bucket=decade&statistic=sum", "validation_failed"},
		{"step_count", "from=2026-11-01&to=2026-11-02&bucket=day&statistic=median", "validation_failed"},
		{"step_count", "from=2026-11-01&to=2026-11-02&bucket=day", "validation_failed"},
		{"step_count", "from=2026-11-01T00:00:00Z&to=2026-11-02&bucket=day&statistic=sum", "validation_failed"},
		{"step_count", "to=2026-11-02&bucket=day&statistic=sum", "validation_failed"},
		{"step_count", "from=2026-11-03&to=2026-11-02&bucket=day&statistic=sum", "period_invalid"},
	} {
		rec := getWith(t, m.handler, seriesPath(tc.key, tc.query), m.aliceCookie)
		status, code := problemCode(t, rec)
		require.Equal(t, http.StatusUnprocessableEntity, status, tc.query)
		require.Equal(t, tc.code, code, tc.query)
	}
}

// Given a dirty range of the user and type that overlaps the period, then updating is true. Given
// one of another type, or outside the period, then false.
func TestSeries_UpdatingTellsADirtyRangeOverlapsThePeriod(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	steps := m.mustTypeByKey(t, "step_count")
	heart := m.mustTypeByKey(t, "heart_rate")

	require.NoError(t, m.store.Aggregates().RecordDirty(t.Context(), m.alice.ID, []store.DirtyRange{
		{TypeID: steps.ID, From: time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)},
	}))

	for _, tc := range []struct {
		key, query string
		want       bool
	}{
		{"step_count", "from=2026-11-10&to=2026-11-10&bucket=day&statistic=sum", true},
		{"step_count", "from=2026-11-01&to=2026-11-30&bucket=week&statistic=sum", true},
		{"step_count", "from=2026-11-11&to=2026-11-30&bucket=day&statistic=sum", false},
		{heart.Key, "from=2026-11-10&to=2026-11-10&bucket=day&statistic=mean", false},
	} {
		body := mustReadSeries(t, getWith(t, m.handler, seriesPath(tc.key, tc.query), m.aliceCookie))
		require.Equal(t, tc.want, body.Updating, tc.key+" "+tc.query)
	}
}

// Given a curated type without samples of the user, then no points. Given an uncurated type with
// samples of the user, then its series. Given an uncurated type without samples of the user, or an
// unknown key, then 404.
func TestSeries_AnswersTheTypesListedToTheUser(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	mine := m.mustEnsureUncurated(t, "Mindful Breaths", "count")
	theirs := m.mustEnsureUncurated(t, "Cold Plunges", "count")

	start := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	m.mustStoreSamples(t, m.alice, m.aliceSource, mine, start)
	m.mustStoreSamples(t, m.bob, m.bobSource, theirs, start)

	query := "from=2026-11-01&to=2026-11-30&bucket=day&statistic=mean"

	curated := mustReadSeries(t, getWith(t, m.handler, seriesPath("heart_rate", query), m.aliceCookie))
	require.Equal(t, "count/min", curated.Unit)
	require.NotNil(t, curated.Points)
	require.Empty(t, curated.Points)

	uncurated := mustReadSeries(t, getWith(t, m.handler, seriesPath(mine.Key, query), m.aliceCookie))
	require.Equal(t, "count", uncurated.Unit)

	for _, key := range []string{theirs.Key, "no_such_metric"} {
		status, code := problemCode(t, getWith(t, m.handler, seriesPath(key, query), m.aliceCookie))
		require.Equal(t, http.StatusNotFound, status, key)
		require.Equal(t, "not_found", code, key)
	}
}

// Given a token without the read scope, then 403 scope_missing. Given no authentication, then 401.
func TestSeries_RequiresTheReadScope(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	ingestToken, _ := m.mustCreateToken(t, m.alice, time.Time{}, store.ScopeIngest)
	path := seriesPath("step_count", "from=2026-11-01&to=2026-11-02&bucket=day&statistic=sum")

	status, code := problemCode(t, getWithAuthorization(t, m.handler, path, "Bearer "+ingestToken))
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "scope_missing", code)

	status, _ = problemCode(t, getWith(t, m.handler, path))
	require.Equal(t, http.StatusUnauthorized, status)
}

// Given the API, then its spec describes GET /api/v1/metrics/{type}/series, which accepts a session
// or a token with the read scope.
func TestSeries_IsInTheSpec(t *testing.T) {
	t.Parallel()

	rec := serve(t, apiHandler(), http.MethodGet, "/api/openapi.json")
	require.Equal(t, http.StatusOK, rec.Code)

	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string                `json:"operationId"`
			Security    []map[string][]string `json:"security"`
		} `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &spec))

	operation := spec.Paths[metricsPath+"/{type}/series"]["get"]
	require.Equal(t, "get-metric-series", operation.OperationID)
	require.Equal(t, []map[string][]string{{"sessionCookie": {}}, {"bearerToken": {"read"}}}, operation.Security)
}
