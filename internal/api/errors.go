package api

import (
	"errors"
	"net/http"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

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

// forbiddenResponse returns a 403 for a request the Account is authenticated for but
// not allowed to make. It is deliberately distinct from a 404: the resource exists and
// is the Account's own, the *operation* is refused — which is exactly the shape of
// deleting an imported Measurement (ADR 0022), where saying "not found" would be a lie
// the client could not act on.
func (s *Server) forbiddenResponse(w http.ResponseWriter, r *http.Request, message string) {
	s.errorResponse(w, r, http.StatusForbidden, message)
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

// signupClosedResponse returns a 409 when the first-run bootstrap is attempted
// on an instance that already has an Account (ADR 0017): web signup is closed
// once initialized. The message reveals nothing beyond that single fact.
func (s *Server) signupClosedResponse(w http.ResponseWriter, r *http.Request) {
	s.errorResponse(w, r, http.StatusConflict, "signup is closed — this instance is already initialized")
}

// rateLimitExceededResponse returns a 429 when a client has made too many
// login attempts too quickly.
func (s *Server) rateLimitExceededResponse(w http.ResponseWriter, r *http.Request) {
	s.errorResponse(w, r, http.StatusTooManyRequests, "too many requests — slow down and try again shortly")
}

// respondRecordError maps a storage-layer record error to a 404 and anything else
// to a 500: the shared tail of every by-id handler. noun names the resource.
//
// Not every ErrRecordNotFound is a 404, which is why this is a helper rather than
// a rule applied everywhere. A session cookie pointing at an account that no
// longer exists is an authentication failure (handleMe), and a Phase or a profile
// that is simply absent is often not an error at all. Those read the sentinel
// themselves, deliberately.
func (s *Server) respondRecordError(w http.ResponseWriter, r *http.Request, err error, noun string) {
	if errors.Is(err, data.ErrRecordNotFound) {
		s.notFoundResponse(w, r, "the requested "+noun+" could not be found")
		return
	}
	s.serverErrorResponse(w, r, err)
}

// respondSeriesError maps read-engine errors to HTTP responses. The input errors
// are semantic (422) rather than parse failures; genuine faults are 500, and an
// aggregation the engine does not serve yet is a 501 because the request was
// well-formed and Verve simply cannot answer it.
func (s *Server) respondSeriesError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, query.ErrUnknownMetric):
		s.failedValidationResponse(w, r, map[string]string{"metric": unknownMetricMsg})
	case errors.Is(err, query.ErrInvalidRange):
		s.failedValidationResponse(w, r, map[string]string{"range_preset": "the range is empty or inverted"})
	case errors.Is(err, query.ErrRangeTooLarge):
		s.failedValidationResponse(w, r, map[string]string{"bucket": "too many buckets for this range; use a coarser bucket"})
	case errors.Is(err, query.ErrUnsupportedAggregation):
		s.errorResponse(w, r, http.StatusNotImplemented, "this metric's aggregation is not served yet")
	default:
		s.serverErrorResponse(w, r, err)
	}
}

// respond writes a JSON success payload, and answers 500 if the write itself
// fails. It is the success twin of errorResponse: a handler's last act in one
// line rather than the same three at every endpoint.
func (s *Server) respond(w http.ResponseWriter, r *http.Request, status int, body envelope) {
	if err := writeJSON(w, status, body, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}
