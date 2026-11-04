package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gauthier-se/verve/internal/config"
)

// csrfHeader is the header that the web app sends on every request other than GET. A cross-site
// form cannot set it, and a cross-site script cannot without a CORS preflight, which Verve never
// allows.
const csrfHeader = "Verve-Csrf"

// CSRFMiddleware rejects with 403 csrf_failed the POST, PUT, PATCH and DELETE requests under /api/
// that do not carry Verve-Csrf: 1, or whose Origin is not the origin of publicURL. Without a public
// URL, Origin must be the scheme and host of the request as resolved by ClientInfoMiddleware. The
// checks also cover unauthenticated requests, such as a login. A request with Authorization: Bearer
// is not checked: a cross-site request cannot carry the header, and AuthenticationMiddleware never
// authenticates it by its cookie.
//
// It must run after ClientInfoMiddleware.
func CSRFMiddleware(publicURL config.PublicURL) func(http.Handler) http.Handler {
	publicOrigin := publicURL.Origin()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !changesState(r.Method) || !strings.HasPrefix(r.URL.Path, apiPrefix) || hasBearer(r) {
				next.ServeHTTP(w, r)

				return
			}

			if r.Header.Get(csrfHeader) != "1" {
				writeCSRFFailed(w, "the request must carry the header "+csrfHeader+": 1")

				return
			}

			expected := publicOrigin
			if expected == "" {
				info := ClientInfoFrom(r.Context())
				expected = info.Scheme + "://" + info.Host
			}

			if !sameOrigin(r.Header.Get("Origin"), expected) {
				writeCSRFFailed(w, "the Origin of the request must be the origin of Verve")

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// changesState reports whether a request with method may change state.
func changesState(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}

	return false
}

func writeCSRFFailed(w http.ResponseWriter, detail string) {
	WriteProblem(w, Problem{Status: http.StatusForbidden, Code: "csrf_failed", Detail: detail})
}

// sameOrigin reports whether the Origin header origin names the origin expected, such as
// https://verve.example.com. They compare ignoring case, and a default port matches its absence,
// since browsers send the origin in lowercase and without a default port.
func sameOrigin(origin, expected string) bool {
	got, ok := normalizeOrigin(origin)
	if !ok {
		return false
	}

	want, ok := normalizeOrigin(expected)

	return ok && got == want
}

// normalizeOrigin returns the origin s, a scheme and a host with an optional port, in lowercase and
// without the default port of its scheme. It returns false when s is not an origin.
func normalizeOrigin(s string) (string, bool) {
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.Path != "" ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", false
	}

	scheme, host := strings.ToLower(u.Scheme), strings.ToLower(u.Host)

	switch scheme {
	case "http":
		host = strings.TrimSuffix(host, ":80")
	case "https":
		host = strings.TrimSuffix(host, ":443")
	}

	return scheme + "://" + host, true
}
