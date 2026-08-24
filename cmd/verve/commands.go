package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/connector"
	"github.com/gauthier-se/verve/internal/connector/registry"
	"github.com/gauthier-se/verve/internal/data"
)

const usage = `verve — self-hosted health data warehouse

Usage:
  verve [-data-dir=DIR] <command> [args]

Commands:
  migrate                          apply database migrations (auto-applied on startup)
  account create --email=EMAIL     create an account (prompts for a password)
  account passwd --email=EMAIL     set an account's password
  import --account=EMAIL FILE      import a health export (Apple Health .zip/export.xml,
                                   Google Health Takeout .zip)
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

// importCommand runs whichever Connector recognizes the export file, scoped to
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

	// The same Exclusions the web import honours, read from the same table: the two
	// import paths must not disagree about what this Account accepts (ADR 0033).
	exclusions, err := app.models.Exclusions.Set(ctx, acc.ID)
	if err != nil {
		return err
	}

	// Which Connector reads the file is the file's business, not a flag's (ADR
	// 0009): every supported export is recognized by its content.
	conn, err := registry.For(path)
	if err != nil {
		return fmt.Errorf("import: %s is not an export Verve reads (%s)",
			path, strings.Join(registry.Labels(), ", "))
	}

	// The artifacts dir (where GPX routes are copied) is created at startup in
	// run(), so it already exists here.
	app.logger.Info("import started", "account", acc.Email, "file", path, "connector", conn.Name())
	report, err := conn.Import(ctx, app.models.ImportStore(), acc.ID, path, connector.Options{
		ArtifactsDir: app.config.artifactsDir(),
		Exclusions:   exclusions,
	})
	if err != nil {
		return err
	}
	renderReport(os.Stdout, report)
	return nil
}

// renderReport writes a human-readable import summary: one line per Metric with
// its added/skipped counts, the Unmapped bin broken down by source type, and a
// grand total.
func renderReport(w io.Writer, r connector.Report) {
	fmt.Fprintf(w, "\nImported %s (%s)\n\n", r.SourceFile, r.Connector)

	slugs := make([]string, 0, len(r.PerMetric))
	for slug := range r.PerMetric {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		c := r.PerMetric[slug]
		fmt.Fprintf(w, "  %-38s %8d added  %8d skipped", slug, c.Added, c.Skipped)
		// The third column appears only where there is one, so an Account with no
		// Exclusion reads the report it has always read.
		if c.Excluded > 0 {
			fmt.Fprintf(w, "  %8d excluded", c.Excluded)
		}
		fmt.Fprintln(w)
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

	// What the archive held and Verve did not read at all. It is not the Unmapped
	// bin: the bin keeps source data the Catalog has no Metric for, whereas these are
	// settings, receipts and READMEs, which are not health records and not Verve's to
	// They are still named, because an import that drops something silently is the
	// failure mode this report exists to avoid.
	if len(r.Ignored) > 0 {
		fmt.Fprintf(w, "\n  Ignored (not health data):\n")
		dirs := make([]string, 0, len(r.Ignored))
		for d := range r.Ignored {
			dirs = append(dirs, d)
		}
		sort.Strings(dirs)
		for _, d := range dirs {
			noun := "files"
			if r.Ignored[d] == 1 {
				noun = "file"
			}
			fmt.Fprintf(w, "    %-52s %8d %s\n", d, r.Ignored[d], noun)
		}
	}

	fmt.Fprintf(w, "\n  Total: %d measurements, %d states, %d sessions, %d routes added",
		r.Added, r.StatesAdded, r.SessionsAdded, r.RoutesAdded)
	fmt.Fprintf(w, " (%d/%d/%d/%d skipped, %d unmapped)\n",
		r.Skipped, r.StatesSkipped, r.SessionsSkipped, r.RoutesSkipped, r.Unmapped)
	if r.Excluded > 0 {
		fmt.Fprintf(w, "  Excluded: %d records your exclusions refused\n", r.Excluded)
	}
	fmt.Fprintln(w)
}

// renderFamily prints one non-scalar family's per-bucket added/skipped tallies
// (States by kind, Sessions by activity type), sorted for a stable report.
func renderFamily(w io.Writer, title string, per map[string]connector.Tally) {
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
