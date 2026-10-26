package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
)

// logCapture is a slog.Handler that records every log record. Attributes added through
// WithAttrs and groups are not needed by the tests and are dropped.
type logCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (*logCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *logCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.records = append(c.records, r.Clone())

	return nil
}

func (c *logCapture) WithAttrs([]slog.Attr) slog.Handler { return c }

func (c *logCapture) WithGroup(string) slog.Handler { return c }

// warnings returns the messages logged at warn level.
func (c *logCapture) warnings() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var messages []string

	for _, r := range c.records {
		if r.Level == slog.LevelWarn {
			messages = append(messages, r.Message)
		}
	}

	return messages
}

// Given requests carrying X-Forwarded-For from an untrusted peer, then a warning suggesting
// VERVE_TRUSTED_PROXIES is logged at most once per hour.
func TestClientInfoMiddleware_UntrustedForwardedWarningOncePerHour(t *testing.T) {
	t.Parallel()

	logs := &logCapture{}
	clk := clock.NewFake(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	h := ClientInfoMiddleware(prefixes(t, "10.0.0.0/8"), slog.New(logs), clk)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	send := func(peer string, forwarded bool) {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		r.RemoteAddr = peer

		if forwarded {
			r.Header.Set("X-Forwarded-For", "1.2.3.4")
		}

		h.ServeHTTP(httptest.NewRecorder(), r)
	}

	send("203.0.113.5:4321", false)
	send("10.0.0.2:4321", true)
	require.Empty(t, logs.warnings(), "no warning without forwarded headers or from a trusted peer")

	send("203.0.113.5:4321", true)
	require.Len(t, logs.warnings(), 1)
	require.Contains(t, logs.warnings()[0], "VERVE_TRUSTED_PROXIES")

	clk.Advance(59 * time.Minute)
	send("203.0.113.5:4321", true)
	require.Len(t, logs.warnings(), 1, "the warning is not repeated within the hour")

	clk.Advance(time.Minute)
	send("203.0.113.5:4321", true)
	require.Len(t, logs.warnings(), 2, "the warning is logged again after an hour")
}
