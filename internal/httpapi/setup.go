package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/auth"
)

// maxSetupBodyBytes bounds the body of a setup. It holds the longest username and password in JSON,
// even with every character escaped.
const maxSetupBodyBytes = 8 * 1024

// setupStatusOutput is the response of GET /api/v1/setup.
type setupStatusOutput struct {
	Body struct {
		Required bool `json:"required" doc:"Whether the instance has no user yet: the first admin is then created with the setup token."`
	}
}

// setupInput is the input of POST /api/v1/setup.
type setupInput struct {
	Body struct {
		Token    string `json:"token" doc:"The setup token, from the setup link that verve serve writes on stderr while no user exists."`
		Username string `json:"username" example:"alice" doc:"The username of the admin: 1 to 64 characters, without @, whitespace, control or invisible characters."`
		Password string `json:"password" doc:"The password of the admin: 12 to 128 characters, and not a common password."`
	}
}

// setupOutput is the response of POST /api/v1/setup.
type setupOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie" doc:"The session cookie."`
	Body      CurrentUser
}

// registerSetup registers GET /api/v1/setup, which tells whether deps.Store holds no user yet, and
// POST /api/v1/setup, which creates the first admin with deps.Setup and sets the session cookie of
// deps.PublicURL.
func registerSetup(api huma.API, deps APIDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-setup-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/setup",
		Summary:     "Get the setup status",
		Description: "Tells whether the instance has no user yet, so that the first admin must be created with the " +
			"setup token. It needs no authentication.",
		Tags: []string{"Setup"},
	}, func(ctx context.Context, _ *struct{}) (*setupStatusOutput, error) {
		required, err := auth.SetupRequired(ctx, deps.Store.Users())
		if err != nil {
			return nil, fmt.Errorf("get setup status: %w", err)
		}

		out := &setupStatusOutput{}
		out.Body.Required = required

		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-first-admin",
		Method:      http.MethodPost,
		Path:        "/api/v1/setup",
		Summary:     "Create the first admin",
		Description: "Creates the first admin with the setup token, opens their session and sets the session " +
			"cookie. Once a user exists, whatever the token, it answers 409 `setup_completed`. A wrong token " +
			"answers 403 `setup_token_invalid`. A username or password that breaks the rules answers 422 " +
			"with the code of the rule: `username_invalid`, `password_too_short`, `password_too_long` or " +
			"`password_too_common`.",
		Tags:          []string{"Setup"},
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  maxSetupBodyBytes,
		Errors: []int{
			http.StatusForbidden, http.StatusConflict,
			http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, in *setupInput) (*setupOutput, error) {
		loggedIn, err := deps.Setup.CreateAdmin(ctx, deps.Store, auth.FirstAdmin{
			Token:    in.Body.Token,
			Username: in.Body.Username,
			Password: in.Body.Password,
		})
		if err != nil {
			return nil, setupError(err)
		}

		return &setupOutput{
			SetCookie: *newSessionCookie(deps.PublicURL, loggedIn.SessionID, loggedIn.Session.ExpiresAt),
			Body:      currentUserOf(loggedIn.User),
		}, nil
	})
}

// setupError returns the problem of the error of a setup: 409 setup_completed once a user exists,
// 403 setup_token_invalid for a wrong token, and 422 with the code of the broken rule for an invalid
// username or password.
func setupError(err error) error {
	switch {
	case errors.Is(err, auth.ErrSetupCompleted):
		return codedProblem(http.StatusConflict, err)
	case errors.Is(err, auth.ErrSetupTokenInvalid):
		return codedProblem(http.StatusForbidden, err)
	case errors.Is(err, auth.ErrUsernameInvalid), errors.Is(err, auth.ErrPasswordTooShort),
		errors.Is(err, auth.ErrPasswordTooLong), errors.Is(err, auth.ErrPasswordTooCommon):
		return codedProblem(http.StatusUnprocessableEntity, err)
	default:
		return fmt.Errorf("create first admin: %w", err)
	}
}

// codedProblem returns the problem of status whose code is the message of err, a stable code.
func codedProblem(status int, err error) *Problem {
	return &Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Code: err.Error()}
}
