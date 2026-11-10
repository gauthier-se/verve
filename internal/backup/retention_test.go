package backup_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/backup"
	"github.com/gauthier-se/verve/internal/clock"
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
			for _, name := range tt.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o600))
			}

			backups := backup.New(backup.Config{Dir: dir}, clock.NewFake(backupTime))
			require.NoError(t, backups.ApplyRetention(tt.keep))

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

// Given a directory and a temporary directory of a backup in progress named like scheduled
// archives, then retention leaves them alone: it only removes regular files.
func TestApplyRetention_LeavesDirectoriesAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	folder := "verve-1.2.3-20261101T030000Z-scheduled.tar.zst"
	archive := "verve-1.2.3-20261109T030000Z-scheduled.tar.zst"

	require.NoError(t, os.Mkdir(filepath.Join(dir, folder), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, archive), nil, 0o600))

	backups := backup.New(backup.Config{Dir: dir}, clock.NewFake(backupTime))
	require.NoError(t, backups.ApplyRetention(1))

	require.ElementsMatch(t, []string{folder, archive}, dirNames(t, dir))
}
