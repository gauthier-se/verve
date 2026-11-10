package backup_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/backup"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// Given the files of a backups directory, when retention keeps a number of scheduled backups, then
// only the newest scheduled archives by the timestamp of their name stay, whatever their version.
// Manual and pre-migration archives, and the files whose name does not parse, are left alone.
func TestApplyRetention(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		files   []string
		keep    int
		removed []string
	}{
		{
			name: "fewer than keep",
			files: []string{
				"verve-1.2.3-20261108T030000Z-scheduled.tar.zst",
				"verve-1.2.3-20261109T030000Z-scheduled.tar.zst",
			},
			keep: 3,
		},
		{
			name: "exactly keep",
			files: []string{
				"verve-1.2.3-20261108T030000Z-scheduled.tar.zst",
				"verve-1.2.3-20261109T030000Z-scheduled.tar.zst",
			},
			keep: 2,
		},
		{
			name: "oldest removed by timestamp, not by version or name",
			files: []string{
				"verve-0.9.0-20261110T030000Z-scheduled.tar.zst",
				"verve-1.2.3-20261107T030000Z-scheduled.tar.zst",
				"verve-1.2.3-20261109T030000Z-scheduled.tar.zst",
				"verve-1.10.0-20261108T030000Z-scheduled.tar.zst",
			},
			keep:    2,
			removed: []string{"verve-1.2.3-20261107T030000Z-scheduled.tar.zst", "verve-1.10.0-20261108T030000Z-scheduled.tar.zst"},
		},
		{
			name: "version with dashes",
			files: []string{
				"verve-1.3.0-rc.1-20261108T030000Z-scheduled.tar.zst",
				"verve-1.3.0-rc.1-20261109T030000Z-scheduled.tar.zst",
			},
			keep:    1,
			removed: []string{"verve-1.3.0-rc.1-20261108T030000Z-scheduled.tar.zst"},
		},
		{
			name: "manual and pre-migration archives never count nor go",
			files: []string{
				"verve-1.2.3-20261101T030000Z-manual.tar.zst",
				"verve-1.2.3-20261102T030000Z-pre-migration.tar.zst",
				"verve-1.2.3-20261108T030000Z-scheduled.tar.zst",
				"verve-1.2.3-20261109T030000Z-scheduled.tar.zst",
				"verve-1.2.3-20261111T030000Z-manual.tar.zst",
				"verve-1.2.3-20261112T030000Z-pre-migration.tar.zst",
			},
			keep:    1,
			removed: []string{"verve-1.2.3-20261108T030000Z-scheduled.tar.zst"},
		},
		{
			name: "names that do not parse",
			files: []string{
				"verve-1.2.3-20261101T030000Z-scheduled.tar.zst.bak",
				"verve-1.2.3-20261102T030000Z-scheduled.tar",
				"verve-1.2.3-2026-11-03T03:00:00Z-scheduled.tar.zst",
				"verve-1.2.3-20261104T030000-scheduled.tar.zst",
				"verve-1.2.3-20261332T030000Z-scheduled.tar.zst",
				"verve--20261105T030000Z-scheduled.tar.zst",
				"verve-20261106T030000Z-scheduled.tar.zst",
				"other-1.2.3-20261107T030000Z-scheduled.tar.zst",
				"verve-1.2.3-20261108T030000Z-weekly.tar.zst",
				"verve-1.2.3-20261108T030000Z-Scheduled.tar.zst",
				"notes.txt",
				"verve-1.2.3-20261109T030000Z-scheduled.tar.zst",
				"verve-1.2.3-20261110T030000Z-scheduled.tar.zst",
			},
			keep:    1,
			removed: []string{"verve-1.2.3-20261109T030000Z-scheduled.tar.zst"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			writeArchives(t, dir, tt.files...)

			backups := backup.New(backup.Config{Dir: dir}, clock.NewFake(backupTime))
			require.NoError(t, backups.ApplyRetention(t.Context(), tt.keep))

			var want []string

			for _, name := range tt.files {
				if !slices.Contains(tt.removed, name) {
					want = append(want, name)
				}
			}

			require.ElementsMatch(t, want, dirNames(t, dir))
		})
	}
}

// Given a directory named like an old scheduled archive, then retention leaves it alone: it only
// removes regular files.
func TestApplyRetention_LeavesDirectoriesAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	folder := "verve-1.2.3-20261101T030000Z-scheduled.tar.zst"
	archive := "verve-1.2.3-20261109T030000Z-scheduled.tar.zst"

	require.NoError(t, os.Mkdir(filepath.Join(dir, folder), 0o700))
	writeArchives(t, dir, archive)

	backups := backup.New(backup.Config{Dir: dir}, clock.NewFake(backupTime))
	require.NoError(t, backups.ApplyRetention(t.Context(), 1))

	require.ElementsMatch(t, []string{folder, archive}, dirNames(t, dir))
}

// Given pre-migration backups, when retention keeps those of the last 3 migrations, then only the 3
// newest pre-migration archives stay, by the timestamp of their name whatever their version.
// Scheduled and manual backups are untouched.
func TestApplyPreMigrationRetention_KeepsTheBackupsOfTheLastThreeMigrations(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeArchives(t, dir,
		"verve-1.0.0-20260801T120000Z-pre-migration.tar.zst",
		"verve-1.10.0-20260901T120000Z-pre-migration.tar.zst",
		"verve-1.1.0-20261001T120000Z-pre-migration.tar.zst",
		"verve-1.2.0-20261101T120000Z-pre-migration.tar.zst",
		"verve-1.2.3-20261110T140305Z-pre-migration.tar.zst",
		"verve-1.0.0-20260101T030000Z-scheduled.tar.zst",
		"verve-1.0.0-20260101T120000Z-manual.tar.zst",
	)

	backups := backup.New(backup.Config{Dir: dir}, clock.NewFake(backupTime))
	require.NoError(t, backups.ApplyPreMigrationRetention(t.Context()))

	require.ElementsMatch(t, []string{
		"verve-1.1.0-20261001T120000Z-pre-migration.tar.zst",
		"verve-1.2.0-20261101T120000Z-pre-migration.tar.zst",
		"verve-1.2.3-20261110T140305Z-pre-migration.tar.zst",
		"verve-1.0.0-20260101T030000Z-scheduled.tar.zst",
		"verve-1.0.0-20260101T120000Z-manual.tar.zst",
	}, dirNames(t, dir))
}

// Given a pre-migration backup, then it holds the database, removes no other archive, since
// retention waits for the migrations to succeed, and records no audit event, since the schema is
// not migrated yet.
func TestCreatePreMigration_RemovesNothingAndRecordsNoEvent(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	old := []string{
		"verve-1.0.0-20260801T120000Z-pre-migration.tar.zst",
		"verve-1.1.0-20260901T120000Z-pre-migration.tar.zst",
		"verve-1.1.0-20261001T120000Z-pre-migration.tar.zst",
	}
	writeArchives(t, f.config.Dir, old...)

	path, err := f.backups.CreatePreMigration(t.Context(), f.store)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(f.config.Dir, "verve-1.2.3-20261110T140305Z-pre-migration.tar.zst"), path)

	require.ElementsMatch(t, append(old, filepath.Base(path)), dirNames(t, f.config.Dir))

	openArchivedDB(t, readArchive(t, path))

	events, err := f.store.AuditEvents().List(t.Context(), store.AuditEventsFilter{}, store.PageRequest{Limit: 10})
	require.NoError(t, err)
	require.Empty(t, events)
}
