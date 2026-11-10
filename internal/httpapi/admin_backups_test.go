package httpapi

import (
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/backup"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
)

// adminBackupsPath is the path of the operations that list and create the backups.
const adminBackupsPath = "/api/v1/admin/backups"

// downloadPath returns the path of the download of the backup of name, which is not escaped.
func downloadPath(name string) string { return adminBackupsPath + "/" + name + "/download" }

// testArchive is the name of an archive of the tests.
const testArchive = "verve-1.2.3-20261110T140305Z-manual.tar.zst"

// adminBackupsTest is an authTest with the session cookies of a member alice and an admin root, the
// backups of its database in a directory of their own, and the handler of a server on both.
type adminBackupsTest struct {
	authTest

	root                    store.User
	aliceCookie, rootCookie string
	// dir is the backups directory.
	dir     string
	backups *backup.Backups
	handler http.Handler
}

// newAdminBackupsTest returns an adminBackupsTest on a fresh store and an empty backups directory,
// at authTestNow.
func newAdminBackupsTest(t *testing.T) adminBackupsTest {
	t.Helper()

	b := adminBackupsTest{authTest: newAuthTest(t)}
	b.root = b.mustCreateUser(t, "root", store.RoleAdmin)
	b.rootCookie = "verve_session=" + b.mustCreateSession(t, b.root)
	b.aliceCookie = "verve_session=" + b.mustCreateSession(t, b.mustCreateUser(t, "alice", store.RoleMember))

	b.dir = filepath.Join(t.TempDir(), "backups")
	require.NoError(t, os.Mkdir(b.dir, 0o700))

	b.backups = backup.New(backup.Config{
		Dir:       b.dir,
		DB:        b.dbPath,
		Version:   "1.2.3",
		FreeSpace: func(string) (uint64, error) { return math.MaxUint64, nil },
	}, b.clock)

	deps := b.deps(t, "")
	deps.Backups = b.backups
	b.handler = NewHandler(deps)

	return b
}

// mustWriteFile writes content to the file of name in the backups directory.
func (b adminBackupsTest) mustWriteFile(t *testing.T, name, content string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(b.dir, name), []byte(content), 0o600))
}

// mustRunJob runs the next queued job with the handlers of the backups, and checks that it
// completed.
func (b adminBackupsTest) mustRunJob(t *testing.T, id int64) {
	t.Helper()

	registry := jobs.NewRegistry()
	backup.Register(registry, b.backups, b.store, 7)

	worker := jobs.NewWorker(b.store, registry, b.clock, slog.New(slog.DiscardHandler), jobs.Options{})

	claimed, err := worker.Tick(t.Context())
	require.NoError(t, err)
	require.True(t, claimed)

	job, err := b.store.Jobs().Get(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, store.JobCompleted, job.Status, job.LastError)
}

// auditEvents returns the events of type.
func (b adminBackupsTest) auditEvents(t *testing.T, eventType string) []store.AuditEvent {
	t.Helper()

	events, err := b.store.AuditEvents().List(t.Context(), store.AuditEventsFilter{Type: eventType},
		store.PageRequest{Limit: 10})
	require.NoError(t, err)

	return events
}

// download gets the download of the backup of name from the phone, with the cookies, and returns
// the response.
func (b adminBackupsTest) download(t *testing.T, name string, cookies ...string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, downloadPath(name), nil)
	r.RemoteAddr = phoneAddr
	r.Header.Set("User-Agent", userAgent)

	return serveWith(b.handler, r, cookies...)
}

// Given an admin, then GET /api/v1/admin/backups returns the name, date, reason and size of each
// backup, newest first by the date of its name, and the next scheduled run. The files and
// directories that are not archives are left out.
func TestAdminBackups_ListsTheBackupsAndTheNextScheduledRun(t *testing.T) {
	t.Parallel()

	b := newAdminBackupsTest(t)

	b.mustWriteFile(t, "verve-1.2.3-20261108T030000Z-scheduled.tar.zst", "sc")
	b.mustWriteFile(t, "verve-1.2.4-20261110T140305Z-manual.tar.zst", "manual!")
	b.mustWriteFile(t, "verve-1.2.3-20261109T120000Z-pre-migration.tar.zst", "pre")
	b.mustWriteFile(t, "notes.txt", "not an archive")
	b.mustWriteFile(t, ".verve-backup.lock", "")
	require.NoError(t, os.Mkdir(filepath.Join(b.dir, "verve-1.2.3-20261111T000000Z-manual.tar.zst"), 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(b.dir, ".verve-backup-123"), 0o700))

	require.NoError(t, b.store.Jobs().SetSchedule(t.Context(), backup.KindRun, time.Date(2026, 11, 11, 3, 0, 0, 0, time.UTC)))

	rec := getWith(t, b.handler, adminBackupsPath, b.rootCookie)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{
		"items": [
			{"name": "verve-1.2.4-20261110T140305Z-manual.tar.zst", "createdAt": "2026-11-10T14:03:05Z", "reason": "manual", "size": 7},
			{"name": "verve-1.2.3-20261109T120000Z-pre-migration.tar.zst", "createdAt": "2026-11-09T12:00:00Z", "reason": "pre-migration", "size": 3},
			{"name": "verve-1.2.3-20261108T030000Z-scheduled.tar.zst", "createdAt": "2026-11-08T03:00:00Z", "reason": "scheduled", "size": 2}
		],
		"nextRun": "2026-11-11T03:00:00Z"
	}`, rec.Body.String())
}

// Given no backup and scheduled backups turned off, then GET /api/v1/admin/backups returns an empty
// list and leaves the next run out.
func TestAdminBackups_NoBackupAndNoScheduleListsNothing(t *testing.T) {
	t.Parallel()

	b := newAdminBackupsTest(t)

	rec := getWith(t, b.handler, adminBackupsPath, b.rootCookie)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"items": []}`, rec.Body.String())
}

// Given POST /api/v1/admin/backups, then a manual backup job is enqueued and the answer is 202 with
// its job id. Once the job ran, the manual backup is listed, and its creation is recorded with the
// admin as actor.
func TestAdminBackups_CreateEnqueuesAManualBackupJob(t *testing.T) {
	t.Parallel()

	b := newAdminBackupsTest(t)

	rec := accountRequest(t, b.handler, http.MethodPost, adminBackupsPath, "", b.rootCookie)

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var created struct {
		JobID int64 `json:"jobId"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	job, err := b.store.Jobs().Get(t.Context(), created.JobID)
	require.NoError(t, err)
	require.Equal(t, store.JobQueued, job.Status)

	require.Empty(t, b.auditEvents(t, audit.TypeBackupCreated), "nothing is recorded before the backup exists")

	b.mustRunJob(t, created.JobID)

	name := "verve-1.2.3-20261104T080000Z-manual.tar.zst"

	rec = getWith(t, b.handler, adminBackupsPath, b.rootCookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var list struct {
		Items []struct {
			Name   string `json:"name"`
			Reason string `json:"reason"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list.Items, 1)
	require.Equal(t, name, list.Items[0].Name)
	require.Equal(t, "manual", list.Items[0].Reason)

	events := b.auditEvents(t, audit.TypeBackupCreated)
	require.Len(t, events, 1)
	require.Equal(t, b.root.ID, events[0].ActorID)
	require.Zero(t, events[0].SubjectID)
	require.JSONEq(t, `{"reason":"manual","file":"`+name+`"}`, string(events[0].Payload))
}

// createdJobID checks that rec answers 202 to POST /api/v1/admin/backups, and returns its job id.
func createdJobID(t *testing.T, rec *httptest.ResponseRecorder) int64 {
	t.Helper()

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	var created struct {
		JobID int64 `json:"jobId"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	return created.JobID
}

// createBackup serves POST /api/v1/admin/backups as root, checks that it answers 202, and returns
// its job id.
func (b adminBackupsTest) createBackup(t *testing.T) int64 {
	t.Helper()

	return createdJobID(t, accountRequest(t, b.handler, http.MethodPost, adminBackupsPath, "", b.rootCookie))
}

// mustClaim claims the next queued job, checks that it is the job of id, and returns it running.
func (b adminBackupsTest) mustClaim(t *testing.T, id int64) store.Job {
	t.Helper()

	job, _, err := b.store.Jobs().Claim(t.Context(), time.Minute)
	require.NoError(t, err)
	require.Equal(t, id, job.ID)

	return job
}

// requireOnlyJob checks that the job of id is the only one queued or running. It claims the job if
// it is queued, completes it, then checks that no other job is left to claim.
func (b adminBackupsTest) requireOnlyJob(t *testing.T, id int64) {
	t.Helper()

	ctx := t.Context()

	job, err := b.store.Jobs().Get(ctx, id)
	require.NoError(t, err)

	if job.Status == store.JobQueued {
		job = b.mustClaim(t, id)
	}

	require.Equal(t, store.JobRunning, job.Status)
	require.NoError(t, b.store.Jobs().Complete(ctx, job.ID, job.Claims))

	_, _, err = b.store.Jobs().Claim(ctx, time.Minute)
	require.ErrorIs(t, err, store.ErrNotFound, "no other job is queued")
}

// Given a backup.create job queued or running, when an admin calls POST /api/v1/admin/backups, then
// the answer is 202 with the id of that job, and no job is queued.
func TestAdminBackups_CreateWhileABackupIsActiveAnswersItsJob(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// prepare brings the job of id to its state.
		prepare func(t *testing.T, b adminBackupsTest, id int64)
	}{
		{name: "Queued", prepare: func(*testing.T, adminBackupsTest, int64) {}},
		{name: "Running", prepare: func(t *testing.T, b adminBackupsTest, id int64) {
			t.Helper()
			b.mustClaim(t, id)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := newAdminBackupsTest(t)

			first := b.createBackup(t)
			tt.prepare(t, b, first)

			require.Equal(t, first, b.createBackup(t))
			b.requireOnlyJob(t, first)
		})
	}
}

// Given that the last backup.create job has ended (completed, failed or cancelled), when an admin
// calls POST /api/v1/admin/backups, then a new job is queued and its id returned.
func TestAdminBackups_CreateAfterTheLastBackupEndedQueuesANewJob(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// end ends the job of id.
		end func(t *testing.T, b adminBackupsTest, id int64)
	}{
		{name: "Completed", end: func(t *testing.T, b adminBackupsTest, id int64) {
			t.Helper()
			b.mustRunJob(t, id)
		}},
		{name: "Failed", end: func(t *testing.T, b adminBackupsTest, id int64) {
			t.Helper()

			job := b.mustClaim(t, id)
			require.NoError(t, b.store.Jobs().Fail(t.Context(), job.ID, job.Claims, "disk full"))
		}},
		{name: "Cancelled", end: func(t *testing.T, b adminBackupsTest, id int64) {
			t.Helper()
			require.NoError(t, b.store.Jobs().Cancel(t.Context(), id))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := newAdminBackupsTest(t)

			first := b.createBackup(t)
			tt.end(t, b, first)

			second := b.createBackup(t)
			require.NotEqual(t, first, second)

			job, err := b.store.Jobs().Get(t.Context(), second)
			require.NoError(t, err)
			require.Equal(t, store.JobQueued, job.Status)
			require.Equal(t, backup.KindCreate, job.Kind)
			b.requireOnlyJob(t, second)
		})
	}
}

// Given two concurrent POST /api/v1/admin/backups, then exactly one job is queued, and both answers
// hold its id.
func TestAdminBackups_ConcurrentCreatesQueueOneJob(t *testing.T) {
	t.Parallel()

	b := newAdminBackupsTest(t)

	const requests = 2

	recs := make([]*httptest.ResponseRecorder, requests)
	start := make(chan struct{})

	var wg sync.WaitGroup

	for i := range recs {
		wg.Go(func() {
			<-start

			recs[i] = accountRequest(t, b.handler, http.MethodPost, adminBackupsPath, "", b.rootCookie)
		})
	}

	close(start)
	wg.Wait()

	id := createdJobID(t, recs[0])
	for _, rec := range recs[1:] {
		require.Equal(t, id, createdJobID(t, rec))
	}

	b.requireOnlyJob(t, id)
}

// Given GET /api/v1/admin/backups/{name}/download, then the archive is streamed as an attachment,
// and its download is recorded with the admin as actor and the client of the request.
func TestAdminBackups_DownloadStreamsTheArchiveAsAnAttachment(t *testing.T) {
	t.Parallel()

	b := newAdminBackupsTest(t)

	content := "zstd compressed tar"
	b.mustWriteFile(t, testArchive, content)

	rec := b.download(t, testArchive, b.rootCookie)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "application/zstd", rec.Header().Get("Content-Type"))
	require.Equal(t, `attachment; filename="`+testArchive+`"`, rec.Header().Get("Content-Disposition"))
	require.Equal(t, strconv.Itoa(len(content)), rec.Header().Get("Content-Length"))
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Equal(t, content, rec.Body.String())

	events := b.auditEvents(t, audit.TypeBackupDownloaded)
	require.Len(t, events, 1)
	require.Equal(t, b.root.ID, events[0].ActorID)
	require.Zero(t, events[0].SubjectID)
	require.Equal(t, "203.0.113.7", events[0].ClientIP)
	require.Equal(t, userAgent, events[0].UserAgent)
	require.JSONEq(t, `{"file":"`+testArchive+`"}`, string(events[0].Payload))
}

// Given a name with a path separator, or that does not match the backup pattern, or no archive of
// that name, then the download answers 404 and records nothing.
func TestAdminBackups_DownloadOfAnUnknownNameAnswers404(t *testing.T) {
	t.Parallel()

	b := newAdminBackupsTest(t)

	b.mustWriteFile(t, "verve.db", "a database")
	b.mustWriteFile(t, "notes.txt", "not an archive")
	b.mustWriteFile(t, ".verve-backup.lock", "")
	require.NoError(t, os.Mkdir(filepath.Join(b.dir, "sub"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(b.dir, "sub", testArchive), []byte("nested"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(b.dir, "verve-1.2.3-20261111T000000Z-manual.tar.zst"), 0o700))
	require.NoError(t, os.Symlink(b.dbPath, filepath.Join(b.dir, "verve-1.2.3-20261112T000000Z-manual.tar.zst")))

	tests := []struct {
		name string
		// escaped is the name as the path of the request holds it.
		escaped string
	}{
		{name: "slash to the data directory", escaped: "..%2Fverve.db"},
		{name: "backslash to the data directory", escaped: "..%5Cverve.db"},
		{name: "archive in a subdirectory", escaped: "sub%2F" + testArchive},
		{name: "file that is not an archive", escaped: "verve.db"},
		{name: "text file", escaped: "notes.txt"},
		{name: "lock of the backups directory", escaped: ".verve-backup.lock"},
		{name: "archive that does not exist", escaped: "verve-1.2.3-20261101T000000Z-manual.tar.zst"},
		{name: "directory named like an archive", escaped: "verve-1.2.3-20261111T000000Z-manual.tar.zst"},
		{name: "link out of the backups directory", escaped: "verve-1.2.3-20261112T000000Z-manual.tar.zst"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := b.download(t, tt.escaped, b.rootCookie)

			requireProblem(t, rec, http.StatusNotFound, "not_found")
		})
	}

	require.Empty(t, b.auditEvents(t, audit.TypeBackupDownloaded))
}

// Given a member, then the backup operations answer 403, enqueue nothing and record nothing. Given
// no session, then 401.
func TestAdminBackups_RequireAnAdmin(t *testing.T) {
	t.Parallel()

	b := newAdminBackupsTest(t)
	b.mustWriteFile(t, testArchive, "archive")

	operations := []struct {
		name  string
		serve func(cookies ...string) *httptest.ResponseRecorder
	}{
		{name: "list", serve: func(cookies ...string) *httptest.ResponseRecorder {
			return getWith(t, b.handler, adminBackupsPath, cookies...)
		}},
		{name: "create", serve: func(cookies ...string) *httptest.ResponseRecorder {
			return accountRequest(t, b.handler, http.MethodPost, adminBackupsPath, "", cookies...)
		}},
		{name: "download", serve: func(cookies ...string) *httptest.ResponseRecorder {
			return b.download(t, testArchive, cookies...)
		}},
	}

	for _, op := range operations {
		requireProblem(t, op.serve(b.aliceCookie), http.StatusForbidden, "forbidden")

		rec := op.serve()
		require.Equal(t, http.StatusUnauthorized, rec.Code, op.name)
		require.JSONEq(t, unauthorizedBody, rec.Body.String(), op.name)
	}

	_, _, err := b.store.Jobs().Claim(t.Context(), time.Minute)
	require.ErrorIs(t, err, store.ErrNotFound, "no job is enqueued")
	require.Empty(t, b.auditEvents(t, audit.TypeBackupDownloaded))
}
