package config

import (
	"net/url"
	"strings"
)

// PublicURL is the external URL of the instance: an absolute http or https URL without
// a path, since hosting under a sub-path is not supported. The zero value means unset.
type PublicURL struct {
	u *url.URL
}

// URL returns a copy of the URL, or nil when unset.
func (p PublicURL) URL() *url.URL {
	if p.u == nil {
		return nil
	}

	u := *p.u

	return &u
}

// IsZero reports whether the public URL is unset.
func (p PublicURL) IsZero() bool {
	return p.u == nil
}

// String returns the URL without a trailing slash, or "" when unset.
func (p PublicURL) String() string {
	if p.u == nil {
		return ""
	}

	return p.u.String()
}

// MarshalText implements encoding.TextMarshaler.
func (p PublicURL) MarshalText() ([]byte, error) {
	return []byte(p.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (p *PublicURL) UnmarshalText(text []byte) error {
	s := string(text)

	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return invalidValue(s, "must be an absolute http or https URL, such as https://verve.example.com")
	}

	switch {
	case u.Host == "":
		return invalidValue(s, "missing host")
	case u.User != nil:
		return invalidValue(s, "user info is not allowed")
	case u.RawQuery != "" || u.ForceQuery:
		return invalidValue(s, "a query is not allowed")
	case u.Fragment != "":
		return invalidValue(s, "a fragment is not allowed")
	case strings.TrimSuffix(u.Path, "/") != "":
		return invalidValue(s, "hosting under a sub-path is not supported")
	}

	u.Path = ""
	u.RawPath = ""
	p.u = u

	return nil
}

// IsSet reports whether the public URL is set.
func (p PublicURL) IsSet() bool {
	return p.u != nil
}

// HTTPS reports whether the public URL uses https.
func (p PublicURL) HTTPS() bool {
	return p.u != nil && p.u.Scheme == "https"
}

// SecureCookies reports whether cookies must be marked Secure, which is when the public URL
// uses https.
func (p PublicURL) SecureCookies() bool {
	return p.HTTPS()
}

// Origin returns the origin of the public URL, such as https://verve.example.com, or "" when unset.
func (p PublicURL) Origin() string {
	if p.u == nil {
		return ""
	}

	return p.u.Scheme + "://" + p.u.Host
}
