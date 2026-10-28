package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// Given serve with an empty data directory, then Run creates the layout and verve.db with
// permissions 0600, and once stopped it closes the database after the HTTP server has stopped.
func TestRun_OpensDatabaseAndClosesItAfterServer(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "data")
	cfg := config.Default()
	cfg.DataDir = dataDir
	cfg.ListenAddr = "127.0.0.1:0"

	logger, logs := testLogger()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- Run(ctx, cfg, logger) }()

	require.Eventually(t, func() bool { return loggedAddr.MatchString(logs.String()) },
		5*time.Second, 10*time.Millisecond, "Run must log the bound address")

	for _, sub := range []string{"uploads", "inbox", "exports", "backups", "map"} {
		info, err := os.Stat(filepath.Join(dataDir, sub))
		require.NoError(t, err, sub)
		require.Equal(t, os.FileMode(0o750), info.Mode().Perm(), sub)
	}

	info, err := os.Stat(filepath.Join(dataDir, "verve.db"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}

	out := logs.String()
	serverStopped := strings.Index(out, `msg="server stopped"`)
	dbClosed := strings.Index(out, `msg="database closed"`)

	require.NotEqual(t, -1, serverStopped, "the server must log its stop")
	require.NotEqual(t, -1, dbClosed, "Run must log that the database is closed")
	require.Less(t, serverStopped, dbClosed, "the database must be closed after the server stopped")
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

// Given VERVE_PUBLIC_URL unset at startup, then Run logs the configuration warnings once at warn level.
func TestRun_LogsConfigurationWarningsOnce(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ListenAddr = "127.0.0.1:0"

	warnings := cfg.Warnings()
	require.NotEmpty(t, warnings)

	logger, logs := testLogger()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- Run(ctx, cfg, logger) }()

	require.Eventually(t, func() bool { return loggedAddr.MatchString(logs.String()) },
		5*time.Second, 10*time.Millisecond, "Run must log the bound address")

	cancel()
	require.NoError(t, <-done)

	for _, w := range warnings {
		require.Equal(t, 1, strings.Count(logs.String(), `level=WARN msg="`+w), w)
	}
}
