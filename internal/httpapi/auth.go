package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	principalKey   struct{}
	sessionIDKey   struct{}
	lookupErrorKey struct{}
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

// AuthenticationMiddleware authenticates the requests under /api/ by the session cookie of an
// instance at publicURL, looked up with sessions in repos, and stores their principal in their
// context. A request without a valid session goes on without a principal: the operations that need
// one answer 401. A request with Authorization: Bearer is never authenticated by its cookie, since
// the CSRF checks do not apply to it.
//
// When the lookup renews the session, the response sends the cookie again with the new expiry, so
// that the browser keeps it as long as the session lives.
//
// A lookup that fails with another error than an invalid session is logged, and the request goes
// on without a principal: the operations that need one answer 500, and the others, such as GET
// /api/v1/meta, answer as usual.
//
// It must run behind the gate, since the lookup needs a migrated database, and behind
// CSRFMiddleware.
func AuthenticationMiddleware(repos store.Repositories, sessions auth.Sessions, publicURL config.PublicURL) func(http.Handler) http.Handler {
	cookieName := sessionCookieNameFor(publicURL)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, apiPrefix) || hasBearer(r) {
				next.ServeHTTP(w, r)

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
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// hasBearer reports whether r carries Authorization: Bearer, whose scheme is case-insensitive.
func hasBearer(r *http.Request) bool {
	scheme, _, found := strings.Cut(r.Header.Get("Authorization"), " ")

	return found && strings.EqualFold(scheme, "Bearer")
}

// requireUser returns the principal of the request of ctx, a 401 problem when it is not
// authenticated, or the error of its session lookup, which Huma answers with 500.
func requireUser(ctx context.Context) (auth.Principal, error) {
	if err, ok := ctx.Value(lookupErrorKey{}).(error); ok {
		return auth.Principal{}, err
	}

	principal, ok := PrincipalFrom(ctx)
	if !ok {
		return auth.Principal{}, huma.NewError(http.StatusUnauthorized, "")
	}

	return principal, nil
}

// requireAdmin returns the principal of the request of ctx when it is an admin, a 401 problem when
// the request is not authenticated, and a 403 problem otherwise.
func requireAdmin(ctx context.Context) (auth.Principal, error) { //nolint:unparam // the admin actions to come record the principal as the actor of their audit event
	principal, err := requireUser(ctx)
	if err != nil {
		return auth.Principal{}, err
	}

	if principal.Role != store.RoleAdmin {
		return auth.Principal{}, huma.NewError(http.StatusForbidden, "")
	}

	return principal, nil
}
