package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/config"
)

var loggedAddr = regexp.MustCompile(`addr=(127\.0\.0\.1:\d+)`)

// Given a data directory that does not exist, when Run starts, then it creates the directory
// with permissions 0750, takes the lock, serves, stops on context cancel and releases the lock.
func TestRun_CreatesDataDirLocksServesAndReleases(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "data")
	cfg := config.Default()
	cfg.DataDir = dataDir
	cfg.ListenAddr = "127.0.0.1:0"

	logger, logs := testLogger()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- Run(ctx, cfg, logger) }()

	var addr string

	require.Eventually(t, func() bool {
		m := loggedAddr.FindStringSubmatch(logs.String())
		if m == nil {
			return false
		}

		addr = m[1]

		return true
	}, 5*time.Second, 10*time.Millisecond, "Run must log the bound address")

	info, err := os.Stat(dataDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	require.Equal(t, os.FileMode(0o750), info.Mode().Perm())

	_, err = AcquireLock(dataDir)
	require.EqualError(t, err, "another Verve process is using "+dataDir)

	res, err := get(t.Context(), "http://"+addr+"/")
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	require.Equal(t, http.StatusNotFound, res.StatusCode)

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}

	lock, err := AcquireLock(dataDir)
	require.NoError(t, err)
	require.NoError(t, lock.Release())
}

// Given another process holds the lock on the data directory, then Run fails with
// `another Verve process is using <data dir>` and never listens.
func TestRun_DataDirLockedFails(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dataDir
	cfg.ListenAddr = "127.0.0.1:0"

	lock, err := AcquireLock(dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Release()) })

	logger, logs := testLogger()

	err = Run(t.Context(), cfg, logger)
	require.EqualError(t, err, "another Verve process is using "+dataDir)
	require.NotRegexp(t, loggedAddr, logs.String())
}
