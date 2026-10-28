package sqlite

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
)

// embeddedMigrations holds the schema migrations, named NNNNN_snake_case_name.sql. They only have
// an Up section: downgrades are not supported, rolling back means restoring a backup.
//
//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// Migration identifies a schema migration.
type Migration struct {
	Version int64
	Name    string
}

// MigrationState is a migration with the time it was applied, zero when it is pending.
type MigrationState struct {
	Migration
	AppliedAt time.Time
}

// MigrateOptions configures Migrate.
type MigrateOptions struct {
	// BeforeMigrate runs before the first pending migration, with the pending versions. When it
	// returns an error, no migration runs. It is not called when the schema is up to date.
	BeforeMigrate func(ctx context.Context, pending []int64) error
	// Logger receives one line per applied migration. Nil discards the logs.
	Logger *slog.Logger
}

// MigrateResult lists the migrations applied by Migrate and the resulting schema version.
type MigrateResult struct {
	Applied []Migration
	Version int64
}

// Migrate applies the pending embedded migrations on the writer connection, each in its own
// transaction. It refuses a database whose schema version is newer than LatestVersion.
func Migrate(ctx context.Context, db *DB, opts MigrateOptions) (MigrateResult, error) {
	return migrate(ctx, db, migrationsFS(), opts)
}

// MigrationStatus lists every embedded migration with the time it was applied, in version order.
// It refuses a database whose schema version is newer than LatestVersion.
func MigrationStatus(ctx context.Context, db *DB) ([]MigrationState, error) {
	return migrationStatus(ctx, db, migrationsFS())
}

// LatestVersion returns the version of the latest embedded migration.
func LatestVersion() int64 {
	latest, err := latestVersion(migrationsFS())
	if err != nil {
		// The embedded file names are checked by TestMigrate_EmbeddedMigrationsApplyCleanly.
		panic(err)
	}

	return latest
}

// CurrentVersion returns the schema version of the database, 0 when no migration was applied.
func CurrentVersion(ctx context.Context, db *DB) (int64, error) {
	return currentVersion(ctx, db, migrationsFS())
}

// migrationsFS returns the embedded migrations directory.
func migrationsFS() fs.FS {
	sub, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		// fs.Sub fails only for an invalid path, and "migrations" is a constant.
		panic(fmt.Sprintf("open embedded migrations: %v", err))
	}

	return sub
}

// latestVersion returns the highest version among the migration files of fsys.
func latestVersion(fsys fs.FS) (int64, error) {
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return 0, fmt.Errorf("list migrations: %w", err)
	}

	var latest int64

	for _, name := range names {
		version, err := goose.NumericComponent(name)
		if err != nil {
			return 0, fmt.Errorf("migration %s: %w", name, err)
		}

		latest = max(latest, version)
	}

	return latest, nil
}

func migrate(ctx context.Context, db *DB, fsys fs.FS, opts MigrateOptions) (MigrateResult, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	p, current, err := newCheckedProvider(ctx, db, fsys)
	if err != nil {
		return MigrateResult{}, err
	}

	pending, err := pendingVersions(ctx, p)
	if err != nil {
		return MigrateResult{}, err
	}

	if len(pending) == 0 {
		logger.InfoContext(ctx, fmt.Sprintf("database schema up to date (version %d)", current))

		return MigrateResult{Version: current}, nil
	}

	if opts.BeforeMigrate != nil {
		if err := opts.BeforeMigrate(ctx, pending); err != nil {
			return MigrateResult{}, fmt.Errorf("before migrate: %w", err)
		}
	}

	var res MigrateResult

	// One migration at a time, so that each one is logged as soon as it is applied.
	for {
		r, err := p.UpByOne(ctx)
		if errors.Is(err, goose.ErrNoNextVersion) {
			break
		}

		if partial, ok := errors.AsType[*goose.PartialError](err); ok {
			return res, fmt.Errorf("migrate database: apply migration %s: %w",
				path.Base(partial.Failed.Source.Path), partial.Err)
		}

		if err != nil {
			return res, fmt.Errorf("migrate database: %w", err)
		}

		m := migrationOf(r.Source)
		res.Applied = append(res.Applied, m)
		logger.InfoContext(ctx, "applied migration", slog.Int64("version", m.Version), slog.String("name", m.Name))
	}

	res.Version, err = dbVersion(ctx, p)
	if err != nil {
		return res, err
	}

	return res, nil
}

func migrationStatus(ctx context.Context, db *DB, fsys fs.FS) ([]MigrationState, error) {
	p, _, err := newCheckedProvider(ctx, db, fsys)
	if err != nil {
		return nil, err
	}

	return status(ctx, p)
}

func currentVersion(ctx context.Context, db *DB, fsys fs.FS) (int64, error) {
	p, err := newProvider(db, fsys)
	if err != nil {
		return 0, err
	}

	return dbVersion(ctx, p)
}

func newProvider(db *DB, fsys fs.FS) (*goose.Provider, error) {
	p, err := goose.NewProvider(goose.DialectSQLite3, db.write, fsys, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return nil, fmt.Errorf("load migrations: %w", err)
	}

	return p, nil
}

// newCheckedProvider returns a provider for the migrations of fsys and the current schema version,
// after checking that the database is not newer than the latest migration: an older binary must
// not touch a schema it does not know.
func newCheckedProvider(ctx context.Context, db *DB, fsys fs.FS) (*goose.Provider, int64, error) {
	p, err := newProvider(db, fsys)
	if err != nil {
		return nil, 0, err
	}

	latest, err := latestVersion(fsys)
	if err != nil {
		return nil, 0, err
	}

	current, err := dbVersion(ctx, p)
	if err != nil {
		return nil, 0, err
	}

	if current > latest {
		return nil, 0, fmt.Errorf(
			"database schema version %d is newer than this binary supports (%d); upgrade Verve or restore a backup",
			current, latest)
	}

	return p, current, nil
}

// status lists the migrations of p with the time they were applied, in version order.
func status(ctx context.Context, p *goose.Provider) ([]MigrationState, error) {
	statuses, err := p.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("read migration status: %w", err)
	}

	states := make([]MigrationState, 0, len(statuses))

	for _, s := range statuses {
		state := MigrationState{Migration: migrationOf(s.Source)}
		if s.State == goose.StateApplied {
			state.AppliedAt = s.AppliedAt.UTC()
		}

		states = append(states, state)
	}

	return states, nil
}

// pendingVersions returns the versions of the migrations of p that are not applied yet.
func pendingVersions(ctx context.Context, p *goose.Provider) ([]int64, error) {
	states, err := status(ctx, p)
	if err != nil {
		return nil, err
	}

	var pending []int64

	for _, s := range states {
		if s.AppliedAt.IsZero() {
			pending = append(pending, s.Version)
		}
	}

	return pending, nil
}

func dbVersion(ctx context.Context, p *goose.Provider) (int64, error) {
	version, err := p.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("read database schema version: %w", err)
	}

	return version, nil
}

// migrationOf returns the version and name of a migration file: 00001_instance_settings.sql is
// version 1 named instance_settings.
func migrationOf(src *goose.Source) Migration {
	base := strings.TrimSuffix(path.Base(src.Path), path.Ext(src.Path))
	_, name, _ := strings.Cut(base, "_")

	return Migration{Version: src.Version, Name: name}
}
