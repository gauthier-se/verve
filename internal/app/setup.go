package app

import (
	"context"
	"fmt"
	"io"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store"
)

// offerSetup issues the setup token and writes the setup link on stderr when users holds no user
// (FR-1 of accounts and administration). The line is written outside the logger, whose redaction
// would hide the token: it is plain text whatever the log format, and written whatever the level.
func offerSetup(ctx context.Context, users store.UsersRepository, token *auth.SetupToken, publicURL config.PublicURL,
	stderr io.Writer,
) error {
	required, err := auth.SetupRequired(ctx, users)
	if err != nil {
		return fmt.Errorf("offer setup: %w", err)
	}

	if !required {
		return nil
	}

	// One write, so that the line never interleaves with a log line.
	_, _ = io.WriteString(stderr, setupLine(publicURL, token.Issue()))

	return nil
}

// setupLine returns the line that gives the setup link of token: on publicURL, or as a path on the
// address of the server when the public URL is unset.
func setupLine(publicURL config.PublicURL, token string) string {
	if publicURL.IsZero() {
		return "Create the first admin at /setup#" + token + " on the address of this server\n"
	}

	return "Create the first admin at " + publicURL.String() + "/setup#" + token + "\n"
}
