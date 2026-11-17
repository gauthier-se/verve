package ingest

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
)

// Given a disk that fills up while a body is appended to an upload, when Append runs, then it
// returns the number of bytes written and synced with an error wrapping ENOSPC, and the file holds
// these bytes, from which the upload resumes.
func TestUploads_Append_DiskFull_ReturnsTheSyncedBytesAndENOSPC(t *testing.T) {
	t.Parallel()

	clk := clock.NewFake(time.Date(2026, 11, 17, 4, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	uploads := NewUploads(dir, NewInbox(t.TempDir(), clk), clk)
	uploads.wrap = func(w io.Writer) io.Writer { return &fullDisk{w: w, n: 4} }

	name, err := uploads.Create(t.Context())
	require.NoError(t, err)

	written, err := uploads.Append(t.Context(), name, 0, strings.NewReader("0123456789"))
	require.ErrorIs(t, err, syscall.ENOSPC)
	require.Equal(t, int64(4), written)

	data, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // G304: a file of the test directory
	require.NoError(t, err)
	require.Equal(t, "0123", string(data))
}
