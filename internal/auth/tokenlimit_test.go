package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
)

// newTestTokenLimiter returns a token limiter on a fake clock at limiterTestNow, and the clock.
func newTestTokenLimiter() (*TokenLimiter, *clock.Fake) {
	clk := clock.NewFake(limiterTestNow)

	return NewTokenLimiter(clk), clk
}

// allowTimes sends count requests with the token of tokenID to l, without waiting between them,
// and requires that each is accepted.
func allowTimes(t *testing.T, l *TokenLimiter, count int, tokenID int64) {
	t.Helper()

	for i := range count {
		require.Zero(t, l.Allow(tokenID), "request %d", i+1)
	}
}

// Given a token above its rate limit, then its next request waits: a burst of 120 requests is
// accepted at once, and the 121st waits 1 second. The other tokens are not limited.
func TestTokenLimiter_TokenAboveItsLimitWaits(t *testing.T) {
	t.Parallel()

	l, _ := newTestTokenLimiter()

	allowTimes(t, l, 120, 3)
	require.Equal(t, time.Second, l.Allow(3))

	allowTimes(t, l, 120, 4)
}

// Given a token past its burst, then it gets 60 requests per minute back, one per second, and a
// request that waits takes nothing: the wait shrinks as time passes, however often the client
// retries.
func TestTokenLimiter_TokenGets60RequestsPerMinute(t *testing.T) {
	t.Parallel()

	l, clk := newTestTokenLimiter()
	allowTimes(t, l, 120, 3)

	clk.Advance(400 * time.Millisecond)
	require.Equal(t, 600*time.Millisecond, l.Allow(3))
	require.Equal(t, 600*time.Millisecond, l.Allow(3))

	clk.Advance(600 * time.Millisecond)
	require.Zero(t, l.Allow(3))
	require.Equal(t, time.Second, l.Allow(3))

	clk.Advance(time.Minute)
	allowTimes(t, l, 60, 3)
	require.Equal(t, time.Second, l.Allow(3))
}

// Given tokens quiet for the 2 minutes that refill their burst, then the limiter forgets them, so
// that its entries do not pile up, and a forgotten token gets its whole burst again. A token whose
// burst is not refilled is kept.
func TestTokenLimiter_QuietTokensAreForgotten(t *testing.T) {
	t.Parallel()

	l, clk := newTestTokenLimiter()
	allowTimes(t, l, 120, 3)

	clk.Advance(time.Minute)
	allowTimes(t, l, 1, 4)
	require.Equal(t, 2, l.entries())

	clk.Advance(time.Minute)
	allowTimes(t, l, 1, 5)
	require.Equal(t, 2, l.entries())

	allowTimes(t, l, 120, 3)
	require.Equal(t, time.Second, l.Allow(3))
}
