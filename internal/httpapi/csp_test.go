package httpapi

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// defaultCSPHeader is the Content-Security-Policy of the default policy.
const defaultCSPHeader = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; font-src 'self'; connect-src 'self'; worker-src 'self' blob:; " +
	"manifest-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

// Given the default policy, then it allows only same-origin resources, inline styles, and data
// and blob images.
func TestDefaultCSP_String(t *testing.T) {
	t.Parallel()

	require.Equal(t, defaultCSPHeader, DefaultCSP().String())
}

// Given the CSP builder with an extra source added to img-src and connect-src, then the header
// contains it once in each directive, and the directive order is deterministic.
func TestCSP_WithAddsSourcesOnceInOrder(t *testing.T) {
	t.Parallel()

	const tiles = "https://tiles.openfreemap.org"

	csp := DefaultCSP().
		With("img-src", tiles).
		With("connect-src", tiles).
		With("img-src", tiles).
		With("connect-src", "'self'")

	want := "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: blob: " + tiles + "; font-src 'self'; connect-src 'self' " + tiles + "; " +
		"worker-src 'self' blob:; manifest-src 'self'; object-src 'none'; base-uri 'self'; " +
		"form-action 'self'; frame-ancestors 'none'"

	for range 10 {
		require.Equal(t, want, csp.String())
	}
}

// Given a policy, when a source is added, then the original policy is unchanged.
func TestCSP_WithIsImmutable(t *testing.T) {
	t.Parallel()

	base := DefaultCSP()
	withTiles := base.With("img-src", "https://tiles.example.com")
	_ = base.With("img-src", "https://other.example.com")

	require.Equal(t, defaultCSPHeader, base.String())
	require.Contains(t, withTiles.String(), "img-src 'self' data: blob: https://tiles.example.com;")
	require.NotContains(t, withTiles.String(), "other.example.com")
}

// Given a source for a directive the policy does not have, then the directive is added after
// the existing ones.
func TestCSP_WithNewDirectiveAppendsIt(t *testing.T) {
	t.Parallel()

	csp := DefaultCSP().With("media-src", "'self'")

	require.Equal(t, defaultCSPHeader+"; media-src 'self'", csp.String())
}
