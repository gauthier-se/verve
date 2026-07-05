package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/gauthier-se/verve/internal/data"
)

const usage = `verve — self-hosted health data warehouse

Usage:
  verve [-data-dir=DIR] <command> [args]

Commands:
  migrate                       apply database migrations (auto-applied on startup)
  account create --email=EMAIL  create an account

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
