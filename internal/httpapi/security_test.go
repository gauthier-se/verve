package httpapi

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireSecurityHeaders checks the security headers of every response, except HSTS.
func requireSecurityHeaders(t *testing.T, h http.Header, msgAndArgs ...any) {
	t.Helper()

	require.Equal(t, defaultCSPHeader, h.Get("Content-Security-Policy"), msgAndArgs...)
	require.Equal(t, "nosniff", h.Get("X-Content-Type-Options"), msgAndArgs...)
	require.Equal(t, "no-referrer", h.Get("Referrer-Policy"), msgAndArgs...)
	require.Equal(t, "camera=(), microphone=(), geolocation=(), payment=(), usb=()",
		h.Get("Permissions-Policy"), msgAndArgs...)
	require.Equal(t, "same-origin", h.Get("Cross-Origin-Opener-Policy"), msgAndArgs...)
	require.Equal(t, "same-origin", h.Get("Cross-Origin-Resource-Policy"), msgAndArgs...)
}

// okHandler answers 200 with a body.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	_, _ = io.WriteString(w, "ok")
})

// Given any response, then it includes the security headers.
func TestSecurityHeaders_SetsHeaders(t *testing.T) {
	t.Parallel()

	rec := serve(t, SecurityHeaders(DefaultCSP(), false)(okHandler), http.MethodGet, "/")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ok", rec.Body.String())
	requireSecurityHeaders(t, rec.Header())
}

// Given HSTS enabled, then responses include Strict-Transport-Security; given it disabled, then
// they do not.
func TestSecurityHeaders_HSTS(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		hsts bool
		want []string
	}{
		{name: "given https, then HSTS is sent", hsts: true, want: []string{"max-age=31536000"}},
		{name: "given http or unset, then no HSTS", hsts: false, want: nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serve(t, SecurityHeaders(DefaultCSP(), tt.hsts)(okHandler), http.MethodGet, "/")

			require.Equal(t, tt.want, rec.Header().Values("Strict-Transport-Security"))
		})
	}
}

// Given the policy builder with extra sources, then the header carries the built policy.
func TestSecurityHeaders_UsesGivenCSP(t *testing.T) {
	t.Parallel()

	csp := DefaultCSP().With("img-src", "https://tiles.example.com")
	rec := serve(t, SecurityHeaders(csp, false)(okHandler), http.MethodGet, "/")

	require.Equal(t, csp.String(), rec.Header().Get("Content-Security-Policy"))
}

// Given a handler that already set Content-Security-Policy, then the middleware does not
// override it.
func TestSecurityHeaders_KeepsHandlerCSP(t *testing.T) {
	t.Parallel()

	const own = "default-src 'none'"

	h := SecurityHeaders(DefaultCSP(), false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Security-Policy", own)
		_, _ = io.WriteString(w, "ok")
	}))

	rec := serve(t, h, http.MethodGet, "/")

	require.Equal(t, []string{own}, rec.Header().Values("Content-Security-Policy"))
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
}

// Given a middleware in front that already set a header, then the middleware keeps its value.
func TestSecurityHeaders_KeepsHeaderSetBefore(t *testing.T) {
	t.Parallel()

	inner := SecurityHeaders(DefaultCSP(), false)(okHandler)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "same-origin")
		inner.ServeHTTP(w, r)
	})

	rec := serve(t, h, http.MethodGet, "/")

	require.Equal(t, []string{"same-origin"}, rec.Header().Values("Referrer-Policy"))
}
