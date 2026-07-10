package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/connector/applehealth"
	"github.com/gauthier-se/verve/internal/data"
)

const usage = `verve — self-hosted health data warehouse

Usage:
  verve [-data-dir=DIR] <command> [args]

Commands:
  migrate                          apply database migrations (auto-applied on startup)
  account create --email=EMAIL     create an account (prompts for a password)
  account passwd --email=EMAIL     set an account's password
  import --account=EMAIL FILE      import an Apple Health export (.zip or export.xml)
  serve [--addr=:8080] [--secure-cookie]
                                   run the JSON API server
  version                          print the build version

Password commands prompt interactively; pass --password-stdin to read the
password from standard input instead (for scripting).

Global flags:
  -data-dir DIR   directory holding verve.db, artifacts/ and imports
                  (env VERVE_DATA_DIR, default ./data)
`

// dispatch routes to the requested subcommand.
func (app *application) dispatch(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("no command given\n\n" + usage)
	}
	switch args[0] {
	case "migrate":
		return app.migrateCommand()
	case "account":
		return app.accountCommand(ctx, args[1:])
	case "import":
		return app.importCommand(ctx, args[1:])
	case "serve":
		return app.serveCommand(ctx, args[1:])
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
}

// migrateCommand is the explicit migration entry point. Migrations already ran
// during startup wiring, so this is an idempotent confirmation.
func (app *application) migrateCommand() error {
	app.logger.Info("database schema up to date")
	return nil
}

// accountCommand handles the `account` subcommand group.
func (app *application) accountCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: verve account (create|passwd) --email=EMAIL")
	}
	switch args[0] {
	case "create":
		return app.accountCreate(ctx, args[1:])
	case "passwd":
		return app.accountPasswd(ctx, args[1:])
	default:
		return fmt.Errorf("unknown account subcommand %q", args[0])
	}
}

// accountCreate creates an Account from --email with an argon2id-hashed password
// (prompted, or read from stdin with --password-stdin).
func (app *application) accountCreate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("account create", flag.ContinueOnError)
	email := fs.String("email", "", "email address of the account to create")
	stdinPw := fs.Bool("password-stdin", false, "read the password from standard input instead of prompting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("account create: --email is required")
	}

	password, err := app.readNewPassword(*stdinPw)
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	acc := &data.Account{Email: *email, PasswordHash: &hash}
	if err := app.models.CreateAccount(ctx, acc); err != nil {
		if errors.Is(err, data.ErrDuplicateEmail) {
			return fmt.Errorf("an account with email %q already exists", *email)
		}
		return err
	}
	app.logger.Info("account created", "id", acc.ID, "email", acc.Email)
	return nil
}

// accountPasswd sets a new password on an existing Account.
func (app *application) accountPasswd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("account passwd", flag.ContinueOnError)
	email := fs.String("email", "", "email address of the account to update")
	stdinPw := fs.Bool("password-stdin", false, "read the password from standard input instead of prompting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("account passwd: --email is required")
	}

	acc, err := app.models.Accounts.GetByEmail(ctx, *email)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			return fmt.Errorf("no account with email %q", *email)
		}
		return err
	}

	password, err := app.readNewPassword(*stdinPw)
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := app.models.Accounts.SetPassword(ctx, acc.ID, hash); err != nil {
		return err
	}
	app.logger.Info("password updated", "id", acc.ID, "email", acc.Email)
	return nil
}

// importCommand runs the Apple Health Connector over an export file, scoped to
// the account named by --account, and prints a readable report to stdout.
func (app *application) importCommand(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	email := fs.String("account", "", "email of the owning account")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("import: --account=EMAIL is required")
	}
	if fs.NArg() != 1 {
		return errors.New("import: exactly one export file argument is required\n\nusage: verve import --account=EMAIL FILE")
	}
	path := fs.Arg(0)

	acc, err := app.models.Accounts.GetByEmail(ctx, *email)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			return fmt.Errorf("no account with email %q — create it first with `verve account create`", *email)
		}
		return err
	}

	// The artifacts dir (where GPX routes are copied) is created at startup in
	// run(), so it already exists here.
	app.logger.Info("import started", "account", acc.Email, "file", path)
	report, err := applehealth.Import(ctx, importStore{
		app.models.Measurements, app.models.States, app.models.Sessions,
	}, acc.ID, path, app.config.artifactsDir())
	if err != nil {
		return err
	}
	renderReport(os.Stdout, report)
	return nil
}

// importStore satisfies applehealth.Store by embedding the family models, whose
// promoted methods (InsertBatch, InsertStateBatch, InsertSession, …) together
// cover the interface — so the Connector writes through one value.
type importStore struct {
	data.MeasurementModel
	data.StateModel
	data.SessionModel
}

// renderReport writes a human-readable import summary: one line per Metric with
// its added/skipped counts, the Unmapped bin broken down by source type, and a
// grand total.
func renderReport(w io.Writer, r applehealth.Report) {
	fmt.Fprintf(w, "\nImported %s\n\n", r.SourceFile)

	slugs := make([]string, 0, len(r.PerMetric))
	for slug := range r.PerMetric {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		c := r.PerMetric[slug]
		fmt.Fprintf(w, "  %-38s %8d added  %8d skipped\n", slug, c.Added, c.Skipped)
	}

	renderFamily(w, "States", r.PerState)
	renderFamily(w, "Sessions", r.PerActivity)

	if r.RoutesAdded > 0 || r.RoutesSkipped > 0 {
		fmt.Fprintf(w, "\n  Routes: %d added, %d skipped\n", r.RoutesAdded, r.RoutesSkipped)
	}

	if len(r.UnmappedTypes) > 0 {
		fmt.Fprintf(w, "\n  Unmapped (kept for a later slice):\n")
		types := make([]string, 0, len(r.UnmappedTypes))
		for t := range r.UnmappedTypes {
			types = append(types, t)
		}
		sort.Strings(types)
		for _, t := range types {
			fmt.Fprintf(w, "    %-52s %8d\n", t, r.UnmappedTypes[t])
		}
	}

	fmt.Fprintf(w, "\n  Total: %d measurements, %d states, %d sessions, %d routes added",
		r.Added, r.StatesAdded, r.SessionsAdded, r.RoutesAdded)
	fmt.Fprintf(w, " (%d/%d/%d/%d skipped, %d unmapped)\n\n",
		r.Skipped, r.StatesSkipped, r.SessionsSkipped, r.RoutesSkipped, r.Unmapped)
}

// renderFamily prints one non-scalar family's per-bucket added/skipped tallies
// (States by kind, Sessions by activity type), sorted for a stable report.
func renderFamily(w io.Writer, title string, per map[string]applehealth.Tally) {
	if len(per) == 0 {
		return
	}
	keys := make([]string, 0, len(per))
	for k := range per {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(w, "\n  %s:\n", title)
	for _, k := range keys {
		c := per[k]
		fmt.Fprintf(w, "    %-38s %8d added  %8d skipped\n", k, c.Added, c.Skipped)
	}
}
