package httpapi

import (
	"net/http"

	"github.com/gauthier-se/verve/internal/audit"
)

// AuditMetaMiddleware stores the client of each request for the audit events recorded behind it.
// It must run after ClientInfoMiddleware.
func AuditMetaMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta := audit.Meta{IP: ClientInfoFrom(r.Context()).IP, UserAgent: r.UserAgent()}

		next.ServeHTTP(w, r.WithContext(audit.WithMeta(r.Context(), meta)))
	})
}
