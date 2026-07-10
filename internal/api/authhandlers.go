package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/data"
)

// accountView is the current Account as returned by login and /v1/auth/me: the
// identity plus the static `Me` profile, never the password hash.
type accountView struct {
	Email         string  `json:"email"`
	DateOfBirth   *string `json:"date_of_birth"`
	BiologicalSex *string `json:"biological_sex"`
	BloodType     *string `json:"blood_type"`
}

func meView(a *data.Account) accountView {
	return accountView{
		Email:         a.Email,
		DateOfBirth:   a.DateOfBirth,
		BiologicalSex: a.BiologicalSex,
		BloodType:     a.BloodType,
	}
}

// handleLogin verifies credentials and, on success, opens a session (server-side
// record + opaque cookie). Rate-limited per IP; a bad email or password returns the
// same 401 to avoid account enumeration.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimiter.allow(clientIP(r)) {
		s.rateLimitExceededResponse(w, r)
		return
	}

	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	v := NewValidator()
	v.Check(input.Email != "", "email", "must be provided")
	v.Check(input.Password != "", "password", "must be provided")
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	acc, err := s.models.Accounts.GetByEmail(r.Context(), input.Email)
	if err != nil && !errors.Is(err, data.ErrRecordNotFound) {
		s.serverErrorResponse(w, r, err)
		return
	}
	if !s.credentialsValid(acc, input.Password) {
		s.invalidCredentialsResponse(w, r)
		return
	}

	if err := s.openSession(w, r, acc); err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	if err := writeJSON(w, http.StatusOK, envelope{"account": meView(acc)}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// openSession mints a session token, records the server-side session, and sets
// the cookie — the shared tail of login and first-run bootstrap, both of which
// leave the caller authenticated.
func (s *Server) openSession(w http.ResponseWriter, r *http.Request, acc *data.Account) error {
	token, err := auth.NewSessionToken()
	if err != nil {
		return err
	}
	session := data.AuthSession{
		TokenHash: auth.HashToken(token),
		AccountID: acc.ID,
		ExpiresAt: time.Now().Add(s.sessionTTL),
	}
	if err := s.models.AuthSessions.Insert(r.Context(), session); err != nil {
		return err
	}
	s.setSessionCookie(w, token, session.ExpiresAt)
	return nil
}

// handleAuthState reports whether the instance still needs its first Account
// (ADR 0017). It is public and leaks exactly one boolean — "instance
// initialized" — never any account detail, so the SPA can pick between the
// create-account and login screens without enabling enumeration.
func (s *Server) handleAuthState(w http.ResponseWriter, r *http.Request) {
	exists, err := s.models.Accounts.Any(r.Context())
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	if err := writeJSON(w, http.StatusOK, envelope{"needs_bootstrap": !exists}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// handleRegister is the first-run bootstrap: while the instance has zero
// Accounts it creates the first one (email + password only), then closes — web
// signup is off once an Account exists (ADR 0017). Emptiness is re-checked
// server-side here, not merely in the SPA, so the closed door is real. On
// success it opens a session like handleLogin (auto-login). Rate-limited per IP
// by the shared login limiter.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimiter.allow(clientIP(r)) {
		s.rateLimitExceededResponse(w, r)
		return
	}

	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &input); err != nil {
		s.badRequestResponse(w, r, err)
		return
	}

	v := NewValidator()
	v.Check(input.Email != "", "email", "must be provided")
	v.Check(input.Password != "", "password", "must be provided")
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	// Server-enforced closure: bootstrap runs only on a virgin instance.
	exists, err := s.models.Accounts.Any(r.Context())
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	if exists {
		s.signupClosedResponse(w, r)
		return
	}

	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	acc := &data.Account{Email: input.Email, PasswordHash: &hash}
	if err := s.models.CreateAccount(r.Context(), acc); err != nil {
		// A racing bootstrap that landed the same email between the Any check and
		// this write means someone else already initialized the instance.
		if errors.Is(err, data.ErrDuplicateEmail) {
			s.signupClosedResponse(w, r)
			return
		}
		s.serverErrorResponse(w, r, err)
		return
	}

	if err := s.openSession(w, r, acc); err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	if err := writeJSON(w, http.StatusCreated, envelope{"account": meView(acc)}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// credentialsValid reports whether password authenticates acc. A missing account
// still runs a verify against a decoy hash, closing the timing side-channel.
func (s *Server) credentialsValid(acc *data.Account, password string) bool {
	if acc == nil || acc.PasswordHash == nil {
		_, _ = auth.VerifyPassword(password, s.decoyHash)
		return false
	}
	ok, err := auth.VerifyPassword(password, *acc.PasswordHash)
	if err != nil {
		s.logger.Error("verify password", "err", err, "account", acc.ID)
		return false
	}
	return ok
}

// handleLogout revokes the request's session server-side and clears the cookie.
// It is idempotent: logging out without (or with a stale) cookie still succeeds.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		if err := s.models.AuthSessions.Delete(r.Context(), auth.HashToken(c.Value)); err != nil {
			s.serverErrorResponse(w, r, err)
			return
		}
	}
	s.clearSessionCookie(w)
	if err := writeJSON(w, http.StatusOK, envelope{"status": "logged out"}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// handleMe returns the authenticated Account and its `Me` profile. It sits behind
// requireAuth, so an account id is always present.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	id, _ := s.accountID(r)
	acc, err := s.models.Accounts.GetByID(r.Context(), id)
	if err != nil {
		// A live session for a deleted account should be impossible (deleting an
		// account cascades its sessions), but treat it as unauthenticated rather
		// than a server error.
		if errors.Is(err, data.ErrRecordNotFound) {
			s.authenticationRequiredResponse(w, r)
			return
		}
		s.serverErrorResponse(w, r, err)
		return
	}
	if err := writeJSON(w, http.StatusOK, envelope{"account": meView(acc)}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// setSessionCookie writes the opaque session cookie: HttpOnly (no JS access),
// SameSite=Lax (sent on top-level navigation, not cross-site subrequests), and
// Secure in production. Its lifetime tracks the server-side session's expiry.
func (s *Server) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie expires the session cookie on the client (MaxAge<0).
func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}
