package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/store"
)

// maxLoginBodyBytes bounds the body of a login. It holds the longest login and password in JSON,
// even with every character escaped.
const maxLoginBodyBytes = 8 * 1024

// loginInput is the input of POST /api/v1/auth/login. A login is at most 254 characters, the
// longest email address, so that the logins an attacker tries stay small in the limiter.
type loginInput struct {
	Body struct {
		Login    string `json:"login" minLength:"1" maxLength:"254" example:"alice" doc:"The username or the email address, in any case."`
		Password string `json:"password" minLength:"1" maxLength:"128" doc:"The password, at most 128 characters."`
	}
}

// loginOutput is the response of POST /api/v1/auth/login.
type loginOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie" doc:"The session cookie."`
	Body      CurrentUser
}

// logoutOutput is the response of POST /api/v1/auth/logout.
type logoutOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie" doc:"The session cookie, cleared."`
}

// registerLogin registers POST /api/v1/auth/login, which logs in with logins through st and sets
// the session cookie of an instance at publicURL, and POST /api/v1/auth/logout, which deletes the
// session of the request with sessions and clears the cookie.
func registerLogin(api huma.API, deps APIDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "login",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/login",
		Summary:     "Log in",
		Description: "Logs in with a username or an email and a password, and sets the session cookie. A " +
			"session that the request already has is replaced. Every failure answers 401 " +
			"`invalid_credentials`. After 5 failures for one login, or 20 for one client IP, attempts " +
			"answer 429 `rate_limited` with `Retry-After` in seconds, the delay doubling from 1 second " +
			"up to 15 minutes.",
		Tags:         []string{"Auth"},
		MaxBodyBytes: maxLoginBodyBytes,
		Errors: []int{
			http.StatusUnauthorized, http.StatusRequestEntityTooLarge,
			http.StatusUnprocessableEntity, http.StatusTooManyRequests,
		},
	}, func(ctx context.Context, in *loginInput) (*loginOutput, error) {
		loggedIn, err := deps.Logins.Login(ctx, deps.Store, auth.Credentials{
			Login:     in.Body.Login,
			Password:  in.Body.Password,
			SessionID: sessionIDFrom(ctx),
		})
		if err != nil {
			return nil, loginError(err)
		}

		return &loginOutput{
			SetCookie: *newSessionCookie(deps.PublicURL, loggedIn.SessionID, loggedIn.Session.ExpiresAt),
			Body:      currentUserOf(loggedIn.User),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "logout",
		Method:        http.MethodPost,
		Path:          "/api/v1/auth/logout",
		Summary:       "Log out",
		Description:   "Deletes the session of the request and clears the session cookie, even without a session.",
		Tags:          []string{"Auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, _ *struct{}) (*logoutOutput, error) {
		if err := logout(ctx, deps.Store, deps.Sessions); err != nil {
			return nil, err
		}

		return &logoutOutput{SetCookie: *clearedSessionCookie(deps.PublicURL)}, nil
	})
}

// loginError returns the problem of the error of a login: 401 invalid_credentials for a failure,
// alike whatever its cause, and 429 rate_limited with Retry-After for a delayed attempt.
func loginError(err error) error {
	var limited *auth.RateLimitedError

	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		return &Problem{
			Type:   "about:blank",
			Title:  http.StatusText(http.StatusUnauthorized),
			Status: http.StatusUnauthorized,
			Code:   "invalid_credentials",
		}
	case errors.As(err, &limited):
		return huma.ErrorWithHeaders(huma.NewError(http.StatusTooManyRequests, ""),
			http.Header{"Retry-After": {retryAfterSeconds(limited.RetryAfter)}})
	default:
		return err
	}
}

// retryAfterSeconds returns wait in whole seconds, rounded up, for a Retry-After header.
func retryAfterSeconds(wait time.Duration) string {
	return strconv.FormatInt(int64((wait+time.Second-1)/time.Second), 10)
}

// logout deletes the session of the request of ctx through st, with sessions, and records the audit
// event of its user. A request without a session, or with an unknown one, has nothing to delete,
// and an expired session has no user to record.
func logout(ctx context.Context, st store.Store, sessions auth.Sessions) error {
	id := sessionIDFrom(ctx)
	if id == "" {
		return nil
	}

	err := st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		err := sessions.Revoke(ctx, tx, id)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}

		if err != nil {
			return err
		}

		principal, ok := PrincipalFrom(ctx)
		if !ok {
			return nil
		}

		return audit.Record(ctx, tx.AuditEvents(), audit.Logout(principal.UserID))
	})
	if err != nil {
		return fmt.Errorf("log out: %w", err)
	}

	return nil
}
