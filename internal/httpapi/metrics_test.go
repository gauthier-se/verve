package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/store"
)

// metricsPath is the path of the metric types.
const metricsPath = "/api/v1/metrics"

// metricsTest is a server whose metric types are those of the shipped catalog, with the users alice
// and bob, each with a source.
type metricsTest struct {
	authTest

	handler     http.Handler
	alice, bob  store.User
	aliceCookie string
	// aliceSource and bobSource are the sources of the samples of alice and bob.
	aliceSource, bobSource store.Source
}

// newMetricsTest returns a metricsTest on a fresh store, at authTestNow.
func newMetricsTest(t *testing.T) metricsTest {
	t.Helper()

	shipped, err := catalog.Shipped()
	require.NoError(t, err)

	m := metricsTest{authTest: newAuthTest(t)}
	require.NoError(t, m.store.MetricTypes().Sync(t.Context(), shipped))

	m.alice = m.mustCreateUser(t, "alice", store.RoleMember)
	m.bob = m.mustCreateUser(t, "bob", store.RoleMember)
	m.aliceCookie = "verve_session=" + m.mustCreateSession(t, m.alice)
	m.aliceSource = m.mustResolveSource(t, m.alice)
	m.bobSource = m.mustResolveSource(t, m.bob)

	deps := m.deps(t, "")
	deps.Catalog = shipped
	m.handler = NewHandler(deps)

	return m
}

// mustResolveSource returns the Apple Watch of user.
func (m metricsTest) mustResolveSource(t *testing.T, user store.User) store.Source {
	t.Helper()

	source, err := m.store.Sources().Resolve(t.Context(), user.ID, store.SourceInfo{Name: "Apple Watch", DeviceModel: "Watch"},
		store.SourceWatch, authTestNow)
	require.NoError(t, err)

	return source
}

// mustTypeByKey returns the metric type of key.
func (m metricsTest) mustTypeByKey(t *testing.T, key string) store.MetricType {
	t.Helper()

	metricType, err := m.store.MetricTypes().GetByKey(t.Context(), key)
	require.NoError(t, err)

	return metricType
}

// mustEnsureUncurated returns the uncurated quantity type of identifier, in count, creating it.
func (m metricsTest) mustEnsureUncurated(t *testing.T, identifier string) store.MetricType {
	t.Helper()

	metricType, err := m.store.MetricTypes().EnsureUncurated(t.Context(), identifier, catalog.KindQuantity, "count")
	require.NoError(t, err)

	return metricType
}

// mustStoreSamples stores, from source of user, one sample of metricType at each start.
func (m metricsTest) mustStoreSamples(t *testing.T, user store.User, source store.Source, metricType store.MetricType, starts ...time.Time) {
	t.Helper()

	samples := make([]store.NewSample, 0, len(starts))
	for _, start := range starts {
		samples = append(samples, store.NewSample{
			Sample: store.Sample{
				TypeID:     metricType.ID,
				SourceID:   source.ID,
				Start:      start,
				End:        start.Add(time.Minute),
				Value:      1,
				Origin:     store.OriginXML,
				NaturalKey: []byte(metricType.Key + start.Format(time.RFC3339Nano)),
			},
			OnConflict: store.ConflictIgnore,
		})
	}

	_, err := m.store.Samples().Upsert(t.Context(), user.ID, samples)
	require.NoError(t, err)
}

// metricItem is a metric type of the response, as the tests read it.
type metricItem struct {
	Key          string           `json:"key"`
	Identifier   string           `json:"identifier"`
	Kind         string           `json:"kind"`
	Unit         string           `json:"unit"`
	Aggregation  string           `json:"aggregation"`
	Group        string           `json:"group"`
	Curated      bool             `json:"curated"`
	Values       []string         `json:"values"`
	Availability *json.RawMessage `json:"availability"`
}

// mustListMetrics serves GET /api/v1/metrics on m with the cookie of alice, requires 200 and
// returns the metric types by key, failing when a key is listed twice.
func (m metricsTest) mustListMetrics(t *testing.T) map[string]metricItem {
	t.Helper()

	rec := getWith(t, m.handler, metricsPath, m.aliceCookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	return metricsByKey(t, rec)
}

// metricsByKey returns the metric types of the response held by rec, by key, failing when a key is
// listed twice.
func metricsByKey(t *testing.T, rec *httptest.ResponseRecorder) map[string]metricItem {
	t.Helper()

	var body struct {
		Items []metricItem `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	byKey := make(map[string]metricItem, len(body.Items))
	for _, item := range body.Items {
		require.NotContains(t, byKey, item.Key)
		byKey[item.Key] = item
	}

	return byKey
}

// Given a session, then GET /api/v1/metrics answers 200 with every curated type: key, identifier,
// kind, unit, aggregation style, group, curated flag, the keys of its category values, and its
// availability, null without samples.
func TestListMetrics_ListsEveryCuratedType(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)

	got := m.mustListMetrics(t)

	shipped, err := catalog.Shipped()
	require.NoError(t, err)

	keys := make([]string, 0, len(shipped.Types()))
	for _, curated := range shipped.Types() {
		keys = append(keys, curated.Key)
	}

	require.ElementsMatch(t, keys, mapKeys(got))

	require.Equal(t, metricItem{
		Key:         "heart_rate",
		Identifier:  "HKQuantityTypeIdentifierHeartRate",
		Kind:        "quantity",
		Unit:        "count/min",
		Aggregation: "discrete",
		Group:       "heart",
		Curated:     true,
		Values:      []string{},
	}, got["heart_rate"])

	require.Equal(t, metricItem{
		Key:         "sleep_analysis",
		Identifier:  "HKCategoryTypeIdentifierSleepAnalysis",
		Kind:        "category",
		Unit:        "count",
		Aggregation: "discrete",
		Group:       "sleep",
		Curated:     true,
		Values:      []string{"in_bed", "awake", "asleep_core", "asleep_deep", "asleep_rem", "asleep_unspecified"},
	}, got["sleep_analysis"])
}

// mapKeys returns the keys of byKey, in any order.
func mapKeys(byKey map[string]metricItem) []string {
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}

	return keys
}

// Given samples of a type, then its availability holds the first and last start and the count of my
// samples, in RFC 3339 UTC, whatever the samples of another user. Given none, then it is null.
func TestListMetrics_AvailabilityCountsMySamples(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	heartRate := m.mustTypeByKey(t, "heart_rate")
	steps := m.mustTypeByKey(t, "step_count")

	first := time.Date(2025, 3, 1, 8, 0, 0, 0, time.UTC)
	m.mustStoreSamples(t, m.alice, m.aliceSource, heartRate, first.Add(48*time.Hour), first, first.Add(time.Hour))
	m.mustStoreSamples(t, m.bob, m.bobSource, heartRate, first.Add(-24*time.Hour), first.Add(72*time.Hour))
	m.mustStoreSamples(t, m.bob, m.bobSource, steps, first)

	got := m.mustListMetrics(t)

	require.NotNil(t, got["heart_rate"].Availability)
	require.JSONEq(t, `{"firstAt":"2025-03-01T08:00:00Z","lastAt":"2025-03-03T08:00:00Z","count":3}`,
		string(*got["heart_rate"].Availability))
	require.Nil(t, got["step_count"].Availability)
}

// Given an uncurated type with samples of mine, then it is listed in the group Other. Given an
// uncurated type that only another user has samples of, then it is not listed.
func TestListMetrics_ListsTheUncuratedTypesOfMySamplesOnly(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	mine := m.mustEnsureUncurated(t, "Mindful Breaths")
	theirs := m.mustEnsureUncurated(t, "Cold Plunges")
	unused := m.mustEnsureUncurated(t, "Sauna Sessions")

	start := time.Date(2025, 3, 1, 8, 0, 0, 0, time.UTC)
	m.mustStoreSamples(t, m.alice, m.aliceSource, mine, start)
	m.mustStoreSamples(t, m.bob, m.bobSource, theirs, start)

	got := m.mustListMetrics(t)

	require.Contains(t, got, mine.Key)
	require.Equal(t, "other", got[mine.Key].Group)
	require.False(t, got[mine.Key].Curated)
	require.Equal(t, "Mindful Breaths", got[mine.Key].Identifier)
	require.NotContains(t, got, theirs.Key)
	require.NotContains(t, got, unused.Key)
}

// Given a type created by EnsureUncurated and a sample of it, then it is listed, with its key,
// identifier, kind and unit, without code changes.
func TestListMetrics_ListsATypeCreatedOnFirstSight(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	created, err := m.store.MetricTypes().EnsureUncurated(t.Context(), "HKQuantityTypeIdentifierFutureMetric",
		catalog.KindQuantity, "count/min")
	require.NoError(t, err)

	m.mustStoreSamples(t, m.alice, m.aliceSource, created, time.Date(2025, 3, 1, 8, 0, 0, 0, time.UTC))

	got := m.mustListMetrics(t)

	require.Contains(t, got, "future_metric")
	item := got["future_metric"]
	item.Availability = nil
	require.Equal(t, metricItem{
		Key:         "future_metric",
		Identifier:  "HKQuantityTypeIdentifierFutureMetric",
		Kind:        "quantity",
		Unit:        "count/min",
		Aggregation: "discrete",
		Group:       "other",
		Curated:     false,
		Values:      []string{},
	}, item)
}

// Given a token with the read scope, then 200. Given a token with only ingest, then 403
// scope_missing. Given no authentication, then 401.
func TestListMetrics_RequiresTheReadScope(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	readToken, _ := m.mustCreateToken(t, m.alice, time.Time{}, store.ScopeRead)
	ingestToken, _ := m.mustCreateToken(t, m.alice, time.Time{}, store.ScopeIngest)

	rec := getWithAuthorization(t, m.handler, metricsPath, "Bearer "+readToken)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, metricsByKey(t, rec), "heart_rate")

	rec = getWithAuthorization(t, m.handler, metricsPath, "Bearer "+ingestToken)
	status, code := problemCode(t, rec)
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "scope_missing", code)

	rec = getWith(t, m.handler, metricsPath)
	status, _ = problemCode(t, rec)
	require.Equal(t, http.StatusUnauthorized, status)
}

// Given the API, then its spec describes GET /api/v1/metrics, which accepts a session or a token
// with the read scope.
func TestListMetrics_IsInTheSpec(t *testing.T) {
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

	operation := spec.Paths[metricsPath]["get"]
	require.Equal(t, "list-metrics", operation.OperationID)
	require.Equal(t, []map[string][]string{{"sessionCookie": {}}, {"bearerToken": {"read"}}}, operation.Security)
}
