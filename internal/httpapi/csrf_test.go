package httpapi

import (
	"cmp"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
)

// Given a request under /api/, then a POST, PUT, PATCH or DELETE without Verve-Csrf: 1, or whose
// Origin differs from the public URL, or from the scheme and host of the request without a public
// URL, answers 403 csrf_failed, authenticated or not. A request with Authorization: Bearer and the
// other methods are not checked.
func TestCSRF_ChecksStateChangingAPIRequests(t *testing.T) {
	t.Parallel()

	const (
		publicURL = "https://verve.example.com"
		target    = "http://127.0.0.1:8080/api/v1/things"
		origin    = "https://verve.example.com"
	)

	// valid are the headers of a state-changing request that passes the checks.
	valid := map[string]string{"Verve-Csrf": "1", "Origin": origin}

	tests := []struct {
		name string
		// publicURL replaces the public URL of the test when not empty, and noPublicURL unsets it.
		publicURL   string
		noPublicURL bool
		trusted     []string
		remoteAddr  string
		method      string
		target      string
		headers     map[string]string
		want        int
	}{
		{name: "POST with the header and the origin", method: http.MethodPost, headers: valid, want: http.StatusOK},
		{name: "PUT with the header and the origin", method: http.MethodPut, headers: valid, want: http.StatusOK},
		{name: "PATCH with the header and the origin", method: http.MethodPatch, headers: valid, want: http.StatusOK},
		{name: "DELETE with the header and the origin", method: http.MethodDelete, headers: valid, want: http.StatusOK},

		{name: "POST without the header", method: http.MethodPost, headers: map[string]string{"Origin": origin}, want: http.StatusForbidden},
		{name: "PUT without the header", method: http.MethodPut, headers: map[string]string{"Origin": origin}, want: http.StatusForbidden},
		{name: "PATCH without the header", method: http.MethodPatch, headers: map[string]string{"Origin": origin}, want: http.StatusForbidden},
		{name: "DELETE without the header", method: http.MethodDelete, headers: map[string]string{"Origin": origin}, want: http.StatusForbidden},
		{
			name:    "POST with the header set to 0",
			method:  http.MethodPost,
			headers: map[string]string{"Verve-Csrf": "0", "Origin": origin},
			want:    http.StatusForbidden,
		},
		{
			name:    "POST with the header set to true",
			method:  http.MethodPost,
			headers: map[string]string{"Verve-Csrf": "true", "Origin": origin},
			want:    http.StatusForbidden,
		},
		{
			name:    "unauthenticated login without the header",
			method:  http.MethodPost,
			target:  "http://127.0.0.1:8080/api/v1/auth/login",
			headers: map[string]string{"Origin": origin},
			want:    http.StatusForbidden,
		},

		{name: "POST without an origin", method: http.MethodPost, headers: map[string]string{"Verve-Csrf": "1"}, want: http.StatusForbidden},
		{
			name:    "POST from another site",
			method:  http.MethodPost,
			headers: map[string]string{"Verve-Csrf": "1", "Origin": "https://evil.example"},
			want:    http.StatusForbidden,
		},
		{
			name:    "POST from the host over http",
			method:  http.MethodPost,
			headers: map[string]string{"Verve-Csrf": "1", "Origin": "http://verve.example.com"},
			want:    http.StatusForbidden,
		},
		{
			name:    "POST from another port of the host",
			method:  http.MethodPost,
			headers: map[string]string{"Verve-Csrf": "1", "Origin": "https://verve.example.com:8443"},
			want:    http.StatusForbidden,
		},
		{
			name:    "POST from a subdomain",
			method:  http.MethodPost,
			headers: map[string]string{"Verve-Csrf": "1", "Origin": "https://app.verve.example.com"},
			want:    http.StatusForbidden,
		},
		{
			name:    "POST from an opaque origin",
			method:  http.MethodPost,
			headers: map[string]string{"Verve-Csrf": "1", "Origin": "null"},
			want:    http.StatusForbidden,
		},
		{
			name:    "POST from an origin with a path",
			method:  http.MethodPost,
			headers: map[string]string{"Verve-Csrf": "1", "Origin": "https://verve.example.com/"},
			want:    http.StatusForbidden,
		},
		{
			name:      "POST to a public URL with capitals",
			publicURL: "https://Verve.Example.com",
			method:    http.MethodPost,
			headers:   valid,
			want:      http.StatusOK,
		},
		{
			name:      "POST to a public URL with the default port",
			publicURL: "https://verve.example.com:443",
			method:    http.MethodPost,
			headers:   valid,
			want:      http.StatusOK,
		},
		{
			name:      "POST to a public URL with another port",
			publicURL: "http://192.168.1.10:8080",
			method:    http.MethodPost,
			headers:   map[string]string{"Verve-Csrf": "1", "Origin": "http://192.168.1.10:8080"},
			want:      http.StatusOK,
		},

		{
			name:        "no public URL, POST from the requested host",
			noPublicURL: true,
			method:      http.MethodPost,
			target:      "http://192.168.1.10:8080/api/v1/things",
			headers:     map[string]string{"Verve-Csrf": "1", "Origin": "http://192.168.1.10:8080"},
			want:        http.StatusOK,
		},
		{
			name:        "no public URL, POST to the default port of the requested host",
			noPublicURL: true,
			method:      http.MethodPost,
			target:      "http://verve.lan:80/api/v1/things",
			headers:     map[string]string{"Verve-Csrf": "1", "Origin": "http://verve.lan"},
			want:        http.StatusOK,
		},
		{
			name:        "no public URL, POST from the requested IPv6 host",
			noPublicURL: true,
			method:      http.MethodPost,
			target:      "http://[fd00::10]:8080/api/v1/things",
			headers:     map[string]string{"Verve-Csrf": "1", "Origin": "http://[fd00::10]:8080"},
			want:        http.StatusOK,
		},
		{
			name:        "no public URL, POST from another host",
			noPublicURL: true,
			method:      http.MethodPost,
			target:      "http://192.168.1.10:8080/api/v1/things",
			headers:     map[string]string{"Verve-Csrf": "1", "Origin": "http://192.168.1.11:8080"},
			want:        http.StatusForbidden,
		},
		{
			name:        "no public URL, POST from the requested host over https",
			noPublicURL: true,
			method:      http.MethodPost,
			target:      "http://192.168.1.10:8080/api/v1/things",
			headers:     map[string]string{"Verve-Csrf": "1", "Origin": "https://192.168.1.10:8080"},
			want:        http.StatusForbidden,
		},
		{
			name:        "no public URL, POST through a trusted proxy",
			noPublicURL: true,
			trusted:     []string{"10.0.0.0/8"},
			remoteAddr:  "10.0.0.2:4321",
			method:      http.MethodPost,
			target:      "http://127.0.0.1:8080/api/v1/things",
			headers: map[string]string{
				"Verve-Csrf": "1", "Origin": "https://verve.lan",
				"X-Forwarded-Proto": "https", "X-Forwarded-Host": "verve.lan",
			},
			want: http.StatusOK,
		},
		{
			name:        "no public URL, POST with forwarded headers of an untrusted peer",
			noPublicURL: true,
			remoteAddr:  "10.0.0.2:4321",
			method:      http.MethodPost,
			target:      "http://127.0.0.1:8080/api/v1/things",
			headers: map[string]string{
				"Verve-Csrf": "1", "Origin": "https://verve.lan",
				"X-Forwarded-Proto": "https", "X-Forwarded-Host": "verve.lan",
			},
			want: http.StatusForbidden,
		},

		{
			name:    "POST with a Bearer token",
			method:  http.MethodPost,
			headers: map[string]string{"Authorization": "Bearer vrv_token"},
			want:    http.StatusOK,
		},
		{
			name:    "DELETE with a Bearer token in lowercase",
			method:  http.MethodDelete,
			headers: map[string]string{"Authorization": "bearer vrv_token"},
			want:    http.StatusOK,
		},
		{
			name:    "POST with Basic credentials",
			method:  http.MethodPost,
			headers: map[string]string{"Authorization": "Basic dXNlcjpwYXNz"},
			want:    http.StatusForbidden,
		},

		{name: "GET without the header and the origin", method: http.MethodGet, want: http.StatusOK},
		{name: "HEAD without the header and the origin", method: http.MethodHead, want: http.StatusOK},
		{name: "OPTIONS without the header and the origin", method: http.MethodOptions, want: http.StatusOK},
		{
			name:    "GET from another site",
			method:  http.MethodGet,
			headers: map[string]string{"Origin": "https://evil.example"},
			want:    http.StatusOK,
		},
		{name: "POST outside /api/", method: http.MethodPost, target: "http://127.0.0.1:8080/some/page", want: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			public := cmp.Or(tt.publicURL, publicURL)
			if tt.noPublicURL {
				public = ""
			}

			h := newHandler(Deps{
				Logger:         slog.New(slog.DiscardHandler),
				Clock:          clock.Real(),
				TrustedProxies: prefixes(t, tt.trusted...),
				PublicURL:      mustPublicURL(t, public),
			}, okHandler)

			r := httptest.NewRequestWithContext(t.Context(), tt.method, cmp.Or(tt.target, target), nil)
			if tt.remoteAddr != "" {
				r.RemoteAddr = tt.remoteAddr
			}

			for name, value := range tt.headers {
				r.Header.Set(name, value)
			}

			rec := serveWith(h, r)

			require.Equal(t, tt.want, rec.Code)

			if tt.want == http.StatusForbidden {
				requireCSRFFailed(t, rec)
			}
		})
	}
}

// appHeaders returns the headers that the web app sends with a POST, PUT, PATCH or DELETE to host,
// which pass the CSRF checks without a public URL.
func appHeaders(host string) http.Header {
	return http.Header{"Verve-Csrf": {"1"}, "Origin": {"http://" + host}}
}

// serveFromApp serves method target on h as the web app sends it, with appHeaders, and returns the
// response.
func serveFromApp(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	r.Header = appHeaders(r.Host)

	return serveWith(h, r)
}

// requireCSRFFailed checks that rec is a 403 csrf_failed problem.
func requireCSRFFailed(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))

	var p Problem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
	require.Equal(t, "csrf_failed", p.Code)
	require.Equal(t, "Forbidden", p.Title)
	require.Equal(t, http.StatusForbidden, p.Status)
}

// Given a GET of the API, of the OpenAPI document, of the health probes or of the web app, then
// nothing changes, even from another site. Given a POST outside /api/, then the router still
// answers it.
func TestCSRF_LeavesOtherRequestsUnchanged(t *testing.T) {
	t.Parallel()

	h := NewHandler(Deps{
		Logger:    slog.New(slog.DiscardHandler),
		Clock:     clock.Real(),
		PublicURL: mustPublicURL(t, "https://verve.example.com"),
		SPA:       newTestSPA(t, testDist()),
	})

	for _, tt := range []struct {
		method, target string
		want           int
	}{
		{method: http.MethodGet, target: "/api/v1/meta", want: http.StatusOK},
		{method: http.MethodGet, target: "/api/openapi.json", want: http.StatusOK},
		{method: http.MethodGet, target: "/api/openapi.yaml", want: http.StatusOK},
		{method: http.MethodGet, target: "/healthz", want: http.StatusOK},
		{method: http.MethodHead, target: "/healthz", want: http.StatusOK},
		{method: http.MethodGet, target: "/readyz", want: http.StatusOK},
		{method: http.MethodGet, target: "/", want: http.StatusOK},
		{method: http.MethodGet, target: "/workouts/123", want: http.StatusOK},
		{method: http.MethodPost, target: "/healthz", want: http.StatusMethodNotAllowed},
		{method: http.MethodPost, target: "/some/page", want: http.StatusMethodNotAllowed},
	} {
		r := httptest.NewRequestWithContext(t.Context(), tt.method, tt.target, nil)
		r.Header.Set("Origin", "https://evil.example")

		rec := serveWith(h, r)

		require.Equal(t, tt.want, rec.Code, tt.method+" "+tt.target)
	}
}
