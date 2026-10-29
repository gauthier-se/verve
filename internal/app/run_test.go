package app

import (
	"context"
	"io"
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
	"github.com/gauthier-se/verve/internal/version"
	"github.com/gauthier-se/verve/internal/webui"
)

var loggedAddr = regexp.MustCompile(`addr=(127\.0\.0\.1:\d+)`)

// testConfig returns the default configuration with dataDir and a random local port.
func testConfig(dataDir string) config.Config {
	cfg := config.Default()
	cfg.DataDir = dataDir
	cfg.ListenAddr = "127.0.0.1:0"

	return cfg
}

// startRun runs Run until it is ready. It returns the bound address, the logs and a function that
// stops Run and checks that it returned without error.
func startRun(t *testing.T, cfg config.Config) (string, *syncBuffer, func()) {
	t.Helper()

	addr, logs, stop := startRunWith(t, cfg, beforeMigrate)

	require.Eventually(t, func() bool {
		return strings.Contains(logs.String(), `msg="server ready"`)
	}, 5*time.Second, 10*time.Millisecond, "Run must log that the server is ready")

	return addr, logs, stop
}

// startRunWith runs Run, with hook called before the pending migrations, until it listens. It
// returns the bound address, the logs and a function that stops Run and checks that it returned
// without error.
func startRunWith(t *testing.T, cfg config.Config, hook beforeMigrateFunc) (string, *syncBuffer, func()) {
	t.Helper()

	logger, logs := testLogger()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- run(ctx, cfg, logger, hook) }()

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

	res, err := get(t.Context(), "http://"+addr+"/healthz")
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	require.Equal(t, http.StatusOK, res.StatusCode)

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

// Given a fresh database, when Run starts, then the server listens before the embedded
// migrations are applied and logged, and it is ready once they are.
func TestRun_ListensBeforeMigrating(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	_, logs, stop := startRun(t, testConfig(dataDir))
	stop()

	out := logs.String()
	listening := strings.Index(out, `msg="server listening"`)
	applied := strings.Index(out, `msg="applied migration" version=1 name=instance_settings`)
	ready := strings.Index(out, `msg="server ready"`)

	require.NotEqual(t, -1, listening, "the server must log that it listens")
	require.Less(t, listening, applied, "the server must listen before the migrations are applied")
	require.Less(t, applied, ready, "the server must be ready once the migrations are applied")

	require.Equal(t, sqlite.LatestVersion(), schemaVersion(t, dataDir))
}

// Given a database whose version is newer than this binary supports, then Run fails and stops
// the server without ever being ready.
func TestRun_NewerDatabaseFails(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	setFutureSchemaVersion(t, dataDir)

	logger, logs := testLogger()

	err := Run(t.Context(), testConfig(dataDir), logger)
	require.ErrorContains(t, err, "is newer than this binary supports")
	require.Contains(t, logs.String(), `msg="server stopped"`)
	require.NotContains(t, logs.String(), `msg="server ready"`)
}

// fetched is a response read in full.
type fetched struct {
	status int
	header http.Header
	body   string
}

func fetch(t *testing.T, url string) fetched {
	t.Helper()

	res, err := get(t.Context(), url)
	require.NoError(t, err)

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	return fetched{
		status: res.StatusCode,
		header: res.Header,
		body:   string(body),
	}
}

// Given startup with pending migrations, then the server listens before migrating: meanwhile
// /healthz is 200, /readyz is 503 with the reason `starting: applying migrations` and every other
// route is a 503 problem with the code "starting". Given the migrations complete, then /readyz is
// 200 with the version and the application routes are served.
func TestRun_GatesApplicationRoutesWhileMigrating(t *testing.T) {
	t.Parallel()

	migrating := make(chan struct{})
	release := make(chan struct{})
	hook := func(ctx context.Context, _ []int64) error {
		close(migrating)

		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	addr, _, stop := startRunWith(t, testConfig(t.TempDir()), hook)
	base := "http://" + addr
	wantVersion := version.Get().Version

	select {
	case <-migrating:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not start migrating")
	}

	res := fetch(t, base+"/healthz")
	require.Equal(t, http.StatusOK, res.status)
	require.JSONEq(t, `{"status":"ok"}`, res.body)

	res = fetch(t, base+"/readyz")
	require.Equal(t, http.StatusServiceUnavailable, res.status)
	require.JSONEq(t, `{"status":"not_ready","version":"`+wantVersion+`","reasons":["starting: applying migrations"]}`, res.body)

	for _, path := range []string{"/", "/api/v1/anything"} {
		res = fetch(t, base+path)
		require.Equal(t, http.StatusServiceUnavailable, res.status, path)
		require.Equal(t, "application/problem+json", res.header.Get("Content-Type"), path)
		require.JSONEq(t, wantStartingProblem, res.body, path)
	}

	close(release)

	require.Eventually(t, func() bool {
		return fetch(t, base+"/readyz").status == http.StatusOK
	}, 5*time.Second, 10*time.Millisecond, "the server must be ready once the migrations complete")

	res = fetch(t, base+"/readyz")
	require.JSONEq(t, `{"status":"ready","version":"`+wantVersion+`"}`, res.body)

	// The gate lets the requests through to the router.
	res = fetch(t, base+"/")
	require.Equal(t, http.StatusOK, res.status)
	require.Equal(t, "text/html; charset=utf-8", res.header.Get("Content-Type"))

	res = fetch(t, base+"/api/v1/anything")
	require.Equal(t, http.StatusNotFound, res.status)
	require.Equal(t, "application/problem+json", res.header.Get("Content-Type"))

	stop()
}

// Given a running server, then it serves the embedded web app, or the page explaining that the
// web UI is not built, on / and on the client routes. Without the web app, Run logs a warning.
func TestRun_ServesWebApp(t *testing.T) {
	t.Parallel()

	addr, logs, stop := startRun(t, testConfig(t.TempDir()))

	for _, path := range []string{"/", "/workouts/123"} {
		res := fetch(t, "http://"+addr+path)
		require.Equal(t, http.StatusOK, res.status, path)
		require.Equal(t, "text/html; charset=utf-8", res.header.Get("Content-Type"), path)
		require.Equal(t, !webui.Built(), strings.Contains(res.body, "The web UI is not built"), path)
	}

	stop()

	require.Equal(t, !webui.Built(), strings.Contains(logs.String(), `level=WARN msg="web app not built`))
}

// Given the server ready and its data directory no longer writable, then /readyz returns 503
// with the reason of the data directory check.
func TestRun_ReadyzReportsFailingChecks(t *testing.T) {
	t.Parallel()
	skipUnlessPermissionsEnforced(t)

	dataDir := t.TempDir()
	addr, _, stop := startRun(t, testConfig(dataDir))

	setWritable(t, dataDir, false)
	res := fetch(t, "http://"+addr+"/readyz")
	setWritable(t, dataDir, true)

	require.Equal(t, http.StatusServiceUnavailable, res.status)
	require.JSONEq(t, `{"status":"not_ready","version":"`+version.Get().Version+`",`+
		`"reasons":["data_dir: not writable: permission denied"]}`, res.body)

	stop()
}

// Given VERVE_PUBLIC_URL with https, then responses include Strict-Transport-Security; given it
// with http or unset, then they do not. Every response carries the Content Security Policy.
func TestRun_SendsHSTSOnlyForHTTPSPublicURL(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		publicURL string
		want      []string
	}{
		{name: "https", publicURL: "https://verve.example.com", want: []string{"max-age=31536000"}},
		{name: "http", publicURL: "http://verve.example.com"},
		{name: "unset"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := testConfig(t.TempDir())
			if tt.publicURL != "" {
				require.NoError(t, cfg.PublicURL.UnmarshalText([]byte(tt.publicURL)))
			}

			addr, _, stop := startRun(t, cfg)

			for _, path := range []string{"/", "/healthz", "/api/v1/meta"} {
				res := fetch(t, "http://"+addr+path)
				require.Equal(t, tt.want, res.header.Values("Strict-Transport-Security"), path)
				require.NotEmpty(t, res.header.Get("Content-Security-Policy"), path)
			}

			stop()
		})
	}
}
