package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/gauthier-se/verve/internal/data"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestApp builds an application backed by a fresh, migrated temp database.
func newTestApp(t *testing.T) *application {
	t.Helper()

	cfg := config{dataDir: t.TempDir()}
	db, err := data.Open(cfg.dbPath())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := data.Migrate(context.Background(), db, testLogger()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return &application{config: cfg, logger: testLogger(), db: db, models: data.NewModels(db)}
}

func TestAccountCreate(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()

	if err := app.accountCreate(ctx, []string{"--email=bob@example.com"}); err != nil {
		t.Fatalf("accountCreate: %v", err)
	}

	got, err := app.models.Accounts.GetByEmail(ctx, "bob@example.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if got.Email != "bob@example.com" {
		t.Errorf("Email = %q, want bob@example.com", got.Email)
	}
}

func TestAccountCreateRequiresEmail(t *testing.T) {
	app := newTestApp(t)
	if err := app.accountCreate(context.Background(), nil); err == nil {
		t.Error("accountCreate without --email should error")
	}
}

func TestAccountCreateDuplicate(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()

	if err := app.accountCreate(ctx, []string{"--email=dup@example.com"}); err != nil {
		t.Fatalf("first accountCreate: %v", err)
	}
	if err := app.accountCreate(ctx, []string{"--email=dup@example.com"}); err == nil {
		t.Error("duplicate accountCreate should error")
	}
}

// TestRunAcceptance exercises the issue's acceptance criteria end-to-end through
// run(): the binary creates/migrates verve.db with no manual step, re-running is
// idempotent, and `account create` inserts a visible account.
func TestRunAcceptance(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// First run: `account create` triggers dir creation, db open, and migration.
	if err := run(ctx, testLogger(), []string{"-data-dir=" + dir, "account", "create", "--email=alice@example.com"}); err != nil {
		t.Fatalf("first run: %v", err)
	}

	dbPath := filepath.Join(dir, "verve.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("verve.db not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "artifacts")); err != nil {
		t.Fatalf("artifacts dir not created: %v", err)
	}

	// Second run against the same data dir must succeed (idempotent migration).
	if err := run(ctx, testLogger(), []string{"-data-dir=" + dir, "migrate"}); err != nil {
		t.Fatalf("second run (migrate): %v", err)
	}

	// The account inserted on the first run is visible.
	db, err := data.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()
	if _, err := data.NewModels(db).Accounts.GetByEmail(ctx, "alice@example.com"); err != nil {
		t.Fatalf("account not visible after run: %v", err)
	}
}
