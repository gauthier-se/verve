package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/store"
)

// maxPasswordBodyBytes bounds the body of a password change. It holds the current and the new
// password in JSON, even with every character escaped.
const maxPasswordBodyBytes = 8 * 1024

// changePasswordInput is the input of POST /api/v1/account/password. The new password has no length
// in the schema: the password policy answers it with its own codes.
type changePasswordInput struct {
	Body struct {
		CurrentPassword string `json:"currentPassword" minLength:"1" maxLength:"128" doc:"The current password of the user."`
		NewPassword     string `json:"newPassword" doc:"The new password: 12 to 128 characters, and not a common password."`
	}
}

// Session is a session of the user of the request, as the sessions page lists it.
type Session struct {
	ID         string    `json:"id" doc:"The public id of the session, which names it in DELETE /api/v1/account/sessions/{id}."`
	Browser    string    `json:"browser,omitempty" example:"Safari" doc:"The browser that the user agent of the login names, left out when unknown. It is only a hint."`
	Device     string    `json:"device,omitempty" example:"iPhone" doc:"The device that the user agent of the login names, left out when unknown. It is only a hint."`
	ClientIP   string    `json:"clientIp,omitempty" example:"203.0.113.7" doc:"The IP address of the client that logged in, left out when unknown."`
	Method     string    `json:"method" enum:"password" doc:"How the user logged in."`
	LastSeenAt time.Time `json:"lastSeenAt" doc:"When the session was last used, to the minute."`
	Current    bool      `json:"current" doc:"Whether this is the session of the request."`
}

// sessionsOutput is the response of GET /api/v1/account/sessions.
type sessionsOutput struct {
	Body struct {
		Items []Session `json:"items" nullable:"false" doc:"The sessions, most recently used first."`
	}
}

// revokeSessionInput is the input of DELETE /api/v1/account/sessions/{id}.
type revokeSessionInput struct {
	ID string `path:"id" doc:"The public id of the session."`
}

// registerAccount registers the operations on the password and the sessions of the user of the
// session: POST /api/v1/account/password, which changes the password with deps.Passwords, and GET
// and DELETE /api/v1/account/sessions, which list and revoke the sessions with deps.Sessions, all
// through deps.Store.
func registerAccount(api huma.API, deps APIDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "change-password",
		Method:      http.MethodPost,
		Path:        "/api/v1/account/password",
		Summary:     "Change the password of the current user",
		Description: "Changes the password and revokes every other session of the user, keeping the current one. " +
			"A wrong current password answers 403 `invalid_credentials` and counts as a failed login of the " +
			"user, which delays their attempts with 429 `rate_limited` as the login does. A new password that " +
			"breaks the rules answers 422 with the code of the rule: `password_too_short`, " +
			"`password_too_long` or `password_too_common`.",
		Tags:          []string{"Account"},
		Security:      sessionSecurity(),
		DefaultStatus: http.StatusNoContent,
		MaxBodyBytes:  maxPasswordBodyBytes,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestEntityTooLarge,
			http.StatusUnprocessableEntity, http.StatusTooManyRequests,
		},
	}, func(ctx context.Context, in *changePasswordInput) (*struct{}, error) {
		principal, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}

		err = deps.Passwords.Change(ctx, deps.Store, auth.PasswordChange{
			UserID:    principal.UserID,
			Current:   in.Body.CurrentPassword,
			New:       in.Body.NewPassword,
			SessionID: sessionIDFrom(ctx),
		})
		if err != nil {
			return nil, changePasswordError(err)
		}

		return &struct{}{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-sessions",
		Method:      http.MethodGet,
		Path:        "/api/v1/account/sessions",
		Summary:     "List the sessions of the current user",
		Description: "Returns the valid sessions of the user, most recently used first, the current one flagged.",
		Tags:        []string{"Account"},
		Security:    sessionSecurity(),
		Errors:      []int{http.StatusUnauthorized},
	}, func(ctx context.Context, _ *struct{}) (*sessionsOutput, error) {
		principal, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}

		sessions, err := deps.Sessions.List(ctx, deps.Store, principal.UserID)
		if err != nil {
			return nil, fmt.Errorf("list sessions: %w", err)
		}

		out := &sessionsOutput{}
		out.Body.Items = make([]Session, 0, len(sessions))

		current := sessionPublicIDFrom(ctx)
		for _, session := range sessions {
			out.Body.Items = append(out.Body.Items, sessionOf(session, current))
		}

		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "revoke-session",
		Method:        http.MethodDelete,
		Path:          "/api/v1/account/sessions/{id}",
		Summary:       "Revoke a session of the current user",
		Description:   "Revokes the session of the user with this public id. A session of another user answers 404.",
		Tags:          []string{"Account"},
		Security:      sessionSecurity(),
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusUnauthorized, http.StatusNotFound},
	}, func(ctx context.Context, in *revokeSessionInput) (*struct{}, error) {
		principal, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}

		err = deps.Store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			if err := deps.Sessions.RevokeByPublicID(ctx, tx, principal.UserID, in.ID); err != nil {
				return err
			}

			return audit.Record(ctx, tx.AuditEvents(), audit.SessionsRevoked(principal.UserID, 1))
		})
		if errors.Is(err, store.ErrNotFound) {
			return nil, huma.NewError(http.StatusNotFound, "")
		}

		if err != nil {
			return nil, fmt.Errorf("revoke session: %w", err)
		}

		return &struct{}{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "revoke-other-sessions",
		Method:        http.MethodDelete,
		Path:          "/api/v1/account/sessions",
		Summary:       "Revoke the other sessions of the current user",
		Description:   "Revokes every session of the user but the current one.",
		Tags:          []string{"Account"},
		Security:      sessionSecurity(),
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusUnauthorized},
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		principal, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}

		err = deps.Store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			revoked, err := deps.Sessions.RevokeOthers(ctx, tx, principal.UserID, sessionIDFrom(ctx))
			if err != nil || revoked == 0 {
				return err
			}

			return audit.Record(ctx, tx.AuditEvents(), audit.SessionsRevoked(principal.UserID, revoked))
		})
		if err != nil {
			return nil, fmt.Errorf("revoke other sessions: %w", err)
		}

		return &struct{}{}, nil
	})
}

// changePasswordError returns the problem of the error of a password change: 403
// invalid_credentials for a wrong current password, 429 rate_limited with Retry-After for a delayed
// attempt, 422 with the code of the broken rule for a weak new password, and 401 for a user deleted
// since the session lookup.
func changePasswordError(err error) error {
	var limited *auth.RateLimitedError

	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		return &Problem{
			Type:   "about:blank",
			Title:  http.StatusText(http.StatusForbidden),
			Status: http.StatusForbidden,
			Code:   "invalid_credentials",
		}
	case errors.As(err, &limited):
		return loginError(err)
	case errors.Is(err, auth.ErrPasswordTooShort), errors.Is(err, auth.ErrPasswordTooLong),
		errors.Is(err, auth.ErrPasswordTooCommon):
		return codedProblem(http.StatusUnprocessableEntity, err)
	case errors.Is(err, store.ErrNotFound):
		return huma.NewError(http.StatusUnauthorized, "")
	default:
		return err
	}
}

// sessionOf returns session as the API describes it, current when its public id is current.
func sessionOf(session store.Session, current string) Session {
	agent := auth.ParseUserAgent(session.UserAgent)

	return Session{
		ID:         session.PublicID,
		Browser:    agent.Browser,
		Device:     agent.Device,
		ClientIP:   session.ClientIP,
		Method:     string(session.Method),
		LastSeenAt: session.LastSeenAt,
		Current:    session.PublicID == current,
	}
}
