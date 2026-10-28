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
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

var loggedAddr = regexp.MustCompile(`addr=(127\.0\.0\.1:\d+)`)

// testConfig returns the default configuration with dataDir and a random local port.
func testConfig(dataDir string) config.Config {
	cfg := config.Default()
	cfg.DataDir = dataDir
	cfg.ListenAddr = "127.0.0.1:0"

	return cfg
}

// startRun runs Run until it listens. It returns the bound address, the logs and a function that
// stops Run and checks that it returned without error.
func startRun(t *testing.T, cfg config.Config) (string, *syncBuffer, func()) {
	t.Helper()

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

	stop := func() {
		t.Helper()
		cancel()

		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return")
		}
	}

	return addr, logs, stop
}

// Given a data directory that does not exist, when Run starts, then it creates the directory
// with permissions 0750, takes the lock, serves, stops on context cancel and releases the lock.
func TestRun_CreatesDataDirLocksServesAndReleases(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "data")
	addr, _, stop := startRun(t, testConfig(dataDir))

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

	stop()

	lock, err := AcquireLock(dataDir)
	require.NoError(t, err)
	require.NoError(t, lock.Release())
}

// Given serve with an empty data directory, then Run creates the layout and verve.db with
// permissions 0600, and once stopped it closes the database after the HTTP server has stopped.
func TestRun_OpensDatabaseAndClosesItAfterServer(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "data")
	_, logs, stop := startRun(t, testConfig(dataDir))

	for _, sub := range []string{"uploads", "inbox", "exports", "backups", "map"} {
		info, err := os.Stat(filepath.Join(dataDir, sub))
		require.NoError(t, err, sub)
		require.Equal(t, os.FileMode(0o750), info.Mode().Perm(), sub)
	}

	info, err := os.Stat(filepath.Join(dataDir, "verve.db"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	stop()

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

	lock, err := AcquireLock(dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Release()) })

	logger, logs := testLogger()

	err = Run(t.Context(), testConfig(dataDir), logger)
	require.EqualError(t, err, "another Verve process is using "+dataDir)
	require.NotRegexp(t, loggedAddr, logs.String())
}

// Given VERVE_PUBLIC_URL unset at startup, then Run logs the configuration warnings once at warn level.
func TestRun_LogsConfigurationWarningsOnce(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t.TempDir())

	warnings := cfg.Warnings()
	require.NotEmpty(t, warnings)

	_, logs, stop := startRun(t, cfg)
	stop()

	for _, w := range warnings {
		require.Equal(t, 1, strings.Count(logs.String(), `level=WARN msg="`+w), w)
	}
}

// Given a fresh database, when Run starts, then the embedded migrations are applied and logged
// before the server listens.
func TestRun_MigratesBeforeServing(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	_, logs, stop := startRun(t, testConfig(dataDir))
	stop()

	out := logs.String()
	applied := strings.Index(out, `msg="applied migration" version=1 name=instance_settings`)
	listening := strings.Index(out, `msg="server listening"`)

	require.NotEqual(t, -1, applied, "Run must log the applied migration")
	require.Less(t, applied, listening, "migrations must be applied before the server listens")

	require.Equal(t, sqlite.LatestVersion(), schemaVersion(t, dataDir))
}

// Given a database whose version is newer than this binary supports, then Run fails and never
// listens.
func TestRun_NewerDatabaseFails(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	setFutureSchemaVersion(t, dataDir)

	logger, logs := testLogger()

	err := Run(t.Context(), testConfig(dataDir), logger)
	require.ErrorContains(t, err, "is newer than this binary supports")
	require.NotRegexp(t, loggedAddr, logs.String())
}
