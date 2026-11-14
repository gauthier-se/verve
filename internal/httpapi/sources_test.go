package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/aggregate"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
)

// sourcesPath is the path of the sources.
const sourcesPath = "/api/v1/sources"

// sourcePath returns the path of the source of id.
func sourcePath(id int64) string {
	return sourcesPath + "/" + strconv.FormatInt(id, 10)
}

// sourcesHour is the hour of the samples of the tests of the sources, and sourcesDay its day.
var sourcesHour = time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)

const sourcesDay = "2026-11-10"

// mustRecomputeAll records a range of every type of user and queues their recomputation, as a
// change of timezone does, so that mustRunJobs computes their first aggregates.
func (m metricsTest) mustRecomputeAll(t *testing.T, user store.User) {
	t.Helper()

	err := m.store.WithTx(t.Context(), func(ctx context.Context, tx store.Repositories) error {
		return aggregate.NewRecorder(nil).RecomputeAll(ctx, tx, user.ID)
	})
	require.NoError(t, err)
}

// mustRunJobs runs the queued jobs with the in-process worker and its recompute handler, until none
// is left, and checks that each completed.
func (m metricsTest) mustRunJobs(t *testing.T) {
	t.Helper()

	registry := jobs.NewRegistry()
	aggregate.Register(registry, m.store)

	worker := jobs.NewWorker(m.store, registry, m.clock, slog.New(slog.DiscardHandler), jobs.Options{})

	for {
		claimed, err := worker.Tick(t.Context())
		require.NoError(t, err)

		if !claimed {
			return
		}
	}
}

// mustDayValue reads the series of the metric of key for sourcesDay with statistic, as alice, and
// returns its value, failing unless it has one point.
func (m metricsTest) mustDayValue(t *testing.T, key, statistic string) float64 {
	t.Helper()

	body := mustReadSeries(t, getWith(t, m.handler,
		seriesPath(key, "from="+sourcesDay+"&to="+sourcesDay+"&bucket=day&statistic="+statistic), m.aliceCookie))
	require.Len(t, body.Points, 1, key)

	return body.Points[0].Value
}

// dirtyTypes returns the ids of the types of the dirty ranges of user, sorted, each once.
func (m metricsTest) dirtyTypes(t *testing.T, user store.User) []int64 {
	t.Helper()

	ranges, err := m.store.Aggregates().ListDirty(t.Context(), user.ID)
	require.NoError(t, err)

	var types []int64
	for _, r := range ranges {
		types = append(types, r.TypeID)
	}

	slices.Sort(types)

	return slices.Compact(types)
}

// sourceItem is a source of the response, as the tests read it.
type sourceItem struct {
	ID            int64  `json:"id"`
	DisplayName   string `json:"displayName"`
	Name          string `json:"name"`
	DeviceModel   string `json:"deviceModel"`
	Kind          string `json:"kind"`
	KindSetByUser bool   `json:"kindSetByUser"`
	FirstSeenAt   string `json:"firstSeenAt"`
	LastSeenAt    string `json:"lastSeenAt"`
	SampleCount   int64  `json:"sampleCount"`
}

// mustListSources requires 200 from rec and returns its sources.
func mustListSources(t *testing.T, rec *httptest.ResponseRecorder) []sourceItem {
	t.Helper()

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Items []sourceItem `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotNil(t, body.Items)

	return body.Items
}

// mustPatchSource sends body as PATCH of the source of id as alice, requires 200 and returns the
// source of the response.
func (m metricsTest) mustPatchSource(t *testing.T, id int64, body string) sourceItem {
	t.Helper()

	rec := accountRequest(t, m.handler, http.MethodPatch, sourcePath(id), body, m.aliceCookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var source sourceItem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &source))

	return source
}

// Given a session or a token with the read scope, when it asks GET /api/v1/sources, then it gets the
// sources of the user, sorted by id, with their display name, name, device model, kind and whether
// the user set it, first and last seen, and sample count, zero for a source without samples, and
// never the sources of another user.
func TestSources_ListsTheSourcesOfTheUserWithTheirSampleCount(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	steps := m.mustTypeByKey(t, "step_count")
	heart := m.mustTypeByKey(t, "heart_rate")
	phone := m.mustResolveNamedSource(t, m.alice, "iPhone de Camille", store.SourcePhone)

	m.mustStoreRawSamples(t, m.alice, steps,
		rawSample{source: m.aliceSource, start: sourcesHour, value: 100},
		rawSample{source: m.aliceSource, start: sourcesHour.Add(time.Minute), value: 50},
	)
	m.mustStoreRawSamples(t, m.alice, heart, rawSample{source: m.aliceSource, start: sourcesHour, value: 60})
	m.mustStoreRawSamples(t, m.bob, steps, rawSample{source: m.bobSource, start: sourcesHour, value: 7})

	readToken, _ := m.mustCreateToken(t, m.alice, time.Time{}, store.ScopeRead)

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"session": getWith(t, m.handler, sourcesPath, m.aliceCookie),
		"token":   getWithAuthorization(t, m.handler, sourcesPath, "Bearer "+readToken),
	} {
		require.JSONEq(t, fmt.Sprintf(`{"items": [
			{
				"id": %d, "displayName": "Apple Watch", "name": "Apple Watch", "deviceModel": "Watch", "kind": "watch",
				"kindSetByUser": false, "firstSeenAt": "2026-11-04T08:00:00Z", "lastSeenAt": "2026-11-04T08:00:00Z",
				"sampleCount": 3
			},
			{
				"id": %d, "displayName": "iPhone de Camille", "name": "iPhone de Camille", "deviceModel": "",
				"kind": "phone", "kindSetByUser": false, "firstSeenAt": "2026-11-04T08:00:00Z",
				"lastSeenAt": "2026-11-04T08:00:00Z", "sampleCount": 0
			}
		]}`, m.aliceSource.ID, phone.ID), rec.Body.String(), name)
	}
}

// Given a session, when it sends PATCH /api/v1/sources/{id} with a display name, then the source is
// renamed, the response and the list show the new name, its identity stays, and nothing is
// recomputed.
func TestSources_PatchWithADisplayNameRenamesWithoutRecomputing(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	steps := m.mustTypeByKey(t, "step_count")
	m.mustStoreRawSamples(t, m.alice, steps, rawSample{source: m.aliceSource, start: sourcesHour, value: 100})

	got := m.mustPatchSource(t, m.aliceSource.ID, `{"displayName":"Ma montre"}`)
	require.Equal(t, "Ma montre", got.DisplayName)
	require.Equal(t, "Apple Watch", got.Name)
	require.Equal(t, "watch", got.Kind)
	require.False(t, got.KindSetByUser)
	require.Equal(t, int64(1), got.SampleCount)

	listed := mustListSources(t, getWith(t, m.handler, sourcesPath, m.aliceCookie))
	require.Equal(t, []sourceItem{got}, listed)

	require.Empty(t, m.dirtyTypes(t, m.alice))

	active, err := m.store.Jobs().ActiveJobs(t.Context(), aggregate.SerialKey(m.alice.ID))
	require.NoError(t, err)
	require.Empty(t, active, "no recompute job is queued")
}

// Given a watch and a phone that both have steps and heart rates in the same hour, and stored
// priorities for the heart rate that list neither, when the user sets the kind of the watch to
// manual, then the kind is stored as set by the user, the types the watch has samples of are
// recomputed in full, stored priorities or not, and not the type it has no samples of, so that the
// phone, now first by default, gives the aggregates once the job has run. Setting the same kind
// again recomputes nothing.
func TestSources_PatchWithAKindStoresItAndRecomputesTheTypesOfTheSource(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	steps := m.mustTypeByKey(t, "step_count")
	heart := m.mustTypeByKey(t, "heart_rate")
	energy := m.mustTypeByKey(t, "active_energy_burned")
	phone := m.mustResolveNamedSource(t, m.alice, "iPhone de Camille", store.SourcePhone)

	m.mustStoreRawSamples(t, m.alice, steps,
		rawSample{source: m.aliceSource, start: sourcesHour, value: 100},
		rawSample{source: phone, start: sourcesHour, value: 80},
	)
	m.mustStoreRawSamples(t, m.alice, heart,
		rawSample{source: m.aliceSource, start: sourcesHour, value: 60},
		rawSample{source: phone, start: sourcesHour, value: 70},
	)
	m.mustStoreRawSamples(t, m.alice, energy, rawSample{source: phone, start: sourcesHour, value: 30})
	require.NoError(t, m.store.SourcePriorities().Set(t.Context(), m.alice.ID, heart.ID, store.SourcePriorities{}))

	m.mustRecomputeAll(t, m.alice)
	m.mustRunJobs(t)
	require.InDelta(t, 100, m.mustDayValue(t, "step_count", "sum"), 0)
	require.InDelta(t, 60, m.mustDayValue(t, "heart_rate", "mean"), 0)

	got := m.mustPatchSource(t, m.aliceSource.ID, `{"kind":"manual"}`)
	require.Equal(t, "manual", got.Kind)
	require.True(t, got.KindSetByUser)
	require.Equal(t, "Apple Watch", got.DisplayName)

	require.Equal(t, sortedIDs(heart.ID, steps.ID), m.dirtyTypes(t, m.alice))

	m.mustRunJobs(t)
	require.InDelta(t, 80, m.mustDayValue(t, "step_count", "sum"), 0)
	require.InDelta(t, 70, m.mustDayValue(t, "heart_rate", "mean"), 0)
	require.InDelta(t, 30, m.mustDayValue(t, "active_energy_burned", "sum"), 0)

	again := m.mustPatchSource(t, m.aliceSource.ID, `{"kind":"manual","displayName":"Ma montre"}`)
	require.Equal(t, "manual", again.Kind)
	require.Equal(t, "Ma montre", again.DisplayName)
	require.Empty(t, m.dirtyTypes(t, m.alice), "the kind did not change")
}

// sortedIDs returns ids, sorted.
func sortedIDs(ids ...int64) []int64 {
	slices.Sort(ids)

	return ids
}

// Given an empty display name, a display name of more than 64 characters or an unknown kind, then
// PATCH /api/v1/sources/{id} answers 422 validation_failed and the source is unchanged.
func TestSources_PatchRefusesAnInvalidChange(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)

	for _, body := range []string{
		`{"displayName":""}`,
		fmt.Sprintf(`{"displayName":%q}`, strings.Repeat("a", 65)),
		`{"kind":"tablet"}`,
		`{"displayName":"Ma montre","kind":"tablet"}`,
	} {
		rec := accountRequest(t, m.handler, http.MethodPatch, sourcePath(m.aliceSource.ID), body, m.aliceCookie)
		status, code := problemCode(t, rec)
		require.Equal(t, http.StatusUnprocessableEntity, status, body)
		require.Equal(t, "validation_failed", code, body)
	}

	listed := mustListSources(t, getWith(t, m.handler, sourcesPath, m.aliceCookie))
	require.Len(t, listed, 1)
	require.Equal(t, "Apple Watch", listed[0].DisplayName)
	require.Equal(t, "watch", listed[0].Kind)
	require.False(t, listed[0].KindSetByUser)
}

// Given a source of another user, or an id that no source has, then PATCH /api/v1/sources/{id}
// answers 404 and the source of the other user is unchanged.
func TestSources_PatchOfASourceOfAnotherUserOrUnknownIsNotFound(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)

	for _, id := range []int64{m.bobSource.ID, m.bobSource.ID + 1000} {
		for _, body := range []string{`{"displayName":"Ma montre"}`, `{"kind":"manual"}`} {
			rec := accountRequest(t, m.handler, http.MethodPatch, sourcePath(id), body, m.aliceCookie)
			requireProblem(t, rec, http.StatusNotFound, "not_found")
		}
	}

	got, err := m.store.Sources().Get(t.Context(), m.bob.ID, m.bobSource.ID)
	require.NoError(t, err)
	require.Equal(t, m.bobSource, got)
}

// Given a token, then PATCH /api/v1/sources/{id} answers 401 unauthorized, as every operation for
// sessions only, and GET /api/v1/sources answers 403 scope_missing to a token without the read
// scope. Given no authentication, then both answer 401.
func TestSources_RequireASessionToChangeAndTheReadScopeToList(t *testing.T) {
	t.Parallel()

	m := newMetricsTest(t)
	readToken, _ := m.mustCreateToken(t, m.alice, time.Time{}, store.ScopeRead, store.ScopeIngest)
	ingestToken, _ := m.mustCreateToken(t, m.alice, time.Time{}, store.ScopeIngest)

	rec := bearerRequest(t, m.handler, http.MethodPatch, sourcePath(m.aliceSource.ID), `{"displayName":"Ma montre"}`, readToken)
	requireProblem(t, rec, http.StatusUnauthorized, "unauthorized")

	status, code := problemCode(t, getWithAuthorization(t, m.handler, sourcesPath, "Bearer "+ingestToken))
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "scope_missing", code)

	requireProblem(t, getWith(t, m.handler, sourcesPath), http.StatusUnauthorized, "unauthorized")
	requireProblem(t, accountRequest(t, m.handler, http.MethodPatch, sourcePath(m.aliceSource.ID), `{"displayName":"Ma montre"}`),
		http.StatusUnauthorized, "unauthorized")

	got, err := m.store.Sources().Get(t.Context(), m.alice.ID, m.aliceSource.ID)
	require.NoError(t, err)
	require.Equal(t, "Apple Watch", got.DisplayName)
}
