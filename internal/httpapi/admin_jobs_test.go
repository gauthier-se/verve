package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store"
)

// adminJobsPath is the path of the operation that lists the jobs.
const adminJobsPath = "/api/v1/admin/jobs"

// adminJobsLease is the lease of the jobs that the tests run.
const adminJobsLease = time.Minute

// adminJobsTest is an authTest with the session cookies of a member alice and an admin root, and
// the handler of a server on its store.
type adminJobsTest struct {
	authTest

	aliceCookie, rootCookie string
	handler                 http.Handler
}

// newAdminJobsTest returns an adminJobsTest on a fresh store, at authTestNow.
func newAdminJobsTest(t *testing.T) adminJobsTest {
	t.Helper()

	j := adminJobsTest{authTest: newAuthTest(t)}
	j.aliceCookie = "verve_session=" + j.mustCreateSession(t, j.mustCreateUser(t, "alice", store.RoleMember))
	j.rootCookie = "verve_session=" + j.mustCreateSession(t, j.mustCreateUser(t, "root", store.RoleAdmin))
	j.handler = j.authTest.handler(t, "")

	return j
}

// at sets the clock to minutes after authTestNow.
func (j adminJobsTest) at(minutes float64) {
	j.clock.Set(authTestNow.Add(time.Duration(minutes * float64(time.Minute))))
}

// mustRun stores job running from now, and returns it.
func (j adminJobsTest) mustRun(t *testing.T, job store.NewJob) store.Job {
	t.Helper()

	got, err := j.store.Jobs().EnqueueClaimed(t.Context(), job, adminJobsLease)
	require.NoError(t, err)

	return got
}

// mustFail fails the running job with message as its error.
func (j adminJobsTest) mustFail(t *testing.T, job store.Job, message string) {
	t.Helper()

	require.NoError(t, j.store.Jobs().Fail(t.Context(), job.ID, job.Claims, message))
}

// jobsPage is a page of jobs, as the tests read it.
type jobsPage struct {
	Items []struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	} `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

// ids returns the ids of the jobs of p, in order.
func (p jobsPage) ids() []int64 {
	ids := make([]int64, 0, len(p.Items))
	for _, item := range p.Items {
		ids = append(ids, item.ID)
	}

	return ids
}

// getJobsPage gets target with the cookie of root, checks that it answers 200, and returns the
// page.
func (j adminJobsTest) getJobsPage(t *testing.T, target string) jobsPage {
	t.Helper()

	rec := getWith(t, j.handler, target, j.rootCookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var page jobsPage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))

	return page
}

// mustFailMany fails count new jobs in one transaction, at the same time, and returns them as they
// were running.
func (j adminJobsTest) mustFailMany(t *testing.T, count int) []store.Job {
	t.Helper()

	failed := make([]store.Job, 0, count)

	err := j.store.WithTx(t.Context(), func(ctx context.Context, tx store.Repositories) error {
		for range count {
			job, err := tx.Jobs().EnqueueClaimed(ctx, testJob("backup.run"), adminJobsLease)
			if err != nil {
				return err
			}

			if err := tx.Jobs().Fail(ctx, job.ID, job.Claims, "disk full"); err != nil {
				return err
			}

			failed = append(failed, job)
		}

		return nil
	})
	require.NoError(t, err)

	return failed
}

// testJob returns a job of kind, whose payload names a file.
func testJob(kind string) store.NewJob {
	return store.NewJob{Kind: kind, Payload: json.RawMessage(`{"file":"export.zip"}`)}
}

// Given an admin, then GET /api/v1/admin/jobs?status=failed returns the failed jobs, newest first by
// the time they ended, with their id, kind, status, attempts, last error and times. A job created
// first and failed last comes first, and the jobs of other statuses are left out.
func TestAdminJobs_ListsTheFailedJobsNewestFirst(t *testing.T) {
	t.Parallel()

	j := newAdminJobsTest(t)
	ctx := t.Context()

	backup := j.mustRun(t, testJob("backup.run"))

	j.at(1)
	require.NoError(t, j.store.Jobs().Retry(ctx, backup.ID, backup.Claims, "disk full", j.clock.Now()))

	backup, _, err := j.store.Jobs().Claim(ctx, adminJobsLease)
	require.NoError(t, err)

	j.at(2)
	imported := j.mustRun(t, store.NewJob{Kind: "import.xml", Payload: json.RawMessage(`{}`), UserID: 1, SerialKey: "import:1"})
	_, err = j.store.Jobs().Enqueue(ctx, store.NewJob{Kind: "backup.run", Payload: json.RawMessage(`{}`), RunAt: authTestNow.Add(time.Hour)})
	require.NoError(t, err)

	j.at(3)
	j.mustFail(t, imported, "malformed job payload")

	j.at(4)
	completed := j.mustRun(t, testJob("audit.purge"))
	require.NoError(t, j.store.Jobs().Complete(ctx, completed.ID, completed.Claims))
	j.mustRun(t, testJob("jobs.purge"))

	j.at(5)
	j.mustFail(t, backup, "disk full")

	rec := getWith(t, j.handler, adminJobsPath+"?status=failed", j.rootCookie)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, fmt.Sprintf(`{"items": [
		{
			"id": %d,
			"kind": "backup.run",
			"status": "failed",
			"attempts": 2,
			"lastError": "disk full",
			"createdAt": "2026-11-04T08:00:00Z",
			"finishedAt": "2026-11-04T08:05:00Z"
		},
		{
			"id": %d,
			"kind": "import.xml",
			"status": "failed",
			"attempts": 1,
			"lastError": "malformed job payload",
			"createdAt": "2026-11-04T08:02:00Z",
			"finishedAt": "2026-11-04T08:03:00Z"
		}
	]}`, backup.ID, imported.ID), rec.Body.String())
}

// Given a job of each finished status whose payload names a file, then each status lists its job
// without the payload.
func TestAdminJobs_NeverReturnsThePayload(t *testing.T) {
	t.Parallel()

	j := newAdminJobsTest(t)
	ctx := t.Context()

	completed := j.mustRun(t, testJob("audit.purge"))
	require.NoError(t, j.store.Jobs().Complete(ctx, completed.ID, completed.Claims))

	j.mustFail(t, j.mustRun(t, testJob("backup.run")), "disk full")

	cancelled, err := j.store.Jobs().Enqueue(ctx, testJob("import.xml"))
	require.NoError(t, err)
	require.NoError(t, j.store.Jobs().Cancel(ctx, cancelled.ID))

	for _, status := range []store.JobStatus{store.JobCompleted, store.JobFailed, store.JobCancelled} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()

			rec := getWith(t, j.handler, adminJobsPath+"?status="+string(status), j.rootCookie)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.NotContains(t, rec.Body.String(), "payload")
			require.NotContains(t, rec.Body.String(), "export.zip")

			var page jobsPage
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
			require.Len(t, page.Items, 1)
			require.Equal(t, string(status), page.Items[0].Status)
		})
	}
}

// Given a member, then 403 forbidden. Given no session, then 401 unauthorized.
func TestAdminJobs_MemberIsForbiddenAndNoSessionIsUnauthorized(t *testing.T) {
	t.Parallel()

	j := newAdminJobsTest(t)
	j.mustFail(t, j.mustRun(t, testJob("backup.run")), "disk full")

	rec := getWith(t, j.handler, adminJobsPath+"?status=failed", j.aliceCookie)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"type":"about:blank","title":"Forbidden","status":403,"code":"forbidden"}`, rec.Body.String())

	rec = getWith(t, j.handler, adminJobsPath+"?status=failed")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.JSONEq(t, unauthorizedBody, rec.Body.String())
}

// Given more jobs than the limit, then a page has items and nextCursor: 50 jobs without limit, at
// most 200 with a larger limit, and the last page has no nextCursor.
func TestAdminJobs_PagesHave50JobsByDefaultAndAtMost200(t *testing.T) {
	t.Parallel()

	j := newAdminJobsTest(t)
	j.mustFailMany(t, 201)

	page := j.getJobsPage(t, adminJobsPath+"?status=failed")
	require.Len(t, page.Items, 50)
	require.NotNil(t, page.NextCursor)

	page = j.getJobsPage(t, adminJobsPath+"?status=failed&limit=500")
	require.Len(t, page.Items, 200)
	require.NotNil(t, page.NextCursor)

	page = j.getJobsPage(t, adminJobsPath+"?status=failed&limit=500&cursor="+url.QueryEscape(*page.NextCursor))
	require.Len(t, page.Items, 1)
	require.Nil(t, page.NextCursor)
}

// Given pages followed by their cursor, when jobs fail between two pages, a new one and one created
// before every listed job, then each job is listed once, newest first, and the jobs that failed
// later are not: they ended after the cursor.
func TestAdminJobs_FollowingTheCursorReturnsEachJobOnceWhileJobsFail(t *testing.T) {
	t.Parallel()

	j := newAdminJobsTest(t)
	late := j.mustRun(t, testJob("import.xml"))

	j.at(1)
	failed := j.mustFailMany(t, 3)

	for minutes := range 2 {
		j.at(float64(2 + minutes))
		j.mustFail(t, j.mustRun(t, testJob("backup.run")), "disk full")
	}

	page := j.getJobsPage(t, adminJobsPath+"?status=failed&limit=2")
	ids := page.ids()

	j.at(4)
	j.mustFail(t, late, "handler panicked")
	j.mustFail(t, j.mustRun(t, testJob("backup.run")), "disk full")

	for page.NextCursor != nil {
		page = j.getJobsPage(t, adminJobsPath+"?status=failed&limit=2&cursor="+url.QueryEscape(*page.NextCursor))
		ids = append(ids, page.ids()...)
	}

	require.Len(t, ids, 5)
	require.Equal(t, []int64{failed[2].ID, failed[1].ID, failed[0].ID}, ids[2:])
	require.NotContains(t, ids, late.ID)
}

// Given no status, or a status that is not finished, then 422 validation_failed, with status as the
// invalid field.
func TestAdminJobs_RejectsAMissingOrUnfinishedStatus(t *testing.T) {
	t.Parallel()

	j := newAdminJobsTest(t)

	tests := []struct {
		name, query, wantError string
	}{
		{
			name:      "missing",
			query:     "",
			wantError: `{"message": "required query parameter is missing", "location": "query.status", "value": ""}`,
		},
		{
			name:      "queued",
			query:     "?status=queued",
			wantError: `{"message": "expected value to be one of \"completed, failed, cancelled\"", "location": "query.status", "value": "queued"}`,
		},
		{
			name:      "running",
			query:     "?status=running",
			wantError: `{"message": "expected value to be one of \"completed, failed, cancelled\"", "location": "query.status", "value": "running"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := getWith(t, j.handler, adminJobsPath+tt.query, j.rootCookie)

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			require.JSONEq(t, `{
				"type": "about:blank",
				"title": "Unprocessable Entity",
				"status": 422,
				"detail": "validation failed",
				"code": "validation_failed",
				"errors": [`+tt.wantError+`]
			}`, rec.Body.String())
		})
	}
}

// Given a malformed cursor, then 422 validation_failed, with cursor as the invalid field.
func TestAdminJobs_MalformedCursorReturnsValidationFailed(t *testing.T) {
	t.Parallel()

	j := newAdminJobsTest(t)

	rec := getWith(t, j.handler, adminJobsPath+"?status=failed&cursor=nope", j.rootCookie)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.JSONEq(t, `{
		"type": "about:blank",
		"title": "Unprocessable Entity",
		"status": 422,
		"detail": "validation failed",
		"code": "validation_failed",
		"errors": [{"message": "invalid cursor", "location": "query.cursor", "value": "nope"}]
	}`, rec.Body.String())
}
