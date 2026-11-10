package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/logging"
	"github.com/gauthier-se/verve/internal/store"
)

// The names of the session cookie. The __Host- prefix makes browsers accept the cookie only when it
// is Secure, set by the host itself for Path=/ and without Domain, so that another host of the
// domain cannot plant it. It needs https.
const (
	sessionCookieName     = "verve_session"
	hostSessionCookieName = "__Host-" + sessionCookieName
)

// sessionCookieSchemeName names sessionCookieScheme in the OpenAPI document.
const sessionCookieSchemeName = "sessionCookie"

// sessionSecurity returns the security requirement of the operations that need a session.
func sessionSecurity() []map[string][]string {
	return []map[string][]string{{sessionCookieSchemeName: {}}}
}

// sessionCookieScheme describes the session cookie in the OpenAPI document. The document is the
// same for every instance, so it names the cookie without the prefix of the https instances.
func sessionCookieScheme() *huma.SecurityScheme {
	return &huma.SecurityScheme{
		Type: "apiKey",
		In:   "cookie",
		Name: sessionCookieName,
		Description: "The session cookie set at login, named `" + hostSessionCookieName + "` when the public " +
			"URL uses https. A `POST`, `PUT`, `PATCH` or `DELETE` must also send the header `Verve-Csrf: 1` " +
			"and an `Origin` matching the public URL.",
	}
}

// bearerTokenSchemeName names bearerTokenScheme in the OpenAPI document.
const bearerTokenSchemeName = "bearerToken"

// bearerTokenScheme describes the API tokens in the OpenAPI document.
func bearerTokenScheme() *huma.SecurityScheme {
	return &huma.SecurityScheme{
		Type:   "http",
		Scheme: "bearer",
		Description: "An API token, `vrv_` followed by 43 base62 characters, sent as `Authorization: Bearer " +
			"<token>`, never in the URL. A token has the scopes `ingest`, `read` or both, and is accepted " +
			"only by the operations that name one of its scopes.",
	}
}

// sessionCookieNameFor returns the name of the session cookie of an instance at publicURL.
func sessionCookieNameFor(publicURL config.PublicURL) string {
	if publicURL.SecureCookies() {
		return hostSessionCookieName
	}

	return sessionCookieName
}

// newSessionCookie returns the session cookie of an instance at publicURL, which holds the session
// identifier id until expiresAt. It is HttpOnly, SameSite=Lax and for Path=/, and Secure with the
// __Host- prefix when publicURL uses https.
func newSessionCookie(publicURL config.PublicURL, id string, expiresAt time.Time) *http.Cookie {
	return &http.Cookie{ //nolint:gosec // G124: not Secure over http, where browsers would drop it (FR-6)
		Name:     sessionCookieNameFor(publicURL),
		Value:    id,
		Path:     "/",
		Expires:  expiresAt,
		Secure:   publicURL.SecureCookies(),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

// clearedSessionCookie returns the cookie that removes the session cookie of an instance at
// publicURL. It keeps the attributes of the session cookie, which the __Host- prefix requires.
func clearedSessionCookie(publicURL config.PublicURL) *http.Cookie {
	cookie := newSessionCookie(publicURL, "", time.Time{}) //nolint:gosec // G124: the attributes of the session cookie (FR-6)
	cookie.MaxAge = -1

	return cookie
}

type (
	principalKey       struct{}
	sessionIDKey       struct{}
	sessionPublicIDKey struct{}
	lookupErrorKey     struct{}
)

// PrincipalFrom returns the principal stored by AuthenticationMiddleware, and false when the
// request is not authenticated.
func PrincipalFrom(ctx context.Context) (auth.Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(auth.Principal)

	return principal, ok
}

// sessionIDFrom returns the session identifier that the cookie of the request carries, valid or
// not, or empty when there is none. Login replaces that session, and logout deletes it.
func sessionIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(sessionIDKey{}).(string)

	return id
}

// sessionPublicIDFrom returns the public id of the valid session of the request, or empty when it
// has none.
func sessionPublicIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(sessionPublicIDKey{}).(string)

	return id
}

// AuthenticationMiddleware authenticates the requests under /api/ by their API token, looked up
// with tokens in repos, or by the session cookie of an instance at publicURL, looked up with
// sessions in repos, and stores their principal in their context.
//
// A request with Authorization: Bearer is authenticated by its token alone, never by its cookie,
// since the CSRF checks do not apply to it. A token that does not authenticate answers 401 on any
// operation, so that the client learns that its credential no longer works: unauthorized for an
// unknown token, and token_revoked, token_expired or user_disabled for a known one. A request with
// a token in its query string answers 400 token_in_query, and the token is never used.
//
// A request without a valid session goes on without a principal: the operations that need one
// answer 401. When the lookup renews the session, the response sends the cookie again with the new
// expiry, so that the browser keeps it as long as the session lives.
//
// A lookup that fails with another error than an invalid credential is logged, and the request
// goes on without a principal: the operations that need one answer 500, and the others, such as
// GET /api/v1/meta, answer as usual.
//
// It must run behind the gate, since the lookup needs a migrated database, and behind
// CSRFMiddleware.
func AuthenticationMiddleware(repos store.Repositories, sessions auth.Sessions, tokens auth.Tokens, publicURL config.PublicURL) func(http.Handler) http.Handler {
	cookieName := sessionCookieNameFor(publicURL)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, apiPrefix) {
				next.ServeHTTP(w, r)

				return
			}

			if hasTokenInQuery(r.URL) {
				WriteProblem(w, Problem{
					Status: http.StatusBadRequest,
					Code:   "token_in_query",
					Detail: "an API token goes in the header Authorization: Bearer, never in the URL",
				})

				return
			}

			if token, ok := bearerToken(r); ok {
				authenticateToken(w, r, next, repos, tokens, token)

				return
			}

			cookie, err := r.Cookie(cookieName)
			if err != nil || cookie.Value == "" {
				next.ServeHTTP(w, r)

				return
			}

			ctx := context.WithValue(r.Context(), sessionIDKey{}, cookie.Value)
			found, err := sessions.Lookup(ctx, repos, cookie.Value)

			switch {
			case errors.Is(err, store.ErrNotFound):
			case err != nil:
				logging.FromContext(ctx).ErrorContext(ctx, "look up session", slog.Any("error", err))
				ctx = context.WithValue(ctx, lookupErrorKey{}, fmt.Errorf("look up session: %w", err))
			default:
				if found.Renewed {
					http.SetCookie(w, newSessionCookie(publicURL, cookie.Value, found.Session.ExpiresAt))
				}

				principal := auth.Principal{UserID: found.Session.UserID, Role: found.User.Role, Method: auth.MethodSession}
				ctx = context.WithValue(ctx, principalKey{}, principal)
				ctx = context.WithValue(ctx, sessionPublicIDKey{}, found.Session.PublicID)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// tokenRejections are the errors of auth.Tokens.Authenticate for a known token, whose messages are
// the codes of their 401 problems.
var tokenRejections = []error{auth.ErrTokenRevoked, auth.ErrTokenExpired, auth.ErrUserDisabled}

// authenticateToken authenticates r by the API token token, looked up with tokens in repos, and
// serves it on next with the principal of the token, or answers 401 when the token does not
// authenticate. A lookup that fails otherwise is logged, and r goes on without a principal.
func authenticateToken(w http.ResponseWriter, r *http.Request, next http.Handler, repos store.Repositories, tokens auth.Tokens, token string) {
	ctx := r.Context()
	found, user, err := tokens.Authenticate(ctx, repos, token, ClientInfoFrom(ctx).IP)

	if errors.Is(err, store.ErrNotFound) {
		WriteProblem(w, Problem{Status: http.StatusUnauthorized, Code: defaultCode(http.StatusUnauthorized)})

		return
	}

	for _, rejected := range tokenRejections {
		if errors.Is(err, rejected) {
			WriteProblem(w, Problem{Status: http.StatusUnauthorized, Code: rejected.Error()})

			return
		}
	}

	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "look up token", slog.Any("error", err))
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, lookupErrorKey{}, fmt.Errorf("look up token: %w", err))))

		return
	}

	principal := auth.Principal{UserID: user.ID, Method: auth.MethodToken, TokenID: found.ID, Scopes: found.Scopes}
	next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, principalKey{}, principal)))
}

// hasBearer reports whether r carries Authorization: Bearer, whose scheme is case-insensitive.
func hasBearer(r *http.Request) bool {
	_, ok := bearerToken(r)

	return ok
}

// bearerToken returns the token of the header Authorization: Bearer of r, and false when r has no
// such header. The scheme is case-insensitive, and the spaces around the token and a second Bearer
// prefix are dropped, since apps such as Health Auto Export take the whole value of the header,
// where the token is pasted with them.
func bearerToken(r *http.Request) (string, bool) {
	scheme, token, found := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}

	token = strings.TrimSpace(token)

	if scheme, rest, found := strings.Cut(token, " "); found && strings.EqualFold(scheme, "Bearer") {
		token = strings.TrimSpace(rest)
	}

	return token, true
}

// hasTokenInQuery reports whether the query string of u holds an API token: the parameter
// access_token of RFC 6750, in any case and even empty, or a parameter or a value that starts like
// an API token.
func hasTokenInQuery(u *url.URL) bool {
	for key, values := range u.Query() {
		if strings.EqualFold(key, "access_token") || auth.LooksLikeAPIToken(key) ||
			slices.ContainsFunc(values, auth.LooksLikeAPIToken) {
			return true
		}
	}

	return false
}

// authenticated returns the principal of the request of ctx, a 401 problem when it is not
// authenticated, or the error of its lookup, which Huma answers with 500.
func authenticated(ctx context.Context) (auth.Principal, error) {
	if err, ok := ctx.Value(lookupErrorKey{}).(error); ok {
		return auth.Principal{}, err
	}

	principal, ok := PrincipalFrom(ctx)
	if !ok {
		return auth.Principal{}, huma.NewError(http.StatusUnauthorized, "")
	}

	return principal, nil
}

// requireUser returns the principal of the request of ctx when it has a session, a 401 problem when
// it has none, also for a request authenticated by a token, or the error of its lookup, which Huma
// answers with 500. The operations accept sessions only unless they call requireScope.
func requireUser(ctx context.Context) (auth.Principal, error) {
	principal, err := authenticated(ctx)
	if err != nil {
		return auth.Principal{}, err
	}

	if principal.Method != auth.MethodSession {
		return auth.Principal{}, huma.NewError(http.StatusUnauthorized, "")
	}

	return principal, nil
}

// requireScope returns the principal of the request of ctx when it has a session, or a token with
// scope: the operations of the public API accept both. It returns a 401 problem when the request is
// not authenticated, a 403 scope_missing problem for a token without scope, or the error of the
// lookup.
func requireScope(ctx context.Context, scope store.TokenScope) (auth.Principal, error) {
	principal, err := authenticated(ctx)
	if err != nil {
		return auth.Principal{}, err
	}

	if principal.Method == auth.MethodToken && !slices.Contains(principal.Scopes, scope) {
		return auth.Principal{}, &Problem{
			Type:   "about:blank",
			Title:  http.StatusText(http.StatusForbidden),
			Status: http.StatusForbidden,
			Detail: "the token lacks the scope " + string(scope),
			Code:   "scope_missing",
		}
	}

	return principal, nil
}

// requireAdmin returns the principal of the request of ctx when it is an admin, a 401 problem when
// the request is not authenticated, and a 403 problem otherwise.
func requireAdmin(ctx context.Context) (auth.Principal, error) {
	principal, err := requireUser(ctx)
	if err != nil {
		return auth.Principal{}, err
	}

	if principal.Role != store.RoleAdmin {
		return auth.Principal{}, huma.NewError(http.StatusForbidden, "")
	}

	return principal, nil
}
