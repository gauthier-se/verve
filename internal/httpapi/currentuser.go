package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/store"
)

// CurrentUser is the user on whose behalf a request runs.
type CurrentUser struct {
	ID       int64  `json:"id" example:"1" doc:"The id of the user."`
	Username string `json:"username" example:"alice" doc:"The username, in the case it was created with."`
	Email    string `json:"email,omitempty" example:"alice@example.com" doc:"The email address, left out when the user has none."`
	Role     string `json:"role" enum:"admin,member" example:"member" doc:"The role of the user."`
	// Timezone is the timezone of Preferences, so that the web app knows at once whether to ask for
	// it.
	Timezone *string `json:"timezone" nullable:"true" example:"Europe/Paris" doc:"The IANA name of the timezone of the user, or null until they confirm one."`
}

// currentUserOutput is the response of GET /api/v1/auth/me.
type currentUserOutput struct {
	Body CurrentUser
}

// registerCurrentUser registers GET /api/v1/auth/me, which reports the user of the session, read
// in repos.
func registerCurrentUser(api huma.API, repos store.Repositories) {
	huma.Register(api, huma.Operation{
		OperationID: "get-current-user",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/me",
		Summary:     "Get the current user",
		Description: "Returns the user of the session.",
		Tags:        []string{"Auth"},
		Security:    sessionSecurity(),
		Errors:      []int{http.StatusUnauthorized},
	}, func(ctx context.Context, _ *struct{}) (*currentUserOutput, error) {
		principal, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}

		user, err := repos.Users().Get(ctx, principal.UserID)
		if err != nil {
			return nil, currentUserError(err)
		}

		current, err := loadCurrentUser(ctx, repos, user)
		if err != nil {
			return nil, currentUserError(err)
		}

		return &currentUserOutput{Body: current}, nil
	})
}

// currentUserError returns the problem of the error of GET /api/v1/auth/me: 401 for a user deleted
// since the session lookup, and the error otherwise.
func currentUserError(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return huma.NewError(http.StatusUnauthorized, "")
	}

	return fmt.Errorf("get current user: %w", err)
}

// loadCurrentUser returns user as the API describes the current user, with their preferences
// read in repos.
func loadCurrentUser(ctx context.Context, repos store.Repositories, user store.User) (CurrentUser, error) {
	prefs, err := repos.Preferences().Get(ctx, user.ID)
	if err != nil {
		return CurrentUser{}, err
	}

	return currentUserOf(user, prefs), nil
}

// currentUserOf returns user, whose preferences are prefs, as the API describes the current user.
func currentUserOf(user store.User, prefs store.Preferences) CurrentUser {
	return CurrentUser{
		ID:       user.ID,
		Username: user.Username,
		Email:    user.Email,
		Role:     string(user.Role),
		Timezone: timezoneOf(prefs),
	}
}
