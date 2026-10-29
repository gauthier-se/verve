package httpapi

import "net/http"

// hstsValue is the Strict-Transport-Security header: one year, without includeSubDomains or
// preload, which would bind other hosts of the domain.
const hstsValue = "max-age=31536000"

// header is a response header and its value.
type header struct {
	name, value string
}

// SecurityHeaders sets the security headers on every response: the Content Security Policy csp,
// X-Content-Type-Options, Referrer-Policy, Permissions-Policy, Cross-Origin-Opener-Policy,
// Cross-Origin-Resource-Policy, and Strict-Transport-Security when hsts is true.
//
// The headers are set before the next handler runs, so that they are also on the responses the
// middlewares in front write in its place, such as the 500 of a panic. Headers set earlier are
// kept. The next handler overrides one with Header().Set; Header().Add would send both values.
func SecurityHeaders(csp CSP, hsts bool) func(http.Handler) http.Handler {
	headers := []header{
		{"Content-Security-Policy", csp.String()},
		{"X-Content-Type-Options", "nosniff"},
		{"Referrer-Policy", "no-referrer"},
		{"Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()"},
		{"Cross-Origin-Opener-Policy", "same-origin"},
		{"Cross-Origin-Resource-Policy", "same-origin"},
	}
	if hsts {
		headers = append(headers, header{"Strict-Transport-Security", hstsValue})
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			for _, hd := range headers {
				if h.Get(hd.name) == "" {
					h.Set(hd.name, hd.value)
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
