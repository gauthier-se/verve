package api

import (
	"fmt"
	"net/http"
)

// recoverPanic turns a panic in any handler into a logged 500 instead of a
// dropped connection. Setting Connection: close lets Go's server tear the
// connection down cleanly after the panic unwinds.
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				w.Header().Set("Connection", "close")
				s.serverErrorResponse(w, r, fmt.Errorf("panic: %v", err))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
