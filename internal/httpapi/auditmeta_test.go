package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/clock"
)

// Given a request through the middleware chain, then the context of the handler carries the audit
// metadata: the client IP resolved through the trusted proxy, and the user agent truncated to 256
// characters.
func TestNewHandler_SetsTheAuditMetaOfTheRequest(t *testing.T) {
	t.Parallel()

	var got audit.Meta

	h := newHandler(Deps{
		Logger:         slog.New(slog.DiscardHandler),
		Clock:          clock.Real(),
		TrustedProxies: prefixes(t, "10.0.0.0/8"),
	}, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = audit.MetaFrom(r.Context())
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	req.Header.Set("User-Agent", strings.Repeat("a", 300))

	h.ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, audit.Meta{
		IP:        netip.MustParseAddr("203.0.113.7"),
		UserAgent: strings.Repeat("a", 256),
	}, got)
}
