// Package httpapi builds the HTTP handler of the server.
package httpapi

import (
	"cmp"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/version"
)

// apiPrefix starts the paths of the API.
const apiPrefix = "/api/"

// Deps holds what the handler needs. It grows as routes are added.
type Deps struct {
	Logger         *slog.Logger
	Clock          clock.Clock
	TrustedProxies config.Prefixes
	// Store holds the sessions and the data of the API. Nil means no request is authenticated.
	Store store.Store
	// Version is the build metadata of the running binary, reported by /readyz and /api/v1/meta.
	Version version.Info
	// Checkers returns the readiness checks of /readyz, called on each request. Nil means none.
	Checkers func() []Checker
	// Gate, when not nil, wraps the router, behind the other middlewares. The server answers the
	// requests there while it starts.
	Gate func(http.Handler) http.Handler
	// SPA serves the web app. Nil means none.
	SPA http.Handler
	// HSTS adds Strict-Transport-Security to every response, for a public URL in https.
	HSTS bool
	// PublicURL names the session cookie and is the origin that the CSRF checks expect. The zero
	// value means unset.
	PublicURL config.PublicURL
	// Hasher has the parameters of the password hashes that logins write when they upgrade one, and
	// that the setup writes. The zero value means auth.DefaultHasher.
	Hasher auth.Hasher
	// SetupToken is the token that creates the first admin. Nil means none: the setup accepts no
	// token.
	SetupToken *auth.SetupToken
	// Jobs queues the jobs of the API, such as the deletion of an account, and wakes the worker. Nil
	// means the jobs are written to the store, where the worker finds them at its next poll.
	Jobs auth.JobQueue
}

// NewHandler returns the root handler of the server: the health probes, the API, a JSON 404
// problem for any unknown path under /api/, and the web app for every other path.
func NewHandler(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", readyz(d.Version.Version, d.Checkers))
	mux.HandleFunc(apiPrefix, apiNotFound)

	sessions := auth.NewSessions(d.Clock)
	hasher := cmp.Or(d.Hasher, auth.DefaultHasher())
	policy := auth.NewPasswordPolicy()
	logins := auth.NewLogins(sessions, hasher, auth.NewLoginLimiter(d.Clock))
	accounts := auth.NewAccounts(hasher, policy)
	passwords := auth.NewPasswords(logins, policy)
	NewAPI(mux, APIDeps{
		Version:        d.Version,
		Store:          d.Store,
		Logins:         logins,
		Sessions:       sessions,
		Passwords:      passwords,
		Deletions:      auth.NewDeletions(passwords, sessions, d.Jobs),
		Setup:          auth.NewSetup(d.SetupToken, accounts, sessions),
		Invitations:    auth.NewInvitations(d.Clock, accounts, sessions),
		PasswordResets: auth.NewPasswordResets(d.Clock, accounts, sessions),
		Admin:          auth.NewAdmin(accounts, sessions),
		PublicURL:      d.PublicURL,
	})

	if d.SPA != nil {
		mux.Handle("/", outsideAPI(d.SPA))
	}

	return newHandler(d, mux)
}

// apiNotFound answers the unknown paths under /api/, whatever the method.
func apiNotFound(w http.ResponseWriter, _ *http.Request) {
	WriteProblem(w, Problem{Status: http.StatusNotFound, Code: "not_found"})
}

// outsideAPI answers with apiNotFound the paths under /api/ that reach it, and passes the others
// to next. The router matches the escaped path, so it sends /api%2fv1/... to the catch-all.
func outsideAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, apiPrefix) {
			apiNotFound(w, r)

			return
		}

		next.ServeHTTP(w, r)
	})
}

// newHandler wraps routes with the middleware chain. Order, outermost first: recovery, client
// info, audit metadata, request logging, security headers, gate, CSRF, authentication, router.
// The gate comes before the CSRF checks and the authentication, whose session lookup needs a
// migrated database. Middlewares added later keep this order.
func newHandler(d Deps, routes http.Handler) http.Handler {
	middlewares := []middleware{
		RecoverMiddleware(d.Logger),
		ClientInfoMiddleware(d.TrustedProxies, d.Logger, d.Clock),
		AuditMetaMiddleware,
		RequestLogMiddleware(d.Logger, d.Clock),
		SecurityHeaders(DefaultCSP(), d.HSTS),
	}

	if d.Gate != nil {
		middlewares = append(middlewares, d.Gate)
	}

	middlewares = append(middlewares, CSRFMiddleware(d.PublicURL))

	if d.Store != nil {
		middlewares = append(middlewares, AuthenticationMiddleware(d.Store, auth.NewSessions(d.Clock), d.PublicURL))
	}

	return chain(routes, middlewares...)
}

// middleware wraps a handler.
type middleware func(http.Handler) http.Handler

// chain wraps h with middlewares, the first one being the outermost.
func chain(h http.Handler, middlewares ...middleware) http.Handler {
	for _, m := range slices.Backward(middlewares) {
		h = m(h)
	}

	return h
}
