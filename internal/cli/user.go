package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store"
)

// maxPasswordInput bounds what --password-stdin reads, in bytes. It is above the longest password,
// so that a longer input fails the password policy rather than being cut.
const maxPasswordInput = 4096

// createUserFunc creates a user in the configured database, also next to a running server.
type createUserFunc func(ctx context.Context, cfg config.Config, account auth.NewAccount) (store.User, error)

// listUsersFunc lists the users of the configured database, newest first.
type listUsersFunc func(ctx context.Context, cfg config.Config) ([]store.User, error)

// setUserStatusFunc enables or disables the user of login in the configured database.
type setUserStatusFunc func(ctx context.Context, cfg config.Config, login string, status store.UserStatus) (store.User, error)

// resetPasswordFunc replaces the password of the user of login in the configured database.
type resetPasswordFunc func(ctx context.Context, cfg config.Config, login, password string) (store.User, error)

// passwordFunc reads a password without echo from a terminal.
type passwordFunc func() (string, error)

func newUserCommand(loadConfig func() (config.Config, error), deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage the users",
		Args:  cobra.NoArgs,
	}

	var admin, passwordStdin bool

	create := &cobra.Command{
		Use:   "create <username>",
		Short: "Create a user, also while the server runs",
		Long: "Create a user who logs in with a password: a member, or an admin with --admin. The password is " +
			"asked twice on the terminal, or read from stdin with --password-stdin. Creating the first admin " +
			"this way completes the setup of the instance.",
		Args: cobra.ExactArgs(1),
		RunE: withConfig(loadConfig, func(cmd *cobra.Command, cfg config.Config) error {
			role := store.RoleMember
			if admin {
				role = store.RoleAdmin
			}

			// The username is the one argument that cobra.ExactArgs lets through.
			return createUser(cmd, cfg, deps, auth.NewAccount{Username: cmd.Flags().Arg(0), Role: role}, passwordStdin)
		}),
	}

	create.Flags().BoolVar(&admin, "admin", false, "create an admin rather than a member")
	create.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read the password from stdin rather than the terminal")

	list := &cobra.Command{
		Use:   "list",
		Short: "List the users, also while the server runs",
		Args:  cobra.NoArgs,
		RunE: withConfig(loadConfig, func(cmd *cobra.Command, cfg config.Config) error {
			users, err := deps.ListUsers(cmd.Context(), cfg)
			if err != nil {
				return fmt.Errorf("list users: %w", err)
			}

			return printUsers(cmd.OutOrStdout(), users)
		}),
	}

	var resetStdin bool

	resetPassword := &cobra.Command{
		Use:   "reset-password <username>",
		Short: "Replace the password of a user and revoke their sessions, also while the server runs",
		Long: "Replace the password of a user and revoke their sessions. The password is asked twice on the " +
			"terminal, or read from stdin with --password-stdin.",
		Args: cobra.ExactArgs(1),
		RunE: withConfig(loadConfig, func(cmd *cobra.Command, cfg config.Config) error {
			login := cmd.Flags().Arg(0)

			password, err := readPassword(cmd, resetStdin, deps.ReadPassword)
			if err != nil {
				return err
			}

			user, err := deps.ResetPassword(cmd.Context(), cfg, login, password)
			if err != nil {
				return userError(err, login, "reset password")
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Reset the password of %s\n", user.Username)

			return nil
		}),
	}

	resetPassword.Flags().BoolVar(&resetStdin, "password-stdin", false, "read the password from stdin rather than the terminal")

	cmd.AddCommand(create, list, resetPassword,
		newSetStatusCommand(loadConfig, deps, "disable", "Disable a user and revoke their sessions", store.UserDisabled, "Disabled"),
		newSetStatusCommand(loadConfig, deps, "enable", "Enable a disabled user again", store.UserActive, "Enabled"))

	return cmd
}

// newSetStatusCommand returns the command use, which gives the user of its argument status and
// prints done with their username.
func newSetStatusCommand(loadConfig func() (config.Config, error), deps Deps, use, short string, status store.UserStatus,
	done string,
) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <username>",
		Short: short + ", also while the server runs",
		Args:  cobra.ExactArgs(1),
		RunE: withConfig(loadConfig, func(cmd *cobra.Command, cfg config.Config) error {
			login := cmd.Flags().Arg(0)

			user, err := deps.SetUserStatus(cmd.Context(), cfg, login, status)
			if err != nil {
				return userError(err, login, use+" user")
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", done, user.Username)

			return nil
		}),
	}
}

// printUsers writes users to w as a table, a dash standing for a missing email or login.
func printUsers(w io.Writer, users []store.User) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	_, _ = fmt.Fprintln(tw, "USERNAME\tEMAIL\tROLE\tSTATUS\tCREATED\tLAST LOGIN")

	for _, user := range users {
		lastLogin := "-"
		if !user.LastLoginAt.IsZero() {
			lastLogin = user.LastLoginAt.Format(time.RFC3339)
		}

		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", user.Username, cmp.Or(user.Email, "-"), user.Role, user.Status,
			user.CreatedAt.Format(time.RFC3339), lastLogin)
	}

	if err := tw.Flush(); err != nil {
		return fmt.Errorf("print users: %w", err)
	}

	return nil
}

// userError returns err as what to change, for the errors of a command on the user of login that
// the person who runs it can fix, or else wrapped with op.
func userError(err error, login, op string) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("no user %q", login)
	case errors.Is(err, auth.ErrLastAdmin):
		return fmt.Errorf("%s is the last active admin", login)
	case errors.Is(err, auth.ErrUserDeleting):
		return fmt.Errorf("%s is being deleted", login)
	}

	if policyErr := passwordPolicyError(err); policyErr != nil {
		return policyErr
	}

	return fmt.Errorf("%s: %w", op, err)
}

// createUser reads the password of account, from stdin when fromStdin is set or else from the
// terminal, creates the user and prints it.
func createUser(cmd *cobra.Command, cfg config.Config, deps Deps, account auth.NewAccount, fromStdin bool) error {
	password, err := readPassword(cmd, fromStdin, deps.ReadPassword)
	if err != nil {
		return err
	}

	account.Password = password

	user, err := deps.CreateUser(cmd.Context(), cfg, account)
	if err != nil {
		return createUserError(err, account.Username)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created %s %s\n", user.Role, user.Username)

	return nil
}

// readPassword returns the password read from stdin when fromStdin is set, or else asked twice
// through terminal, which is nil when stdin is not a terminal.
func readPassword(cmd *cobra.Command, fromStdin bool, terminal passwordFunc) (string, error) {
	if fromStdin {
		return readPasswordStdin(cmd.InOrStdin())
	}

	if terminal == nil {
		return "", errors.New("stdin is not a terminal: pass --password-stdin to read the password from it")
	}

	password, err := askPassword(cmd.ErrOrStderr(), "Password: ", terminal)
	if err != nil {
		return "", err
	}

	confirmation, err := askPassword(cmd.ErrOrStderr(), "Confirm password: ", terminal)
	if err != nil {
		return "", err
	}

	if password != confirmation {
		return "", errors.New("passwords do not match")
	}

	return password, nil
}

// askPassword writes prompt to w and reads a password through terminal.
func askPassword(w io.Writer, prompt string, terminal passwordFunc) (string, error) {
	_, _ = io.WriteString(w, prompt)
	password, err := terminal()
	// The terminal does not echo the newline that ends the password either.
	_, _ = io.WriteString(w, "\n")

	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}

	return password, nil
}

// readPasswordStdin returns what r holds, without a final newline, as echo and most scripts end it.
func readPasswordStdin(r io.Reader) (string, error) {
	input, err := io.ReadAll(io.LimitReader(r, maxPasswordInput))
	if err != nil {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}

	password := strings.TrimSuffix(string(input), "\n")

	return strings.TrimSuffix(password, "\r"), nil
}

// createUserError returns err as what to change, for the errors that the person who runs the command
// can fix.
func createUserError(err error, username string) error {
	if policyErr := passwordPolicyError(err); policyErr != nil {
		return policyErr
	}

	switch {
	case errors.Is(err, auth.ErrUsernameInvalid):
		return fmt.Errorf("username must have 1 to %d characters, without @, whitespace, control or invisible characters",
			auth.MaxUsernameLength)
	case errors.Is(err, store.ErrConflict):
		return fmt.Errorf("username %q is taken", username)
	default:
		return fmt.Errorf("create user: %w", err)
	}
}

// passwordPolicyError returns the rule of the password policy that err breaks as what to change, or
// nil when err is not an error of the policy.
func passwordPolicyError(err error) error {
	switch {
	case errors.Is(err, auth.ErrPasswordTooShort):
		return fmt.Errorf("password must have at least %d characters", auth.MinPasswordLength)
	case errors.Is(err, auth.ErrPasswordTooLong):
		return fmt.Errorf("password must have at most %d characters", auth.MaxPasswordLength)
	case errors.Is(err, auth.ErrPasswordTooCommon):
		return errors.New("password is too common: choose another one")
	default:
		return nil
	}
}

// terminalPassword returns the function that reads a password without echo from the terminal of f,
// or nil when f is not a terminal.
func terminalPassword(f *os.File) passwordFunc {
	fd := int(f.Fd()) //nolint:gosec // G115: a file descriptor fits in an int
	if !term.IsTerminal(fd) {
		return nil
	}

	return func() (string, error) {
		password, err := term.ReadPassword(fd)

		return string(password), err
	}
}
