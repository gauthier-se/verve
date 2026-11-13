package native_test

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/ingest/native"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
	"github.com/gauthier-se/verve/internal/workouts/route"
)

// jobTime is the current time of the job tests when they start.
var jobTime = time.Date(2026, 11, 10, 20, 0, 0, 0, time.UTC)

// jobLease is the lease of the workers of the job tests.
const jobLease = time.Minute

// stepsPayload is a payload of three step count samples and the deletion of a uuid that matches
// nothing.
const stepsPayload = `{
  "format": "verve.ingest.v1",
  "client": {"name": "Verve for iOS", "version": "1.0.0", "deviceModel": "iPhone17,1", "osVersion": "27.1"},
  "samples": [
    {"uuid": "6F1C2B9E-3D4A-4E0B-9C59-1A2B3C4D5E61", "type": "HKQuantityTypeIdentifierStepCount",
     "start": "2026-11-10T08:00:00+01:00", "end": "2026-11-10T08:10:00+01:00", "value": 1240, "unit": "count",
     "source": {"name": "Apple Watch", "device": {"model": "Watch"}}},
    {"uuid": "6F1C2B9E-3D4A-4E0B-9C59-1A2B3C4D5E62", "type": "HKQuantityTypeIdentifierStepCount",
     "start": "2026-11-10T08:10:00+01:00", "end": "2026-11-10T08:20:00+01:00", "value": 860, "unit": "count",
     "source": {"name": "Apple Watch", "device": {"model": "Watch"}}},
    {"uuid": "6F1C2B9E-3D4A-4E0B-9C59-1A2B3C4D5E63", "type": "HKQuantityTypeIdentifierStepCount",
     "start": "2026-11-10T08:20:00+01:00", "end": "2026-11-10T08:30:00+01:00", "value": 15, "unit": "count",
     "source": {"name": "Apple Watch", "device": {"model": "Watch"}}}
  ],
  "deletions": ["5A4B3C2D-1E0F-4A9B-8C7D-6E5F4A3B2C1D"]
}`

// hookedPipeline is a pipeline that calls before, when set, ahead of handing each record to the
// pipeline, outside any transaction: with batches of one record, before the batch of position.
type hookedPipeline struct {
	pipeline *ingest.Pipeline

	mu     sync.Mutex
	before func(ctx context.Context, position int64) error
}

func (h *hookedPipeline) Run(ctx context.Context, userID, importID int64, records iter.Seq2[ingest.Record, error]) error {
	hooked := func(yield func(ingest.Record, error) bool) {
		for r, err := range records {
			if err == nil {
				if hookErr := h.call(ctx, r.Position); hookErr != nil {
					yield(ingest.Record{}, hookErr)

					return
				}
			}

			if !yield(r, err) {
				return
			}
		}
	}

	return h.pipeline.Run(ctx, userID, importID, hooked)
}

func (h *hookedPipeline) call(ctx context.Context, position int64) error {
	h.mu.Lock()
	before := h.before
	h.mu.Unlock()

	if before == nil {
		return nil
	}

	return before(ctx, position)
}

func (h *hookedPipeline) set(before func(ctx context.Context, position int64) error) {
	h.mu.Lock()
	h.before = before
	h.mu.Unlock()
}

// jobEnv is a migrated store with the shipped catalog and a user, an inbox, and a worker that runs
// the import.native jobs through a pipeline of batches of one record, all on one fake clock.
type jobEnv struct {
	store  store.Store
	clk    *clock.Fake
	user   store.User
	inbox  *ingest.Inbox
	dir    string
	worker *jobs.Worker
	hook   *hookedPipeline
}

func newJobEnv(t *testing.T) *jobEnv {
	t.Helper()

	ctx := t.Context()
	clk := clock.NewFake(jobTime)

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "verve.db"))
	require.NoError(t, err)

	_, err = sqlite.Migrate(ctx, db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	s := sqlite.New(db, clk)
	t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

	cat, err := catalog.Shipped()
	require.NoError(t, err)
	require.NoError(t, s.MetricTypes().Sync(ctx, cat))

	activities, err := catalog.ShippedActivities()
	require.NoError(t, err)

	user, err := s.Users().Create(ctx, store.NewUser{Username: "alice", Role: store.RoleMember})
	require.NoError(t, err)

	enc, err := zstd.NewWriter(nil)
	require.NoError(t, err)

	dec, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(64<<20))
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, enc.Close())
		dec.Close()
	})

	hook := &hookedPipeline{pipeline: ingest.NewPipeline(ingest.Config{
		Store:      s,
		Catalog:    cat,
		Activities: activities,
		Codec:      route.NewCodec(enc, dec),
		BatchSize:  1,
	})}

	dir := t.TempDir()
	inbox := ingest.NewInbox(dir, clk)

	registry := jobs.NewRegistry()
	native.Register(registry, native.JobDeps{Store: s, Pipeline: hook, Inbox: inbox})

	worker := jobs.NewWorker(s, registry, clk, slog.New(slog.DiscardHandler), jobs.Options{
		Lease:             jobLease,
		HeartbeatInterval: 10 * time.Millisecond,
	})

	return &jobEnv{store: s, clk: clk, user: user, inbox: inbox, dir: dir, worker: worker, hook: hook}
}

// push does what POST /api/v1/ingest does: it writes payload to the inbox, and creates the import
// and its job in one transaction. It returns both.
func (e *jobEnv) push(t *testing.T, payload string) (store.Import, store.Job) {
	t.Helper()

	ctx := t.Context()

	name, err := e.inbox.Write(ctx, strings.NewReader(payload))
	require.NoError(t, err)

	var (
		imp    store.Import
		queued store.Job
	)

	err = e.store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		imp, err = tx.Imports().Create(ctx, store.NewImport{UserID: e.user.ID, Origin: store.OriginNative, InboxFile: name})
		if err != nil {
			return err
		}

		job, err := native.Job(e.user.ID, imp.ID)
		if err != nil {
			return err
		}

		queued, err = tx.Jobs().Enqueue(ctx, job)

		return err
	})
	require.NoError(t, err)

	return imp, queued
}

// tick runs the oldest due job with ctx, and requires that there was one.
func (e *jobEnv) tick(ctx context.Context, t *testing.T) {
	t.Helper()

	claimed, err := e.worker.Tick(ctx)
	require.NoError(t, err)
	require.True(t, claimed)
}

func (e *jobEnv) importOf(t *testing.T, id int64) store.Import {
	t.Helper()

	imp, err := e.store.Imports().Get(t.Context(), e.user.ID, id)
	require.NoError(t, err)

	return imp
}

func (e *jobEnv) jobOf(t *testing.T, id int64) store.Job {
	t.Helper()

	job, err := e.store.Jobs().Get(t.Context(), id)
	require.NoError(t, err)

	return job
}

// samples returns the step count samples of the user.
func (e *jobEnv) samples(t *testing.T) []store.Sample {
	t.Helper()

	steps, err := e.store.MetricTypes().GetByKey(t.Context(), "step_count")
	require.NoError(t, err)

	got, err := e.store.Samples().List(t.Context(), e.user.ID, store.SamplesQuery{
		TypeID: steps.ID,
		From:   time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		To:     time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		Limit:  100,
	})
	require.NoError(t, err)

	return got
}

// requireInboxFile requires that the inbox file of imp exists when want is true, and is gone
// otherwise.
func (e *jobEnv) requireInboxFile(t *testing.T, imp store.Import, want bool) {
	t.Helper()

	require.NotEmpty(t, imp.InboxFile)

	_, err := os.Stat(filepath.Join(e.dir, imp.InboxFile))
	if want {
		require.NoError(t, err, "the inbox file must be kept")
	} else {
		require.ErrorIs(t, err, os.ErrNotExist, "the inbox file must be deleted")
	}
}

// uninterruptedReport runs payload in a fresh environment, in one run, and returns its report.
func uninterruptedReport(t *testing.T, payload string) store.ImportReport {
	t.Helper()

	e := newJobEnv(t)
	imp, _ := e.push(t, payload)
	e.tick(t.Context(), t)

	got := e.importOf(t, imp.ID)
	require.Equal(t, store.ImportCompleted, got.Status)

	return got.Report
}

// Given a queued import, when its job runs, then the import runs, the pipeline processes its inbox
// file, the import ends completed with its report and the client of the payload, the file is
// deleted and the job completes.
func TestImportJob_QueuedImport_CompletesAndDeletesItsInboxFile(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t)
	imp, queued := e.push(t, stepsPayload)
	require.Equal(t, store.ImportQueued, imp.Status)

	var statuses []store.ImportStatus

	e.hook.set(func(context.Context, int64) error {
		statuses = append(statuses, e.importOf(t, imp.ID).Status)

		return nil
	})

	e.clk.Advance(time.Second)
	e.tick(t.Context(), t)

	require.Equal(t, []store.ImportStatus{store.ImportRunning, store.ImportRunning, store.ImportRunning, store.ImportRunning}, statuses,
		"the import is running while the pipeline processes it")

	got := e.importOf(t, imp.ID)
	require.Equal(t, store.ImportCompleted, got.Status)
	require.Equal(t, jobTime.Add(time.Second), got.StartedAt)
	require.False(t, got.FinishedAt.IsZero())
	require.Equal(t, int64(4), got.Position)
	require.Equal(t, store.ImportCounters{Received: 3, Inserted: 3, NotFound: 1}, got.Report.Total)
	require.Equal(t, map[string]string{
		"name": "Verve for iOS", "version": "1.0.0", "deviceModel": "iPhone17,1", "osVersion": "27.1",
	}, got.Client)
	require.Len(t, e.samples(t), 3)
	e.requireInboxFile(t, got, false)

	job := e.jobOf(t, queued.ID)
	require.Equal(t, store.JobCompleted, job.Status)
	require.Equal(t, e.user.ID, job.UserID)
	require.JSONEq(t, fmt.Sprintf(`{"importId": %d}`, imp.ID), string(job.Payload))
}

// Given a payload that fails as a whole, when its job runs, then the import ends failed with the
// reason as its sample error message, the job fails without retry, nothing is stored and the inbox
// file is kept for diagnosis.
func TestImportJob_PayloadThatFailsAsAWhole_FailsWithoutRetryAndKeepsTheFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		reason  string
	}{
		{name: "not JSON", payload: `{"format": "verve.ingest.v1",`, reason: "not valid JSON"},
		{name: "other format", payload: `{"format": "verve.ingest.v2", "client": {"name": "x", "version": "1"}}`, reason: "format"},
		{name: "samples not an array", payload: `{"format": "verve.ingest.v1", "client": {"name": "x", "version": "1"}, "samples": {}}`, reason: "samples"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := newJobEnv(t)
			imp, queued := e.push(t, tt.payload)

			e.tick(t.Context(), t)

			got := e.importOf(t, imp.ID)
			require.Equal(t, store.ImportFailed, got.Status)
			require.False(t, got.FinishedAt.IsZero())
			require.Len(t, got.Report.Errors, 1)
			require.Contains(t, got.Report.Errors[0], tt.reason)
			require.Zero(t, got.Report.Total)
			require.Empty(t, e.samples(t))
			e.requireInboxFile(t, got, true)

			job := e.jobOf(t, queued.ID)
			require.Equal(t, store.JobFailed, job.Status, "the job is not retried")
			require.Equal(t, int64(1), job.Attempts)

			claimed, err := e.worker.Tick(t.Context())
			require.NoError(t, err)
			require.False(t, claimed, "nothing is left to run")
		})
	}
}

// Given an import whose inbox file is gone, when its job runs, then the import ends failed with
// the reason as its sample error message, and the job fails without retry.
func TestImportJob_MissingInboxFile_FailsWithoutRetry(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t)
	imp, queued := e.push(t, stepsPayload)
	require.NoError(t, e.inbox.Remove(imp.InboxFile))

	e.tick(t.Context(), t)

	got := e.importOf(t, imp.ID)
	require.Equal(t, store.ImportFailed, got.Status)
	require.Equal(t, []string{"the payload of the import is missing from the inbox"}, got.Report.Errors)
	require.Equal(t, store.JobFailed, e.jobOf(t, queued.ID).Status)
}

// Given two pushes of one user, then their jobs share the serial key import:<user id>, and they
// are processed one at a time, in arrival order: the second waits while the first runs.
func TestImportJob_TwoPushesOfOneUser_RunOneAtATimeInArrivalOrder(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t)
	ctx := t.Context()

	first, firstJob := e.push(t, stepsPayload)
	second, secondJob := e.push(t, stepsPayload)

	key := fmt.Sprintf("import:%d", e.user.ID)
	require.Equal(t, key, firstJob.SerialKey)
	require.Equal(t, key, secondJob.SerialKey)

	var others []bool

	e.hook.set(func(ctx context.Context, _ int64) error {
		claimed, err := e.worker.Tick(ctx)
		if err != nil {
			return err
		}

		others = append(others, claimed)

		return nil
	})

	e.tick(ctx, t)
	e.hook.set(nil)

	require.Equal(t, []bool{false, false, false, false}, others, "no job of the key is claimed while the first runs")
	require.Equal(t, store.ImportCompleted, e.importOf(t, first.ID).Status)
	require.Equal(t, store.ImportQueued, e.importOf(t, second.ID).Status, "the second waits for the first")

	e.tick(ctx, t)

	got := e.importOf(t, second.ID)
	require.Equal(t, store.ImportCompleted, got.Status)
	require.Equal(t, store.ImportCounters{Received: 3, Duplicates: 3, NotFound: 1}, got.Report.Total)
	require.True(t, got.StartedAt.Compare(e.importOf(t, first.ID).FinishedAt) >= 0)
}

// Given a process stopped in the middle of an import, after its first batch, when the job is
// claimed again once its lease expired, then it resumes from the stored position and completes
// with the report of an uninterrupted run.
func TestImportJob_ProcessStoppedMidImport_ResumesWithTheReportOfAnUninterruptedRun(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t)
	imp, queued := e.push(t, stepsPayload)

	stopped, stop := context.WithCancel(t.Context())

	e.hook.set(func(ctx context.Context, position int64) error {
		if position == 1 {
			// The process stops once the first batch is committed.
			stop()
			<-ctx.Done()
		}

		return nil
	})

	e.tick(stopped, t)
	e.hook.set(nil)

	interrupted := e.importOf(t, imp.ID)
	require.Equal(t, store.ImportRunning, interrupted.Status)
	require.Equal(t, int64(1), interrupted.Position, "the first batch is committed")
	require.Len(t, e.samples(t), 1)
	e.requireInboxFile(t, interrupted, true)
	require.Equal(t, store.JobRunning, e.jobOf(t, queued.ID).Status, "the stopped process could not release the job")

	e.clk.Advance(jobLease + time.Second)
	e.tick(t.Context(), t)

	got := e.importOf(t, imp.ID)
	require.Equal(t, store.ImportCompleted, got.Status)
	require.Equal(t, interrupted.StartedAt, got.StartedAt, "a resumed import keeps the time it first ran")
	require.Equal(t, uninterruptedReport(t, stepsPayload), got.Report)
	require.Len(t, e.samples(t), 3)
	e.requireInboxFile(t, got, false)

	job := e.jobOf(t, queued.ID)
	require.Equal(t, store.JobCompleted, job.Status)
	require.Equal(t, int64(1), job.LostLeases)
}

// Given a cancellation of the job of a running import, then the import ends cancelled at the next
// batch boundary, the batches committed before stay, the inbox file is deleted and the job ends
// cancelled.
func TestImportJob_Cancelled_EndsCancelledAndKeepsTheCommittedData(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t)
	imp, queued := e.push(t, stepsPayload)

	e.hook.set(func(ctx context.Context, position int64) error {
		if position == 1 {
			if err := e.store.Jobs().Cancel(ctx, queued.ID); err != nil {
				return err
			}

			// The heartbeat reports the cancellation to the handler.
			<-ctx.Done()
		}

		return nil
	})

	e.tick(t.Context(), t)

	got := e.importOf(t, imp.ID)
	require.Equal(t, store.ImportCancelled, got.Status)
	require.False(t, got.FinishedAt.IsZero())
	require.Equal(t, int64(1), got.Position)
	require.Equal(t, store.ImportCounters{Received: 1, Inserted: 1}, got.Report.Total)
	require.Len(t, e.samples(t), 1, "the committed batch stays")
	e.requireInboxFile(t, got, false)

	job := e.jobOf(t, queued.ID)
	require.Equal(t, store.JobCancelled, job.Status)
	require.Zero(t, job.Attempts)
}

// Given the job of an import that already ended, such as one cancelled while it was queued, when
// it runs, then it changes nothing and completes.
func TestImportJob_FinishedImport_ChangesNothing(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t)
	imp, queued := e.push(t, stepsPayload)

	cancelled, err := e.store.Imports().SetStatus(t.Context(), e.user.ID, imp.ID, store.ImportCancelled)
	require.NoError(t, err)

	e.tick(t.Context(), t)

	require.Equal(t, cancelled, e.importOf(t, imp.ID))
	require.Empty(t, e.samples(t))
	require.Equal(t, store.JobCompleted, e.jobOf(t, queued.ID).Status)
}

// Given a job whose import no longer exists, such as one purged with the account of its user, when
// it runs, then it fails at once, without retry.
func TestImportJob_MissingImport_FailsWithoutRetry(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t)

	job, err := native.Job(e.user.ID, 4242)
	require.NoError(t, err)

	queued, err := e.store.Jobs().Enqueue(t.Context(), job)
	require.NoError(t, err)

	e.tick(t.Context(), t)

	got := e.jobOf(t, queued.ID)
	require.Equal(t, store.JobFailed, got.Status)
	require.Equal(t, int64(1), got.Attempts)
	require.NotContains(t, got.LastError, "importId", "the error never quotes the payload")
}
