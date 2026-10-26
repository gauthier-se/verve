package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
)

// untrustedWarningInterval is the minimum time between two warnings about forwarded headers
// sent by an untrusted peer.
const untrustedWarningInterval = time.Hour

// Supported forwarded headers. The RFC 7239 Forwarded header is ignored.
const (
	headerForwardedFor   = "X-Forwarded-For"
	headerForwardedProto = "X-Forwarded-Proto"
	headerForwardedHost  = "X-Forwarded-Host"
)

// ClientInfo describes the client of a request, as resolved by ClientInfoMiddleware.
type ClientInfo struct {
	// IP is the client address, IPv4-mapped addresses being unmapped.
	IP netip.Addr
	// Scheme is "http" or "https".
	Scheme string
	// Host is the host requested by the client, with its port if any.
	Host string
	// ViaTrustedProxy reports whether the immediate peer is a trusted proxy, whose
	// X-Forwarded-* headers were honored.
	ViaTrustedProxy bool
}

type clientInfoKey struct{}

// ClientInfoFrom returns the client information stored by ClientInfoMiddleware, or the zero
// value when the middleware did not run.
func ClientInfoFrom(ctx context.Context) ClientInfo {
	info, _ := ctx.Value(clientInfoKey{}).(ClientInfo)

	return info
}

// ClientInfoMiddleware resolves the client IP, scheme and host of each request and stores them
// in its context. X-Forwarded-For, X-Forwarded-Proto and X-Forwarded-Host are honored only
// when the immediate peer belongs to trusted; the RFC 7239 Forwarded header is ignored. When
// an untrusted peer sends X-Forwarded-* headers, a warning is logged at most once per hour.
func ClientInfoMiddleware(trusted config.Prefixes, logger *slog.Logger, clk clock.Clock) func(http.Handler) http.Handler {
	warner := &untrustedWarner{logger: logger, clock: clk}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info := resolveClientInfo(r, trusted)
			if !info.ViaTrustedProxy && hasForwardedHeaders(r.Header) {
				warner.warn(info.IP)
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientInfoKey{}, info)))
		})
	}
}

func resolveClientInfo(r *http.Request, trusted config.Prefixes) ClientInfo {
	info := ClientInfo{Scheme: "http", Host: r.Host}
	if r.TLS != nil {
		info.Scheme = "https"
	}

	if addrPort, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
		info.IP = normalize(addrPort.Addr())
	}

	if !isTrusted(trusted, info.IP) {
		return info
	}

	info.ViaTrustedProxy = true

	if ip, ok := forwardedFor(r.Header, trusted); ok {
		info.IP = ip
	}

	switch proto := strings.ToLower(lastValue(r.Header, headerForwardedProto)); proto {
	case "http", "https":
		info.Scheme = proto
	}

	if host := lastValue(r.Header, headerForwardedHost); host != "" {
		info.Host = host
	}

	return info
}

// forwardedFor walks X-Forwarded-For from the right, skipping trusted hops, and returns the
// first untrusted address. When every entry is trusted it returns the leftmost one. It reports
// false when the header is absent or an invalid entry is met before an untrusted address.
func forwardedFor(h http.Header, trusted config.Prefixes) (netip.Addr, bool) {
	entries := headerList(h, headerForwardedFor)

	var ip netip.Addr

	for _, entry := range slices.Backward(entries) {
		addr, err := netip.ParseAddr(entry)
		if err != nil || addr.Zone() != "" {
			return netip.Addr{}, false
		}

		ip = normalize(addr)
		if !isTrusted(trusted, ip) {
			return ip, true
		}
	}

	return ip, ip.IsValid()
}

// headerList returns the comma-separated entries of every occurrence of the header, trimmed.
func headerList(h http.Header, name string) []string {
	var entries []string

	for _, value := range h.Values(name) {
		for entry := range strings.SplitSeq(value, ",") {
			entries = append(entries, strings.TrimSpace(entry))
		}
	}

	return entries
}

// lastValue returns the rightmost entry of the header, the one added by the closest proxy.
func lastValue(h http.Header, name string) string {
	entries := headerList(h, name)
	if len(entries) == 0 {
		return ""
	}

	return entries[len(entries)-1]
}

func hasForwardedHeaders(h http.Header) bool {
	for _, name := range []string{headerForwardedFor, headerForwardedProto, headerForwardedHost} {
		if len(h.Values(name)) > 0 {
			return true
		}
	}

	return false
}

func normalize(addr netip.Addr) netip.Addr {
	return addr.WithZone("").Unmap()
}

func isTrusted(trusted config.Prefixes, addr netip.Addr) bool {
	return slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(addr) })
}

// untrustedWarner logs the untrusted forwarded headers warning at most once per interval.
type untrustedWarner struct {
	logger *slog.Logger
	clock  clock.Clock

	mu   sync.Mutex
	last time.Time
}

func (u *untrustedWarner) warn(peer netip.Addr) {
	u.mu.Lock()

	now := u.clock.Now()
	due := u.last.IsZero() || now.Sub(u.last) >= untrustedWarningInterval

	if due {
		u.last = now
	}

	u.mu.Unlock()

	if due {
		u.logger.Warn("ignoring X-Forwarded-* headers from an untrusted peer: if Verve runs behind a reverse proxy, "+
			"set VERVE_TRUSTED_PROXIES to its address", slog.String("peer", peer.String()))
	}
}
