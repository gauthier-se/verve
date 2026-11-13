package ingest

import (
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
)

// fullDisk writes n bytes, then fails as a full disk does.
type fullDisk struct {
	w io.Writer
	n int
}

func (d *fullDisk) Write(p []byte) (int, error) {
	if len(p) > d.n {
		written, err := d.w.Write(p[:d.n])
		d.n -= written

		if err != nil {
			return written, err
		}

		return written, syscall.ENOSPC
	}

	written, err := d.w.Write(p)
	d.n -= written

	return written, err
}

// Given a disk that fills up while a body is written to the inbox, when Write runs, then it
// returns an error wrapping ENOSPC and leaves no file, so that the client retries later.
func TestInbox_Write_DiskFull_ReturnsENOSPCAndLeavesNoFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inbox := NewInbox(dir, clock.NewFake(time.Date(2026, 11, 13, 4, 0, 0, 0, time.UTC)))
	inbox.wrap = func(w io.Writer) io.Writer { return &fullDisk{w: w, n: 4} }

	_, err := inbox.Write(t.Context(), strings.NewReader(`{"format":"verve.ingest.v1"}`))
	require.ErrorIs(t, err, syscall.ENOSPC)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
}
