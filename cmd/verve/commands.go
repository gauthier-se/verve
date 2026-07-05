package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/gauthier-se/verve/internal/connector/applehealth"
	"github.com/gauthier-se/verve/internal/data"
)

const usage = `verve — self-hosted health data warehouse

Usage:
  verve [-data-dir=DIR] <command> [args]

Commands:
  migrate                          apply database migrations (auto-applied on startup)
  account create --email=EMAIL     create an account
  import --account=EMAIL FILE      import an Apple Health export (.zip or export.xml)

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
		return errors.New("usage: verve account create --email=EMAIL")
	}
	switch args[0] {
	case "create":
		return app.accountCreate(ctx, args[1:])
	default:
		return fmt.Errorf("unknown account subcommand %q", args[0])
	}
}

// accountCreate creates an Account from --email. Password hashing lands in
// slice 05, so the account is created without one for now.
func (app *application) accountCreate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("account create", flag.ContinueOnError)
	email := fs.String("email", "", "email address of the account to create")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("account create: --email is required")
	}

	acc := &data.Account{Email: *email}
	if err := app.models.Accounts.Insert(ctx, acc); err != nil {
		if errors.Is(err, data.ErrDuplicateEmail) {
			return fmt.Errorf("an account with email %q already exists", *email)
		}
		return err
	}
	app.logger.Info("account created", "id", acc.ID, "email", acc.Email)
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

	app.logger.Info("import started", "account", acc.Email, "file", path)
	report, err := applehealth.Import(ctx, app.models.Measurements, acc.ID, path)
	if err != nil {
		return err
	}
	renderReport(os.Stdout, report)
	return nil
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

	fmt.Fprintf(w, "\n  Total: %d added, %d skipped, %d unmapped\n\n", r.Added, r.Skipped, r.Unmapped)
}
