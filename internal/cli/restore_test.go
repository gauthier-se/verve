package cli

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/app"
	"github.com/gauthier-se/verve/internal/config"
)

// fakeRestorer records the calls of the restore runner and returns a canned result.
type fakeRestorer struct {
	calls   int
	cfg     config.Config
	archive string
	result  app.RestoreResult
	err     error
}

func (f *fakeRestorer) restore(_ context.Context, cfg config.Config, archive string, _ *slog.Logger) (app.RestoreResult, error) {
	f.calls++
	f.cfg = cfg
	f.archive = archive

	return f.result, f.err
}

func depsWithRestorer(env map[string]string, r *fakeRestorer) Deps {
	deps := depsWithEnv(env)
	deps.Restore = r.restore

	return deps
}

// Given verve restore <file>, then it restores the file with the loaded configuration and prints
// the pre-restore backup, what it restored and the migrations it applied.
func TestRestore_PrintsWhatItDid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result app.RestoreResult
		out    string
	}{
		{
			name: "database and key, migrated",
			result: app.RestoreResult{
				PreRestore: "/srv/verve/backups/verve-0.4.0-20261110T140305Z-pre-restore.tar.zst",
				SecretKey:  true,
				Migrations: app.MigrateResult{Applied: []app.Migration{{Version: 13, Name: "jobs_user_id"}}, Version: 13},
			},
			out: "Backed up the current database to /srv/verve/backups/verve-0.4.0-20261110T140305Z-pre-restore.tar.zst\n" +
				"Restored the database and the secret key of verve-0.3.0-20261101T030000Z-scheduled.tar.zst\n" +
				"Applied 00013_jobs_user_id\n" +
				"Database schema migrated to version 13 (1 migration applied)\n",
		},
		{
			name:   "database only, on a new data directory",
			result: app.RestoreResult{Migrations: app.MigrateResult{Version: 13}},
			out: "Restored the database of verve-0.3.0-20261101T030000Z-scheduled.tar.zst, which holds no secret key: " +
				"the current key stays\n" +
				"Database schema up to date (version 13)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &fakeRestorer{result: tt.result}
			archive := "/srv/backups/verve-0.3.0-20261101T030000Z-scheduled.tar.zst"

			res := run(t, depsWithRestorer(map[string]string{"VERVE_DATA_DIR": "/srv/verve"}, r), "restore", archive)

			require.Equal(t, 0, res.code, res.stderr)
			require.Equal(t, 1, r.calls)
			require.Equal(t, "/srv/verve", r.cfg.DataDir)
			require.Equal(t, archive, r.archive)
			require.Equal(t, tt.out, res.stdout)
		})
	}
}

// Given verve restore when the restore fails, then it exits with status 1 and prints the error.
func TestRestore_ErrorExitsNonZero(t *testing.T) {
	t.Parallel()

	r := &fakeRestorer{err: errors.New("invalid archive backup.tar.zst: archive holds no verve.db")}

	res := run(t, depsWithRestorer(nil, r), "restore", "backup.tar.zst")

	require.Equal(t, 1, res.code)
	require.Equal(t, "Error: invalid archive backup.tar.zst: archive holds no verve.db\n", res.stderr)
	require.Empty(t, res.stdout)
}

// Given verve restore without a file or with two, then it fails and restores nothing.
func TestRestore_NeedsOneFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "no file", want: "Error: accepts 1 arg(s), received 0\n"},
		{name: "two files", args: []string{"a.tar.zst", "b.tar.zst"}, want: "Error: accepts 1 arg(s), received 2\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &fakeRestorer{}

			res := run(t, depsWithRestorer(nil, r), append([]string{"restore"}, tt.args...)...)

			require.Equal(t, 1, res.code)
			require.Equal(t, tt.want, res.stderr)
			require.Zero(t, r.calls)
		})
	}
}

// Given serve holds the data directory lock, when verve restore runs, then it refuses with the lock
// error and the backup stays unrestored.
func TestRestore_ServerRunningIsRefused(t *testing.T) {
	t.Parallel()

	deps, dataDir := userDeps(t)
	deps.Backup = app.Backup
	deps.Restore = app.Restore

	backup := run(t, deps, "backup")
	require.Equal(t, 0, backup.code, backup.stderr)

	lock, err := app.AcquireLock(dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Release()) })

	res := run(t, deps, "restore", strings.TrimSuffix(backup.stdout, "\n"))

	require.Equal(t, 1, res.code)
	require.Equal(t, "Error: another Verve process is using "+dataDir+": stop verve serve before restoring a backup\n", res.stderr)
	require.Empty(t, res.stdout)
}

// Given a backup made by verve backup and a stopped server, when verve restore runs on it, then it
// restores it and keeps the current database as a pre-restore backup.
func TestRestore_RestoresABackupOfVerveBackup(t *testing.T) {
	t.Parallel()

	deps, dataDir := userDeps(t)
	deps.Backup = app.Backup
	deps.Restore = app.Restore

	backup := run(t, deps, "backup")
	require.Equal(t, 0, backup.code, backup.stderr)

	archive := strings.TrimSuffix(backup.stdout, "\n")

	res := run(t, deps, "restore", archive)

	require.Equal(t, 0, res.code, res.stderr)

	lines := strings.Split(strings.TrimSuffix(res.stdout, "\n"), "\n")
	require.Len(t, lines, 3, res.stdout)
	require.Regexp(t, "^Backed up the current database to "+regexp.QuoteMeta(filepath.Join(dataDir, "backups")+string(filepath.Separator))+
		`verve-.+-pre-restore\.tar\.zst$`, lines[0])
	require.Equal(t, "Restored the database of "+filepath.Base(archive)+", which holds no secret key: the current key stays", lines[1])
	require.Regexp(t, `^Database schema up to date \(version \d+\)$`, lines[2])
}
