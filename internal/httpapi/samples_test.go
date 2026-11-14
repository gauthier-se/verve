package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store"
)

// samplesPath returns the path of the raw samples of the metric of key, with query.
func samplesPath(key, query string) string {
	return metricsPath + "/" + key + "/samples?" + query
}

// rangeQuery returns the query of the range [from, to), as RFC 3339 instants.
func rangeQuery(from, to time.Time) string {
	return "from=" + url.QueryEscape(from.Format(time.RFC3339)) + "&to=" + url.QueryEscape(to.Format(time.RFC3339))
}

// rawSample is a sample to store in the tests of the raw samples.
type rawSample struct {
	source   store.Source
	start    time.Time
	duration time.Duration
	value    float64
	category string
}

// mustStoreRawSamples stores samples of metricType for user, in order, so that their ids follow it.
func (m metricsTest) mustStoreRawSamples(t *testing.T, user store.User, metricType store.MetricType, samples ...rawSample) {
	t.Helper()

	news := make([]store.NewSample, 0, len(samples))
	for i, s := range samples {
		news = append(news, store.NewSample{
			Sample: store.Sample{
				TypeID:     metricType.ID,
				SourceID:   s.source.ID,
				Start:      s.start,
				End:        s.start.Add(s.duration),
				UTCOffset:  3600,
				Value:      s.value,
				Category:   s.category,
				Origin:     store.OriginXML,
				NaturalKey: []byte(metricType.Key + s.start.Format(time.RFC3339Nano) + strconv.Itoa(i)),
			},
			OnConflict: store.ConflictIgnore,
		})
	}

	_, err := m.store.Samples().Upsert(t.Context(), user.ID, news)
	require.NoError(t, err)
}

// mustResolveNamedSource returns the source of user named name, of kind, creating it.
func (m metricsTest) mustResolveNamedSource(t *testing.T, user store.User, name string, kind store.SourceKind) store.Source {
	t.Helper()

	source, err := m.store.Sources().Resolve(t.Context(), user.ID, store.SourceInfo{Name: name}, kind, authTestNow)
	require.NoError(t, err)

	return source
}

// mustSelectHours writes, as the hourly rows of user and metricType, the selection of the source of
// each start.
func (m metricsTest) mustSelectHours(t *testing.T, user store.User, metricType store.MetricType, selected map[time.Time]store.Source) {
	t.Helper()

	rows := make([]store.AggregateRow, 0, len(selected))
	for start, source := range selected {
		rows = append(rows, store.AggregateRow{
			Start: start, Sum: 1, Count: 1, Min: new(1.0), Max: new(1.0), Last: new(1.0), LastAt: start,
			Selection: store.SelectedSource, SourceID: source.ID,
		})
	}

	err := m.store.Aggregates().ReplaceHourly(t.Context(), user.ID, metricType.ID,
		time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC), rows)
	require.NoError(t, err)
}

// samplesBody is the response of the raw samples, as the tests read it.
type samplesBody struct {
	Unit       string       `json:"unit"`
	Items      []sampleItem `json:"items"`
	NextCursor string       `json:"nextCursor"`
}

// sampleItem is a raw sample of the response, as the tests read it.
type sampleItem struct {
	ID        int64    `json:"id"`
	Start     string   `json:"start"`
	End       string   `json:"end"`
	UTCOffset int      `json:"utcOffset"`
	Value     float64  `json:"value"`
	Min       *float64 `json:"min"`
	Max       *float64 `json:"max"`
	Category  string   `json:"category"`
	Source    struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"source"`
	UsedInAggregates bool `json:"usedInAggregates"`
}

// mustReadSamples requires 200 from rec and returns its samples.
func mustReadSamples(t *testing.T, rec *httptest.ResponseRecorder) samplesBody {
	t.Helper()

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body samplesBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	return body
}

// values returns the values of the samples of b, in order.
func (b samplesBody) values() []float64 {
	values := make([]float64, 0, len(b.Items))
	for _, item := range b.Items {
		values = append(values, item.Value)
	}

	return values
}

// Given a session or a token with the read scope, when it asks the raw samples of a type with from
// and to as RFC 3339 instants, then it gets the samples of the user of that type that start in the
// range, ordered by start then id, with their source, whatever the samples of another type or user.
func TestSamples_AnswersTheSamplesOfTheRangeOrderedByStartThenID(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	heart := m.mustTypeByKey(t, "heart_rate")
	steps := m.mustTypeByKey(t, "step_count")
	at := func(hour, minute int) time.Time { return time.Date(2026, 11, 10, hour, minute, 0, 0, time.UTC) }

	m.mustStoreRawSamples(t, m.alice, heart,
		rawSample{source: m.aliceSource, start: at(9, 0), value: 64},
		rawSample{source: m.aliceSource, start: at(7, 59), value: 1},
		rawSample{source: m.aliceSource, start: at(8, 0), value: 61},
		rawSample{source: m.aliceSource, start: at(8, 0), value: 62},
		rawSample{source: m.aliceSource, start: at(8, 30), duration: time.Minute, value: 63},
		rawSample{source: m.aliceSource, start: at(10, 0), value: 2},
	)
	m.mustStoreRawSamples(t, m.alice, steps, rawSample{source: m.aliceSource, start: at(8, 15), value: 3})
	m.mustStoreRawSamples(t, m.bob, heart, rawSample{source: m.bobSource, start: at(8, 15), value: 4})

	readToken, _ := m.mustCreateToken(t, m.alice, time.Time{}, store.ScopeRead)
	path := samplesPath("heart_rate", rangeQuery(at(8, 0), at(10, 0)))

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"session": getWith(t, m.handler, path, m.aliceCookie),
		"token":   getWithAuthorization(t, m.handler, path, "Bearer "+readToken),
	} {
		body := mustReadSamples(t, rec)
		require.Equal(t, "count/min", body.Unit, name)
		require.Equal(t, []float64{61, 62, 63, 64}, body.values(), name)
		require.Empty(t, body.NextCursor, name)
		require.Less(t, body.Items[0].ID, body.Items[1].ID, name)

		item := body.Items[2]
		require.Equal(t, "2026-11-10T08:30:00Z", item.Start, name)
		require.Equal(t, "2026-11-10T08:31:00Z", item.End, name)
		require.Equal(t, 3600, item.UTCOffset, name)
		require.Nil(t, item.Min, name)
		require.Nil(t, item.Max, name)
		require.Empty(t, item.Category, name)
		require.Equal(t, m.aliceSource.ID, item.Source.ID, name)
		require.Equal(t, "Apple Watch", item.Source.Name, name)
	}
}

// Given more samples than the limit, then the page holds limit samples and a cursor, and following
// the cursors returns every sample once, in order.
func TestSamples_PagesByCursorWhileMoreRemain(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	heart := m.mustTypeByKey(t, "heart_rate")
	from := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)

	// Two samples share each start, so that a page can end between them.
	var samples []rawSample
	for i := range 5 {
		start := from.Add(time.Duration(i) * time.Minute)
		samples = append(samples,
			rawSample{source: m.aliceSource, start: start, value: float64(2 * i)},
			rawSample{source: m.aliceSource, start: start, value: float64(2*i + 1)})
	}

	m.mustStoreRawSamples(t, m.alice, heart, samples...)

	var values []float64

	query := rangeQuery(from, from.Add(time.Hour)) + "&limit=3"
	pages := 0

	for cursor := ""; ; pages++ {
		path := samplesPath("heart_rate", query)
		if cursor != "" {
			path += "&cursor=" + cursor
		}

		body := mustReadSamples(t, getWith(t, m.handler, path, m.aliceCookie))
		values = append(values, body.values()...)

		if body.NextCursor == "" {
			require.Len(t, body.Items, 1)

			break
		}

		require.Len(t, body.Items, 3)
		cursor = body.NextCursor
	}

	require.Equal(t, 3, pages)
	require.Equal(t, []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, values)
}

// Given no limit, then a page holds 500 samples.
func TestSamples_LimitsAPageTo500ByDefault(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	heart := m.mustTypeByKey(t, "heart_rate")
	from := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)

	samples := make([]rawSample, 0, 501)
	for i := range 501 {
		samples = append(samples, rawSample{source: m.aliceSource, start: from.Add(time.Duration(i) * time.Second), value: float64(i)})
	}

	m.mustStoreRawSamples(t, m.alice, heart, samples...)

	body := mustReadSamples(t, getWith(t, m.handler, samplesPath("heart_rate", rangeQuery(from, from.Add(time.Hour))), m.aliceCookie))
	require.Len(t, body.Items, 500)
	require.NotEmpty(t, body.NextCursor)

	body = mustReadSamples(t, getWith(t, m.handler,
		samplesPath("heart_rate", rangeQuery(from, from.Add(time.Hour))+"&limit=5000"), m.aliceCookie))
	require.Len(t, body.Items, 501)
	require.Empty(t, body.NextCursor)
}

// Given a limit out of 1 to 5,000, a malformed instant or cursor, or no from or to, then 422
// validation_failed. Given to not after from, then 422 period_invalid.
func TestSamples_RefusesInvalidQueriesWith422(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	valid := "from=2026-11-10T00:00:00Z&to=2026-11-11T00:00:00Z"

	for _, tc := range []struct {
		query string
		code  string
	}{
		{valid + "&limit=0", "validation_failed"},
		{valid + "&limit=5001", "validation_failed"},
		{valid + "&cursor=not-a-cursor", "validation_failed"},
		{"from=2026-11-10&to=2026-11-11T00:00:00Z", "validation_failed"},
		{"to=2026-11-11T00:00:00Z", "validation_failed"},
		{"from=2026-11-10T00:00:00Z", "validation_failed"},
		{"from=2026-11-11T00:00:00Z&to=2026-11-10T00:00:00Z", "period_invalid"},
		{"from=2026-11-10T00:00:00Z&to=2026-11-10T00:00:00Z", "period_invalid"},
	} {
		status, code := problemCode(t, getWith(t, m.handler, samplesPath("heart_rate", tc.query), m.aliceCookie))
		require.Equal(t, http.StatusUnprocessableEntity, status, tc.query)
		require.Equal(t, tc.code, code, tc.query)
	}
}

// Given a range longer than VERVE_RAW_MAX_DAYS days, then 422 range_too_long. Given a range of
// exactly that many days, then its samples.
func TestSamples_RefusesARangeLongerThanRawMaxDays(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		maxDays int
		to      time.Time
		ok      bool
	}{
		{maxDays: 7, to: from.AddDate(0, 0, 7), ok: true},
		{maxDays: 7, to: from.AddDate(0, 0, 7).Add(time.Millisecond), ok: false},
		{maxDays: 1, to: from.AddDate(0, 0, 1), ok: true},
		{maxDays: 1, to: from.AddDate(0, 0, 2), ok: false},
		{maxDays: 366, to: from.AddDate(0, 0, 366), ok: true},
	} {
		m := newMetricsTestWith(t, func(d *Deps) { d.RawMaxDays = tc.maxDays })
		path := samplesPath("heart_rate", "from="+from.Format(time.RFC3339Nano)+"&to="+tc.to.Format(time.RFC3339Nano))
		rec := getWith(t, m.handler, path, m.aliceCookie)

		if tc.ok {
			require.Equal(t, http.StatusOK, rec.Code, path)

			continue
		}

		status, code := problemCode(t, rec)
		require.Equal(t, http.StatusUnprocessableEntity, status, path)
		require.Equal(t, "range_too_long", code, path)
	}
}

// Given maxPoints on a quantity type, then the whole range is returned downsampled to at most that
// many points, the first and last kept, without a cursor, even beyond the default limit. Given
// maxPoints of at least the number of samples, then every sample.
func TestSamples_MaxPointsDownsamplesTheWholeRange(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	heart := m.mustTypeByKey(t, "heart_rate")
	from := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)

	samples := make([]rawSample, 0, 600)
	for i := range 600 {
		samples = append(samples, rawSample{source: m.aliceSource, start: from.Add(time.Duration(i) * time.Minute), value: float64(60 + i%13)})
	}

	m.mustStoreRawSamples(t, m.alice, heart, samples...)

	query := rangeQuery(from, from.AddDate(0, 0, 1))

	body := mustReadSamples(t, getWith(t, m.handler, samplesPath("heart_rate", query+"&maxPoints=550&limit=10"), m.aliceCookie))
	require.Len(t, body.Items, 550)
	require.Empty(t, body.NextCursor)
	require.Equal(t, "2026-11-10T00:00:00Z", body.Items[0].Start)
	require.Equal(t, "2026-11-10T09:59:00Z", body.Items[549].Start)

	for i := 1; i < len(body.Items); i++ {
		require.Less(t, body.Items[i-1].Start, body.Items[i].Start)
	}

	body = mustReadSamples(t, getWith(t, m.handler, samplesPath("heart_rate", query+"&maxPoints=2"), m.aliceCookie))
	require.Equal(t, []float64{60, 60 + 599%13}, body.values())

	body = mustReadSamples(t, getWith(t, m.handler, samplesPath("heart_rate", query+"&maxPoints=5000"), m.aliceCookie))
	require.Len(t, body.Items, 600)
}

// Given maxPoints with a cursor, then 422 max_points_with_cursor. Given maxPoints on a category type,
// then 422 max_points_unsupported. Given maxPoints out of 2 to 5,000, then 422 validation_failed.
func TestSamples_RefusesMaxPointsWithACursorOrOnACategoryType(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	query := "from=2026-11-10T00:00:00Z&to=2026-11-11T00:00:00Z"
	cursor := encodeCursor(store.Cursor{At: time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC), ID: 1})

	for _, tc := range []struct {
		key, query string
		code       string
	}{
		{"heart_rate", query + "&maxPoints=100&cursor=" + cursor, "max_points_with_cursor"},
		{"sleep_analysis", query + "&maxPoints=100", "max_points_unsupported"},
		{"heart_rate", query + "&maxPoints=1", "validation_failed"},
		{"heart_rate", query + "&maxPoints=5001", "validation_failed"},
	} {
		status, code := problemCode(t, getWith(t, m.handler, samplesPath(tc.key, tc.query), m.aliceCookie))
		require.Equal(t, http.StatusUnprocessableEntity, status, tc.query)
		require.Equal(t, tc.code, code, tc.query)
	}
}

// Given a category type, then its samples carry their category value.
func TestSamples_AnswersTheCategoryOfACategorySample(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	sleep := m.mustTypeByKey(t, "sleep_analysis")
	start := time.Date(2026, 11, 10, 1, 0, 0, 0, time.UTC)
	m.mustStoreRawSamples(t, m.alice, sleep, rawSample{source: m.aliceSource, start: start, duration: time.Hour, value: 1, category: "asleep_core"})

	body := mustReadSamples(t, getWith(t, m.handler, samplesPath("sleep_analysis", rangeQuery(start, start.Add(time.Hour))), m.aliceCookie))
	require.Len(t, body.Items, 1)
	require.Equal(t, "asleep_core", body.Items[0].Category)
}

// Given a sample whose source is the one selected for its local hour, in the timezone of the user,
// then usedInAggregates is true. Given a sample of another source, of an ignored source, or of an
// hour without aggregates yet, then false.
func TestSamples_UsedInAggregatesTellsTheSourceSelectedForTheLocalHour(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	heart := m.mustTypeByKey(t, "heart_rate")
	phone := m.mustResolveNamedSource(t, m.alice, "iPhone", store.SourcePhone)
	ring := m.mustResolveNamedSource(t, m.alice, "Ring", store.SourceRing)

	// Asia/Kolkata is UTC+5:30: its local hours start at half past the UTC hours.
	m.mustSetTimezone(t, m.alice, "Asia/Kolkata")
	require.NoError(t, m.store.SourcePriorities().Set(t.Context(), m.alice.ID, heart.ID, store.SourcePriorities{
		Ignored: []int64{ring.ID},
	}))

	at := func(hour, minute int) time.Time { return time.Date(2026, 11, 10, hour, minute, 0, 0, time.UTC) }
	m.mustSelectHours(t, m.alice, heart, map[time.Time]store.Source{
		at(7, 30): m.aliceSource,
		at(8, 30): phone,
	})

	m.mustStoreRawSamples(t, m.alice, heart,
		rawSample{source: m.aliceSource, start: at(8, 10), value: 1},
		rawSample{source: phone, start: at(8, 20), value: 2},
		rawSample{source: ring, start: at(8, 25), value: 3},
		rawSample{source: m.aliceSource, start: at(8, 40), value: 4},
		rawSample{source: phone, start: at(8, 50), value: 5},
		rawSample{source: m.aliceSource, start: at(9, 40), value: 6},
	)

	body := mustReadSamples(t, getWith(t, m.handler, samplesPath("heart_rate", rangeQuery(at(8, 0), at(10, 0))), m.aliceCookie))
	require.Equal(t, []float64{1, 2, 3, 4, 5, 6}, body.values())

	used := make([]bool, 0, len(body.Items))
	for _, item := range body.Items {
		used = append(used, item.UsedInAggregates)
	}

	require.Equal(t, []bool{true, false, false, false, true, false}, used)
}

// Given a type in combine mode, then usedInAggregates is true for every source that is not ignored,
// whatever the hourly rows.
func TestSamples_UsedInAggregatesInCombineModeTellsTheSourcesNotIgnored(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	heart := m.mustTypeByKey(t, "heart_rate")
	phone := m.mustResolveNamedSource(t, m.alice, "iPhone", store.SourcePhone)
	ring := m.mustResolveNamedSource(t, m.alice, "Ring", store.SourceRing)

	require.NoError(t, m.store.SourcePriorities().Set(t.Context(), m.alice.ID, heart.ID, store.SourcePriorities{
		Ignored: []int64{phone.ID}, Combine: true,
	}))

	start := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	m.mustStoreRawSamples(t, m.alice, heart,
		rawSample{source: m.aliceSource, start: start, value: 1},
		rawSample{source: phone, start: start.Add(time.Minute), value: 2},
		rawSample{source: ring, start: start.Add(2 * time.Minute), value: 3},
	)

	body := mustReadSamples(t, getWith(t, m.handler, samplesPath("heart_rate", rangeQuery(start, start.Add(time.Hour))), m.aliceCookie))
	require.Len(t, body.Items, 3)
	require.True(t, body.Items[0].UsedInAggregates)
	require.False(t, body.Items[1].UsedInAggregates)
	require.True(t, body.Items[2].UsedInAggregates)
}

// Given a curated type without samples of the user, then no samples. Given an uncurated type without
// samples of the user, or an unknown key, then 404.
func TestSamples_AnswersTheTypesListedToTheUser(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	mine := m.mustEnsureUncurated(t, "Mindful Breaths", "count")
	theirs := m.mustEnsureUncurated(t, "Cold Plunges", "count")

	start := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	m.mustStoreSamples(t, m.alice, m.aliceSource, mine, start)
	m.mustStoreSamples(t, m.bob, m.bobSource, theirs, start)

	query := rangeQuery(start, start.Add(time.Hour))

	curated := mustReadSamples(t, getWith(t, m.handler, samplesPath("heart_rate", query), m.aliceCookie))
	require.NotNil(t, curated.Items)
	require.Empty(t, curated.Items)

	uncurated := mustReadSamples(t, getWith(t, m.handler, samplesPath(mine.Key, query), m.aliceCookie))
	require.Len(t, uncurated.Items, 1)

	for _, key := range []string{theirs.Key, "no_such_metric"} {
		status, code := problemCode(t, getWith(t, m.handler, samplesPath(key, query), m.aliceCookie))
		require.Equal(t, http.StatusNotFound, status, key)
		require.Equal(t, "not_found", code, key)
	}
}

// Given a token without the read scope, then 403 scope_missing. Given no authentication, then 401.
func TestSamples_RequiresTheReadScope(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	ingestToken, _ := m.mustCreateToken(t, m.alice, time.Time{}, store.ScopeIngest)
	path := samplesPath("heart_rate", "from=2026-11-10T00:00:00Z&to=2026-11-11T00:00:00Z")

	status, code := problemCode(t, getWithAuthorization(t, m.handler, path, "Bearer "+ingestToken))
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "scope_missing", code)

	status, _ = problemCode(t, getWith(t, m.handler, path))
	require.Equal(t, http.StatusUnauthorized, status)
}

// Given the API, then its spec describes GET /api/v1/metrics/{type}/samples, which accepts a session
// or a token with the read scope.
func TestSamples_IsInTheSpec(t *testing.T) {
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

	operation := spec.Paths[metricsPath+"/{type}/samples"]["get"]
	require.Equal(t, "list-metric-samples", operation.OperationID)
	require.Equal(t, []map[string][]string{{"sessionCookie": {}}, {"bearerToken": {"read"}}}, operation.Security)
}
