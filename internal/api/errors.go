package api

import "net/http"

// errorResponse is the single choke point for error payloads: a JSON {"error":
// message} at the given status. message is a string or a field→msg map (422).
func (s *Server) errorResponse(w http.ResponseWriter, r *http.Request, status int, message any) {
	if err := writeJSON(w, status, envelope{"error": message}, nil); err != nil {
		// The response is already compromised; log and fall back to a bare 500.
		s.logger.Error("write error response", "err", err, "method", r.Method, "uri", r.URL.RequestURI())
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// serverErrorResponse logs the underlying error (never leaked to the client)
// and returns a generic 500.
func (s *Server) serverErrorResponse(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("server error", "err", err, "method", r.Method, "uri", r.URL.RequestURI())
	s.errorResponse(w, r, http.StatusInternalServerError, "the server encountered a problem and could not process your request")
}

// badRequestResponse returns a 400 with the error's message — used for input the
// server could not even parse.
func (s *Server) badRequestResponse(w http.ResponseWriter, r *http.Request, err error) {
	s.errorResponse(w, r, http.StatusBadRequest, err.Error())
}

// notFoundResponse returns a 404 with the given message.
func (s *Server) notFoundResponse(w http.ResponseWriter, r *http.Request, message string) {
	s.errorResponse(w, r, http.StatusNotFound, message)
}

// failedValidationResponse returns a 422 carrying the per-field validation
// errors, so a client can point at exactly which parameter was wrong.
func (s *Server) failedValidationResponse(w http.ResponseWriter, r *http.Request, errors map[string]string) {
	s.errorResponse(w, r, http.StatusUnprocessableEntity, errors)
}

// authenticationRequiredResponse returns a 401 for a request that must be
// authenticated but is not. The WWW-Authenticate header names the scheme so a
// client knows a session cookie is expected.
func (s *Server) authenticationRequiredResponse(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", "Cookie")
	s.errorResponse(w, r, http.StatusUnauthorized, "you must be authenticated to access this resource")
}

// invalidCredentialsResponse returns a 401 for a failed login. The message is
// deliberately generic — it never reveals whether the email exists — to avoid
// account enumeration.
func (s *Server) invalidCredentialsResponse(w http.ResponseWriter, r *http.Request) {
	s.errorResponse(w, r, http.StatusUnauthorized, "invalid email or password")
}

// rateLimitExceededResponse returns a 429 when a client has made too many
// login attempts too quickly.
func (s *Server) rateLimitExceededResponse(w http.ResponseWriter, r *http.Request) {
	s.errorResponse(w, r, http.StatusTooManyRequests, "too many requests — slow down and try again shortly")
}
