package auth

import (
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/gauthier-se/verve/internal/clock"
)

const (
	// tokenRequestsPerMinute is the sustained rate of the requests of a token, and tokenBurst the
	// requests it may send at once: generous, so that a scheduled sync sending batches is not
	// limited (FR-9 of API tokens).
	tokenRequestsPerMinute = 60
	tokenBurst             = 120

	// tokenRefill is how long a token takes to get its whole burst back. A token quiet for that long
	// has a full burst, the state of a token never seen: forgetting it changes nothing.
	tokenRefill = tokenBurst * time.Minute / tokenRequestsPerMinute
)

// TokenLimiter limits the requests of each API token to tokenRequestsPerMinute, with bursts of
// tokenBurst (FR-9 of API tokens). Its entries are in memory, so that a restart clears them, and
// they are forgotten once their token is quiet long enough to have its burst back: the tokens seen
// do not pile up. It is safe for concurrent use.
type TokenLimiter struct {
	clk clock.Clock

	mu        sync.Mutex
	byToken   map[int64]*tokenRequests
	nextSweep time.Time
}

// tokenRequests is the rate limit state of a token.
type tokenRequests struct {
	limiter *rate.Limiter
	// last is the time of its last request.
	last time.Time
}

// NewTokenLimiter returns a limiter without entries, which takes the current time from clk.
func NewTokenLimiter(clk clock.Clock) *TokenLimiter {
	return &TokenLimiter{clk: clk, byToken: map[int64]*tokenRequests{}}
}

// Allow counts a request with the token of tokenID, and returns 0 when it may go on, or how long
// the client must wait before its next request. A request that must wait is not counted.
func (l *TokenLimiter) Allow(tokenID int64) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clk.Now()
	l.sweep(now)

	requests, ok := l.byToken[tokenID]
	if !ok {
		requests = &tokenRequests{limiter: rate.NewLimiter(rate.Every(time.Minute/tokenRequestsPerMinute), tokenBurst)}
		l.byToken[tokenID] = requests
	}

	requests.last = now

	reservation := requests.limiter.ReserveN(now, 1)

	wait := reservation.DelayFrom(now)
	if wait > 0 {
		reservation.CancelAt(now)
	}

	return wait
}

// sweep removes the entries of the tokens quiet for tokenRefill, at most once per sweepInterval.
// The caller holds mu.
func (l *TokenLimiter) sweep(now time.Time) {
	if now.Before(l.nextSweep) {
		return
	}

	for id, requests := range l.byToken {
		if now.Sub(requests.last) >= tokenRefill {
			delete(l.byToken, id)
		}
	}

	l.nextSweep = now.Add(sweepInterval)
}

// entries returns the number of tokens that the limiter holds the requests of.
func (l *TokenLimiter) entries() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.byToken)
}
