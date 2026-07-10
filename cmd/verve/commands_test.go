package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/data"
)

// tinyExport is a minimal Apple Health export written to a temp file for the
// import-command tests.
const tinyExport = `<HealthData locale="en_US">
 <Record type="HKQuantityTypeIdentifierStepCount" sourceName="Watch" unit="count" startDate="2024-01-01 08:00:00 +0000" endDate="2024-01-01 09:00:00 +0000" value="120"/>
 <Record type="HKCategoryTypeIdentifierSleepAnalysis" sourceName="Watch" startDate="2024-01-01 23:00:00 +0000" endDate="2024-01-02 06:00:00 +0000" value="HKCategoryValueSleepAnalysisAsleepCore"/>
</HealthData>`

func writeExport(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "export.xml")
	if err := os.WriteFile(path, []byte(tinyExport), 0o600); err != nil {
		t.Fatalf("write export: %v", err)
	}
	return path
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestApp builds an application backed by a fresh, migrated temp database.
// stdout is discarded; stdin is set per-test with feedStdin when a command needs
// a password.
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
	return &application{config: cfg, logger: testLogger(), db: db, models: data.NewModels(db), stdout: io.Discard}
}

// feedStdin points app.stdin at a pipe pre-loaded with content, so a
// --password-stdin command reads content as its password.
func feedStdin(t *testing.T, app *application, content string) {
	t.Helper()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := pw.WriteString(content); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	pw.Close()
	app.stdin = pr
	t.Cleanup(func() { pr.Close() })
}

// testPassword is a valid (>= minPasswordLength) password for CLI tests.
const testPassword = "s3cret-pass"

func TestAccountCreate(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	feedStdin(t, app, testPassword+"\n")

	if err := app.accountCreate(ctx, []string{"--email=bob@example.com", "--password-stdin"}); err != nil {
		t.Fatalf("accountCreate: %v", err)
	}

	got, err := app.models.Accounts.GetByEmail(ctx, "bob@example.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if got.Email != "bob@example.com" {
		t.Errorf("Email = %q, want bob@example.com", got.Email)
	}
	// The password is stored as an argon2id hash and verifies.
	if got.PasswordHash == nil {
		t.Fatal("PasswordHash is nil, want a stored hash")
	}
	if ok, err := auth.VerifyPassword(testPassword, *got.PasswordHash); err != nil || !ok {
		t.Errorf("VerifyPassword = %v, %v; want true, nil", ok, err)
	}

	// Creation seeds the default "Aperçu" dashboard through the shared path, so a
	// fresh account never faces an empty app (ADR 0018).
	dashboards, err := app.models.Dashboards.ListByAccount(ctx, got.ID)
	if err != nil {
		t.Fatalf("ListByAccount: %v", err)
	}
	if len(dashboards) != 1 || dashboards[0].Name != "Aperçu" {
		t.Fatalf("seeded dashboards = %+v, want one named Aperçu", dashboards)
	}
}

func TestAccountCreateRejectsShortPassword(t *testing.T) {
	app := newTestApp(t)
	feedStdin(t, app, "short\n")
	if err := app.accountCreate(context.Background(), []string{"--email=x@example.com", "--password-stdin"}); err == nil {
		t.Error("accountCreate with a too-short password should error")
	}
}

func TestAccountPasswd(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()

	feedStdin(t, app, testPassword+"\n")
	if err := app.accountCreate(ctx, []string{"--email=carol@example.com", "--password-stdin"}); err != nil {
		t.Fatalf("accountCreate: %v", err)
	}

	const newPassword = "brand-new-pass"
	feedStdin(t, app, newPassword+"\n")
	if err := app.accountPasswd(ctx, []string{"--email=carol@example.com", "--password-stdin"}); err != nil {
		t.Fatalf("accountPasswd: %v", err)
	}

	got, err := app.models.Accounts.GetByID(ctx, 1)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if ok, _ := auth.VerifyPassword(newPassword, *got.PasswordHash); !ok {
		t.Error("new password does not verify after passwd")
	}
	if ok, _ := auth.VerifyPassword(testPassword, *got.PasswordHash); ok {
		t.Error("old password still verifies after passwd")
	}
}

func TestAccountPasswdUnknownAccount(t *testing.T) {
	app := newTestApp(t)
	feedStdin(t, app, testPassword+"\n")
	if err := app.accountPasswd(context.Background(), []string{"--email=ghost@example.com", "--password-stdin"}); err == nil {
		t.Error("passwd for a nonexistent account should error")
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

	feedStdin(t, app, testPassword+"\n")
	if err := app.accountCreate(ctx, []string{"--email=dup@example.com", "--password-stdin"}); err != nil {
		t.Fatalf("first accountCreate: %v", err)
	}
	feedStdin(t, app, testPassword+"\n")
	if err := app.accountCreate(ctx, []string{"--email=dup@example.com", "--password-stdin"}); err == nil {
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
	// The password comes from a stdin pipe (--password-stdin).
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	pw.WriteString(testPassword + "\n")
	pw.Close()
	defer pr.Close()
	if err := run(ctx, testLogger(), pr, io.Discard, []string{"-data-dir=" + dir, "account", "create", "--email=alice@example.com", "--password-stdin"}); err != nil {
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
	if err := run(ctx, testLogger(), os.Stdin, io.Discard, []string{"-data-dir=" + dir, "migrate"}); err != nil {
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

func TestImportCommand(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	feedStdin(t, app, testPassword+"\n")
	if err := app.accountCreate(ctx, []string{"--email=me@example.com", "--password-stdin"}); err != nil {
		t.Fatalf("accountCreate: %v", err)
	}
	path := writeExport(t, t.TempDir())

	if err := app.importCommand(ctx, []string{"--account=me@example.com", path}); err != nil {
		t.Fatalf("importCommand: %v", err)
	}

	acc, err := app.models.Accounts.GetByEmail(ctx, "me@example.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	var count int
	if err := app.db.QueryRowContext(ctx,
		`SELECT count(*) FROM measurements WHERE account_id = ? AND metric = 'steps'`, acc.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("steps rows = %d, want 1", count)
	}

	// Re-importing the same file is idempotent: no new rows.
	if err := app.importCommand(ctx, []string{"--account=me@example.com", path}); err != nil {
		t.Fatalf("second importCommand: %v", err)
	}
	if err := app.db.QueryRowContext(ctx,
		`SELECT count(*) FROM measurements WHERE account_id = ?`, acc.ID).Scan(&count); err != nil {
		t.Fatalf("count all: %v", err)
	}
	if count != 1 {
		t.Errorf("measurement rows after re-import = %d, want 1", count)
	}
}

func TestImportCommandUnknownAccount(t *testing.T) {
	app := newTestApp(t)
	path := writeExport(t, t.TempDir())
	err := app.importCommand(context.Background(), []string{"--account=ghost@example.com", path})
	if err == nil {
		t.Error("import for a nonexistent account should error")
	}
}

func TestImportCommandRequiresArgs(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	if err := app.importCommand(ctx, []string{"somefile.xml"}); err == nil {
		t.Error("import without --account should error")
	}
	if err := app.importCommand(ctx, []string{"--account=me@example.com"}); err == nil {
		t.Error("import without a file argument should error")
	}
}
