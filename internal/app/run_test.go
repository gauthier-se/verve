package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
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

	addr, logs, stop := startRunWith(t, cfg, migrationPlan{}, jobs.NewRegistry())

	require.Eventually(t, func() bool {
		return strings.Contains(logs.String(), `msg="server ready"`)
	}, 5*time.Second, 10*time.Millisecond, "Run must log that the server is ready")

	return addr, logs, stop
}

// startRunWith runs Run, with the migrations of plan and the job kinds of registry, until it
// listens. It returns the bound address, the logs, which stderr shares, and a
// function that stops Run and checks that it returned without error.
func startRunWith(t *testing.T, cfg config.Config, plan migrationPlan, registry *jobs.Registry) (string, *syncBuffer, func()) {
	t.Helper()

	logger, logs := testLogger()
	addr, stop := runUntilListening(t, cfg, logger, logs, logs, plan, registry)

	return addr, logs, stop
}

// runUntilListening runs Run with logger and stderr, the migrations of plan and the job kinds of
// registry, until trace, a text log that logger writes to, shows the bound address. It returns that
// address and a function that stops Run and checks that it returned without error.
func runUntilListening(t *testing.T, cfg config.Config, logger *slog.Logger, stderr io.Writer, trace *syncBuffer,
	plan migrationPlan, registry *jobs.Registry,
) (string, func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- run(ctx, cfg, logger, stderr, plan, registry) }()

	var addr string

	require.Eventually(t, func() bool {
		m := loggedAddr.FindStringSubmatch(trace.String())
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

	return addr, stop
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

	err = Run(t.Context(), testConfig(dataDir), logger, logs)
	require.EqualError(t, err, "another Verve process is using "+dataDir)
	require.NotRegexp(t, loggedAddr, logs.String())
}

// Given no VERVE_SECRET_KEY and no secret.key, when Run starts, then it generates secret.key with
// permissions 0600. Given a restart, then the file stays the same.
func TestRun_GeneratesTheSecretKeyAndKeepsItAcrossRestarts(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t.TempDir())
	path := filepath.Join(cfg.DataDir, "secret.key")

	_, _, stop := startRun(t, cfg)
	stop()

	first, err := os.ReadFile(path) //nolint:gosec // G304: a test file
	require.NoError(t, err)
	require.Len(t, first, 64)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	_, _, stop = startRun(t, cfg)
	stop()

	second, err := os.ReadFile(path) //nolint:gosec // G304: a test file
	require.NoError(t, err)
	require.Equal(t, first, second)
}

// Given VERVE_SECRET_KEY, when Run starts, then no file is created.
func TestRun_ConfiguredSecretKeyCreatesNoFile(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t.TempDir())
	cfg.SecretKey = configuredKey

	_, _, stop := startRun(t, cfg)
	stop()

	require.NoFileExists(t, filepath.Join(cfg.DataDir, "secret.key"))
}

// Given a secret.key that cannot be read, then Run fails with a clear error, never listens and
// releases the lock.
func TestRun_UnreadableSecretKeyFails(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "secret.key")
	require.NoError(t, os.Mkdir(path, 0o750))

	logger, logs := testLogger()

	// Bounded, because a Run that does not fail serves until its context ends.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err := Run(ctx, testConfig(dataDir), logger, logs)
	require.ErrorContains(t, err, "read secret key: ")
	require.ErrorContains(t, err, path)
	require.NotRegexp(t, loggedAddr, logs.String())

	lock, err := AcquireLock(dataDir)
	require.NoError(t, err)
	require.NoError(t, lock.Release())
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

// Given a running server and a command next to it holding a read transaction, when the server
// stops, then it does not wait for the busy timeout, logs no error and closes the database.
func TestRun_CommandHoldsDatabaseOpen_StopsWithoutStallOrError(t *testing.T) {
	t.Parallel()

	// Well below the busy timeout, the time a checkpoint that waits stalls for.
	const stallLimit = 2 * time.Second

	ctx := t.Context()
	cfg := testConfig(filepath.Join(t.TempDir(), "data"))
	_, logs, stop := startRun(t, cfg)

	command, err := sqlite.Open(ctx, filepath.Join(cfg.DataDir, dbFile))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, command.CloseShared()) })

	reader, err := command.Read().BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Rollback() })

	// The first read takes the snapshot that the transaction then holds.
	var version int64
	require.NoError(t, reader.QueryRowContext(ctx, "SELECT max(version_id) FROM goose_db_version").Scan(&version))
	require.Equal(t, sqlite.LatestVersion(), version)

	start := time.Now()

	stop()
	require.Less(t, time.Since(start), stallLimit)

	require.NotContains(t, logs.String(), "level=ERROR")
	require.Contains(t, logs.String(), `msg="database closed"`)
}

// Given startup, then the worker starts after the migrations are applied. Given shutdown, then it
// stops before the database closes.
func TestRun_StartsWorkerAfterMigrationsAndStopsItBeforeTheDatabaseCloses(t *testing.T) {
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

	_, logs, stop := startRunWith(t, testConfig(t.TempDir()), migrationPlan{before: hook}, jobs.NewRegistry())

	select {
	case <-migrating:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not start migrating")
	}

	require.NotContains(t, logs.String(), `msg="worker started"`)

	close(release)

	require.Eventually(t, func() bool {
		return strings.Contains(logs.String(), `msg="worker started"`)
	}, 5*time.Second, 10*time.Millisecond, "the worker must start once the migrations complete")

	stop()

	out := logs.String()
	workerStarted := strings.Index(out, `msg="worker started"`)
	workerStopped := strings.Index(out, `msg="worker stopped"`)
	dbClosed := strings.Index(out, `msg="database closed"`)

	require.Less(t, strings.LastIndex(out, `msg="applied migration"`), workerStarted)
	require.NotEqual(t, -1, workerStopped, "the worker must log its stop")
	require.Less(t, workerStopped, dbClosed, "the worker must stop before the database closes")
}

// Given a start, then the types of the shipped catalog are stored once the migrations are applied
// and before the worker starts.
func TestRun_SyncsTheMetricTypesAfterMigrationsAndBeforeTheWorker(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := testConfig(t.TempDir())

	_, logs, stop := startRun(t, cfg)
	stop()

	out := logs.String()
	synced := strings.Index(out, `msg="metric types synchronized"`)

	require.NotEqual(t, -1, synced, "Run must log that the metric types are synchronized")
	require.Less(t, strings.LastIndex(out, `msg="applied migration"`), synced)
	require.Less(t, synced, strings.Index(out, `msg="worker started"`))

	shipped, err := catalog.Shipped()
	require.NoError(t, err)

	stored, err := withSharedDB(ctx, cfg, func(s store.Store) ([]store.MetricType, error) {
		return s.MetricTypes().List(ctx)
	})
	require.NoError(t, err)
	require.Len(t, stored, len(shipped.Types()))

	for i, want := range shipped.Types() {
		require.Equal(t, want.Identifier, stored[i].Identifier)
		require.True(t, stored[i].Curated, want.Identifier)
	}
}

// Given a session, then GET /api/v1/metrics lists the types of the shipped catalog with their
// category values.
func TestRun_ListsTheMetricTypesOfTheShippedCatalog(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := testConfig(t.TempDir())

	_, err := Migrate(ctx, cfg, discardLogger())
	require.NoError(t, err)

	db, err := sqlite.Open(ctx, filepath.Join(cfg.DataDir, dbFile))
	require.NoError(t, err)

	s := sqlite.New(db, clock.Real())

	user, err := s.Users().Create(ctx, store.NewUser{Username: "alice", Role: store.RoleMember})
	require.NoError(t, err)

	id, _, err := auth.NewSessions(clock.Real()).Create(ctx, s, auth.NewSession{UserID: user.ID, Method: store.LoginPassword})
	require.NoError(t, err)
	require.NoError(t, db.CloseShared())

	addr, _, stop := startRun(t, cfg)
	defer stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/v1/metrics", nil)
	require.NoError(t, err)
	req.Header.Set("Cookie", "verve_session="+id)

	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	require.Equal(t, http.StatusOK, res.StatusCode, string(raw))

	var body struct {
		Items []struct {
			Key    string   `json:"key"`
			Values []string `json:"values"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))

	values := make(map[string][]string, len(body.Items))
	for _, item := range body.Items {
		values[item.Key] = item.Values
	}

	require.Contains(t, values, "heart_rate")
	require.Contains(t, values["sleep_analysis"], "asleep_core")
}

// Given step samples pushed to POST /api/v1/ingest and the jobs run by the worker of the server,
// then the series of step_count by day returns their daily sums.
func TestRun_ReadsPushedStepsAsADailySeries(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := testConfig(t.TempDir())

	_, err := Migrate(ctx, cfg, discardLogger())
	require.NoError(t, err)

	token, err := withSharedDB(ctx, cfg, func(s store.Store) (string, error) {
		user, err := s.Users().Create(ctx, store.NewUser{Username: "alice", Role: store.RoleMember})
		if err != nil {
			return "", err
		}

		token, _, err := auth.NewTokens(clock.Real()).Create(ctx, s, auth.NewAPIToken{
			UserID: user.ID, Name: "iPhone", Scopes: []store.TokenScope{store.ScopeIngest, store.ScopeRead},
		})

		return token, err
	})
	require.NoError(t, err)

	addr, _, stop := startRun(t, cfg)
	defer stop()

	// send fails through c, which may be the collector of EventuallyWithT.
	send := func(c require.TestingT, method, path, body string) (int, []byte) {
		req, err := http.NewRequestWithContext(ctx, method, "http://"+addr+path, strings.NewReader(body))
		require.NoError(c, err)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		res, err := http.DefaultClient.Do(req)
		require.NoError(c, err)

		raw, err := io.ReadAll(res.Body)
		require.NoError(c, err)
		require.NoError(c, res.Body.Close())

		return res.StatusCode, raw
	}

	sample := func(uuid, start, end string, value int) string {
		return `{"uuid": "` + uuid + `", "type": "HKQuantityTypeIdentifierStepCount", "start": "` + start +
			`", "end": "` + end + `", "value": ` + strconv.Itoa(value) + `, "unit": "count", ` +
			`"source": {"name": "Apple Watch", "device": {"model": "Watch"}}}`
	}

	status, raw := send(t, http.MethodPost, "/api/v1/ingest", `{
		"format": "verve.ingest.v1",
		"client": {"name": "Verve for iOS", "version": "1.0.0"},
		"samples": [`+strings.Join([]string{
		sample("2C7B5E1A-9D3F-4B6C-8E0A-1F2D3C4B5A69", "2026-11-10T08:00:00Z", "2026-11-10T08:10:00Z", 1240),
		sample("5F0D2B7C-1A3E-4C9B-8D6F-2E4A6C8B0D13", "2026-11-10T18:00:00Z", "2026-11-10T18:30:00Z", 760),
		sample("9A4C6E8B-3D5F-4A1C-9B7E-0F2D4B6A8C35", "2026-11-11T09:00:00Z", "2026-11-11T09:10:00Z", 500),
	}, ",")+`]
	}`)
	require.Equal(t, http.StatusAccepted, status, string(raw))

	var series struct {
		Updating bool `json:"updating"`
		Points   []struct {
			Start string  `json:"start"`
			Value float64 `json:"value"`
		} `json:"points"`
	}

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		status, raw := send(c, http.MethodGet,
			"/api/v1/metrics/step_count/series?from=2026-11-01&to=2026-11-30&bucket=day&statistic=sum", "")
		require.Equal(c, http.StatusOK, status, string(raw))
		require.NoError(c, json.Unmarshal(raw, &series))
		require.False(c, series.Updating)
		require.Len(c, series.Points, 2)
	}, 10*time.Second, 20*time.Millisecond, "the jobs must compute the daily sums")

	require.Equal(t, "2026-11-10T00:00:00Z", series.Points[0].Start)
	require.InDelta(t, 2000, series.Points[0].Value, 1e-9)
	require.Equal(t, "2026-11-11T00:00:00Z", series.Points[1].Start)
	require.InDelta(t, 500, series.Points[1].Value, 1e-9)
}

// Given shutdown and a handler that takes time to stop, then the database closes after it returned,
// so that its result is still written.
func TestRun_ShutdownWaitsForARunningJobBeforeClosingTheDatabase(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := testConfig(t.TempDir())

	_, err := Migrate(ctx, cfg, discardLogger())
	require.NoError(t, err)

	started := make(chan struct{})
	registry := jobs.NewRegistry()
	kind := jobs.Register(registry, "test.slow", jobs.KindOptions{}, func(ctx context.Context, _ jobs.Job[struct{}]) error {
		close(started)
		<-ctx.Done()
		time.Sleep(100 * time.Millisecond)

		return nil
	})

	slow, err := kind.New(struct{}{})
	require.NoError(t, err)

	queued, err := withSharedDB(ctx, cfg, func(s store.Store) (store.Job, error) {
		return s.Jobs().Enqueue(ctx, slow)
	})
	require.NoError(t, err)

	_, _, stop := startRunWith(t, cfg, migrationPlan{}, registry)

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler did not start")
	}

	stop()

	job, err := withSharedDB(ctx, cfg, func(s store.Store) (store.Job, error) {
		return s.Jobs().Get(ctx, queued.ID)
	})
	require.NoError(t, err)
	require.Equal(t, store.JobCompleted, job.Status)
}

// storedSchedules returns the next runs stored in the database of cfg.
func storedSchedules(t *testing.T, cfg config.Config) map[string]time.Time {
	t.Helper()

	ctx := t.Context()

	schedules, err := withSharedDB(ctx, cfg, func(s store.Store) (map[string]time.Time, error) {
		return s.Jobs().ListSchedules(ctx)
	})
	require.NoError(t, err)

	return schedules
}

// Given serve on a new database, then the purges of jobs, of audit events, of expired sessions and of
// the inbox are scheduled daily, in a day, and no job is enqueued for them yet. Given the defaults, then the
// backup is scheduled at the next 03:00 in the local time of the process.
func TestRun_SchedulesTheDailyPurgesAndBackups(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t.TempDir())
	before := time.Now()
	_, _, stop := startRun(t, cfg)

	require.Eventually(t, func() bool {
		return len(storedSchedules(t, cfg)) == 5
	}, 5*time.Second, 10*time.Millisecond, "the server must schedule the purges and the backup")

	stop()

	schedules := storedSchedules(t, cfg)
	for _, name := range []string{"jobs.purge", "audit.purge", "cleanup.sessions", "cleanup.inbox"} {
		require.WithinDuration(t, time.Now().Add(24*time.Hour), schedules[name], time.Minute, name)
	}

	local := before.Local()

	next := time.Date(local.Year(), local.Month(), local.Day(), 3, 0, 0, 0, time.Local)
	if !next.After(local) {
		next = time.Date(local.Year(), local.Month(), local.Day()+1, 3, 0, 0, 0, time.Local)
	}

	require.True(t, next.Equal(schedules["backup.run"]), "backup.run at %s, want %s", schedules["backup.run"], next)
}

// Given the backup schedule off, then serve schedules the purges and no backup.
func TestRun_BackupScheduleOffSchedulesNoBackup(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t.TempDir())
	cfg.Backup.Schedule = config.BackupScheduleOff
	_, _, stop := startRun(t, cfg)

	require.Eventually(t, func() bool {
		return len(storedSchedules(t, cfg)) == 4
	}, 5*time.Second, 10*time.Millisecond, "the server must schedule the purges")

	stop()

	require.NotContains(t, storedSchedules(t, cfg), "backup.run")
}

// Given an audit retention of 30 days and the purge of audit events missed during downtime, then it
// runs after startup and removes only the events older than 30 days.
func TestRun_PurgesTheAuditEventsOlderThanTheConfiguredRetention(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := testConfig(t.TempDir())
	cfg.Audit.RetentionDays = 30

	_, err := Migrate(ctx, cfg, discardLogger())
	require.NoError(t, err)

	db, err := sqlite.Open(ctx, filepath.Join(cfg.DataDir, dbFile))
	require.NoError(t, err)

	now := time.Now().UTC()
	event := store.NewAuditEvent{Type: "system.backup_created", Payload: json.RawMessage(`{}`)}

	old, err := sqlite.New(db, clock.NewFake(now.Add(-31*24*time.Hour))).AuditEvents().Append(ctx, event)
	require.NoError(t, err)

	recent, err := sqlite.New(db, clock.NewFake(now.Add(-29*24*time.Hour))).AuditEvents().Append(ctx, event)
	require.NoError(t, err)

	require.NoError(t, sqlite.New(db, clock.NewFake(now)).Jobs().SetSchedule(ctx, "audit.purge", now.Add(-time.Hour)))
	require.NoError(t, db.CloseShared())

	_, _, stop := startRun(t, cfg)

	require.Eventually(t, func() bool {
		_, err := withSharedDB(ctx, cfg, func(s store.Store) (store.AuditEvent, error) {
			return s.AuditEvents().Get(ctx, old.ID)
		})

		return errors.Is(err, store.ErrNotFound)
	}, 5*time.Second, 10*time.Millisecond, "the purge must remove the old event")

	stop()

	_, err = withSharedDB(ctx, cfg, func(s store.Store) (store.AuditEvent, error) {
		return s.AuditEvents().Get(ctx, recent.ID)
	})
	require.NoError(t, err)
}

// Given the purge of expired sessions missed during downtime, then it runs after startup and
// removes the sessions that have expired, and only them.
func TestRun_PurgesTheExpiredSessions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := testConfig(t.TempDir())

	_, err := Migrate(ctx, cfg, discardLogger())
	require.NoError(t, err)

	db, err := sqlite.Open(ctx, filepath.Join(cfg.DataDir, dbFile))
	require.NoError(t, err)

	now := time.Now().UTC()
	s := sqlite.New(db, clock.NewFake(now.Add(-60*24*time.Hour)))

	user, err := s.Users().Create(ctx, store.NewUser{Username: "alice", Role: store.RoleMember})
	require.NoError(t, err)

	for id, expiresAt := range map[string]time.Time{
		"expired": now.Add(-time.Hour),
		"valid":   now.Add(24 * time.Hour),
	} {
		_, err := s.Sessions().Create(ctx, store.NewSession{
			IDHash:    []byte(id),
			PublicID:  id,
			UserID:    user.ID,
			Method:    store.LoginPassword,
			ExpiresAt: expiresAt,
		})
		require.NoError(t, err)
	}

	require.NoError(t, s.Jobs().SetSchedule(ctx, "cleanup.sessions", now.Add(-time.Hour)))
	require.NoError(t, db.CloseShared())

	_, _, stop := startRun(t, cfg)

	getSession := func(id string) error {
		_, err := withSharedDB(ctx, cfg, func(s store.Store) (store.Session, error) {
			session, _, err := s.Sessions().Get(ctx, []byte(id))

			return session, err
		})

		return err
	}

	require.Eventually(t, func() bool {
		return errors.Is(getSession("expired"), store.ErrNotFound)
	}, 5*time.Second, 10*time.Millisecond, "the purge must remove the expired session")

	stop()

	require.NoError(t, getSession("valid"))
}

// Given the purge of jobs missed during downtime, then it runs after startup and removes a job
// finished for more than 7 days, and its next run is computed from now.
func TestRun_RunsAPurgeMissedDuringDowntime(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := testConfig(t.TempDir())

	_, err := Migrate(ctx, cfg, discardLogger())
	require.NoError(t, err)

	db, err := sqlite.Open(ctx, filepath.Join(cfg.DataDir, dbFile))
	require.NoError(t, err)

	now := time.Now().UTC()
	lastWeek := sqlite.New(db, clock.NewFake(now.Add(-8*24*time.Hour))).Jobs()

	old, err := lastWeek.Enqueue(ctx, store.NewJob{Kind: "test.old", Payload: json.RawMessage(`{}`)})
	require.NoError(t, err)

	claimed, _, err := lastWeek.Claim(ctx, time.Minute)
	require.NoError(t, err)
	require.NoError(t, lastWeek.Complete(ctx, claimed.ID, claimed.Claims))
	require.NoError(t, lastWeek.SetSchedule(ctx, "jobs.purge", now.Add(-time.Hour)))
	require.NoError(t, db.CloseShared())

	_, _, stop := startRun(t, cfg)

	require.Eventually(t, func() bool {
		_, err := withSharedDB(ctx, cfg, func(s store.Store) (store.Job, error) {
			return s.Jobs().Get(ctx, old.ID)
		})

		return errors.Is(err, store.ErrNotFound)
	}, 5*time.Second, 10*time.Millisecond, "the purge must remove the old job")

	stop()

	require.WithinDuration(t, time.Now().Add(24*time.Hour), storedSchedules(t, cfg)["jobs.purge"], time.Minute)
}

// Given a job queued before startup, then the worker of the server runs it. Its kind is unknown, so
// that the test does not depend on a handler.
func TestRun_WorkerRunsAJobQueuedBeforeStartup(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := testConfig(t.TempDir())

	_, err := Migrate(ctx, cfg, discardLogger())
	require.NoError(t, err)

	queued, err := withSharedDB(ctx, cfg, func(s store.Store) (store.Job, error) {
		return s.Jobs().Enqueue(ctx, store.NewJob{Kind: "test.unknown", Payload: json.RawMessage(`{}`)})
	})
	require.NoError(t, err)

	_, _, stop := startRun(t, cfg)

	require.Eventually(t, func() bool {
		job, err := withSharedDB(ctx, cfg, func(s store.Store) (store.Job, error) {
			return s.Jobs().Get(ctx, queued.ID)
		})

		return err == nil && job.Status == store.JobFailed && job.LastError == "unknown job kind"
	}, 5*time.Second, 10*time.Millisecond, "the worker must run the job")

	stop()
}

// Given a database whose version is newer than this binary supports, then Run fails and stops
// the server without ever being ready.
func TestRun_NewerDatabaseFails(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	setFutureSchemaVersion(t, dataDir)

	logger, logs := testLogger()

	err := Run(t.Context(), testConfig(dataDir), logger, logs)
	require.ErrorContains(t, err, "is newer than this binary supports")
	require.Contains(t, logs.String(), `msg="server stopped"`)
	require.NotContains(t, logs.String(), `msg="server ready"`)
}

// Given pending migrations on a database with data, when serve starts, then it creates a
// pre-migration backup before the first migration. Given the backup runs, then /readyz keeps
// answering 503 with the reason `starting: applying migrations`.
func TestRun_BacksUpBeforeMigratingWhileStarting(t *testing.T) {
	t.Parallel()

	cfg := dataDirWithData(t)

	// The backup waits for the lock of the backups directory until the test releases it.
	unlock := lockBackupsDir(t, cfg.DataDir)

	addr, logs, stop := startRunWith(t, cfg, migrationPlan{source: upgradeMigrations(t)}, jobs.NewRegistry())

	require.Eventually(t, func() bool {
		return strings.Contains(logs.String(), `msg="backing up the database before migrating"`)
	}, 5*time.Second, 10*time.Millisecond, "Run must start the backup")

	res := fetch(t, "http://"+addr+"/readyz")
	require.Equal(t, http.StatusServiceUnavailable, res.status)
	require.JSONEq(t, `{"status":"not_ready","version":"`+version.Get().Version+`","reasons":["starting: applying migrations"]}`,
		res.body)
	require.Empty(t, backupArchives(t, cfg.DataDir))

	unlock()

	require.Eventually(t, func() bool {
		return strings.Contains(logs.String(), `msg="server ready"`)
	}, 5*time.Second, 10*time.Millisecond, "Run must be ready once the backup and the migrations complete")

	stop()

	archives := backupArchives(t, cfg.DataDir)
	require.Len(t, archives, 1)
	require.Regexp(t, preMigrationArchive, archives[0])
	require.False(t, hasUpgradeTable(t, archivedDB(t, filepath.Join(newLayout(cfg.DataDir).Backups, archives[0]))))
	require.True(t, hasUpgradeTable(t, openDataDirDB(t, cfg.DataDir)))
}

// Given pending migrations and a failing backup, when serve starts, then no migration runs and
// startup fails with the reason, without ever being ready.
func TestRun_FailingBackupFailsStartup(t *testing.T) {
	t.Parallel()

	cfg := dataDirWithData(t)
	breakBackups(t, cfg.DataDir)

	logger, logs := testLogger()

	err := run(t.Context(), cfg, logger, logs, migrationPlan{source: upgradeMigrations(t)}, jobs.NewRegistry())
	require.ErrorContains(t, err, failingBackupError)
	require.NotContains(t, logs.String(), `msg="server ready"`)
	require.False(t, hasUpgradeTable(t, openDataDirDB(t, cfg.DataDir)))
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

	addr, _, stop := startRunWith(t, testConfig(t.TempDir()), migrationPlan{before: hook}, jobs.NewRegistry())
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

// Given VERVE_PUBLIC_URL with https, then the session cookie with the __Host- prefix authenticates
// GET /api/v1/auth/me and the cookie without it does not, and a POST under /api/ must come from the
// public URL, even when its Origin is the host it was sent to.
func TestRun_AuthenticatesAndChecksCSRFAgainstThePublicURL(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := testConfig(t.TempDir())
	require.NoError(t, cfg.PublicURL.UnmarshalText([]byte("https://verve.example.com")))

	_, err := Migrate(ctx, cfg, discardLogger())
	require.NoError(t, err)

	db, err := sqlite.Open(ctx, filepath.Join(cfg.DataDir, dbFile))
	require.NoError(t, err)

	s := sqlite.New(db, clock.Real())

	user, err := s.Users().Create(ctx, store.NewUser{Username: "alice", Role: store.RoleMember})
	require.NoError(t, err)

	id, _, err := auth.NewSessions(clock.Real()).Create(ctx, s, auth.NewSession{UserID: user.ID, Method: store.LoginPassword})
	require.NoError(t, err)
	require.NoError(t, db.CloseShared())

	addr, _, stop := startRun(t, cfg)
	base := "http://" + addr

	send := func(method, path string, header http.Header) int {
		t.Helper()

		req, err := http.NewRequestWithContext(ctx, method, base+path, nil)
		require.NoError(t, err)

		req.Header = header

		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		// Read to the end like fetch: closing the body unread made stop time out under load.
		_, err = io.Copy(io.Discard, res.Body)
		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		return res.StatusCode
	}

	require.Equal(t, http.StatusOK,
		send(http.MethodGet, "/api/v1/auth/me", http.Header{"Cookie": {"__Host-verve_session=" + id}}))
	require.Equal(t, http.StatusUnauthorized,
		send(http.MethodGet, "/api/v1/auth/me", http.Header{"Cookie": {"verve_session=" + id}}))
	require.Equal(t, http.StatusForbidden,
		send(http.MethodPost, "/api/v1/anything", http.Header{"Verve-Csrf": {"1"}, "Origin": {base}}))
	require.Equal(t, http.StatusNotFound,
		send(http.MethodPost, "/api/v1/anything", http.Header{"Verve-Csrf": {"1"}, "Origin": {"https://verve.example.com"}}))

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
