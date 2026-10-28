package webui

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

// Given the placeholder only, then the web app is not built. Given an index.html, then it is.
func TestBuilt_OnlyWithIndex(t *testing.T) {
	t.Parallel()

	placeholder := fstest.MapFS{".gitignore": {Data: []byte("*\n!.gitignore\n")}}
	require.False(t, built(placeholder))

	placeholder["index.html"] = &fstest.MapFile{Data: []byte("<!doctype html>")}
	require.True(t, built(placeholder))
}

// Given the embedded directory, then FS is its content: the committed .gitignore is at its root.
func TestFS_IsTheDistDirectory(t *testing.T) {
	t.Parallel()

	_, err := fs.Stat(FS(), ".gitignore")
	require.NoError(t, err)
}
