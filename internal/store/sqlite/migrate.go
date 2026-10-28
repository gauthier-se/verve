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
func MigrationStatus(ctx context.Context, db *DB) ([]MigrationState, error) {
	return migrationStatus(ctx, db, migrationsFS())
}

// LatestVersion returns the version of the latest embedded migration.
func LatestVersion() int64 {
	entries, err := fs.ReadDir(migrationsFS(), ".")
	if err != nil {
		panic(fmt.Sprintf("read embedded migrations: %v", err))
	}

	var latest int64

	for _, entry := range entries {
		if version, err := goose.NumericComponent(entry.Name()); err == nil {
			latest = max(latest, version)
		}
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

func newProvider(db *DB, fsys fs.FS) (*goose.Provider, error) {
	p, err := goose.NewProvider(goose.DialectSQLite3, db.write, fsys, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return nil, fmt.Errorf("load migrations: %w", err)
	}

	return p, nil
}

func migrate(ctx context.Context, db *DB, fsys fs.FS, opts MigrateOptions) (MigrateResult, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	p, err := newProvider(db, fsys)
	if err != nil {
		return MigrateResult{}, err
	}

	states, err := providerStatus(ctx, p)
	if err != nil {
		return MigrateResult{}, err
	}

	var pending []int64

	for _, s := range states {
		if s.AppliedAt.IsZero() {
			pending = append(pending, s.Version)
		}
	}

	if len(pending) == 0 {
		version, err := dbVersion(ctx, p)
		if err != nil {
			return MigrateResult{}, err
		}

		logger.InfoContext(ctx, fmt.Sprintf("database schema up to date (version %d)", version))

		return MigrateResult{Version: version}, nil
	}

	if opts.BeforeMigrate != nil {
		if err := opts.BeforeMigrate(ctx, pending); err != nil {
			return MigrateResult{}, fmt.Errorf("before migrate: %w", err)
		}
	}

	results, upErr := p.Up(ctx)

	if partial, ok := errors.AsType[*goose.PartialError](upErr); ok {
		results = partial.Applied
		upErr = fmt.Errorf("apply migration %s: %w", path.Base(partial.Failed.Source.Path), partial.Err)
	}

	var res MigrateResult

	for _, r := range results {
		m := migrationOf(r.Source)
		res.Applied = append(res.Applied, m)
		logger.InfoContext(ctx, "applied migration", slog.Int64("version", m.Version), slog.String("name", m.Name))
	}

	if upErr != nil {
		return res, fmt.Errorf("migrate database: %w", upErr)
	}

	res.Version, err = dbVersion(ctx, p)
	if err != nil {
		return res, err
	}

	return res, nil
}

func migrationStatus(ctx context.Context, db *DB, fsys fs.FS) ([]MigrationState, error) {
	p, err := newProvider(db, fsys)
	if err != nil {
		return nil, err
	}

	return providerStatus(ctx, p)
}

func currentVersion(ctx context.Context, db *DB, fsys fs.FS) (int64, error) {
	p, err := newProvider(db, fsys)
	if err != nil {
		return 0, err
	}

	return dbVersion(ctx, p)
}

// providerStatus lists the migrations of p with their applied time, after checking that the
// database is not newer than the migrations.
func providerStatus(ctx context.Context, p *goose.Provider) ([]MigrationState, error) {
	current, err := dbVersion(ctx, p)
	if err != nil {
		return nil, err
	}

	sources := p.ListSources()
	if latest := sources[len(sources)-1].Version; current > latest {
		return nil, fmt.Errorf(
			"database schema version %d is newer than this binary supports (%d); upgrade Verve or restore a backup",
			current, latest)
	}

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
