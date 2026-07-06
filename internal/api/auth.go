package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/data"
)

// sessionCookieName is the cookie carrying the opaque session token.
const sessionCookieName = "verve_session"

// contextKey is an unexported type for request-context keys, so no other package
// can collide with ours.
type contextKey string

// accountIDKey holds the authenticated Account's id in a request context.
const accountIDKey contextKey = "accountID"

// withAccountID returns r carrying id as the authenticated Account.
func withAccountID(r *http.Request, id int64) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), accountIDKey, id))
}

// accountID returns the authenticated Account id on the request, and whether one
// is present. Handlers behind requireAuth can rely on ok being true.
func (s *Server) accountID(r *http.Request) (int64, bool) {
	id, ok := r.Context().Value(accountIDKey).(int64)
	return id, ok
}

// authResolver turns a request into an authenticated Account. It is the seam for
// ADR 0008's future forward-auth mode: v1 ships only sessionResolver (cookie →
// session → Account), but a resolver trusting a reverse-proxy identity header
// slots in here with no change to the middleware or handlers. ok is false for an
// anonymous request (no/*unknown credentials); err is reserved for genuine
// faults (e.g. the database being unreachable).
type authResolver interface {
	resolve(ctx context.Context, r *http.Request) (accountID int64, ok bool, err error)
}

// sessionResolver authenticates a request from its session cookie by looking the
// token's hash up in the sessions table.
type sessionResolver struct {
	sessions data.AuthSessionModel
}

func (sr sessionResolver) resolve(ctx context.Context, r *http.Request) (int64, bool, error) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return 0, false, nil // http.ErrNoCookie → anonymous, not a fault
	}
	id, err := sr.sessions.AccountIDByToken(ctx, auth.HashToken(c.Value))
	if errors.Is(err, data.ErrRecordNotFound) {
		return 0, false, nil // stale or forged cookie → anonymous
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// authenticate resolves the request's Account (if any) via the configured
// resolver and, when found, injects it into the request context. It never
// rejects — enforcement is requireAuth's job — so public endpoints stay reachable
// while protected ones can read the identity. Responses vary by cookie.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Cookie")
		id, ok, err := s.resolver.resolve(r.Context(), r)
		if err != nil {
			s.serverErrorResponse(w, r, err)
			return
		}
		if ok {
			r = withAccountID(r, id)
		}
		next.ServeHTTP(w, r)
	})
}

// requireAuth rejects an unauthenticated request with 401; otherwise it passes
// through to next. It assumes authenticate ran earlier in the chain.
func (s *Server) requireAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.accountID(r); !ok {
			s.authenticationRequiredResponse(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
