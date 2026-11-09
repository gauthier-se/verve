package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/store"
)

// maxAdminUserBodyBytes bounds the body of the status and role changes, which hold one short value.
const maxAdminUserBodyBytes = 1024

// AdminUser is what an admin sees of an account: its metadata, never its health data (FR-9 and
// FR-10 of accounts and administration).
type AdminUser struct {
	ID          int64      `json:"id" example:"2" doc:"The id of the user."`
	Username    string     `json:"username" example:"bob" doc:"The username, in the case it was created with."`
	Email       string     `json:"email,omitempty" example:"bob@example.com" doc:"The email address, left out when the user has none."`
	Role        string     `json:"role" enum:"admin,member" example:"member" doc:"The role of the user."`
	Status      string     `json:"status" enum:"active,disabled,deleting" example:"active" doc:"The status of the account. A disabled user cannot log in."`
	CreatedAt   time.Time  `json:"createdAt" doc:"When the account was created."`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty" doc:"When the user last logged in, left out when they never did."`
}

// adminUsersInput is the input of GET /api/v1/admin/users.
type adminUsersInput struct {
	PageParams
}

// adminUsersOutput is the response of GET /api/v1/admin/users.
type adminUsersOutput struct {
	Body Page[AdminUser]
}

// setUserStatusInput is the input of PUT /api/v1/admin/users/{id}/status.
type setUserStatusInput struct {
	ID   int64 `path:"id" doc:"The id of the user."`
	Body struct {
		Status string `json:"status" enum:"active,disabled" doc:"The status to set: disabled revokes the sessions of the user and refuses their logins until active again."`
	}
}

// setUserRoleInput is the input of PUT /api/v1/admin/users/{id}/role.
type setUserRoleInput struct {
	ID   int64 `path:"id" doc:"The id of the user."`
	Body struct {
		Role string `json:"role" enum:"admin,member" doc:"The role to set. A change revokes the sessions of the user."`
	}
}

// adminUserOutput is the response of the status and role changes.
type adminUserOutput struct {
	Body AdminUser
}

// registerAdminUsers registers GET /api/v1/admin/users, the accounts for an admin, read in
// deps.Store, and PUT /api/v1/admin/users/{id}/status and /role, which change them with
// deps.Admin.
func registerAdminUsers(api huma.API, deps APIDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "list-admin-users",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/users",
		Summary:     "List the users",
		Description: "Returns the accounts newest first, with their metadata only: never their health data.",
		Tags:        []string{"Admin"},
		Security:    sessionSecurity(),
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *adminUsersInput) (*adminUsersOutput, error) {
		if _, err := requireAdmin(ctx); err != nil {
			return nil, err
		}

		users, next, err := listPage(in.PageParams, func(page store.PageRequest) ([]store.User, error) {
			return deps.Store.Users().List(ctx, page)
		}, store.User.Cursor)
		if err != nil {
			return nil, fmt.Errorf("list users: %w", err)
		}

		items := make([]AdminUser, 0, len(users))
		for _, user := range users {
			items = append(items, adminUserOf(user))
		}

		return &adminUsersOutput{Body: Page[AdminUser]{Items: items, NextCursor: next}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-user-status",
		Method:      http.MethodPut,
		Path:        "/api/v1/admin/users/{id}/status",
		Summary:     "Enable or disable a user",
		Description: "Sets the status of the user and returns the user. Disabling revokes every session of the " +
			"user at once, and their logins fail until they are active again. Disabling the last active " +
			"admin answers 409 `last_admin`, and a change of an account being deleted 409 `user_deleting`.",
		Tags:         []string{"Admin"},
		Security:     sessionSecurity(),
		MaxBodyBytes: maxAdminUserBodyBytes,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict,
			http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, in *setUserStatusInput) (*adminUserOutput, error) {
		principal, err := requireAdmin(ctx)
		if err != nil {
			return nil, err
		}

		user, err := deps.Admin.SetStatus(ctx, deps.Store, auth.StatusChange{
			ActorID: principal.UserID,
			UserID:  in.ID,
			Status:  store.UserStatus(in.Body.Status),
		})
		if err != nil {
			return nil, adminUserError("set user status", err)
		}

		return &adminUserOutput{Body: adminUserOf(user)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-user-role",
		Method:      http.MethodPut,
		Path:        "/api/v1/admin/users/{id}/role",
		Summary:     "Change the role of a user",
		Description: "Sets the role of the user and returns the user. A change revokes every session of the " +
			"user, so that they log in again with a new session. Demoting the last active admin answers " +
			"409 `last_admin`, and a change of an account being deleted 409 `user_deleting`.",
		Tags:         []string{"Admin"},
		Security:     sessionSecurity(),
		MaxBodyBytes: maxAdminUserBodyBytes,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict,
			http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, in *setUserRoleInput) (*adminUserOutput, error) {
		principal, err := requireAdmin(ctx)
		if err != nil {
			return nil, err
		}

		user, err := deps.Admin.SetRole(ctx, deps.Store, auth.RoleChange{
			ActorID: principal.UserID,
			UserID:  in.ID,
			Role:    store.UserRole(in.Body.Role),
		})
		if err != nil {
			return nil, adminUserError("set user role", err)
		}

		return &adminUserOutput{Body: adminUserOf(user)}, nil
	})
}

// adminUserOf returns what an admin sees of user.
func adminUserOf(user store.User) AdminUser {
	out := AdminUser{
		ID:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		Role:      string(user.Role),
		Status:    string(user.Status),
		CreatedAt: user.CreatedAt,
	}

	if !user.LastLoginAt.IsZero() {
		out.LastLoginAt = &user.LastLoginAt
	}

	return out
}

// adminUserError returns the problem of the error of a change of a user: 404 for an unknown user
// and 409 last_admin or user_deleting. Any other error is wrapped with op.
func adminUserError(op string, err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return huma.NewError(http.StatusNotFound, "")
	case errors.Is(err, auth.ErrLastAdmin), errors.Is(err, auth.ErrUserDeleting):
		return codedProblem(http.StatusConflict, err)
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}
