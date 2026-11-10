package backup_test

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/backup"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

var backupTime = time.Date(2026, 11, 10, 14, 3, 5, 0, time.UTC)

// secretKey is the content of the secret key file of the tests.
const secretKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// fixture is a data directory with a migrated database and a secret key file, and the backups of
// that directory.
type fixture struct {
	store   store.Store
	config  backup.Config
	clock   *clock.Fake
	backups *backup.Backups
}

func newFixture(t *testing.T) fixture {
	t.Helper()

	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "verve.db")
	keyPath := filepath.Join(dataDir, "secret.key")
	backupsDir := filepath.Join(dataDir, "backups")

	require.NoError(t, os.Mkdir(backupsDir, 0o750))
	require.NoError(t, os.WriteFile(keyPath, []byte(secretKey), 0o600))

	db, err := sqlite.Open(t.Context(), dbPath)
	require.NoError(t, err)

	_, err = sqlite.Migrate(t.Context(), db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	s := sqlite.New(db, clock.Real())
	t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

	require.NoError(t, s.InstanceSettings().Set(t.Context(), "map.mode", "none"))

	cfg := backup.Config{
		Dir:       backupsDir,
		DB:        dbPath,
		SecretKey: keyPath,
		Version:   "1.2.3",
		FreeSpace: func(string) (uint64, error) { return 1 << 40, nil },
	}
	clk := clock.NewFake(backupTime)

	return fixture{store: s, config: cfg, clock: clk, backups: backup.New(cfg, clk)}
}

// entry is a file of an archive.
type entry struct {
	mode    int64
	content []byte
}

// readArchive returns the files of the archive at path, by name.
func readArchive(t *testing.T, path string) map[string]entry {
	t.Helper()

	f, err := os.Open(path) //nolint:gosec // G304: the path is in the temporary directory of the test
	require.NoError(t, err)

	defer func() { require.NoError(t, f.Close()) }()

	zr, err := zstd.NewReader(f)
	require.NoError(t, err)

	defer zr.Close()

	entries := map[string]entry{}
	tr := tar.NewReader(zr)

	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return entries
		}

		require.NoError(t, err)

		content, err := io.ReadAll(tr)
		require.NoError(t, err)

		entries[header.Name] = entry{mode: header.Mode, content: content}
	}
}

// openArchivedDB writes the database of entries to a file, requires that it passes the integrity
// check and returns it as a store.
func openArchivedDB(t *testing.T, entries map[string]entry) store.Store {
	t.Helper()

	dbEntry, ok := entries["verve.db"]
	require.True(t, ok, "the archive must hold verve.db")

	path := filepath.Join(t.TempDir(), "verve.db")
	require.NoError(t, os.WriteFile(path, dbEntry.content, 0o600))

	db, err := sqlite.Open(t.Context(), path)
	require.NoError(t, err)

	var integrity string
	require.NoError(t, db.Read().QueryRowContext(t.Context(), "PRAGMA integrity_check").Scan(&integrity))
	require.Equal(t, "ok", integrity)

	s := sqlite.New(db, clock.Real())
	t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

	return s
}

// dirNames returns the names of the entries of dir.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

// Given a backup, then the archive is backups/verve-<version>-<UTC timestamp>-<reason>.tar.zst
// with the permissions 0600, and holds the database, which passes the integrity check, and the
// secret key.
func TestCreate_ArchiveHoldsTheDatabaseAndTheSecretKey(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	path, err := f.backups.Create(t.Context(), f.store, backup.ReasonManual)
	require.NoError(t, err)

	require.Equal(t, filepath.Join(f.config.Dir, "verve-1.2.3-20261110T140305Z-manual.tar.zst"), path)
	require.Equal(t, []string{"verve-1.2.3-20261110T140305Z-manual.tar.zst"}, dirNames(t, f.config.Dir))

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	entries := readArchive(t, path)
	require.Len(t, entries, 2)
	require.Equal(t, []byte(secretKey), entries["secret.key"].content)
	require.Equal(t, int64(0o600), entries["secret.key"].mode)
	require.Equal(t, int64(0o600), entries["verve.db"].mode)

	setting, err := openArchivedDB(t, entries).InstanceSettings().Get(t.Context(), "map.mode")
	require.NoError(t, err)
	require.Equal(t, "none", setting.Value)
}

// Given the secret key set through VERVE_SECRET_KEY, so no key file, then the archive holds only
// the database. The same goes when the key file does not exist yet.
func TestCreate_WithoutKeyFile_ArchiveHoldsOnlyTheDatabase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		secretKey func(cfg backup.Config) string
	}{
		{name: "key configured", secretKey: func(backup.Config) string { return "" }},
		{name: "key file not generated yet", secretKey: func(cfg backup.Config) string {
			return filepath.Join(filepath.Dir(cfg.Dir), "missing.key")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			cfg := f.config
			cfg.SecretKey = tt.secretKey(cfg)

			path, err := backup.New(cfg, f.clock).Create(t.Context(), f.store, backup.ReasonManual)
			require.NoError(t, err)

			entries := readArchive(t, path)
			require.Len(t, entries, 1)
			openArchivedDB(t, entries)
		})
	}
}

// failingStore fails the snapshot or the audit event of a backup.
type failingStore struct {
	store.Store

	snapshotErr error
	auditErr    error
}

func (s failingStore) Snapshot(ctx context.Context, path string) error {
	if s.snapshotErr != nil {
		return s.snapshotErr
	}

	return s.Store.Snapshot(ctx, path)
}

func (s failingStore) AuditEvents() store.AuditEventsRepository {
	return failingAudit{AuditEventsRepository: s.Store.AuditEvents(), err: s.auditErr}
}

type failingAudit struct {
	store.AuditEventsRepository

	err error
}

func (a failingAudit) Append(ctx context.Context, event store.NewAuditEvent) (store.AuditEvent, error) {
	if a.err != nil {
		return store.AuditEvent{}, a.err
	}

	return a.AuditEventsRepository.Append(ctx, event)
}

// Given a failure at any step, then the backup fails, no archive exists and the temporary files
// are removed.
func TestCreate_Failure_LeavesNoFile(t *testing.T) {
	t.Parallel()

	errBroken := errors.New("broken")

	tests := []struct {
		name  string
		setup func(t *testing.T, f *fixture) store.Store
	}{
		{name: "snapshot", setup: func(_ *testing.T, f *fixture) store.Store {
			return failingStore{Store: f.store, snapshotErr: errBroken}
		}},
		{name: "secret key unreadable", setup: func(t *testing.T, f *fixture) store.Store {
			require.NoError(t, os.Remove(f.config.SecretKey))
			require.NoError(t, os.Mkdir(f.config.SecretKey, 0o700))

			return f.store
		}},
		{name: "archive name taken", setup: func(t *testing.T, f *fixture) store.Store {
			taken := filepath.Join(f.config.Dir, "verve-1.2.3-20261110T140305Z-manual.tar.zst")
			require.NoError(t, os.MkdirAll(filepath.Join(taken, "content"), 0o700))

			return f.store
		}},
		{name: "audit event", setup: func(_ *testing.T, f *fixture) store.Store {
			return failingStore{Store: f.store, auditErr: errBroken}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			s := tt.setup(t, &f)
			before := dirNames(t, f.config.Dir)

			path, err := f.backups.Create(t.Context(), s, backup.ReasonManual)
			require.Error(t, err)
			require.Empty(t, path)

			require.Equal(t, before, dirNames(t, f.config.Dir))
		})
	}
}

// Given the temporary files of a backup that crashed, then the next backup removes them.
func TestCreate_RemovesTheTemporaryFilesOfACrashedBackup(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	crashed := filepath.Join(f.config.Dir, ".verve-backup-crashed")
	require.NoError(t, os.Mkdir(crashed, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(crashed, "verve.db"), []byte("partial"), 0o600))

	path, err := f.backups.Create(t.Context(), f.store, backup.ReasonManual)
	require.NoError(t, err)

	require.Equal(t, []string{filepath.Base(path)}, dirNames(t, f.config.Dir))
}

// blockingStore blocks the snapshot of a backup until release is closed, after started is closed.
type blockingStore struct {
	store.Store

	started chan struct{}
	release chan struct{}
}

func (s blockingStore) Snapshot(ctx context.Context, path string) error {
	close(s.started)
	<-s.release

	return s.Store.Snapshot(ctx, path)
}

// Given a backup in progress, then another backup meanwhile keeps its temporary files, and both
// complete.
func TestCreate_KeepsTheTemporaryFilesOfABackupInProgress(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	blocked := blockingStore{Store: f.store, started: make(chan struct{}), release: make(chan struct{})}

	type result struct {
		path string
		err  error
	}

	first := make(chan result, 1)

	go func() {
		path, err := f.backups.Create(t.Context(), blocked, backup.ReasonManual)
		first <- result{path: path, err: err}
	}()

	select {
	case <-blocked.started:
	case res := <-first:
		require.FailNow(t, "the first backup ended before its snapshot", "path %q, error %v", res.path, res.err)
	}

	f.clock.Advance(time.Second)

	second, err := f.backups.Create(t.Context(), f.store, backup.ReasonManual)
	close(blocked.release)

	require.NoError(t, err)

	res := <-first
	require.NoError(t, res.err)
	require.NotEqual(t, second, res.path)
	require.ElementsMatch(t, []string{filepath.Base(res.path), filepath.Base(second)}, dirNames(t, f.config.Dir))

	openArchivedDB(t, readArchive(t, res.path))
}

// Given a backup, then the audit log records it, without actor, with its reason and file name.
func TestCreate_RecordsTheAuditEvent(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	_, err := f.backups.Create(t.Context(), f.store, backup.ReasonManual)
	require.NoError(t, err)

	events, err := f.store.AuditEvents().List(t.Context(), store.AuditEventsFilter{}, store.PageRequest{Limit: 10})
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "backup.created", events[0].Type)
	require.Zero(t, events[0].ActorID)
	require.JSONEq(t, `{"reason":"manual","file":"verve-1.2.3-20261110T140305Z-manual.tar.zst"}`, string(events[0].Payload))
}
