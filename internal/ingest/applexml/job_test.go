package applexml_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/ingest/applexml"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
	"github.com/gauthier-se/verve/internal/synthexport"
	"github.com/gauthier-se/verve/internal/workouts"
	"github.com/gauthier-se/verve/internal/workouts/route"
)

// jobBatchSize is the batch size of the pipeline of the job tests, small enough that the fixture
// takes several batches.
const jobBatchSize = 100

// fixtureRecords is the number of records of the fixture, one per line of testdata/export.golden.
const fixtureRecords = 1670

// stoppingPipeline is a pipeline that calls stop, when set, once it has read stopAfter records,
// then waits for the context of the run to be done, as a process that shuts down.
type stoppingPipeline struct {
	pipeline *ingest.Pipeline

	mu        sync.Mutex
	stopAfter int
	stop      func()
}

func (p *stoppingPipeline) Run(ctx context.Context, userID, importID int64, records iter.Seq2[ingest.Record, error]) error {
	p.mu.Lock()
	stopAfter, stop := p.stopAfter, p.stop
	p.stop = nil
	p.mu.Unlock()

	read := 0
	stopping := func(yield func(ingest.Record, error) bool) {
		for r, err := range records {
			read++

			if stop != nil && read == stopAfter {
				stop()
				<-ctx.Done()
			}

			if !yield(r, err) {
				return
			}
		}
	}

	return p.pipeline.Run(ctx, userID, importID, stopping)
}

// stopOnce makes the next run call stop once it has read n records.
func (p *stoppingPipeline) stopOnce(n int, stop func()) {
	p.mu.Lock()
	p.stopAfter, p.stop = n, stop
	p.mu.Unlock()
}

// xmlJobEnv is a migrated store with the shipped catalog and a user, an inbox, and a worker that
// runs the import.xml jobs through a pipeline with the duplicate detector, all on one fake clock.
type xmlJobEnv struct {
	store    store.Store
	clk      *clock.Fake
	user     store.User
	inbox    *ingest.Inbox
	dir      string
	worker   *jobs.Worker
	pipeline *stoppingPipeline
}

func newXMLJobEnv(t *testing.T) *xmlJobEnv {
	t.Helper()

	ctx := t.Context()
	clk := clock.NewFake(importTime)

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

	user, err := s.Users().Create(ctx, store.NewUser{Username: "alex", Role: store.RoleMember})
	require.NoError(t, err)

	enc, err := zstd.NewWriter(nil)
	require.NoError(t, err)

	dec, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(64<<20))
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, enc.Close())
		dec.Close()
	})

	pipeline := &stoppingPipeline{pipeline: ingest.NewPipeline(ingest.Config{
		Store:      s,
		Catalog:    cat,
		Activities: activities,
		Codec:      route.NewCodec(enc, dec),
		Duplicates: workouts.NewDetector(activities),
		BatchSize:  jobBatchSize,
	})}

	dir := t.TempDir()
	inbox := ingest.NewInbox(dir, clk)

	registry := jobs.NewRegistry()
	applexml.Register(registry, ingest.ImportJobDeps{Store: s, Pipeline: pipeline, Inbox: inbox})

	worker := jobs.NewWorker(s, registry, clk, slog.New(slog.DiscardHandler), jobs.Options{
		Lease:             time.Minute,
		HeartbeatInterval: 10 * time.Millisecond,
	})

	return &xmlJobEnv{store: s, clk: clk, user: user, inbox: inbox, dir: dir, worker: worker, pipeline: pipeline}
}

// upload writes data to the inbox and creates the XML import of the user and its job in one
// transaction, as the completion of an upload does. It returns both.
func (e *xmlJobEnv) upload(t *testing.T, data []byte) (store.Import, store.Job) {
	t.Helper()

	name, err := e.inbox.Write(t.Context(), bytes.NewReader(data))
	require.NoError(t, err)

	return e.create(t, store.NewImport{UserID: e.user.ID, Origin: store.OriginXML, InboxFile: name}, "")
}

// create creates imp and its import.xml job, whose payload has path, in one transaction.
func (e *xmlJobEnv) create(t *testing.T, imp store.NewImport, path string) (store.Import, store.Job) {
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

		job, err := applexml.Job(imp.UserID, created.ID, path)
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
func (e *xmlJobEnv) tick(ctx context.Context, t *testing.T) {
	t.Helper()

	claimed, err := e.worker.Tick(ctx)
	require.NoError(t, err)
	require.True(t, claimed)
}

func (e *xmlJobEnv) importOf(t *testing.T, id int64) store.Import {
	t.Helper()

	imp, err := e.store.Imports().Get(t.Context(), e.user.ID, id)
	require.NoError(t, err)

	return imp
}

func (e *xmlJobEnv) jobOf(t *testing.T, id int64) store.Job {
	t.Helper()

	job, err := e.store.Jobs().Get(t.Context(), id)
	require.NoError(t, err)

	return job
}

// fixtureZip returns the fixture of #138 written as a zip.
func fixtureZip(t *testing.T) []byte {
	t.Helper()

	data, err := os.ReadFile(writeExport(t, t.TempDir(), synthexport.Fixture()))
	require.NoError(t, err)

	return data
}

// fixtureXML returns the export.xml of the fixture.
func fixtureXML(t *testing.T) []byte {
	t.Helper()

	r, _ := openExportXML(t, writeExport(t, t.TempDir(), synthexport.Fixture()))

	data, err := io.ReadAll(r)
	require.NoError(t, err)

	return data
}

// Given an XML import whose inbox file holds the fixture, when its job import.xml runs, then the
// import ends completed, its report counts the records per type and the workouts, with the period
// and the routes, its inbox file is removed, and the routes of the run and of the walk are stored
// with their workouts.
func TestXMLImportJob_Fixture_CompletesWithItsReportAndItsRoutes(t *testing.T) {
	t.Parallel()

	e := newXMLJobEnv(t)
	imp, queued := e.upload(t, fixtureZip(t))

	e.tick(t.Context(), t)

	got := e.importOf(t, imp.ID)
	require.Equal(t, store.ImportCompleted, got.Status)
	require.Equal(t, store.JobCompleted, e.jobOf(t, queued.ID).Status)
	require.NoFileExists(t, filepath.Join(e.dir, imp.InboxFile))

	report := got.Report
	require.Equal(t, int64(fixtureRecords), report.Total.Received)
	require.Zero(t, report.Total.Invalid)
	require.Len(t, report.Types, 26, "24 types and 2 unsupported identifiers")
	require.Equal(t, int64(288), report.Types["step_count"].Received)
	require.Equal(t, int64(144), report.Types["heart_rate"].Received)
	require.Equal(t, store.ImportCounters{Received: 6, Unsupported: 6}, report.Types["ActivitySummary"])
	require.Equal(t, store.ImportCounters{Received: 2, Unsupported: 2}, report.Types["HKDataTypeIdentifierElectrocardiogram"])
	require.Equal(t, store.ImportCounters{Received: 5, Inserted: 5}, report.Workouts)

	var received int64
	for _, counters := range report.Types {
		received += counters.Received
	}

	require.Equal(t, report.Total.Received, received+report.Workouts.Received, "every record counts under its type or the workouts")

	require.Equal(t, store.ImportPeriod{
		Start: time.Date(2025, 12, 29, 22, 52, 0, 0, time.UTC),
		End:   time.Date(2026, 1, 4, 23, 0, 0, 0, time.UTC),
	}, report.Period)
	require.Equal(t, int64(2), report.Routes)

	stored, err := e.store.Workouts().List(t.Context(), e.user.ID, store.WorkoutsQuery{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		Limit: 100, WithDuplicates: true,
	})
	require.NoError(t, err)

	var routed []string

	for _, w := range stored {
		rt, err := e.store.Workouts().GetRoute(t.Context(), e.user.ID, w.ID)
		if err == nil {
			require.Positive(t, rt.PointCount)
			routed = append(routed, w.Activity+" "+w.Start.Format(time.RFC3339))
		} else {
			require.ErrorIs(t, err, store.ErrNotFound)
		}
	}

	require.ElementsMatch(t, []string{"running 2025-12-30T06:00:00Z", "walking 2026-01-03T06:28:00Z"}, routed)
}

// Given the same export imported a second time, then the second report counts no record inserted
// or updated.
func TestXMLImportJob_SameExportTwice_InsertsAndUpdatesNothing(t *testing.T) {
	t.Parallel()

	e := newXMLJobEnv(t)
	data := fixtureZip(t)

	e.upload(t, data)
	e.tick(t.Context(), t)

	second, _ := e.upload(t, data)
	e.tick(t.Context(), t)

	got := e.importOf(t, second.ID)
	require.Equal(t, store.ImportCompleted, got.Status)
	require.Equal(t, int64(fixtureRecords), got.Report.Total.Received)
	require.Zero(t, got.Report.Total.Inserted)
	require.Zero(t, got.Report.Total.Updated)
}

// Given a job stopped after some batches, when it runs again, then it resumes at the stored
// position and its report equals the one of an uninterrupted run.
func TestXMLImportJob_StoppedAfterSomeBatches_ResumesWithTheReportOfAnUninterruptedRun(t *testing.T) {
	t.Parallel()

	whole := newXMLJobEnv(t)
	wholeImp, _ := whole.upload(t, fixtureZip(t))
	whole.tick(t.Context(), t)
	want := whole.importOf(t, wholeImp.ID)

	e := newXMLJobEnv(t)
	imp, queued := e.upload(t, fixtureZip(t))

	stopped, stop := context.WithCancel(t.Context())
	e.pipeline.stopOnce(750, stop)
	e.tick(stopped, t)

	interrupted := e.importOf(t, imp.ID)
	require.Equal(t, store.ImportRunning, interrupted.Status, "a shutdown leaves the import running")
	require.Equal(t, int64(700), interrupted.Report.Total.Received, "7 batches are committed")
	require.Equal(t, store.JobRunning, e.jobOf(t, queued.ID).Status, "the stopped process could not release the job")

	e.clk.Advance(2 * time.Minute)
	e.tick(t.Context(), t)

	got := e.importOf(t, imp.ID)
	require.Equal(t, store.ImportCompleted, got.Status)
	require.Equal(t, want.Position, got.Position)
	require.Equal(t, want.Report, got.Report)
}

// Given a job whose payload has a path and an import without inbox file, then it reads that file
// and never removes it. Given that file gone, then the import fails without retry, as for a missing
// inbox file.
func TestXMLImportJob_PathInPayload_ReadsTheFileInPlace(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()

		e := newXMLJobEnv(t)
		path := writeExport(t, t.TempDir(), synthexport.Fixture())

		imp, _ := e.create(t, store.NewImport{UserID: e.user.ID, Origin: store.OriginCLI}, path)
		e.tick(t.Context(), t)

		got := e.importOf(t, imp.ID)
		require.Equal(t, store.ImportCompleted, got.Status)
		require.Equal(t, int64(fixtureRecords), got.Report.Total.Received)
		require.FileExists(t, path, "the file of a CLI import is never removed")
	})

	t.Run("gone", func(t *testing.T) {
		t.Parallel()

		e := newXMLJobEnv(t)
		path := filepath.Join(t.TempDir(), "export.zip")

		imp, queued := e.create(t, store.NewImport{UserID: e.user.ID, Origin: store.OriginCLI}, path)
		e.tick(t.Context(), t)

		got := e.importOf(t, imp.ID)
		require.Equal(t, store.ImportFailed, got.Status)
		require.Equal(t, []string{"the file of the import is missing"}, got.Report.Errors)

		job := e.jobOf(t, queued.ID)
		require.Equal(t, store.JobFailed, job.Status, "the job is not retried")
		require.Equal(t, int64(1), job.Attempts)
	})
}

// corruptedZip returns the zip of the fixture with bytes overwritten in the middle of the
// compressed data of its export.xml, so that the records before them are read.
func corruptedZip(t *testing.T) []byte {
	t.Helper()

	data := fixtureZip(t)

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)

	const name = "apple_health_export/export.xml"

	for _, entry := range zr.File {
		if entry.Name == name {
			return corrupt(data, dataOffset(t, data, name)+int(entry.CompressedSize64/2), 64) //nolint:gosec // G115: the size of a generated entry
		}
	}

	t.Fatalf("the zip has no entry %s", name)

	return nil
}

// Given a file that is not an export, a truncated document or a corrupted zip, when its job runs,
// then the import fails at once, without retry, with the fixed message of its cause as its sample
// error message, and the batches committed before stay.
func TestXMLImportJob_FileThatFailsAsAWhole_FailsAtOnceAndKeepsTheCommittedBatches(t *testing.T) {
	t.Parallel()

	xml := fixtureXML(t)

	tests := []struct {
		name      string
		data      []byte
		message   string
		committed bool
	}{
		{name: "not an export", data: []byte("name,value\nsteps,1240\n"), message: "the file is not an Apple Health export"},
		{name: "truncated document", data: xml[:len(xml)/2], message: "export.xml is truncated", committed: true},
		{name: "malformed document", data: bytes.Replace(xml, []byte("</HealthData>"), []byte("</Health>"), 1), message: "export.xml is not well-formed XML", committed: true},
		{name: "corrupted zip", data: corruptedZip(t), message: "the zip file is corrupted", committed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := newXMLJobEnv(t)
			imp, queued := e.upload(t, tt.data)

			e.tick(t.Context(), t)

			got := e.importOf(t, imp.ID)
			require.Equal(t, store.ImportFailed, got.Status)
			require.Equal(t, []string{tt.message}, got.Report.Errors)
			require.FileExists(t, filepath.Join(e.dir, imp.InboxFile), "the file of a failed import is kept")

			if tt.committed {
				require.Positive(t, got.Report.Total.Inserted, "the batches committed before stay")
			}

			job := e.jobOf(t, queued.ID)
			require.Equal(t, store.JobFailed, job.Status, "the job is not retried")
			require.Equal(t, int64(1), job.Attempts)
		})
	}
}
