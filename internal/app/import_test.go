package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/ingest/applexml"
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
