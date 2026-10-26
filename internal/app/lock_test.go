package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Given another process holds the lock on the data directory, then acquiring it fails with
// `another Verve process is using <data dir>`.
func TestLock_SecondAcquireFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	first, err := AcquireLock(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, first.Release()) })

	second, err := AcquireLock(dir)
	require.Nil(t, second)
	require.EqualError(t, err, "another Verve process is using "+dir)
}

// Given the lock was released (the process has exited), then a new process can acquire it immediately.
func TestLock_ReleaseAllowsReacquire(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	first, err := AcquireLock(dir)
	require.NoError(t, err)
	require.NoError(t, first.Release())

	second, err := AcquireLock(dir)
	require.NoError(t, err)
	require.NoError(t, second.Release())
}
