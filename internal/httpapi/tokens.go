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

// maxTokenBodyBytes bounds the body of a token creation. It holds the longest name, every scope
// and an expiry in JSON, even with every character escaped.
const maxTokenBodyBytes = 4 * 1024

// createTokenInput is the input of POST /api/v1/account/tokens.
type createTokenInput struct {
	Body struct {
		Name      string     `json:"name" minLength:"1" maxLength:"64" example:"Health Auto Export" doc:"The name of the token, which tells the automations apart: 1 to 64 characters."`
		Scopes    []string   `json:"scopes" nullable:"false" minItems:"1" uniqueItems:"true" enum:"ingest,read" example:"[\"ingest\"]" doc:"The scopes of the token, one or more distinct ones: ingest for the ingestion endpoints, read for the read-only endpoints of the public API."`
		ExpiresAt *time.Time `json:"expiresAt,omitempty" doc:"When the token stops being valid, after the current time. Left out, the token never expires."`
	}
}

// APIToken is an API token of the user of the request, as the tokens page lists it: never the
// token or its hash.
type APIToken struct {
	ID         int64      `json:"id" example:"3" doc:"The id of the token, which names it in DELETE /api/v1/account/tokens/{id}."`
	Name       string     `json:"name" example:"Health Auto Export" doc:"The name of the token."`
	Scopes     []string   `json:"scopes" nullable:"false" enum:"ingest,read" example:"[\"ingest\"]" doc:"The scopes of the token, in the order they were given."`
	Prefix     string     `json:"prefix" example:"vrv_3fK9" doc:"The first 8 characters of the token, to tell the tokens apart."`
	CreatedAt  time.Time  `json:"createdAt" doc:"When the token was created."`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty" doc:"When the token stops being valid, included, left out when it never expires."`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty" doc:"When the token was last used, written at most once per minute, left out when it never was."`
	LastUsedIP string     `json:"lastUsedIp,omitempty" example:"203.0.113.7" doc:"The IP address of the client that last used the token, left out when unknown or never used."`
	RevokedAt  *time.Time `json:"revokedAt,omitempty" doc:"When the token was revoked, left out while it is not."`
}

// CreatedAPIToken is a token just created, with the token itself, answered once.
type CreatedAPIToken struct {
	APIToken

	Token string `json:"token" doc:"The token, vrv_ followed by 43 base62 characters, to send as Authorization: Bearer <token>. It is answered once: only its hash is stored."`
}

// createTokenOutput is the response of POST /api/v1/account/tokens.
type createTokenOutput struct {
	Body CreatedAPIToken
}

// tokensOutput is the response of GET /api/v1/account/tokens.
type tokensOutput struct {
	Body struct {
		Items []APIToken `json:"items" nullable:"false" doc:"The tokens, revoked ones included, newest first."`
	}
}

// revokeTokenInput is the input of DELETE /api/v1/account/tokens/{id}.
type revokeTokenInput struct {
	ID int64 `path:"id" doc:"The id of the token."`
}

// registerTokens registers GET and POST /api/v1/account/tokens and DELETE
// /api/v1/account/tokens/{id}, which list, create and revoke the API tokens of the user of the
// session with deps.Tokens, through deps.Store. They accept sessions only: a token cannot create
// tokens.
func registerTokens(api huma.API, deps APIDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "create-token",
		Method:      http.MethodPost,
		Path:        "/api/v1/account/tokens",
		Summary:     "Create an API token",
		Description: "Creates an API token of the current user and answers it, once: only its hash and its prefix " +
			"are stored. An unknown, missing or repeated scope answers 422 `validation_failed`, and an expiry " +
			"that is not in the future 422 `expiry_in_past`. A request with a token answers 401: a token " +
			"cannot create tokens.",
		Tags:          []string{"Account"},
		Security:      sessionSecurity(),
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  maxTokenBodyBytes,
		Errors: []int{
			http.StatusUnauthorized, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, in *createTokenInput) (*createTokenOutput, error) {
		principal, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}

		scopes := make([]store.TokenScope, 0, len(in.Body.Scopes))
		for _, scope := range in.Body.Scopes {
			scopes = append(scopes, store.TokenScope(scope))
		}

		var expiresAt time.Time
		if in.Body.ExpiresAt != nil {
			expiresAt = *in.Body.ExpiresAt
		}

		token, created, err := deps.Tokens.Create(ctx, deps.Store, auth.NewAPIToken{
			UserID:    principal.UserID,
			Name:      in.Body.Name,
			Scopes:    scopes,
			ExpiresAt: expiresAt,
		})

		switch {
		case errors.Is(err, auth.ErrExpiryInPast):
			return nil, codedProblem(http.StatusUnprocessableEntity, err)
		case errors.Is(err, store.ErrNotFound):
			// The user was deleted since the session lookup.
			return nil, huma.NewError(http.StatusUnauthorized, "")
		case err != nil:
			return nil, fmt.Errorf("create token: %w", err)
		}

		return &createTokenOutput{Body: CreatedAPIToken{APIToken: apiTokenOf(created), Token: token}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-tokens",
		Method:      http.MethodGet,
		Path:        "/api/v1/account/tokens",
		Summary:     "List the API tokens of the current user",
		Description: "Returns the API tokens of the user, revoked ones included, newest first, with their usage: " +
			"never the token or its hash. A request with a token answers 401.",
		Tags:     []string{"Account"},
		Security: sessionSecurity(),
		Errors:   []int{http.StatusUnauthorized},
	}, func(ctx context.Context, _ *struct{}) (*tokensOutput, error) {
		principal, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}

		tokens, err := deps.Store.APITokens().ListByUser(ctx, principal.UserID)
		if err != nil {
			return nil, fmt.Errorf("list tokens: %w", err)
		}

		out := &tokensOutput{}
		out.Body.Items = make([]APIToken, 0, len(tokens))

		for _, token := range tokens {
			out.Body.Items = append(out.Body.Items, apiTokenOf(token))
		}

		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "revoke-token",
		Method:      http.MethodDelete,
		Path:        "/api/v1/account/tokens/{id}",
		Summary:     "Revoke an API token of the current user",
		Description: "Revokes the API token of the user with this id: the next request with it answers 401 " +
			"`token_revoked`. The token stays listed, with its revocation time, and revoking it again changes " +
			"nothing. A token of another user answers 404. A request with a token answers 401.",
		Tags:          []string{"Account"},
		Security:      sessionSecurity(),
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusUnauthorized, http.StatusNotFound},
	}, func(ctx context.Context, in *revokeTokenInput) (*struct{}, error) {
		principal, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}

		err = deps.Tokens.Revoke(ctx, deps.Store, principal.UserID, in.ID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, huma.NewError(http.StatusNotFound, "")
		}

		if err != nil {
			return nil, fmt.Errorf("revoke token: %w", err)
		}

		return &struct{}{}, nil
	})
}

// apiTokenOf returns token as the API describes it.
func apiTokenOf(token store.APIToken) APIToken {
	scopes := make([]string, 0, len(token.Scopes))
	for _, scope := range token.Scopes {
		scopes = append(scopes, string(scope))
	}

	return APIToken{
		ID:         token.ID,
		Name:       token.Name,
		Scopes:     scopes,
		Prefix:     token.Prefix,
		CreatedAt:  token.CreatedAt,
		ExpiresAt:  optionalTime(token.ExpiresAt),
		LastUsedAt: optionalTime(token.LastUsedAt),
		LastUsedIP: token.LastUsedIP,
		RevokedAt:  optionalTime(token.RevokedAt),
	}
}

// optionalTime returns a pointer to t, or nil when t is zero, for a time left out when unset.
func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}

	return &t
}
