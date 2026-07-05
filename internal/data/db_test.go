package data

import (
	"context"
	"database/sql"
	"path/filepath"
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

	var count int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count != 1 {
		t.Errorf("schema_migrations rows = %d, want 1", count)
	}

	// The accounts table must exist and be usable after re-migration.
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `SELECT id FROM accounts LIMIT 0`); err != nil {
		t.Errorf("accounts table not usable: %v", err)
	}
}
