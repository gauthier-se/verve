package httpapi

import (
	"slices"
	"strings"
)

// CSP is a Content Security Policy: directives with their sources, kept in order so that the
// header is the same on every response. The zero value is an empty policy. It is immutable: With
// returns a new policy.
type CSP struct {
	directives []cspDirective
}

// cspDirective is a directive of a CSP and its sources.
type cspDirective struct {
	name    string
	sources []string
}

// DefaultCSP returns the policy of Verve: same-origin resources only, with inline styles because
// UI libraries set them for positioning, and data and blob images and blob workers. Scripts
// never need inline code.
func DefaultCSP() CSP {
	return CSP{directives: []cspDirective{
		{name: "default-src", sources: []string{"'self'"}},
		{name: "script-src", sources: []string{"'self'"}},
		{name: "style-src", sources: []string{"'self'", "'unsafe-inline'"}},
		{name: "img-src", sources: []string{"'self'", "data:", "blob:"}},
		{name: "font-src", sources: []string{"'self'"}},
		{name: "connect-src", sources: []string{"'self'"}},
		{name: "worker-src", sources: []string{"'self'", "blob:"}},
		{name: "manifest-src", sources: []string{"'self'"}},
		{name: "object-src", sources: []string{"'none'"}},
		{name: "base-uri", sources: []string{"'self'"}},
		{name: "form-action", sources: []string{"'self'"}},
		{name: "frame-ancestors", sources: []string{"'none'"}},
	}}
}

// With returns a copy of the policy with source added to directive, after its existing sources.
// A source the directive already has is not added again. A directive the policy does not have
// is added after the existing ones.
func (c CSP) With(directive, source string) CSP {
	directives := make([]cspDirective, len(c.directives))
	for i, d := range c.directives {
		directives[i] = cspDirective{name: d.name, sources: slices.Clone(d.sources)}
	}

	i := slices.IndexFunc(directives, func(d cspDirective) bool { return d.name == directive })
	if i < 0 {
		return CSP{directives: append(directives, cspDirective{name: directive, sources: []string{source}})}
	}

	if !slices.Contains(directives[i].sources, source) {
		directives[i].sources = append(directives[i].sources, source)
	}

	return CSP{directives: directives}
}

// String returns the value of the Content-Security-Policy header.
func (c CSP) String() string {
	parts := make([]string, len(c.directives))
	for i, d := range c.directives {
		parts[i] = d.name + " " + strings.Join(d.sources, " ")
	}

	return strings.Join(parts, "; ")
}
