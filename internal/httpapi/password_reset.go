package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/auth"
)

// createResetLinkInput is the input of POST /api/v1/admin/users/{id}/password-reset.
type createResetLinkInput struct {
	ID int64 `path:"id" doc:"The id of the user."`
}

// ResetLink is a password reset link that an admin created for a user.
type ResetLink struct {
	UserID    int64     `json:"userId" example:"2" doc:"The id of the user whose password the link resets."`
	ExpiresAt time.Time `json:"expiresAt" doc:"When the link stops being valid, 24 hours after its creation."`
	Link      string    `json:"link" doc:"The single-use link to give to the user, answered once: <VERVE_PUBLIC_URL>/reset-password#<token>, or /reset-password#<token> to append to the origin of the client when the public URL is not set."`
}

// createResetLinkOutput is the response of POST /api/v1/admin/users/{id}/password-reset.
type createResetLinkOutput struct {
	Body ResetLink
}

// resetPasswordInput is the input of POST /api/v1/password-reset.
type resetPasswordInput struct {
	Body struct {
		Token    string `json:"token" doc:"The token of the reset link, from the fragment of its link."`
		Password string `json:"password" doc:"The new password: 12 to 128 characters, and not a common password."`
	}
}

// registerPasswordResets registers POST /api/v1/admin/users/{id}/password-reset, which creates a
// reset link with deps.PasswordResets and answers it at deps.PublicURL, and POST
// /api/v1/password-reset, which uses it, both through deps.Store.
func registerPasswordResets(api huma.API, deps APIDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "create-password-reset-link",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/users/{id}/password-reset",
		Summary:     "Create a password reset link",
		Description: "Creates a password reset link valid 24 hours for the user and answers it, once: only the " +
			"hash of its token is stored. The earlier links of the user stop working. The admin never sets " +
			"nor sees the password. An account being deleted answers 409 `user_deleting`.",
		Tags:          []string{"Admin"},
		Security:      sessionSecurity(),
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *createResetLinkInput) (*createResetLinkOutput, error) {
		principal, err := requireAdmin(ctx)
		if err != nil {
			return nil, err
		}

		created, err := deps.PasswordResets.Create(ctx, deps.Store, principal.UserID, in.ID)
		if err != nil {
			return nil, adminUserError("create password reset link", err)
		}

		return &createResetLinkOutput{Body: ResetLink{
			UserID:    created.Link.UserID,
			ExpiresAt: created.Link.ExpiresAt,
			Link:      singleUseLink(deps.PublicURL, "/reset-password", created.Token),
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "reset-password",
		Method:      http.MethodPost,
		Path:        "/api/v1/password-reset",
		Summary:     "Reset a password",
		Description: "Replaces the password of the user of the reset link, marks the link used and revokes every " +
			"session of the user, who then logs in with the new password. The token is in the body so that " +
			"it never reaches a log. A used, expired or unknown link answers 410 `reset_link_invalid`. A " +
			"password that breaks the rules answers 422 with the code of the rule: `password_too_short`, " +
			"`password_too_long` or `password_too_common`, and the link stays valid. It needs no " +
			"authentication.",
		Tags:          []string{"Password reset"},
		DefaultStatus: http.StatusNoContent,
		MaxBodyBytes:  maxInvitationBodyBytes,
		Errors:        []int{http.StatusGone, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *resetPasswordInput) (*struct{}, error) {
		if err := deps.PasswordResets.Reset(ctx, deps.Store, in.Body.Token, in.Body.Password); err != nil {
			return nil, resetPasswordError(err)
		}

		return &struct{}{}, nil
	})
}

// resetPasswordError returns the problem of the error of a password reset: 410 for an invalid link
// and 422 with the code of the broken rule. Any other error is wrapped.
func resetPasswordError(err error) error {
	switch {
	case errors.Is(err, auth.ErrResetLinkInvalid):
		return codedProblem(http.StatusGone, err)
	case auth.IsPasswordPolicyError(err):
		return codedProblem(http.StatusUnprocessableEntity, err)
	default:
		return fmt.Errorf("reset password: %w", err)
	}
}
