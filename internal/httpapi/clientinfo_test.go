package httpapi

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
)

// resolve sends r through ClientInfoMiddleware and returns what the handler sees.
func resolve(t *testing.T, trusted config.Prefixes, r *http.Request) ClientInfo {
	t.Helper()

	var got ClientInfo

	mw := ClientInfoMiddleware(trusted, slog.New(slog.DiscardHandler), clock.NewFake(time.Unix(0, 0)))
	mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = ClientInfoFrom(r.Context())
	})).ServeHTTP(httptest.NewRecorder(), r)

	return got
}

func prefixes(t *testing.T, values ...string) config.Prefixes {
	t.Helper()

	p := make(config.Prefixes, 0, len(values))
	for _, v := range values {
		p = append(p, netip.MustParsePrefix(v))
	}

	return p
}

func TestClientInfoMiddleware_ClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		trusted []string
		peer    string
		xff     []string
		want    string
		viaPrxy bool
	}{
		{
			name: "given no trusted proxies, then the peer address is used and X-Forwarded-For is ignored",
			peer: "203.0.113.5:4321",
			xff:  []string{"1.2.3.4"},
			want: "203.0.113.5",
		},
		{
			name:    "given an untrusted peer, then X-Forwarded-For is ignored",
			trusted: []string{"10.0.0.0/8"},
			peer:    "203.0.113.5:4321",
			xff:     []string{"1.2.3.4"},
			want:    "203.0.113.5",
		},
		{
			name:    "given a trusted peer, then the forwarded address is used",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.2:4321",
			xff:     []string{"198.51.100.7"},
			want:    "198.51.100.7",
			viaPrxy: true,
		},
		{
			name:    "given a trusted peer without X-Forwarded-For, then the peer address is used",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.2:4321",
			want:    "10.0.0.2",
			viaPrxy: true,
		},
		{
			name:    "given trusted hops on the right, then they are skipped",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.2:4321",
			xff:     []string{"198.51.100.7, 10.0.0.9"},
			want:    "198.51.100.7",
			viaPrxy: true,
		},
		{
			name:    "given spoofed entries on the left, then the rightmost untrusted address wins",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.2:4321",
			xff:     []string{"6.6.6.6, 198.51.100.7"},
			want:    "198.51.100.7",
			viaPrxy: true,
		},
		{
			name:    "given several X-Forwarded-For headers, then they form a single list",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.2:4321",
			xff:     []string{"6.6.6.6", "198.51.100.7, 10.0.0.9"},
			want:    "198.51.100.7",
			viaPrxy: true,
		},
		{
			name:    "given only trusted entries, then the leftmost one is used",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.2:4321",
			xff:     []string{"10.0.0.7, 10.0.0.9"},
			want:    "10.0.0.7",
			viaPrxy: true,
		},
		{
			name:    "given an unknown entry, then the peer address is used",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.2:4321",
			xff:     []string{"198.51.100.7, unknown"},
			want:    "10.0.0.2",
			viaPrxy: true,
		},
		{
			name:    "given an empty entry, then the peer address is used",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.2:4321",
			xff:     []string{"198.51.100.7, "},
			want:    "10.0.0.2",
			viaPrxy: true,
		},
		{
			name:    "given an out of range entry, then the peer address is used",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.2:4321",
			xff:     []string{"198.51.100.7, 999.1.1.1"},
			want:    "10.0.0.2",
			viaPrxy: true,
		},
		{
			name:    "given an invalid entry left of the client, then it is never reached",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.2:4321",
			xff:     []string{"unknown, 198.51.100.7"},
			want:    "198.51.100.7",
			viaPrxy: true,
		},
		{
			name:    "given a trusted IPv6 peer, then the forwarded IPv6 address is used",
			trusted: []string{"fd00::/8"},
			peer:    "[fd00::1]:1234",
			xff:     []string{"2001:db8::1"},
			want:    "2001:db8::1",
			viaPrxy: true,
		},
		{
			name:    "given an IPv4-mapped peer, then it matches an IPv4 prefix",
			trusted: []string{"10.0.0.0/8"},
			peer:    "[::ffff:10.0.0.2]:1234",
			xff:     []string{"198.51.100.7"},
			want:    "198.51.100.7",
			viaPrxy: true,
		},
		{
			name:    "given an IPv4-mapped forwarded entry, then it is normalized",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.2:4321",
			xff:     []string{"198.51.100.7, ::ffff:10.0.0.9"},
			want:    "198.51.100.7",
			viaPrxy: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			r.RemoteAddr = tt.peer

			for _, v := range tt.xff {
				r.Header.Add("X-Forwarded-For", v)
			}

			got := resolve(t, prefixes(t, tt.trusted...), r)

			require.Equal(t, netip.MustParseAddr(tt.want), got.IP)
			require.Equal(t, tt.viaPrxy, got.ViaTrustedProxy)
		})
	}
}

func TestClientInfoMiddleware_SchemeAndHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		peer       string
		tls        bool
		headers    map[string]string
		wantScheme string
		wantHost   string
	}{
		{
			name:       "given a trusted peer with forwarded proto and host, then they are used",
			peer:       "10.0.0.2:4321",
			headers:    map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "verve.example.com"},
			wantScheme: "https",
			wantHost:   "verve.example.com",
		},
		{
			name:       "given a trusted peer with several forwarded values, then the rightmost is used",
			peer:       "10.0.0.2:4321",
			headers:    map[string]string{"X-Forwarded-Proto": "http, https", "X-Forwarded-Host": "evil.example, verve.example.com"},
			wantScheme: "https",
			wantHost:   "verve.example.com",
		},
		{
			name:       "given a trusted peer with an unsupported proto, then the TLS state is used",
			peer:       "10.0.0.2:4321",
			headers:    map[string]string{"X-Forwarded-Proto": "gopher"},
			wantScheme: "http",
			wantHost:   "internal:8080",
		},
		{
			name:       "given a trusted peer with uppercase proto, then it is normalized",
			peer:       "10.0.0.2:4321",
			headers:    map[string]string{"X-Forwarded-Proto": "HTTPS"},
			wantScheme: "https",
			wantHost:   "internal:8080",
		},
		{
			name:       "given a trusted peer without forwarded headers, then the TLS state and Host are used",
			peer:       "10.0.0.2:4321",
			tls:        true,
			wantScheme: "https",
			wantHost:   "internal:8080",
		},
		{
			name:       "given an untrusted peer with forwarded headers, then they are ignored",
			peer:       "203.0.113.5:4321",
			headers:    map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "verve.example.com"},
			wantScheme: "http",
			wantHost:   "internal:8080",
		},
		{
			name:       "given an untrusted peer over TLS, then the scheme is https",
			peer:       "203.0.113.5:4321",
			tls:        true,
			headers:    map[string]string{"X-Forwarded-Proto": "http"},
			wantScheme: "https",
			wantHost:   "internal:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://internal:8080/", nil)
			r.RemoteAddr = tt.peer

			if tt.tls {
				r.TLS = &tls.ConnectionState{}
			}

			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}

			got := resolve(t, prefixes(t, "10.0.0.0/8"), r)

			require.Equal(t, tt.wantScheme, got.Scheme)
			require.Equal(t, tt.wantHost, got.Host)
		})
	}
}

// Given no middleware ran, then ClientInfoFrom returns the zero value.
func TestClientInfoFrom_WithoutMiddleware(t *testing.T) {
	t.Parallel()

	require.Equal(t, ClientInfo{}, ClientInfoFrom(t.Context()))
}
