package ingest_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
)

// testImportKind is the kind of the import jobs of the lifecycle tests, whose payload holds one step
// count per line.
const testImportKind = "test.import"

// importJobLease is the lease of the workers of the lifecycle tests.
const importJobLease = time.Minute

// The sample error messages of the payloads of testImportKind that fail as a whole.
const (
	notAPayload   = "the file is not a test payload"
	brokenPayload = "the test payload is broken"
)

// stepsOfPayload is a payload of testImportKind of three step counts.
const stepsOfPayload = "1240\n860\n15\n"

// importJobEnv is a pipeline environment with an inbox and a worker that runs the jobs of
// testImportKind through a pipeline of batches of one record, on one fake clock.
type importJobEnv struct {
	*pipelineEnv

	clk    *clock.Fake
	inbox  *ingest.Inbox
	dir    string
	worker *jobs.Worker

	mu sync.Mutex
	// before, when set, runs before the record of each position is read, outside any transaction:
	// with batches of one record, the records before it are committed.
	before func(ctx context.Context, position int64) error
}

func newImportJobEnv(t *testing.T) *importJobEnv {
	t.Helper()

	clk := clock.NewFake(pipelineTime)
	env := newPipelineEnvOn(t, clk)
	dir := t.TempDir()

	e := &importJobEnv{pipelineEnv: env, clk: clk, inbox: ingest.NewInbox(dir, clk), dir: dir}

	registry := jobs.NewRegistry()
	ingest.RegisterImport(registry, testImportKind, ingest.ImportJobDeps{
		Store:    env.store,
		Pipeline: env.pipeline(func(cfg *ingest.Config) { cfg.BatchSize = 1 }),
		Inbox:    e.inbox,
	}, e.open)

	e.worker = jobs.NewWorker(env.store, registry, clk, slog.New(slog.DiscardHandler), jobs.Options{
		Lease:             importJobLease,
		HeartbeatInterval: 10 * time.Millisecond,
	})

	return e
}

// open opens the payload of f: a step count sample per line, at the position of its line. A
// payload that starts with "not" fails as a whole at once, and a line "broken" fails the payload
// as a whole when it is read.
func (e *importJobEnv) open(ctx context.Context, _ store.ImportsRepository, _ store.Import, f *os.File) (iter.Seq2[ingest.Record, error], error) {
	var lines []string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(lines) > 0 && strings.HasPrefix(lines[0], "not") {
		return nil, ingest.Reject(notAPayload, errors.New("unexpected first line"))
	}

	return func(yield func(ingest.Record, error) bool) {
		for i, line := range lines {
			position := int64(i)

			if err := e.call(ctx, position); err != nil {
				yield(ingest.Record{}, err)

				return
			}

			if line == "broken" {
				yield(ingest.Record{}, ingest.Reject(brokenPayload, fmt.Errorf("line %d is broken", i)))

				return
			}

			value, err := strconv.ParseFloat(line, 64)
			if err != nil {
				yield(ingest.Record{}, err)

				return
			}

			start := time.Date(2026, 11, 10, 7, i*10, 0, 0, time.UTC).Format(time.RFC3339)
			r := quantity(ingest.QuantitySample{
				Type: "HKQuantityTypeIdentifierStepCount", Start: start, End: start, Value: value, Unit: "count", Source: watch,
			})
			r.Position = position

			if !yield(r, nil) {
				return
			}
		}
	}, nil
}

func (e *importJobEnv) call(ctx context.Context, position int64) error {
	e.mu.Lock()
	before := e.before
	e.mu.Unlock()

	if before == nil {
		return nil
	}

	return before(ctx, position)
}

func (e *importJobEnv) setBefore(before func(ctx context.Context, position int64) error) {
	e.mu.Lock()
	e.before = before
	e.mu.Unlock()
}

// push writes payload to the inbox, and creates the native import of the user and its job in one
// transaction, as a push endpoint does. It returns both.
func (e *importJobEnv) push(t *testing.T, payload string) (store.Import, store.Job) {
	t.Helper()

	name, err := e.inbox.Write(t.Context(), strings.NewReader(payload))
	require.NoError(t, err)

	return e.create(t, store.NewImport{UserID: e.user.ID, Origin: store.OriginNative, InboxFile: name}, "")
}

// create creates imp and its job, whose payload has path, in one transaction, and returns both.
func (e *importJobEnv) create(t *testing.T, imp store.NewImport, path string) (store.Import, store.Job) {
	t.Helper()

	var (
		created store.Import
		queued  store.Job
	)

	err := e.store.WithTx(t.Context(), func(ctx context.Context, tx store.Repositories) error {
		var err error

		created, err = tx.Imports().Create(ctx, imp)
		if err != nil {
			return err
		}

		job, err := ingest.NewImportJob(testImportKind, imp.UserID, ingest.JobPayload{ImportID: created.ID, Path: path})
		if err != nil {
			return err
		}

		queued, err = tx.Jobs().Enqueue(ctx, job)

		return err
	})
	require.NoError(t, err)

	return created, queued
}

// tick runs the oldest due job with ctx, and requires that there was one.
func (e *importJobEnv) tick(ctx context.Context, t *testing.T) {
	t.Helper()

	claimed, err := e.worker.Tick(ctx)
	require.NoError(t, err)
	require.True(t, claimed)
}

func (e *importJobEnv) jobOf(t *testing.T, id int64) store.Job {
	t.Helper()

	job, err := e.store.Jobs().Get(t.Context(), id)
	require.NoError(t, err)

	return job
}

// requireInboxFile requires that the inbox file of imp exists when want is true, and is gone
// otherwise.
func (e *importJobEnv) requireInboxFile(t *testing.T, imp store.Import, want bool) {
	t.Helper()

	require.NotEmpty(t, imp.InboxFile)

	if want {
		require.FileExists(t, filepath.Join(e.dir, imp.InboxFile))
	} else {
		require.NoFileExists(t, filepath.Join(e.dir, imp.InboxFile))
	}
}

// eventTypes returns the types of the audit events of the imports of the user, oldest first.
func (e *importJobEnv) eventTypes(t *testing.T) []string {
	t.Helper()

	events, err := e.store.AuditEvents().List(t.Context(), store.AuditEventsFilter{UserID: e.user.ID}, store.PageRequest{Limit: 100})
	require.NoError(t, err)

	var types []string

	for _, event := range slices.Backward(events) {
		if strings.HasPrefix(event.Type, "import.") {
			types = append(types, event.Type)
		}
	}

	return types
}

// requireFailedAtOnce requires that the import of id ended failed with message as its only sample
// error message, and that its job of jobID failed at its first attempt, without retry.
func (e *importJobEnv) requireFailedAtOnce(t *testing.T, id, jobID int64, message string) store.Import {
	t.Helper()

	got := e.importOf(t, id)
	require.Equal(t, store.ImportFailed, got.Status)
	require.Equal(t, []string{message}, got.Report.Errors)

	job := e.jobOf(t, jobID)
	require.Equal(t, store.JobFailed, job.Status, "the job is not retried")
	require.Equal(t, int64(1), job.Attempts)

	return got
}

// Given a queued import, when its job runs, then the import runs, its records are stored, it ends
// completed with import.started and import.completed recorded, and its inbox file is removed.
func TestImportJob_QueuedImport_CompletesAndRemovesItsInboxFile(t *testing.T) {
	t.Parallel()

	e := newImportJobEnv(t)
	imp, queued := e.push(t, stepsOfPayload)

	e.tick(t.Context(), t)

	got := e.importOf(t, imp.ID)
	require.Equal(t, store.ImportCompleted, got.Status)
	require.Equal(t, store.ImportCounters{Received: 3, Inserted: 3}, got.Report.Total)
	require.Len(t, e.samples(t, "HKQuantityTypeIdentifierStepCount"), 3)
	e.requireInboxFile(t, got, false)
	require.Equal(t, store.JobCompleted, e.jobOf(t, queued.ID).Status)
	require.Equal(t, []string{"import.started", "import.completed"}, e.eventTypes(t))
}

// Given a payload that its kind rejects as a whole, at once or once some batches are committed,
// when its job runs, then the import fails without retry with the reason of the kind as its sample
// error message, the committed batches stay, and its inbox file is kept for a diagnosis.
func TestImportJob_RejectedPayload_FailsAtOnceAndKeepsTheCommittedBatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		reason  string
		stored  int
	}{
		{name: "when opened", payload: "not steps\n", reason: notAPayload},
		{name: "while read", payload: "1240\n860\nbroken\n15\n", reason: brokenPayload, stored: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := newImportJobEnv(t)
			imp, queued := e.push(t, tt.payload)

			e.tick(t.Context(), t)

			got := e.requireFailedAtOnce(t, imp.ID, queued.ID, tt.reason)
			require.Equal(t, int64(tt.stored), got.Report.Total.Inserted)
			e.requireInboxFile(t, got, true)
			require.Equal(t, []string{"import.started", "import.failed"}, e.eventTypes(t))
		})
	}
}

// Given an import whose inbox file is gone, when its job runs, then the import fails without
// retry, with the reason as its sample error message.
func TestImportJob_MissingInboxFile_FailsWithoutRetry(t *testing.T) {
	t.Parallel()

	e := newImportJobEnv(t)
	imp, queued := e.push(t, stepsOfPayload)
	require.NoError(t, e.inbox.Remove(t.Context(), imp.InboxFile))

	e.tick(t.Context(), t)

	e.requireFailedAtOnce(t, imp.ID, queued.ID, "the payload of the import is missing from the inbox")
}

// Given a job whose payload has a path and an import without inbox file, when it runs, then it
// reads the file at that path and never removes it. Given that file gone, then the import fails
// without retry, as for a missing inbox file.
func TestImportJob_PathInPayload_ReadsTheFileInPlace(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()

		e := newImportJobEnv(t)
		path := filepath.Join(t.TempDir(), "steps.txt")
		require.NoError(t, os.WriteFile(path, []byte(stepsOfPayload), 0o600))

		imp, _ := e.create(t, store.NewImport{UserID: e.user.ID, Origin: store.OriginCLI}, path)

		e.tick(t.Context(), t)

		got := e.importOf(t, imp.ID)
		require.Equal(t, store.ImportCompleted, got.Status)
		require.Equal(t, store.ImportCounters{Received: 3, Inserted: 3}, got.Report.Total)
		require.FileExists(t, path, "the file of a path is never removed")
	})

	t.Run("gone", func(t *testing.T) {
		t.Parallel()

		e := newImportJobEnv(t)
		path := filepath.Join(t.TempDir(), "steps.txt")

		imp, queued := e.create(t, store.NewImport{UserID: e.user.ID, Origin: store.OriginCLI}, path)

		e.tick(t.Context(), t)

		e.requireFailedAtOnce(t, imp.ID, queued.ID, "the file of the import is missing")
	})
}

// Given a cancellation of the job of a running import, then the import ends cancelled at the next
// batch boundary, the committed batches stay, import.cancelled is recorded, and its inbox file is
// removed.
func TestImportJob_Cancelled_EndsCancelledAtTheNextBatchBoundary(t *testing.T) {
	t.Parallel()

	e := newImportJobEnv(t)
	imp, queued := e.push(t, stepsOfPayload)

	e.setBefore(func(ctx context.Context, position int64) error {
		if position != 1 {
			return nil
		}

		if err := e.store.Jobs().Cancel(ctx, queued.ID); err != nil {
			return err
		}

		// The heartbeat delivers the cancellation.
		<-ctx.Done()

		return nil
	})

	e.tick(t.Context(), t)

	got := e.importOf(t, imp.ID)
	require.Equal(t, store.ImportCancelled, got.Status)
	require.Equal(t, int64(1), got.Report.Total.Inserted, "the batch before the boundary stays")
	e.requireInboxFile(t, got, false)
	require.Equal(t, store.JobCancelled, e.jobOf(t, queued.ID).Status)
	require.Equal(t, []string{"import.started", "import.cancelled"}, e.eventTypes(t))
}

// Given records that keep failing, when the job of an import runs out of attempts, then the import
// ends failed with a sample error message that says so, and its inbox file is kept.
func TestImportJob_LastAttemptFails_FailsTheImport(t *testing.T) {
	t.Parallel()

	e := newImportJobEnv(t)
	imp, queued := e.push(t, stepsOfPayload)

	e.setBefore(func(context.Context, int64) error {
		return errors.New("read inbox file: input/output error")
	})

	var statuses []store.ImportStatus

	for range 5 {
		e.tick(t.Context(), t)
		statuses = append(statuses, e.importOf(t, imp.ID).Status)
		e.clk.Advance(2 * time.Hour)
	}

	require.Equal(t, []store.ImportStatus{
		store.ImportRunning, store.ImportRunning, store.ImportRunning, store.ImportRunning, store.ImportFailed,
	}, statuses, "the import runs again until the last attempt")

	got := e.importOf(t, imp.ID)
	require.Equal(t, []string{"the import stopped after the last attempt of its job failed"}, got.Report.Errors)
	e.requireInboxFile(t, got, true)
	require.Equal(t, store.JobFailed, e.jobOf(t, queued.ID).Status)
}

// dies claims the job of queued as a process that dies while it runs the import of imp: the import
// is running, the job holds its lease, whileRunning runs if any, and the lease expires.
func (e *importJobEnv) dies(t *testing.T, imp store.Import, queued store.Job, whileRunning ...func()) {
	t.Helper()

	ctx := t.Context()

	claimed, _, err := e.store.Jobs().Claim(ctx, importJobLease)
	require.NoError(t, err)
	require.Equal(t, queued.ID, claimed.ID)

	_, err = e.store.Imports().SetStatus(ctx, e.user.ID, imp.ID, store.ImportRunning)
	require.NoError(t, err)

	for _, fn := range whileRunning {
		fn()
	}

	e.clk.Advance(importJobLease)
}

// Given an import whose job lost its lease 5 times, when a claim abandons the job, then the import
// ends failed with a sample error message that says so, and its inbox file is kept.
func TestImportJob_AbandonedAfterFiveLostLeases_FailsTheImport(t *testing.T) {
	t.Parallel()

	e := newImportJobEnv(t)
	imp, queued := e.push(t, stepsOfPayload)

	for range 5 {
		e.dies(t, imp, queued)
	}

	claimed, err := e.worker.Tick(t.Context())
	require.NoError(t, err)
	require.False(t, claimed)
	require.Equal(t, store.JobFailed, e.jobOf(t, queued.ID).Status)

	got := e.importOf(t, imp.ID)
	require.Equal(t, store.ImportFailed, got.Status)
	require.Equal(t, []string{"the import stopped after its job was abandoned: its process stopped 5 times while running it"}, got.Report.Errors)
	e.requireInboxFile(t, got, true)
	require.Equal(t, []string{"import.failed"}, e.eventTypes(t))
}

// Given a cancellation of the job of a running import whose process died, when a claim cancels the
// job without a handler, then the import ends cancelled and its inbox file is removed.
func TestImportJob_CancelledAfterItsProcessDied_EndsCancelled(t *testing.T) {
	t.Parallel()

	e := newImportJobEnv(t)
	imp, queued := e.push(t, stepsOfPayload)

	e.dies(t, imp, queued, func() {
		require.NoError(t, e.store.Jobs().Cancel(t.Context(), queued.ID))
	})

	claimed, err := e.worker.Tick(t.Context())
	require.NoError(t, err)
	require.False(t, claimed)
	require.Equal(t, store.JobCancelled, e.jobOf(t, queued.ID).Status)

	got := e.importOf(t, imp.ID)
	require.Equal(t, store.ImportCancelled, got.Status)
	e.requireInboxFile(t, got, false)
	require.Equal(t, []string{"import.cancelled"}, e.eventTypes(t))
}

// Given an import without inbox file whose job has a path, when its job is cancelled after its
// process died, then the import ends cancelled and the file of its path stays.
func TestImportJob_CancelledImportOfAPath_KeepsTheFile(t *testing.T) {
	t.Parallel()

	e := newImportJobEnv(t)
	path := filepath.Join(t.TempDir(), "steps.txt")
	require.NoError(t, os.WriteFile(path, []byte(stepsOfPayload), 0o600))

	imp, queued := e.create(t, store.NewImport{UserID: e.user.ID, Origin: store.OriginCLI}, path)

	e.dies(t, imp, queued, func() {
		require.NoError(t, e.store.Jobs().Cancel(t.Context(), queued.ID))
	})

	claimed, err := e.worker.Tick(t.Context())
	require.NoError(t, err)
	require.False(t, claimed)
	require.Equal(t, store.ImportCancelled, e.importOf(t, imp.ID).Status)
	require.FileExists(t, path)
}
