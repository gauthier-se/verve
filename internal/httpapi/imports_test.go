package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/store"
)

// importsPath is the path of the import list.
const importsPath = "/api/v1/imports"

// importPath and cancelImportPath return the paths of the import of id and of its cancellation.
func importPath(id int64) string       { return fmt.Sprintf("%s/%d", importsPath, id) }
func cancelImportPath(id int64) string { return fmt.Sprintf("%s/%d/cancel", importsPath, id) }

// zeroCounters is the JSON of total counters that are all at zero.
const zeroCounters = `{"received":0,"inserted":0,"updated":0,"duplicates":0,"blocked":0,"invalid":0,
	"unsupported":0,"suspicious":0,"warnings":0,"deleted":0,"notFound":0}`

// importsTest is an ingestTest with its handler on the real inbox, and bob, another user.
type importsTest struct {
	ingestTest

	handler http.Handler
	bob     store.User
}

func newImportsTest(t *testing.T) importsTest {
	t.Helper()

	it := newIngestTest(t)
	bob := it.mustCreateUser(t, "bob", store.RoleMember)

	return importsTest{
		ingestTest: it,
		handler:    it.handler(t, it.inbox()),
		bob:        bob,
	}
}

// mustRun moves the queued import of id of user to running, as its job does when it starts.
func (it importsTest) mustRun(t *testing.T, user store.User, id int64) store.Import {
	t.Helper()

	imp, err := it.store.Imports().SetStatus(t.Context(), user.ID, id, store.ImportRunning)
	require.NoError(t, err)

	return imp
}

// mustFinish moves the import of id of user to running, then ends it with status.
func (it importsTest) mustFinish(t *testing.T, user store.User, id int64, status store.ImportStatus) store.Import {
	t.Helper()

	it.mustRun(t, user, id)

	imp, err := it.store.Imports().SetStatus(t.Context(), user.ID, id, status)
	require.NoError(t, err)

	return imp
}

// mustCreateForeignImport stores a queued native import of bob.
func (it importsTest) mustCreateForeignImport(t *testing.T) store.Import {
	t.Helper()

	imp, err := it.store.Imports().Create(t.Context(), store.NewImport{UserID: it.bob.ID, Origin: store.OriginNative})
	require.NoError(t, err)

	return imp
}

// mustClaimJob claims the oldest due job, which the tests require to be the job of imp.
func (it importsTest) mustClaimJob(t *testing.T, imp store.Import) store.Job {
	t.Helper()

	job, _, err := it.store.Jobs().Claim(t.Context(), time.Minute)
	require.NoError(t, err)
	require.JSONEq(t, fmt.Sprintf(`{"importId": %d}`, imp.ID), string(job.Payload))

	return job
}

// tokenRequest serves method target on h with the token in Authorization: Bearer, and returns the
// response.
func tokenRequest(t *testing.T, h http.Handler, method, target, token string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), method, "http://example.com"+target, nil)
	r.Header.Set("Authorization", "Bearer "+token)

	return serveWith(h, r)
}

// cancelImport posts the cancellation of the import of id on the handler of it with cookie, as the
// web app does from the phone of the tests.
func (it importsTest) cancelImport(t *testing.T, id int64, cookie string) *httptest.ResponseRecorder {
	t.Helper()

	return accountRequest(t, it.handler, http.MethodPost, cancelImportPath(id), "", cookie)
}

// listedIDs returns the ids of the items of the page of rec, and its next cursor.
func listedIDs(t *testing.T, rec *httptest.ResponseRecorder) ([]int64, string) {
	t.Helper()

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var page struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
		NextCursor string `json:"nextCursor"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))

	ids := make([]int64, 0, len(page.Items))
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}

	return ids, page.NextCursor
}

// Given imports of alice of several origins and statuses, then GET /api/v1/imports answers them
// newest first, each with its origin, client, token name when there is one, status, times and
// total counters only, and never the imports of another user.
func TestImports_ListAnswersTheImportsOfTheUserNewestFirst(t *testing.T) {
	t.Parallel()

	it := newImportsTest(t)
	ctx := t.Context()
	token, record := it.mustCreateToken(t, it.alice, time.Time{}, store.ScopeIngest)

	xml, err := it.store.Imports().Create(ctx, store.NewImport{UserID: it.alice.ID, Origin: store.OriginXML})
	require.NoError(t, err)

	it.clock.Advance(time.Minute)
	it.mustRun(t, it.alice, xml.ID)

	progress := store.ImportProgress{Position: 3, Report: store.ImportReport{
		Total:  store.ImportCounters{Received: 3, Inserted: 2, Invalid: 1},
		Types:  map[string]store.ImportCounters{"heart_rate": {Received: 3, Inserted: 2, Invalid: 1}},
		Errors: []string{"record 2 (HKQuantityTypeIdentifierHeartRate): end before start"},
	}}
	require.NoError(t, it.store.Imports().SaveProgress(ctx, it.alice.ID, xml.ID, progress))

	it.clock.Advance(time.Minute)
	it.mustFinish(t, it.alice, xml.ID, store.ImportCompleted)

	it.clock.Advance(time.Minute)
	pushed, _ := it.mustPush(t, it.handler, ingestPayload, "", token)
	require.Equal(t, "sync", pushed.TokenName)

	it.clock.Advance(time.Minute)
	running, _ := it.mustPush(t, it.handler, ingestPayload, it.aliceCookie, "")
	it.mustRun(t, it.alice, running.ID)
	require.NoError(t, it.store.Imports().SetClient(ctx, it.alice.ID, running.ID,
		map[string]string{"name": "Verve for iOS", "version": "1.0.0"}))

	_, err = it.store.Imports().Create(ctx, store.NewImport{UserID: it.bob.ID, Origin: store.OriginXML})
	require.NoError(t, err)

	rec := getWith(t, it.handler, importsPath, it.aliceCookie)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, fmt.Sprintf(`{"items":[
		{"id":%d,"origin":"native","status":"running","client":{"name":"Verve for iOS","version":"1.0.0"},
		 "createdAt":"2026-11-04T08:04:00Z","startedAt":"2026-11-04T08:04:00Z","report":{"total":%s}},
		{"id":%d,"origin":"native","status":"queued","tokenName":"sync","createdAt":"2026-11-04T08:03:00Z",
		 "report":{"total":%s}},
		{"id":%d,"origin":"xml","status":"completed","createdAt":"2026-11-04T08:00:00Z",
		 "startedAt":"2026-11-04T08:01:00Z","finishedAt":"2026-11-04T08:02:00Z",
		 "report":{"total":{"received":3,"inserted":2,"updated":0,"duplicates":0,"blocked":0,"invalid":1,
		  "unsupported":0,"suspicious":0,"warnings":0,"deleted":0,"notFound":0}}}
	]}`, running.ID, zeroCounters, pushed.ID, zeroCounters, xml.ID), rec.Body.String())

	stored, err := it.store.Imports().Get(ctx, it.alice.ID, pushed.ID)
	require.NoError(t, err)
	require.Equal(t, record.ID, stored.TokenID)
}

// Given more imports than a page, then GET /api/v1/imports answers them in pages that the cursor
// follows, and the status filter keeps the imports of one status. An unknown status answers 422.
func TestImports_ListIsPaginatedAndFilteredByStatus(t *testing.T) {
	t.Parallel()

	it := newImportsTest(t)

	var queued, all []int64

	for i := range 5 {
		it.clock.Advance(time.Second)

		imp, _ := it.mustPush(t, it.handler, ingestPayload, it.aliceCookie, "")
		if i%2 == 0 {
			it.mustFinish(t, it.alice, imp.ID, store.ImportCancelled)
		} else {
			queued = append([]int64{imp.ID}, queued...)
		}

		all = append([]int64{imp.ID}, all...)
	}

	var got []int64

	target := importsPath + "?limit=2"

	for {
		ids, next := listedIDs(t, getWith(t, it.handler, target, it.aliceCookie))
		require.LessOrEqual(t, len(ids), 2)

		got = append(got, ids...)

		if next == "" {
			break
		}

		target = importsPath + "?limit=2&cursor=" + next
	}

	require.Equal(t, all, got)

	ids, next := listedIDs(t, getWith(t, it.handler, importsPath+"?status=queued", it.aliceCookie))
	require.Equal(t, queued, ids)
	require.Empty(t, next)

	ids, _ = listedIDs(t, getWith(t, it.handler, importsPath+"?status=running", it.aliceCookie))
	require.Empty(t, ids)

	rec := getWith(t, it.handler, importsPath+"?status=paused", it.aliceCookie)
	status, code := problemCode(t, rec)
	require.Equal(t, http.StatusUnprocessableEntity, status)
	require.Equal(t, "validation_failed", code)
}

// Given a token with the read scope, then GET /api/v1/imports answers the imports of its user. A
// token with only the ingest scope answers 403 scope_missing, and no session or token 401.
func TestImports_ListAcceptsASessionOrATokenWithTheReadScope(t *testing.T) {
	t.Parallel()

	it := newImportsTest(t)
	imp, _ := it.mustPush(t, it.handler, ingestPayload, it.aliceCookie, "")

	reader, _ := it.mustCreateToken(t, it.alice, time.Time{}, store.ScopeRead)

	ids, _ := listedIDs(t, tokenRequest(t, it.handler, http.MethodGet, importsPath, reader))
	require.Equal(t, []int64{imp.ID}, ids)

	ingester, _ := it.mustCreateToken(t, it.alice, time.Time{}, store.ScopeIngest)

	status, code := problemCode(t, tokenRequest(t, it.handler, http.MethodGet, importsPath, ingester))
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "scope_missing", code)

	rec := getWith(t, it.handler, importsPath)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.JSONEq(t, unauthorizedBody, rec.Body.String())
}

// Given an import with a report, then GET /api/v1/imports/{id} answers it with its counters in
// total, per type key and for the workouts, and its sample error messages, for a session and for a
// token with the ingest or the read scope.
func TestImports_GetAnswersTheImportWithItsWholeReport(t *testing.T) {
	t.Parallel()

	it := newImportsTest(t)
	ctx := t.Context()

	pushed, _ := it.mustPush(t, it.handler, ingestPayload, it.aliceCookie, "")

	it.clock.Advance(time.Minute)
	it.mustRun(t, it.alice, pushed.ID)

	progress := store.ImportProgress{Position: 4, Report: store.ImportReport{
		Total: store.ImportCounters{Received: 4, Inserted: 2, Invalid: 1, Unsupported: 1, Warnings: 1},
		Types: map[string]store.ImportCounters{
			"heart_rate":                            {Received: 2, Inserted: 1, Invalid: 1},
			"HKDataTypeIdentifierElectrocardiogram": {Received: 1, Unsupported: 1},
		},
		Workouts: store.ImportCounters{Received: 1, Inserted: 1, Warnings: 1},
		Errors:   []string{`record 1 (HKQuantityTypeIdentifierHeartRate): unit "bpm" is not a unit of count/min`},
	}}
	require.NoError(t, it.store.Imports().SaveProgress(ctx, it.alice.ID, pushed.ID, progress))

	want := fmt.Sprintf(`{"id":%d,"origin":"native","status":"running","createdAt":"2026-11-04T08:00:00Z",
		"startedAt":"2026-11-04T08:01:00Z","report":{
		 "total":{"received":4,"inserted":2,"updated":0,"duplicates":0,"blocked":0,"invalid":1,"unsupported":1,
		  "suspicious":0,"warnings":1,"deleted":0,"notFound":0},
		 "types":{"heart_rate":{"received":2,"inserted":1,"invalid":1},
		  "HKDataTypeIdentifierElectrocardiogram":{"received":1,"unsupported":1}},
		 "workouts":{"received":1,"inserted":1,"warnings":1},
		 "errors":["record 1 (HKQuantityTypeIdentifierHeartRate): unit \"bpm\" is not a unit of count/min"]}}`,
		pushed.ID)

	rec := getWith(t, it.handler, importPath(pushed.ID), it.aliceCookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, want, rec.Body.String())

	for _, scope := range []store.TokenScope{store.ScopeIngest, store.ScopeRead} {
		token, _ := it.mustCreateToken(t, it.alice, time.Time{}, scope)

		rec := tokenRequest(t, it.handler, http.MethodGet, importPath(pushed.ID), token)
		require.Equal(t, http.StatusOK, rec.Code, "scope %s: %s", scope, rec.Body.String())
		require.JSONEq(t, want, rec.Body.String(), "scope %s", scope)
	}
}

// Given an import of another user, or an unknown id, then GET /api/v1/imports/{id} answers 404
// not_found, for a session as for a token. Without a session or a token, it answers 401.
func TestImports_GetOfAnImportOfAnotherUserAnswers404(t *testing.T) {
	t.Parallel()

	it := newImportsTest(t)
	foreign := it.mustCreateForeignImport(t)
	token, _ := it.mustCreateToken(t, it.alice, time.Time{}, store.ScopeIngest)

	for _, rec := range []*httptest.ResponseRecorder{
		getWith(t, it.handler, importPath(foreign.ID), it.aliceCookie),
		getWith(t, it.handler, importPath(foreign.ID+100), it.aliceCookie),
		tokenRequest(t, it.handler, http.MethodGet, importPath(foreign.ID), token),
	} {
		status, code := problemCode(t, rec)
		require.Equal(t, http.StatusNotFound, status)
		require.Equal(t, "not_found", code)
	}

	require.Equal(t, http.StatusUnauthorized, getWith(t, it.handler, importPath(foreign.ID)).Code)
}

// Given a queued import, when its user posts POST /api/v1/imports/{id}/cancel with their session,
// then 202 with the cancelled import: its job is cancelled at once, import.cancelled is recorded
// with the client of the request and the counters, and its inbox file is deleted.
func TestImports_CancelOfAQueuedImport_CancelsItAndItsJob(t *testing.T) {
	t.Parallel()

	it := newImportsTest(t)
	pushed, stored := it.mustPush(t, it.handler, ingestPayload, it.aliceCookie, "")

	it.clock.Advance(time.Minute)

	rec := it.cancelImport(t, pushed.ID, it.aliceCookie)

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.JSONEq(t, fmt.Sprintf(`{"id":%d,"origin":"native","status":"cancelled","createdAt":"2026-11-04T08:00:00Z",
		"finishedAt":"2026-11-04T08:01:00Z","report":{"total":%s,"types":{},"workouts":{},"errors":[]}}`,
		pushed.ID, zeroCounters), rec.Body.String())

	got, err := it.store.Imports().Get(t.Context(), it.alice.ID, pushed.ID)
	require.NoError(t, err)
	require.Equal(t, store.ImportCancelled, got.Status)

	_, _, err = it.store.Jobs().Claim(t.Context(), time.Minute)
	require.ErrorIs(t, err, store.ErrNotFound, "the job is cancelled, never claimed")

	_, err = os.Stat(filepath.Join(it.inboxDir, stored.InboxFile))
	require.ErrorIs(t, err, os.ErrNotExist, "the inbox file is deleted")

	events, err := it.store.AuditEvents().List(t.Context(), store.AuditEventsFilter{Type: audit.TypeImportCancelled},
		store.PageRequest{Limit: 10})
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, it.alice.ID, events[0].ActorID)
	require.Zero(t, events[0].SubjectID)
	require.Equal(t, "203.0.113.7", events[0].ClientIP)
	require.Equal(t, userAgent, events[0].UserAgent)
	require.JSONEq(t, fmt.Sprintf(`{"id":%d,"origin":"native","counters":%s}`, pushed.ID, zeroCounters),
		string(events[0].Payload))
}

// Given a running import whose job runs, when its user cancels it, then 202 with the import still
// running: the cancellation of its job is requested, and the job sets the final status at the
// next batch boundary. Nothing is recorded yet.
func TestImports_CancelOfARunningImport_RequestsTheCancellationOfItsJob(t *testing.T) {
	t.Parallel()

	it := newImportsTest(t)
	pushed, stored := it.mustPush(t, it.handler, ingestPayload, it.aliceCookie, "")

	job := it.mustClaimJob(t, stored)
	it.mustRun(t, it.alice, pushed.ID)

	rec := it.cancelImport(t, pushed.ID, it.aliceCookie)

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.JSONEq(t, `"running"`, mustField(t, rec.Body.Bytes(), "status"))

	cancelRequested, err := it.store.Jobs().Heartbeat(t.Context(), job.ID, job.Claims, time.Minute)
	require.NoError(t, err)
	require.True(t, cancelRequested, "the job learns of the cancellation at its next heartbeat")

	got, err := it.store.Imports().Get(t.Context(), it.alice.ID, pushed.ID)
	require.NoError(t, err)
	require.Equal(t, store.ImportRunning, got.Status)
	require.Equal(t, []string{stored.InboxFile}, it.inboxFiles(t), "the job removes the file once it ends the import")

	events, err := it.store.AuditEvents().List(t.Context(), store.AuditEventsFilter{Type: audit.TypeImportCancelled},
		store.PageRequest{Limit: 10})
	require.NoError(t, err)
	require.Empty(t, events, "the job records the end of the import")
}

// Given a running import whose job waits for its retry, or whose job is gone, when its user cancels
// it, then no handler can end it: the request cancels it as it does a queued import.
func TestImports_CancelOfARunningImportWithoutAHandler_CancelsIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// prepare leaves the job of imp without a handler.
		prepare func(t *testing.T, it importsTest, imp store.Import)
	}{
		{
			name: "job waiting for its retry",
			prepare: func(t *testing.T, it importsTest, imp store.Import) {
				t.Helper()

				job := it.mustClaimJob(t, imp)
				it.mustRun(t, it.alice, imp.ID)
				require.NoError(t, it.store.Jobs().Retry(t.Context(), job.ID, job.Claims, "disk error", authTestNow.Add(time.Hour)))
			},
		},
		{
			name: "job abandoned",
			prepare: func(t *testing.T, it importsTest, imp store.Import) {
				t.Helper()

				job := it.mustClaimJob(t, imp)
				it.mustRun(t, it.alice, imp.ID)
				require.NoError(t, it.store.Jobs().Fail(t.Context(), job.ID, job.Claims, "lost"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			it := newImportsTest(t)
			pushed, stored := it.mustPush(t, it.handler, ingestPayload, it.aliceCookie, "")
			tt.prepare(t, it, stored)

			rec := it.cancelImport(t, pushed.ID, it.aliceCookie)

			require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
			require.JSONEq(t, `"cancelled"`, mustField(t, rec.Body.Bytes(), "status"))
			require.Empty(t, it.inboxFiles(t), "the inbox file is deleted")

			events, err := it.store.AuditEvents().List(t.Context(), store.AuditEventsFilter{Type: audit.TypeImportCancelled},
				store.PageRequest{Limit: 10})
			require.NoError(t, err)
			require.Len(t, events, 1)
		})
	}
}

// Given a finished import, when its user cancels it, then 409 import_finished and it keeps its
// status. Given an import of another user, then 404 not_found.
func TestImports_CancelOfAFinishedImportAnswers409(t *testing.T) {
	t.Parallel()

	it := newImportsTest(t)

	for _, status := range []store.ImportStatus{store.ImportCompleted, store.ImportFailed, store.ImportCancelled} {
		pushed, _ := it.mustPush(t, it.handler, ingestPayload, it.aliceCookie, "")
		it.mustFinish(t, it.alice, pushed.ID, status)

		got, code := problemCode(t, it.cancelImport(t, pushed.ID, it.aliceCookie))
		require.Equal(t, http.StatusConflict, got, "status %s", status)
		require.Equal(t, "import_finished", code, "status %s", status)

		stored, err := it.store.Imports().Get(t.Context(), it.alice.ID, pushed.ID)
		require.NoError(t, err)
		require.Equal(t, status, stored.Status)
	}

	foreign := it.mustCreateForeignImport(t)

	status, code := problemCode(t, it.cancelImport(t, foreign.ID, it.aliceCookie))
	require.Equal(t, http.StatusNotFound, status)
	require.Equal(t, "not_found", code)

	got, err := it.store.Imports().Get(t.Context(), it.bob.ID, foreign.ID)
	require.NoError(t, err)
	require.Equal(t, store.ImportQueued, got.Status)
}

// Given a token, whatever its scopes, then POST /api/v1/imports/{id}/cancel answers 401
// unauthorized, as every operation for sessions only, and the import stays queued.
func TestImports_CancelWithATokenAnswers401(t *testing.T) {
	t.Parallel()

	it := newImportsTest(t)
	pushed, _ := it.mustPush(t, it.handler, ingestPayload, it.aliceCookie, "")
	token, _ := it.mustCreateToken(t, it.alice, time.Time{}, store.ScopeIngest, store.ScopeRead)

	rec := tokenRequest(t, it.handler, http.MethodPost, cancelImportPath(pushed.ID), token)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.JSONEq(t, unauthorizedBody, rec.Body.String())

	got, err := it.store.Imports().Get(t.Context(), it.alice.ID, pushed.ID)
	require.NoError(t, err)
	require.Equal(t, store.ImportQueued, got.Status)
}
