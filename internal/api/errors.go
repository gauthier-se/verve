package api

import "net/http"

// errorResponse is the single choke point for error payloads: every failure is
// a JSON object {"error": message} at the given status, so clients parse errors
// the same way everywhere. message is any so it can be a string or a field→msg
// map for validation failures.
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
