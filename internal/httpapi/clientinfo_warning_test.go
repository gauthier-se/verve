package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
)

const untrustedWarning = `level=WARN msg="ignoring X-Forwarded-* headers from an untrusted peer`

// Given requests carrying X-Forwarded-For from an untrusted peer, then a warning suggesting
// VERVE_TRUSTED_PROXIES is logged at most once per hour.
func TestClientInfoMiddleware_UntrustedForwardedWarningOncePerHour(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	clk := clock.NewFake(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	h := ClientInfoMiddleware(prefixes(t, "10.0.0.0/8"), slog.New(slog.NewTextHandler(&logs, nil)), clk)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	send := func(peer string, forwarded bool) {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		r.RemoteAddr = peer

		if forwarded {
			r.Header.Set("X-Forwarded-For", "1.2.3.4")
		}

		h.ServeHTTP(httptest.NewRecorder(), r)
	}

	count := func() int { return strings.Count(logs.String(), untrustedWarning) }

	send("203.0.113.5:4321", false)
	send("10.0.0.2:4321", true)
	require.Zero(t, count(), "no warning without forwarded headers or from a trusted peer")

	send("203.0.113.5:4321", true)
	require.Equal(t, 1, count())
	require.Contains(t, logs.String(), "VERVE_TRUSTED_PROXIES")

	clk.Advance(59 * time.Minute)
	send("203.0.113.5:4321", true)
	require.Equal(t, 1, count(), "the warning is not repeated within the hour")

	clk.Advance(time.Minute)
	send("203.0.113.5:4321", true)
	require.Equal(t, 2, count(), "the warning is logged again after an hour")
}
