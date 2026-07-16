package data

import (
	"context"
	"database/sql"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"testing"
)

// openTestDB opens a fresh, fully-migrated database in a temp file and returns
// it with its models. The file is cleaned up by t.TempDir.
func openTestDB(t *testing.T) (*sql.DB, Models) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "verve.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := Migrate(context.Background(), db, testLogger()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db, NewModels(db)
}

func TestOpenAppliesPragmas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "verve.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	tests := map[string]struct {
		pragma string
		want   string
	}{
		"WAL journal":  {"journal_mode", "wal"},
		"foreign keys": {"foreign_keys", "1"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var got string
			if err := db.QueryRow("PRAGMA " + tc.pragma).Scan(&got); err != nil {
				t.Fatalf("PRAGMA %s: %v", tc.pragma, err)
			}
			if got != tc.want {
				t.Errorf("PRAGMA %s = %q, want %q", tc.pragma, got, tc.want)
			}
		})
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "verve.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Applying twice must not error and must not double-record versions.
	for i := 0; i < 2; i++ {
		if err := Migrate(context.Background(), db, testLogger()); err != nil {
			t.Fatalf("Migrate (run %d): %v", i+1, err)
		}
	}

	// Each embedded migration must be recorded exactly once, no matter how many
	// times Migrate runs.
	wantVersions, err := fs.Glob(migrationsFS, migrationsDir+"/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count != len(wantVersions) {
		t.Errorf("schema_migrations rows = %d, want %d", count, len(wantVersions))
	}

	// The accounts table must exist and be usable after re-migration.
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `SELECT id FROM accounts LIMIT 0`); err != nil {
		t.Errorf("accounts table not usable: %v", err)
	}
}

// TestMigrationBackfillsPanelMetrics pins the 0007 data copy: a Panel written
// under the pre-0007 schema (scalar metric/chart_type columns) must come out of
// the migration as one panel_metrics row at position 0, with the old columns
// gone.
func TestMigrationBackfillsPanelMetrics(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "verve.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	// Apply everything before 0007 so a legacy single-metric panel can exist.
	if err := ensureMigrationsTable(ctx, db); err != nil {
		t.Fatalf("ensureMigrationsTable: %v", err)
	}
	versions, err := fs.Glob(migrationsFS, migrationsDir+"/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(versions)
	for _, v := range versions {
		version := path.Base(v)
		if version >= "0007" {
			break
		}
		if err := applyMigration(ctx, db, version); err != nil {
			t.Fatalf("apply %s: %v", version, err)
		}
	}

	// A legacy account, dashboard, and single-metric panel.
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (id, email) VALUES (1, 'legacy@example.com')`); err != nil {
		t.Fatalf("insert legacy account: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO dashboards (id, account_id, name, range_preset) VALUES (1, 1, 'Legacy', '30d')`); err != nil {
		t.Fatalf("insert legacy dashboard: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO panels (id, dashboard_id, account_id, metric, chart_type, width, position)
		 VALUES (1, 1, 1, 'steps', 'bar', 1, 0)`); err != nil {
		t.Fatalf("insert legacy panel: %v", err)
	}

	// Completing the migrations must copy the panel into panel_metrics.
	if err := Migrate(ctx, db, testLogger()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	got, err := NewModels(db).Panels.GetByID(ctx, 1, 1)
	if err != nil {
		t.Fatalf("GetByID after migration: %v", err)
	}
	if len(got.Metrics) != 1 || got.Metrics[0] != (PanelMetric{Metric: "steps", ChartType: "bar"}) {
		t.Errorf("migrated panel metrics = %+v, want steps/bar", got.Metrics)
	}

	// The scalar columns are gone — one representation only.
	if _, err := db.ExecContext(ctx, `SELECT metric FROM panels LIMIT 0`); err == nil {
		t.Errorf("panels.metric still exists after 0007")
	}
}
