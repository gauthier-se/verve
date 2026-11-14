package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store"
)

// metricSourcesPath returns the path of the sources of the metric of key.
func metricSourcesPath(key string) string {
	return metricsPath + "/" + key + "/sources"
}

// prioritiesJSON returns the priorities of a response, of the sources ordered, then ignored, as
// {"id", "displayName", "kind"}.
func prioritiesJSON(combine bool, ordered, ignored []store.Source) string {
	items := func(sources []store.Source) string {
		out := "["

		for i, s := range sources {
			if i > 0 {
				out += ","
			}

			out += fmt.Sprintf(`{"id":%d,"displayName":%q,"kind":%q}`, s.ID, s.DisplayName, s.Kind)
		}

		return out + "]"
	}

	return fmt.Sprintf(`{"order":%s,"ignored":%s,"combine":%t}`, items(ordered), items(ignored), combine)
}

// putPriorities sends body as PUT of the sources of the metric of key as alice, and returns the
// response.
func (m metricsTest) putPriorities(t *testing.T, key, body string) *httptest.ResponseRecorder {
	t.Helper()

	return accountRequest(t, m.handler, http.MethodPut, metricSourcesPath(key), body, m.aliceCookie)
}

// Given a session or a token with the read scope, when it asks GET /api/v1/metrics/{type}/sources,
// then it gets the resolved order of the sources of the user that have data of the type or are
// stored for it, the ignored ones and the combine flag. A source missing from the stored order takes
// its place by its kind, and a source without data of the type that is not stored is left out, as
// are the sources of another user. Without stored priorities, the order is the default one.
func TestMetricSources_GetAnswersTheResolvedPrioritiesOfTheSourcesOfTheType(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	steps := m.mustTypeByKey(t, "step_count")
	heart := m.mustTypeByKey(t, "heart_rate")
	phone := m.mustResolveNamedSource(t, m.alice, "iPhone de Camille", store.SourcePhone)
	app := m.mustResolveNamedSource(t, m.alice, "Course", store.SourceApp)
	manual := m.mustResolveNamedSource(t, m.alice, "Santé", store.SourceManual)
	m.mustResolveNamedSource(t, m.alice, "Balance", store.SourceScale)

	m.mustStoreRawSamples(t, m.alice, steps,
		rawSample{source: app, start: sourcesHour, value: 1},
		rawSample{source: phone, start: sourcesHour, value: 2},
		rawSample{source: m.aliceSource, start: sourcesHour, value: 3},
	)
	m.mustStoreRawSamples(t, m.alice, heart,
		rawSample{source: phone, start: sourcesHour, value: 60},
		rawSample{source: m.aliceSource, start: sourcesHour, value: 61},
	)
	m.mustStoreRawSamples(t, m.bob, steps, rawSample{source: m.bobSource, start: sourcesHour, value: 4})
	require.NoError(t, m.store.SourcePriorities().Set(t.Context(), m.alice.ID, steps.ID, store.SourcePriorities{
		Order: []int64{phone.ID}, Ignored: []int64{manual.ID},
	}))

	readToken, _ := m.mustCreateToken(t, m.alice, time.Time{}, store.ScopeRead)

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"session": getWith(t, m.handler, metricSourcesPath("step_count"), m.aliceCookie),
		"token":   getWithAuthorization(t, m.handler, metricSourcesPath("step_count"), "Bearer "+readToken),
	} {
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.JSONEq(t, prioritiesJSON(false, []store.Source{m.aliceSource, phone, app}, []store.Source{manual}),
			rec.Body.String(), name)
	}

	rec := getWith(t, m.handler, metricSourcesPath("heart_rate"), m.aliceCookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, prioritiesJSON(false, []store.Source{m.aliceSource, phone}, nil), rec.Body.String())

	rec = getWith(t, m.handler, metricSourcesPath("body_mass"), m.aliceCookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, prioritiesJSON(false, nil, nil), rec.Body.String(), "a curated type without data")
}

// Given a watch and a phone with steps in the same hour, the watch first by default, when the user
// sends PUT /api/v1/metrics/{type}/sources with the phone first, then the priorities are stored, the
// response and GET answer them resolved, and the type is recomputed in full, so that the phone gives
// the aggregates once the job has run, and the other types are not recomputed.
func TestMetricSources_PutStoresThePrioritiesAndRecomputesTheType(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	steps := m.mustTypeByKey(t, "step_count")
	heart := m.mustTypeByKey(t, "heart_rate")
	phone := m.mustResolveNamedSource(t, m.alice, "iPhone de Camille", store.SourcePhone)
	app := m.mustResolveNamedSource(t, m.alice, "Course", store.SourceApp)

	m.mustStoreRawSamples(t, m.alice, steps,
		rawSample{source: m.aliceSource, start: sourcesHour, value: 100},
		rawSample{source: phone, start: sourcesHour, value: 80},
		rawSample{source: app, start: sourcesHour, value: 50},
	)
	m.mustStoreRawSamples(t, m.alice, heart, rawSample{source: m.aliceSource, start: sourcesHour, value: 60})

	m.mustRecomputeAll(t, m.alice)
	m.mustRunJobs(t)
	require.InDelta(t, 100, m.mustDayValue(t, "step_count", "sum"), 0)

	rec := m.putPriorities(t, "step_count",
		fmt.Sprintf(`{"order":[%d,%d],"ignored":[%d],"combine":false}`, phone.ID, m.aliceSource.ID, app.ID))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	want := prioritiesJSON(false, []store.Source{phone, m.aliceSource}, []store.Source{app})
	require.JSONEq(t, want, rec.Body.String())
	require.JSONEq(t, want, getWith(t, m.handler, metricSourcesPath("step_count"), m.aliceCookie).Body.String())
	require.Equal(t, []int64{steps.ID}, m.dirtyTypes(t, m.alice))

	m.mustRunJobs(t)
	require.InDelta(t, 80, m.mustDayValue(t, "step_count", "sum"), 0)
	require.InDelta(t, 60, m.mustDayValue(t, "heart_rate", "mean"), 0)

	rec = m.putPriorities(t, "step_count", fmt.Sprintf(`{"order":[],"ignored":[%d,%d],"combine":false}`, phone.ID, m.aliceSource.ID))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	m.mustRunJobs(t)
	require.InDelta(t, 50, m.mustDayValue(t, "step_count", "sum"), 0, "only the app is left")
}

// Given a discrete type, when the user sends PUT /api/v1/metrics/{type}/sources with the combine
// mode, then it is stored and answered.
func TestMetricSources_PutSetsTheCombineModeOfADiscreteType(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	heart := m.mustTypeByKey(t, "heart_rate")
	m.mustStoreRawSamples(t, m.alice, heart, rawSample{source: m.aliceSource, start: sourcesHour, value: 60})

	rec := m.putPriorities(t, "heart_rate", `{"order":[],"ignored":[],"combine":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, prioritiesJSON(true, []store.Source{m.aliceSource}, nil), rec.Body.String())

	stored, err := m.store.SourcePriorities().Get(t.Context(), m.alice.ID, heart.ID)
	require.NoError(t, err)
	require.True(t, stored.Combine)
}

// Given priorities that list a source of another user, a source that does not exist or a source
// twice, ordered and ignored included, then PUT /api/v1/metrics/{type}/sources answers 422 with the
// reason, and given the combine mode on a type that is not discrete, 422 combine_not_discrete. The
// priorities stay as they were, and nothing is recomputed.
func TestMetricSources_PutRefusesInvalidPriorities(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	steps := m.mustTypeByKey(t, "step_count")
	phone := m.mustResolveNamedSource(t, m.alice, "iPhone de Camille", store.SourcePhone)
	m.mustStoreRawSamples(t, m.alice, steps, rawSample{source: m.aliceSource, start: sourcesHour, value: 100})

	before := store.SourcePriorities{Order: []int64{phone.ID}}
	require.NoError(t, m.store.SourcePriorities().Set(t.Context(), m.alice.ID, steps.ID, before))

	tests := []struct {
		name string
		body string
		code string
	}{
		{
			name: "source of another user",
			body: fmt.Sprintf(`{"order":[%d],"ignored":[],"combine":false}`, m.bobSource.ID),
			code: "source_unknown",
		},
		{
			name: "source that does not exist",
			body: fmt.Sprintf(`{"order":[],"ignored":[%d],"combine":false}`, phone.ID+1000),
			code: "source_unknown",
		},
		{
			name: "source ordered twice",
			body: fmt.Sprintf(`{"order":[%d,%d],"ignored":[],"combine":false}`, phone.ID, phone.ID),
			code: "source_listed_twice",
		},
		{
			name: "source ordered and ignored",
			body: fmt.Sprintf(`{"order":[%d],"ignored":[%d],"combine":false}`, phone.ID, phone.ID),
			code: "source_listed_twice",
		},
		{
			name: "combine mode on a cumulative type",
			body: fmt.Sprintf(`{"order":[%d],"ignored":[],"combine":true}`, m.aliceSource.ID),
			code: "combine_not_discrete",
		},
	}

	for _, tt := range tests {
		requireProblem(t, m.putPriorities(t, "step_count", tt.body), http.StatusUnprocessableEntity, tt.code)

		stored, err := m.store.SourcePriorities().Get(t.Context(), m.alice.ID, steps.ID)
		require.NoError(t, err, tt.name)
		require.Equal(t, before, stored, tt.name)
		require.Empty(t, m.dirtyTypes(t, m.alice), tt.name)
	}
}

// Given a type that does not exist, or an uncurated type that only another user has samples of, then
// GET and PUT /api/v1/metrics/{type}/sources answer 404, and nothing is stored.
func TestMetricSources_UnknownOrUnlistedTypeIsNotFound(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	theirs := m.mustEnsureUncurated(t, "HKQuantityTypeIdentifierTheirs", "count")
	m.mustStoreRawSamples(t, m.bob, theirs, rawSample{source: m.bobSource, start: sourcesHour, value: 1})

	for _, key := range []string{"no_such_type", theirs.Key} {
		requireProblem(t, getWith(t, m.handler, metricSourcesPath(key), m.aliceCookie), http.StatusNotFound, "not_found")
		requireProblem(t, m.putPriorities(t, key, `{"order":[],"ignored":[],"combine":false}`), http.StatusNotFound, "not_found")
	}

	_, err := m.store.SourcePriorities().Get(t.Context(), m.alice.ID, theirs.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// Given a token, then PUT /api/v1/metrics/{type}/sources answers 401 unauthorized, as every
// operation for sessions only, and GET answers 403 scope_missing to a token without the read scope.
// Given no authentication, then both answer 401.
func TestMetricSources_RequireASessionToChangeAndTheReadScopeToRead(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	readToken, _ := m.mustCreateToken(t, m.alice, time.Time{}, store.ScopeRead, store.ScopeIngest)
	ingestToken, _ := m.mustCreateToken(t, m.alice, time.Time{}, store.ScopeIngest)
	body := `{"order":[],"ignored":[],"combine":false}`

	requireProblem(t, bearerRequest(t, m.handler, http.MethodPut, metricSourcesPath("step_count"), body, readToken),
		http.StatusUnauthorized, "unauthorized")

	status, code := problemCode(t, getWithAuthorization(t, m.handler, metricSourcesPath("step_count"), "Bearer "+ingestToken))
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "scope_missing", code)

	requireProblem(t, getWith(t, m.handler, metricSourcesPath("step_count")), http.StatusUnauthorized, "unauthorized")
	requireProblem(t, accountRequest(t, m.handler, http.MethodPut, metricSourcesPath("step_count"), body),
		http.StatusUnauthorized, "unauthorized")

	steps := m.mustTypeByKey(t, "step_count")
	_, err := m.store.SourcePriorities().Get(t.Context(), m.alice.ID, steps.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}
