package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// Given an empty data directory, then the subdirectories uploads/, inbox/, exports/, backups/
// and map/ are created with permissions 0750.
func TestEnsureDataDir_EmptyDirCreatesLayout(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "data")

	layout, err := EnsureDataDir(root)
	require.NoError(t, err)

	require.Equal(t, Layout{
		Root:    root,
		DB:      filepath.Join(root, "verve.db"),
		Lock:    filepath.Join(root, "verve.lock"),
		Uploads: filepath.Join(root, "uploads"),
		Inbox:   filepath.Join(root, "inbox"),
		Exports: filepath.Join(root, "exports"),
		Backups: filepath.Join(root, "backups"),
		Map:     filepath.Join(root, "map"),
	}, layout)

	for _, dir := range []string{root, layout.Uploads, layout.Inbox, layout.Exports, layout.Backups, layout.Map} {
		info, err := os.Stat(dir)
		require.NoError(t, err, dir)
		require.True(t, info.IsDir(), dir)
		require.Equal(t, os.FileMode(0o750), info.Mode().Perm(), dir)
	}

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 5, "the writability check must not leave files behind")
}

// Given a data directory that already has its layout, then EnsureDataDir succeeds again.
func TestEnsureDataDir_ExistingLayoutIsKept(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	_, err := EnsureDataDir(root)
	require.NoError(t, err)

	_, err = EnsureDataDir(root)
	require.NoError(t, err)
}

// Given a data directory that is not writable, then it fails with
// `data directory <path> is not writable: <cause>`.
func TestEnsureDataDir_NotWritableFails(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("directory permissions are not enforced on Windows")
	}

	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	root := t.TempDir()
	require.NoError(t, os.Chmod(root, 0o500))
	t.Cleanup(func() { require.NoError(t, os.Chmod(root, 0o750)) })

	_, err := EnsureDataDir(root)
	require.ErrorContains(t, err, "data directory "+root+" is not writable: ")
	require.ErrorIs(t, err, fs.ErrPermission)
}
