package app

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"iter"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/ingest/applexml"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
	"github.com/gauthier-se/verve/internal/synthexport"
)

// fixtureRecords is the number of records of the fixture of synthexport.
const fixtureRecords = 1670

// importTest is a migrated data directory with the shipped metric types and a user, alice, and a
// store of its database, opened next to the commands of the test as by another process.
type importTest struct {
	cfg   config.Config
	store store.Store
	alice store.User
}

func newImportTest(t *testing.T) importTest {
	t.Helper()

	ctx := t.Context()
	cfg := testConfig(t.TempDir())

	_, err := Migrate(ctx, cfg, discardLogger())
	require.NoError(t, err)

	db, err := sqlite.Open(ctx, filepath.Join(cfg.DataDir, dbFile))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.CloseShared()) })

	st := sqlite.New(db, clock.Real())

	shipped, err := catalog.Shipped()
	require.NoError(t, err)
	require.NoError(t, st.MetricTypes().Sync(ctx, shipped))

	it := importTest{cfg: cfg, store: st}
	it.alice = it.createUser(t, "alice", store.UserActive)

	return it
}

// createUser stores a member named username with status.
func (it importTest) createUser(t *testing.T, username string, status store.UserStatus) store.User {
	t.Helper()

	user, err := it.store.Users().Create(t.Context(), store.NewUser{Username: username, Role: store.RoleMember})
	require.NoError(t, err)

	if status != store.UserActive {
		user, err = it.store.Users().SetStatus(t.Context(), user.ID, status)
		require.NoError(t, err)
	}

	return user
}

// importsOf returns the imports of user.
func (it importTest) importsOf(t *testing.T, user store.User) []store.Import {
	t.Helper()

	imports, err := it.store.Imports().List(t.Context(), user.ID, store.ImportsFilter{}, store.PageRequest{Limit: 10})
	require.NoError(t, err)

	return imports
}

// jobsWith returns the jobs of status.
func (it importTest) jobsWith(t *testing.T, status store.JobStatus) []store.Job {
	t.Helper()

	found, err := it.store.Jobs().ListByStatus(t.Context(), status, store.PageRequest{Limit: 10})
	require.NoError(t, err)

	return found
}

// writeFixture writes the export.zip of the fixture into a new directory and returns its path.
func writeFixture(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "export.zip")

	f, err := os.Create(path) //nolint:gosec // G304: the path is in the temporary directory of the test
	require.NoError(t, err)
	require.NoError(t, synthexport.Write(t.Context(), f, synthexport.Fixture()))
	require.NoError(t, f.Close())

	return path
}

// checksum returns the SHA-256 of the file at path.
func checksum(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // G304: the path is in the temporary directory of the test
	require.NoError(t, err)

	return sha256.Sum256(data)
}

// noWait fails the test when an import waits.
func noWait(t *testing.T) func() {
	t.Helper()

	return func() { t.Error("the import waited") }
}

// Given ImportXML on the fixture, then it creates an import of origin cli without inbox file, whose
// job import.xml holds the absolute path of the file and runs in this process, and returns the
// import once it completed, with its report. The file is still there, unchanged.
func TestImportXML_Fixture_CompletesInThisProcess(t *testing.T) {
	t.Parallel()

	it := newImportTest(t)
	path := writeFixture(t)
	before := checksum(t, path)

	imp, err := ImportXML(t.Context(), it.cfg, XMLImport{
		Login: "alice", Path: path, Waiting: noWait(t), Progress: func(float64) {},
	}, discardLogger())
	require.NoError(t, err)

	require.Equal(t, store.ImportCompleted, imp.Status)
	require.Equal(t, store.OriginCLI, imp.Origin)
	require.Equal(t, it.alice.ID, imp.UserID)
	require.Empty(t, imp.InboxFile)
	require.Equal(t, int64(fixtureRecords), imp.Report.Total.Received)
	require.Equal(t, store.ImportPeriod{
		Start: time.Date(2025, 12, 29, 22, 52, 0, 0, time.UTC),
		End:   time.Date(2026, 1, 4, 23, 0, 0, 0, time.UTC),
	}, imp.Report.Period)
	require.Equal(t, int64(2), imp.Report.Routes)

	stored := it.importsOf(t, it.alice)
	require.Len(t, stored, 1)
	require.Equal(t, imp.ID, stored[0].ID)
	require.Equal(t, store.ImportCompleted, stored[0].Status)

	// No worker runs: the job completed in this process.
	completed := it.jobsWith(t, store.JobCompleted)
	require.Len(t, completed, 1)
	require.Equal(t, applexml.KindImport, completed[0].Kind)
	require.Equal(t, it.alice.ID, completed[0].UserID)
	require.Equal(t, ingest.SerialKey(it.alice.ID), completed[0].SerialKey)

	payload, err := json.Marshal(map[string]any{"importId": imp.ID, "path": path})
	require.NoError(t, err)
	require.JSONEq(t, string(payload), string(completed[0].Payload))

	require.FileExists(t, path)
	require.Equal(t, before, checksum(t, path))

	inbox, err := os.ReadDir(newLayout(it.cfg.DataDir).Inbox)
	require.NoError(t, err)
	require.Empty(t, inbox)
}

// Given a username in another case than at creation, then ImportXML imports for that user.
func TestImportXML_LoginInAnotherCase_NamesTheSameUser(t *testing.T) {
	t.Parallel()

	it := newImportTest(t)
	elodie := it.createUser(t, "Élodie", store.UserActive)

	imp, err := ImportXML(t.Context(), it.cfg, XMLImport{
		Login: "éLODIE", Path: writeFixture(t), Waiting: noWait(t), Progress: func(float64) {},
	}, discardLogger())
	require.NoError(t, err)

	require.Equal(t, store.ImportCompleted, imp.Status)
	require.Equal(t, elodie.ID, imp.UserID)
	require.Empty(t, it.importsOf(t, it.alice))
}

// Given an unknown, disabled or deleted user, or a path that cannot be opened, then ImportXML
// returns why and creates neither an import nor a job.
func TestImportXML_Refused_CreatesNoImport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		login string
		path  func(t *testing.T) string
		check func(t *testing.T, err error)
	}{
		{
			name:  "unknown user",
			login: "bob",
			path:  writeFixture,
			check: func(t *testing.T, err error) { require.ErrorIs(t, err, store.ErrNotFound) },
		},
		{
			name:  "disabled user",
			login: "carol",
			path:  writeFixture,
			check: func(t *testing.T, err error) { require.ErrorIs(t, err, auth.ErrUserDisabled) },
		},
		{
			name:  "user being deleted",
			login: "dave",
			path:  writeFixture,
			check: func(t *testing.T, err error) { require.ErrorIs(t, err, auth.ErrUserDeleting) },
		},
		{
			name:  "missing file",
			login: "alice",
			path:  func(t *testing.T) string { return filepath.Join(t.TempDir(), "export.zip") },
			check: func(t *testing.T, err error) {
				var pathErr *os.PathError
				require.ErrorAs(t, err, &pathErr)
				require.ErrorIs(t, err, fs.ErrNotExist)
			},
		},
		{
			name:  "directory",
			login: "alice",
			path:  func(t *testing.T) string { return t.TempDir() },
			check: func(t *testing.T, err error) {
				var pathErr *os.PathError
				require.ErrorAs(t, err, &pathErr)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			it := newImportTest(t)
			users := []store.User{
				it.alice,
				it.createUser(t, "carol", store.UserDisabled),
				it.createUser(t, "dave", store.UserDeleting),
			}

			_, err := ImportXML(t.Context(), it.cfg, XMLImport{
				Login: tt.login, Path: tt.path(t), Waiting: noWait(t), Progress: func(float64) {},
			}, discardLogger())
			tt.check(t, err)

			for _, user := range users {
				require.Empty(t, it.importsOf(t, user), user.Username)

				active, err := it.store.Jobs().ActiveJobs(t.Context(), ingest.SerialKey(user.ID))
				require.NoError(t, err)
				require.Empty(t, active, user.Username)
			}

			for _, status := range []store.JobStatus{store.JobCompleted, store.JobFailed, store.JobCancelled} {
				require.Empty(t, it.jobsWith(t, status), status)
			}
		})
	}
}

// Given a file that is not an export, then the import fails with its sample error message, and
// ImportXML returns it failed.
func TestImportXML_NotAnExport_FailsWithItsSampleErrorMessage(t *testing.T) {
	t.Parallel()

	it := newImportTest(t)
	path := filepath.Join(t.TempDir(), "notes.txt")
	require.NoError(t, os.WriteFile(path, []byte("not an export\n"), 0o600))

	imp, err := ImportXML(context.WithoutCancel(t.Context()), it.cfg, XMLImport{
		Login: "alice", Path: path, Waiting: noWait(t), Progress: func(float64) {},
	}, discardLogger())
	require.NoError(t, err)

	require.Equal(t, store.ImportFailed, imp.Status)
	require.Equal(t, []string{"the file is not an Apple Health export"}, imp.Report.Errors)
	require.Len(t, it.jobsWith(t, store.JobFailed), 1)
	require.FileExists(t, path)
}

// pausingPipeline is a pipeline that, once it has read after records, closes paused and waits for
// the context of the run to be done, as a command that is stopped or killed while it imports.
type pausingPipeline struct {
	pipeline ingest.ImportPipeline
	after    int
	paused   chan struct{}
}

func (p *pausingPipeline) Run(ctx context.Context, userID, importID int64, records iter.Seq2[ingest.Record, error]) error {
	read := 0

	return p.pipeline.Run(ctx, userID, importID, func(yield func(ingest.Record, error) bool) {
		for r, err := range records {
			read++

			if read == p.after {
				close(p.paused)
				<-ctx.Done()
			}

			if !yield(r, err) {
				return
			}
		}
	})
}

// pauseAfter returns the options of an import whose batches hold 100 records and that pauses once
// it has read 150, closing paused, so that one batch is committed. Progress is reported every
// millisecond.
func pauseAfter(paused chan struct{}) importOptions {
	return importOptions{
		progressInterval: time.Millisecond,
		batchSize:        100,
		wrapPipeline: func(p ingest.ImportPipeline) ingest.ImportPipeline {
			return &pausingPipeline{pipeline: p, after: 150, paused: paused}
		},
	}
}

// holdImportKey creates an XML import of user whose job runs in another process, as an upload that
// the server imports, and returns its job.
func (it importTest) holdImportKey(t *testing.T, user store.User) store.Job {
	t.Helper()

	ctx := t.Context()

	imp, err := it.store.Imports().Create(ctx, store.NewImport{UserID: user.ID, Origin: store.OriginXML, InboxFile: "upload"})
	require.NoError(t, err)

	job, err := applexml.Job(user.ID, imp.ID, "")
	require.NoError(t, err)

	_, err = it.store.Jobs().Enqueue(ctx, job)
	require.NoError(t, err)

	claimed, _, err := it.store.Jobs().Claim(ctx, time.Minute)
	require.NoError(t, err)

	return claimed
}

// importResult is what importXML returned.
type importResult struct {
	imp store.Import
	err error
}

// importInBackground runs importXML in the background, and returns a channel that receives what it
// returned.
func (it importTest) importInBackground(ctx context.Context, req XMLImport, opts importOptions) <-chan importResult {
	done := make(chan importResult, 1)

	go func() {
		imp, err := importXML(ctx, it.cfg, req, discardLogger(), opts)
		done <- importResult{imp: imp, err: err}
	}()

	return done
}

// receive waits for a value on ch.
func receive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal(what)

		var zero T

		return zero
	}
}

// Given an import of the same user running in the server, then ImportXML says that it waits, creates
// its import only once that one ended, and then runs it.
func TestImportXML_OtherImportRunning_WaitsThenRunsItsOwn(t *testing.T) {
	t.Parallel()

	it := newImportTest(t)
	holder := it.holdImportKey(t, it.alice)

	var waits atomic.Int64

	waiting := make(chan struct{}, 1)
	done := it.importInBackground(t.Context(), XMLImport{
		Login: "alice", Path: writeFixture(t), Progress: func(float64) {},
		Waiting: func() {
			waits.Add(1)
			waiting <- struct{}{}
		},
	}, importOptions{progressInterval: time.Hour, waitInterval: 5 * time.Millisecond})

	receive(t, waiting, "the import did not wait")
	time.Sleep(20 * time.Millisecond)
	require.Len(t, it.importsOf(t, it.alice), 1, "only the running import exists while the command waits")

	require.NoError(t, it.store.Jobs().Complete(t.Context(), holder.ID, holder.Claims))

	res := receive(t, done, "the import did not return")
	require.NoError(t, res.err)
	require.Equal(t, store.ImportCompleted, res.imp.Status)
	require.Equal(t, store.OriginCLI, res.imp.Origin)
	require.Equal(t, int64(1), waits.Load())
	require.Len(t, it.importsOf(t, it.alice), 2)
}

// Given ImportXML stopped while it waits for another import of the user, then it returns the
// context error and leaves neither an import nor a job behind.
func TestImportXML_StoppedWhileWaiting_LeavesNoImport(t *testing.T) {
	t.Parallel()

	it := newImportTest(t)
	holder := it.holdImportKey(t, it.alice)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	res := receive(t, it.importInBackground(ctx, XMLImport{
		Login: "alice", Path: writeFixture(t), Progress: func(float64) {}, Waiting: cancel,
	}, importOptions{progressInterval: time.Hour, waitInterval: time.Hour}), "the import did not return")
	require.ErrorIs(t, res.err, context.Canceled)

	require.Len(t, it.importsOf(t, it.alice), 1)

	active, err := it.store.Jobs().ActiveJobs(t.Context(), ingest.SerialKey(it.alice.ID))
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, holder.ID, active[0].ID)
}

// Given ImportXML interrupted while its import runs, then it reports the progress of the import
// meanwhile, the import ends cancelled with the batches committed before, its job ends cancelled,
// and ImportXML returns the import. The file stays.
func TestImportXML_Interrupted_EndsCancelled(t *testing.T) {
	t.Parallel()

	it := newImportTest(t)
	path := writeFixture(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var (
		mu       sync.Mutex
		reported []float64
	)

	paused := make(chan struct{})
	done := it.importInBackground(ctx, XMLImport{
		Login: "alice", Path: path, Waiting: noWait(t),
		Progress: func(p float64) {
			mu.Lock()
			defer mu.Unlock()

			reported = append(reported, p)
		},
	}, pauseAfter(paused))

	receive(t, paused, "the import did not start")
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		return len(reported) > 0 && reported[len(reported)-1] > 0
	}, 10*time.Second, time.Millisecond, "the progress of the committed batch is reported")

	cancel()

	res := receive(t, done, "the import did not return")
	require.NoError(t, res.err)
	require.Equal(t, store.ImportCancelled, res.imp.Status)
	require.GreaterOrEqual(t, res.imp.Report.Total.Received, int64(100))
	require.Less(t, res.imp.Report.Total.Received, int64(fixtureRecords))
	require.Len(t, it.jobsWith(t, store.JobCancelled), 1)
	require.FileExists(t, path)

	mu.Lock()
	defer mu.Unlock()

	require.IsNonDecreasing(t, reported)

	for _, p := range reported {
		require.True(t, p >= 0 && p < 1, "progress %v", p)
	}
}

// The environment of the process that TestImportXML_Killed_TheServerResumesTheJob starts.
const (
	envHelperDataDir = "VERVE_TEST_IMPORT_DATA_DIR"
	envHelperPath    = "VERVE_TEST_IMPORT_PATH"
)

// TestImportXMLHelperProcess is the command that TestImportXML_Killed_TheServerResumesTheJob runs in
// another process and kills: it imports for alice, prints "paused" once a batch is committed, and
// then waits until it is killed.
func TestImportXMLHelperProcess(t *testing.T) {
	t.Parallel()

	dataDir := os.Getenv(envHelperDataDir)
	if dataDir == "" {
		t.Skip("the process of another test")
	}

	paused := make(chan struct{})

	go func() {
		<-paused
		fmt.Println("paused") //nolint:forbidigo // the signal to the test that runs the process
	}()

	_, err := importXML(context.Background(), testConfig(dataDir), XMLImport{
		Login: "alice", Path: os.Getenv(envHelperPath), Waiting: func() {}, Progress: func(float64) {},
	}, discardLogger(), pauseAfter(paused))
	t.Fatalf("the import returned instead of waiting to be killed: %v", err)
}

// Given the command killed while its import runs, then the server resumes the job from the stored
// position once its lease expires, reading the same path, and completes the import with the report
// of the whole export.
func TestImportXML_Killed_TheServerResumesTheJob(t *testing.T) {
	t.Parallel()

	it := newImportTest(t)
	path := writeFixture(t)
	before := checksum(t, path)
	ctx := t.Context()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestImportXMLHelperProcess$") //nolint:gosec // G204: the test binary
	cmd.Env = append(os.Environ(), envHelperDataDir+"="+it.cfg.DataDir, envHelperPath+"="+path)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	paused := make(chan string, 1)

	go func() {
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		paused <- line
	}()

	require.Equal(t, "paused\n", receive(t, paused, "the command did not import"))
	require.NoError(t, cmd.Process.Kill())
	require.Error(t, cmd.Wait(), "the command was killed")

	running := it.importsOf(t, it.alice)
	require.Len(t, running, 1)
	require.Equal(t, store.ImportRunning, running[0].Status)
	require.Positive(t, running[0].Position)
	require.Equal(t, int64(100), running[0].Report.Total.Received)

	// The server, on the same database, once the lease of the job has expired.
	db, err := sqlite.Open(ctx, filepath.Join(it.cfg.DataDir, dbFile))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.CloseShared()) })

	clk := clock.NewFake(time.Now().Add(2 * time.Minute))
	server := sqlite.New(db, clk)

	shipped, err := catalog.Shipped()
	require.NoError(t, err)

	ing, err := newIngestion(server, shipped, newLayout(it.cfg.DataDir).Inbox, clk, 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, ing.Close()) })

	registry := jobs.NewRegistry()
	registerJobs(registry, server, clk, it.cfg, ing)

	worker := jobs.NewWorker(server, registry, clk, discardLogger(), jobs.Options{})

	var imp store.Import

	for range 10 {
		claimed, err := worker.Tick(ctx)
		require.NoError(t, err)

		imp, err = server.Imports().Get(ctx, it.alice.ID, running[0].ID)
		require.NoError(t, err)

		if !claimed || imp.Status.Finished() {
			break
		}
	}

	require.Equal(t, store.ImportCompleted, imp.Status)
	require.Equal(t, int64(fixtureRecords), imp.Report.Total.Received, "the resumed job skips the records of the committed batch")
	require.Equal(t, int64(2), imp.Report.Routes)

	completed := it.jobsWith(t, store.JobCompleted)
	require.Len(t, completed, 1)
	require.Equal(t, int64(2), completed[0].Claims)

	require.Equal(t, before, checksum(t, path))
}
