package httpapi

import (
	"net"
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
	public := publicURL.URL()

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

			info := ClientInfoFrom(r.Context())

			scheme, host := info.Scheme, info.Host
			if public != nil {
				scheme, host = public.Scheme, public.Host
			}

			if !isOrigin(r.Header.Get("Origin"), scheme, host) {
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

// isOrigin reports whether the Origin header origin names scheme and host, a host with an optional
// port. Schemes and hosts compare ignoring case, and a default port matches its absence, since
// browsers send the origin in lowercase and without a default port.
func isOrigin(origin, scheme, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.User != nil {
		return false
	}

	return strings.EqualFold(u.Scheme, scheme) &&
		strings.EqualFold(withoutDefaultPort(u.Host, u.Scheme), withoutDefaultPort(host, scheme))
}

// withoutDefaultPort returns host without its port when the port is the default one of scheme.
func withoutDefaultPort(host, scheme string) string {
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}

	if (strings.EqualFold(scheme, "http") && port == "80") || (strings.EqualFold(scheme, "https") && port == "443") {
		// SplitHostPort removes the brackets of an IPv6 address.
		if strings.Contains(name, ":") {
			return "[" + name + "]"
		}

		return name
	}

	return host
}
