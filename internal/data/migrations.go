package data

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const migrationsDir = "migrations"

// Migrate applies every embedded SQL migration not yet recorded in
// schema_migrations, in filename order, each within its own transaction. It is
// idempotent: already-applied migrations are skipped, so it is safe to run on
// every startup and via `verve migrate`.
func Migrate(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	if err := ensureMigrationsTable(ctx, db); err != nil {
		return err
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationsFS, migrationsDir)
	if err != nil {
		return fmt.Errorf("data: read migrations dir: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		version := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(version, ".sql") {
			continue
		}
		if applied[version] {
			logger.Debug("migration already applied", "version", version)
			continue
		}
		if err := applyMigration(ctx, db, version); err != nil {
			return err
		}
		logger.Info("applied migration", "version", version)
	}
	return nil
}

// ensureMigrationsTable creates the version ledger if absent, so both Migrate
// and partial-application (tests) share one definition.
func ensureMigrationsTable(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	) STRICT`); err != nil {
		return fmt.Errorf("data: create schema_migrations: %w", err)
	}
	return nil
}

func appliedVersions(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("data: read applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("data: scan applied migration: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: iterate applied migrations: %w", err)
	}
	return applied, nil
}

func applyMigration(ctx context.Context, db *sql.DB, version string) error {
	stmt, err := migrationsFS.ReadFile(migrationsDir + "/" + version)
	if err != nil {
		return fmt.Errorf("data: read migration %s: %w", version, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("data: begin migration %s: %w", version, err)
	}
	defer tx.Rollback() // no-op after a successful Commit

	if _, err := tx.ExecContext(ctx, string(stmt)); err != nil {
		return fmt.Errorf("data: exec migration %s: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
		return fmt.Errorf("data: record migration %s: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("data: commit migration %s: %w", version, err)
	}
	return nil
}
