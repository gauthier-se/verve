package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/auth"
)

// deleteAccountInput is the input of DELETE /api/v1/account.
type deleteAccountInput struct {
	Body struct {
		Password string `json:"password" minLength:"1" maxLength:"128" doc:"The password of the user."`
	}
}

// deleteAccountOutput is the response of DELETE /api/v1/account.
type deleteAccountOutput struct {
	SetCookie string `header:"Set-Cookie" doc:"The session cookie, cleared: the deletion revokes every session of the user."`
}

// deleteUserInput is the input of DELETE /api/v1/admin/users/{id}.
type deleteUserInput struct {
	ID   int64 `path:"id" doc:"The id of the user."`
	Body struct {
		Confirmation string `json:"confirmation" maxLength:"256" doc:"The exact username of the user, as the admin typed it to confirm."`
	}
}

// deleteUserOutput is the response of DELETE /api/v1/admin/users/{id}.
type deleteUserOutput struct {
	// SetCookie is empty, and left out, unless the admin deletes their own account.
	SetCookie string `header:"Set-Cookie" doc:"The session cookie, cleared, when the admin deletes their own account."`
	Body      AdminUser
}

// registerAccountDeletion registers DELETE /api/v1/account and DELETE /api/v1/admin/users/{id},
// which request the deletion of an account with deps.Deletions, through deps.Store.
func registerAccountDeletion(api huma.API, deps APIDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "delete-account",
		Method:      http.MethodDelete,
		Path:        "/api/v1/account",
		Summary:     "Delete the account of the current user",
		Description: "Disables the account at once, revokes every session of the user and clears the session " +
			"cookie, then removes the account and all its data in a background job. The username stays " +
			"taken until the job has finished. A wrong password answers 403 `invalid_credentials` and " +
			"counts as a failed login of the user, which delays their attempts with 429 `rate_limited` as " +
			"the login does. The last active admin answers 409 `last_admin`.",
		Tags:          []string{"Account"},
		Security:      sessionSecurity(),
		DefaultStatus: http.StatusAccepted,
		MaxBodyBytes:  maxPasswordBodyBytes,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict,
			http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity, http.StatusTooManyRequests,
		},
	}, func(ctx context.Context, in *deleteAccountInput) (*deleteAccountOutput, error) {
		principal, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}

		err = deps.Deletions.DeleteOwn(ctx, deps.Store, auth.OwnDeletion{UserID: principal.UserID, Password: in.Body.Password})
		if err != nil {
			return nil, deleteAccountError(err)
		}

		return &deleteAccountOutput{SetCookie: clearedSessionCookie(deps.PublicURL).String()}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-user",
		Method:      http.MethodDelete,
		Path:        "/api/v1/admin/users/{id}",
		Summary:     "Delete a user",
		Description: "Disables the account at once and revokes every session of the user, then removes the " +
			"account and all its data in a background job, and returns the user, being deleted. The " +
			"confirmation must be the exact username of the user, or the request answers 422 " +
			"`confirmation_mismatch`. The last active admin answers 409 `last_admin`, and an account " +
			"already being deleted 409 `user_deleting`.",
		Tags:          []string{"Admin"},
		Security:      sessionSecurity(),
		DefaultStatus: http.StatusAccepted,
		MaxBodyBytes:  maxAdminUserBodyBytes,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict,
			http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, in *deleteUserInput) (*deleteUserOutput, error) {
		principal, err := requireAdmin(ctx)
		if err != nil {
			return nil, err
		}

		user, err := deps.Deletions.DeleteAsAdmin(ctx, deps.Store, auth.AdminDeletion{
			ActorID:      principal.UserID,
			UserID:       in.ID,
			Confirmation: in.Body.Confirmation,
		})
		if errors.Is(err, auth.ErrConfirmationMismatch) {
			return nil, codedProblem(http.StatusUnprocessableEntity, err)
		}

		if err != nil {
			return nil, adminUserError("delete user", err)
		}

		out := &deleteUserOutput{Body: adminUserOf(user)}
		if user.ID == principal.UserID {
			out.SetCookie = clearedSessionCookie(deps.PublicURL).String()
		}

		return out, nil
	})
}

// deleteAccountError returns the problem of the error of the deletion of one's own account: those of
// a wrong password, as for a password change, and 409 last_admin.
func deleteAccountError(err error) error {
	if errors.Is(err, auth.ErrLastAdmin) || errors.Is(err, auth.ErrUserDeleting) {
		return codedProblem(http.StatusConflict, err)
	}

	return changePasswordError(err)
}
