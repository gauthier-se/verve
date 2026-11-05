package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

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
		RunE: func(cmd *cobra.Command, args []string) error {
			return withConfig(loadConfig, func(cmd *cobra.Command, cfg config.Config) error {
				role := store.RoleMember
				if admin {
					role = store.RoleAdmin
				}

				return createUser(cmd, cfg, deps, auth.NewAccount{Username: args[0], Role: role}, passwordStdin)
			})(cmd, args)
		},
	}

	create.Flags().BoolVar(&admin, "admin", false, "create an admin rather than a member")
	create.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read the password from stdin rather than the terminal")

	cmd.AddCommand(create)

	return cmd
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
	switch {
	case errors.Is(err, auth.ErrUsernameInvalid):
		return errors.New("username must have 1 to 64 characters, without @, whitespace, control or invisible characters")
	case errors.Is(err, auth.ErrPasswordTooShort):
		return errors.New("password must have at least 12 characters")
	case errors.Is(err, auth.ErrPasswordTooLong):
		return errors.New("password must have at most 128 characters")
	case errors.Is(err, auth.ErrPasswordTooCommon):
		return errors.New("password is too common: choose another one")
	case errors.Is(err, store.ErrConflict):
		return fmt.Errorf("username %q is taken", username)
	default:
		return fmt.Errorf("create user: %w", err)
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
