package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeFiles creates files, by slash-separated path, under dir.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
}

// readFiles returns the files under dir by slash-separated path.
func readFiles(t *testing.T, dir string) map[string]string {
	t.Helper()

	files := map[string]string{}
	fsys := os.DirFS(dir)

	require.NoError(t, fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		content, err := fs.ReadFile(fsys, name)
		files[name] = string(content)

		return err
	}))

	return files
}

// Given a previous build in the destination, then syncing replaces it with the Vite output,
// removing the stale files and directories but keeping the .gitignore.
func TestSyncDist_ReplacesPreviousBuildAndKeepsGitignore(t *testing.T) {
	t.Parallel()

	src, dst := t.TempDir(), t.TempDir()
	writeFiles(t, src, map[string]string{
		"index.html":                "new index",
		"assets/index-3f9a1c2b.js":  "new js",
		"icons/nested/icon-192.png": "png",
	})
	writeFiles(t, dst, map[string]string{
		".gitignore":               "*\n!.gitignore\n",
		"index.html":               "old index",
		"assets/index-00000000.js": "old js",
		"stale/file.txt":           "stale",
	})

	require.NoError(t, syncDist(src, dst))

	require.Equal(t, map[string]string{
		".gitignore":                "*\n!.gitignore\n",
		"index.html":                "new index",
		"assets/index-3f9a1c2b.js":  "new js",
		"icons/nested/icon-192.png": "png",
	}, readFiles(t, dst))
}

// Given a missing Vite output, then syncing fails and leaves the destination untouched.
func TestSyncDist_MissingSourceFails(t *testing.T) {
	t.Parallel()

	dst := t.TempDir()
	writeFiles(t, dst, map[string]string{".gitignore": "*\n!.gitignore\n", "index.html": "index"})

	err := syncDist(filepath.Join(t.TempDir(), "missing"), dst)
	require.ErrorIs(t, err, fs.ErrNotExist)

	require.Equal(t, map[string]string{".gitignore": "*\n!.gitignore\n", "index.html": "index"}, readFiles(t, dst))
}
