package audit

import (
	"context"
	"net/netip"
)

// maxUserAgent is the number of characters kept of a user agent, which the client chooses.
const maxUserAgent = 256

// Meta describes the client of a request. Jobs and CLI commands have none.
type Meta struct {
	IP        netip.Addr
	UserAgent string
}

type metaKey struct{}

// WithMeta returns a copy of ctx carrying meta, with its user agent truncated.
func WithMeta(ctx context.Context, meta Meta) context.Context {
	meta.UserAgent = truncate(meta.UserAgent, maxUserAgent)

	return context.WithValue(ctx, metaKey{}, meta)
}

// MetaFrom returns the meta stored by WithMeta, or the zero value outside requests.
func MetaFrom(ctx context.Context) Meta {
	meta, _ := ctx.Value(metaKey{}).(Meta)

	return meta
}

// truncate returns the first limit characters of s.
func truncate(s string, limit int) string {
	count := 0

	for i := range s {
		if count == limit {
			return s[:i]
		}

		count++
	}

	return s
}
