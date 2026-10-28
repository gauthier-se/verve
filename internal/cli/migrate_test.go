package cli

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/app"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/logging/loggingtest"
)

// fakeMigrator records the calls of the migration runners and returns canned results.
type fakeMigrator struct {
	migrateCalls int
	statusCalls  int
	cfg          config.Config
	result       app.MigrateResult
	states       []app.MigrationState
	err          error
}

func (f *fakeMigrator) migrate(_ context.Context, cfg config.Config, _ *slog.Logger) (app.MigrateResult, error) {
	f.migrateCalls++
	f.cfg = cfg

	return f.result, f.err
}

func (f *fakeMigrator) status(_ context.Context, cfg config.Config) ([]app.MigrationState, error) {
	f.statusCalls++
	f.cfg = cfg

	return f.states, f.err
}

func depsWithMigrator(env map[string]string, m *fakeMigrator) Deps {
	deps := depsWithEnv(env)
	deps.Migrate = m.migrate
	deps.MigrationStatus = m.status

	return deps
}

// Given `verve migrate` with pending migrations, then they are applied with the loaded
// configuration and a summary of the applied migrations is printed.
func TestMigrate_PrintsSummaryOfApplied(t *testing.T) {
	t.Parallel()

	m := &fakeMigrator{result: app.MigrateResult{
		Applied: []app.Migration{{Version: 1, Name: "instance_settings"}, {Version: 2, Name: "users"}},
		Version: 2,
	}}

	res := run(t, depsWithMigrator(map[string]string{"VERVE_DATA_DIR": "/srv/verve"}, m), "migrate")

	require.Equal(t, 0, res.code, res.stderr)
	require.Equal(t, 1, m.migrateCalls)
	require.Equal(t, "/srv/verve", m.cfg.DataDir)
	require.Equal(t,
		"Applied 00001_instance_settings\n"+
			"Applied 00002_users\n"+
			"Database schema migrated to version 2 (2 migrations applied)\n",
		res.stdout)
}

// Given `verve migrate` on an up-to-date database, then it prints that nothing was applied.
func TestMigrate_UpToDatePrintsVersion(t *testing.T) {
	t.Parallel()

	m := &fakeMigrator{result: app.MigrateResult{Version: 3}}

	res := run(t, depsWithMigrator(nil, m), "migrate")

	require.Equal(t, 0, res.code, res.stderr)
	require.Equal(t, "Database schema up to date (version 3)\n", res.stdout)
}

// Given `verve migrate` when migrating fails, then it exits with status 1 and prints the error.
func TestMigrate_ErrorExitsNonZero(t *testing.T) {
	t.Parallel()

	m := &fakeMigrator{err: errors.New(
		"database schema version 9 is newer than this binary supports (3); upgrade Verve or restore a backup")}

	res := run(t, depsWithMigrator(nil, m), "migrate")

	require.Equal(t, 1, res.code)
	require.Contains(t, res.stderr,
		"database schema version 9 is newer than this binary supports (3); upgrade Verve or restore a backup")
	require.Empty(t, res.stdout)
}

// Given `verve migrate status`, then each migration is listed with its applied time or `pending`,
// and nothing is applied.
func TestMigrateStatus_ListsEachMigration(t *testing.T) {
	t.Parallel()

	appliedAt := time.Date(2026, 10, 28, 9, 30, 0, 0, time.FixedZone("CET", 3600))
	m := &fakeMigrator{states: []app.MigrationState{
		{Migration: app.Migration{Version: 1, Name: "instance_settings"}, AppliedAt: appliedAt},
		{Migration: app.Migration{Version: 2, Name: "users"}},
	}}

	res := run(t, depsWithMigrator(nil, m), "migrate", "status")

	require.Equal(t, 0, res.code, res.stderr)
	require.Equal(t, 1, m.statusCalls)
	require.Zero(t, m.migrateCalls)
	require.Equal(t,
		"VERSION  NAME               APPLIED\n"+
			"00001    instance_settings  2026-10-28T08:30:00Z\n"+
			"00002    users              pending\n",
		res.stdout)
}

// Given an invalid configuration, then migrate and migrate status exit with status 1 and never
// touch the database.
func TestMigrate_InvalidConfigExits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "migrate", args: []string{"migrate"}},
		{name: "migrate status", args: []string{"migrate", "status"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := &fakeMigrator{}

			res := run(t, depsWithMigrator(map[string]string{"VERVE_LOG_LEVEL": "verbose"}, m), tt.args...)

			require.Equal(t, 1, res.code)
			require.Contains(t, res.stderr, "invalid configuration")
			require.Contains(t, res.stderr, `log.level (VERVE_LOG_LEVEL): invalid value "verbose"`)
			require.Zero(t, m.migrateCalls+m.statusCalls)
		})
	}
}

// Given VERVE_LOG_FORMAT=json, then migrate gets a logger writing JSON lines to stderr.
func TestMigrate_LoggerFollowsLogConfig(t *testing.T) {
	t.Parallel()

	deps := depsWithEnv(map[string]string{"VERVE_LOG_FORMAT": "json"})
	deps.Migrate = func(_ context.Context, _ config.Config, logger *slog.Logger) (app.MigrateResult, error) {
		logger.Info("applied migration", slog.Int64("version", 1))

		return app.MigrateResult{Version: 1}, nil
	}

	res := run(t, deps, "migrate")

	require.Equal(t, 0, res.code, res.stderr)

	lines := loggingtest.DecodeLines(t, res.stderr)
	require.Len(t, lines, 1)
	require.Equal(t, "applied migration", lines[0]["msg"])
	require.InDelta(t, 1, lines[0]["version"], 0)
}
