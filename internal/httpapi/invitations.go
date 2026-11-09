package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store"
)

// maxLinkBodyBytes bounds the body of the operations of the single-use links. It holds the
// longest username, email, token and password in JSON, even with every character escaped.
const maxLinkBodyBytes = 8 * 1024

// createInvitationInput is the input of POST /api/v1/admin/invitations. The username and the email
// have no rules in the schema: the operation answers them with its own codes.
type createInvitationInput struct {
	Body struct {
		Username string `json:"username" example:"bob" doc:"The username of the account to create: 1 to 64 characters, without @, whitespace, control or invisible characters."`
		Email    string `json:"email,omitempty" example:"bob@example.com" doc:"The email of the account, optional. It holds an @."`
		Role     string `json:"role" enum:"admin,member" doc:"The role of the account."`
	}
}

// Invitation is an invitation that an admin created, with its link.
type Invitation struct {
	ID        int64     `json:"id" doc:"The id of the invitation."`
	Username  string    `json:"username" example:"bob" doc:"The username of the account that the invitation creates."`
	Email     string    `json:"email,omitempty" example:"bob@example.com" doc:"The email of the account, left out when there is none."`
	Role      string    `json:"role" enum:"admin,member" doc:"The role of the account."`
	ExpiresAt time.Time `json:"expiresAt" doc:"When the invitation stops being valid, 72 hours after its creation."`
	Link      string    `json:"link" doc:"The single-use link to send to the invitee, answered once: <VERVE_PUBLIC_URL>/invite#<token>, or /invite#<token> to append to the origin of the client when the public URL is not set."`
}

// createInvitationOutput is the response of POST /api/v1/admin/invitations.
type createInvitationOutput struct {
	Body Invitation
}

// inspectInvitationInput is the input of POST /api/v1/invitations/inspect.
type inspectInvitationInput struct {
	Body struct {
		Token string `json:"token" doc:"The token of the invitation, from the fragment of its link."`
	}
}

// InvitationDetails is what the invitee sees of an invitation before accepting it.
type InvitationDetails struct {
	Username  string    `json:"username" example:"bob" doc:"The username of the account that the invitation creates."`
	ExpiresAt time.Time `json:"expiresAt" doc:"When the invitation stops being valid."`
}

// inspectInvitationOutput is the response of POST /api/v1/invitations/inspect.
type inspectInvitationOutput struct {
	Body InvitationDetails
}

// acceptInvitationInput is the input of POST /api/v1/invitations/accept.
type acceptInvitationInput struct {
	Body struct {
		Token    string `json:"token" doc:"The token of the invitation, from the fragment of its link."`
		Password string `json:"password" doc:"The password of the new account: 12 to 128 characters, and not a common password."`
	}
}

// acceptInvitationOutput is the response of POST /api/v1/invitations/accept.
type acceptInvitationOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie" doc:"The session cookie."`
	Body      CurrentUser
}

// registerInvitations registers POST /api/v1/admin/invitations, which creates an invitation with
// deps.Invitations and answers its link at deps.PublicURL, and POST /api/v1/invitations/inspect and
// /accept, which read and accept it, all through deps.Store.
func registerInvitations(api huma.API, deps APIDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "create-invitation",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/invitations",
		Summary:     "Invite a user",
		Description: "Creates an invitation valid 72 hours and answers its single-use link, once: only the hash " +
			"of its token is stored. A username or email that a user has, ignoring case, answers 409 " +
			"`username_taken` or `email_taken`. A username or email that breaks the rules answers 422 " +
			"`username_invalid` or `email_invalid`.",
		Tags:          []string{"Admin"},
		Security:      sessionSecurity(),
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  maxLinkBodyBytes,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict,
			http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, in *createInvitationInput) (*createInvitationOutput, error) {
		principal, err := requireAdmin(ctx)
		if err != nil {
			return nil, err
		}

		created, err := deps.Invitations.Create(ctx, deps.Store, auth.NewInvitation{
			Username: in.Body.Username,
			Email:    in.Body.Email,
			Role:     store.UserRole(in.Body.Role),
			AdminID:  principal.UserID,
		})
		if err != nil {
			return nil, invitationError("create invitation", err)
		}

		invitation := created.Invitation

		return &createInvitationOutput{Body: Invitation{
			ID:        invitation.ID,
			Username:  invitation.Username,
			Email:     invitation.Email,
			Role:      string(invitation.Role),
			ExpiresAt: invitation.ExpiresAt,
			Link:      singleUseLink(deps.PublicURL, "/invite", created.Token),
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "inspect-invitation",
		Method:      http.MethodPost,
		Path:        "/api/v1/invitations/inspect",
		Summary:     "Inspect an invitation",
		Description: "Returns the username and the expiry of the invitation of the token, which the body carries " +
			"so that it never reaches a log. A used, expired or unknown invitation answers 410 " +
			"`invitation_invalid`. It needs no authentication.",
		Tags:         []string{"Invitations"},
		MaxBodyBytes: maxLinkBodyBytes,
		Errors:       []int{http.StatusGone, http.StatusRequestEntityTooLarge},
	}, func(ctx context.Context, in *inspectInvitationInput) (*inspectInvitationOutput, error) {
		invitation, err := deps.Invitations.Inspect(ctx, deps.Store, in.Body.Token)
		if err != nil {
			return nil, invitationError("inspect invitation", err)
		}

		return &inspectInvitationOutput{Body: InvitationDetails{
			Username:  invitation.Username,
			ExpiresAt: invitation.ExpiresAt,
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "accept-invitation",
		Method:      http.MethodPost,
		Path:        "/api/v1/invitations/accept",
		Summary:     "Accept an invitation",
		Description: "Creates the account of the invitation with the password, marks the invitation used, opens " +
			"the session and sets the session cookie. A used, expired or unknown invitation answers 410 " +
			"`invitation_invalid`. A password that breaks the rules answers 422 with the code of the rule: " +
			"`password_too_short`, `password_too_long` or `password_too_common`, and the invitation stays " +
			"valid. A username or email that a user took since the invitation answers 409 `username_taken` " +
			"or `email_taken`. It needs no authentication.",
		Tags:          []string{"Invitations"},
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  maxLinkBodyBytes,
		Errors: []int{
			http.StatusConflict, http.StatusGone,
			http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, in *acceptInvitationInput) (*acceptInvitationOutput, error) {
		loggedIn, err := deps.Invitations.Accept(ctx, deps.Store, in.Body.Token, in.Body.Password)
		if err != nil {
			return nil, invitationError("accept invitation", err)
		}

		current, err := loadCurrentUser(ctx, deps.Store, loggedIn.User)
		if err != nil {
			return nil, fmt.Errorf("accept invitation: %w", err)
		}

		return &acceptInvitationOutput{
			SetCookie: *newSessionCookie(deps.PublicURL, loggedIn.SessionID, loggedIn.Session.ExpiresAt),
			Body:      current,
		}, nil
	})
}

// singleUseLink returns the link to the page at path with token at publicURL, or its path when
// publicURL is unset. The token goes in the fragment, which never reaches a server (ADR 0010).
func singleUseLink(publicURL config.PublicURL, path, token string) string {
	return publicURL.String() + path + "#" + token
}

// invitationError returns the problem of the error of an invitation operation: 410 for an invalid
// invitation, 409 for a taken login, and 422 with the code of the broken rule. Any other error is
// wrapped with op.
func invitationError(op string, err error) error {
	switch {
	case errors.Is(err, auth.ErrInvitationInvalid):
		return codedProblem(http.StatusGone, err)
	case errors.Is(err, auth.ErrUsernameTaken), errors.Is(err, auth.ErrEmailTaken):
		return codedProblem(http.StatusConflict, err)
	case errors.Is(err, auth.ErrUsernameInvalid), errors.Is(err, auth.ErrEmailInvalid), auth.IsPasswordPolicyError(err):
		return codedProblem(http.StatusUnprocessableEntity, err)
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}
