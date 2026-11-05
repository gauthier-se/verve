package auth

import (
	"net/netip"
	"sync"
	"time"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

const (
	// The numbers of failed logins after which the next attempts of a login, or of a client IP,
	// are delayed.
	loginFailureLimit = 5
	ipFailureLimit    = 20

	// The delay after the failure that reaches the limit, which doubles with each further failure
	// up to maxLoginDelay. There is no permanent lockout.
	firstLoginDelay = time.Second
	maxLoginDelay   = 15 * time.Minute

	// forgetFailuresAfter is how long the failures of a login or a client IP are kept after their
	// delay ends, or after the last of them below the limit, without a new failure. Waiting for it
	// costs an attacker more than the longest delay, and the counters it keeps stay few: each
	// failure takes a hashing slot.
	forgetFailuresAfter = time.Hour

	// sweepInterval is the shortest time between two removals of the forgotten failures.
	sweepInterval = time.Minute
)

// LoginLimiter delays the attempts of a login or a client IP with too many failed logins (FR-4).
// Its counters are in memory, so that a restart clears them, and they are forgotten once quiet:
// the logins an attacker tries do not pile up. It is safe for concurrent use.
type LoginLimiter struct {
	clk clock.Clock

	mu        sync.Mutex
	logins    failureCounters[string]
	ips       failureCounters[netip.Addr]
	nextSweep time.Time
}

// NewLoginLimiter returns a limiter without failures, which takes the current time from clk.
func NewLoginLimiter(clk clock.Clock) *LoginLimiter {
	return &LoginLimiter{
		clk:    clk,
		logins: failureCounters[string]{limit: loginFailureLimit, byKey: map[string]*failures{}},
		ips:    failureCounters[netip.Addr]{limit: ipFailureLimit, byKey: map[netip.Addr]*failures{}},
	}
}

// Wait returns how long the client at ip must wait before it tries to log in as login, or 0 when
// it may try now.
func (l *LoginLimiter) Wait(login string, ip netip.Addr) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.sweep()

	return max(l.logins.wait(store.FoldCase(login), now), l.ips.wait(ip, now))
}

// Fail records a failed login as login from ip. It counts whether or not a user has login, so that
// the delays do not reveal which logins exist.
func (l *LoginLimiter) Fail(login string, ip netip.Addr) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.sweep()
	l.logins.fail(store.FoldCase(login), now)
	l.ips.fail(ip, now)
}

// Reset forgets the failures of login, after it logged in. The failures of the client IP stay, so
// that logging in to one's own account between guesses does not clear them.
func (l *LoginLimiter) Reset(login string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.logins.byKey, store.FoldCase(login))
}

// sweep removes the forgotten failures, at most once per sweepInterval, and returns the current
// time. The caller holds mu.
func (l *LoginLimiter) sweep() time.Time {
	now := l.clk.Now()

	if !now.Before(l.nextSweep) {
		l.logins.sweep(now)
		l.ips.sweep(now)
		l.nextSweep = now.Add(sweepInterval)
	}

	return now
}

// counters returns the number of logins and client IPs that the limiter holds failures of.
func (l *LoginLimiter) counters() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.logins.byKey) + len(l.ips.byKey)
}

// failures counts the failed logins of a login or a client IP.
type failures struct {
	count int
	// until is when the next attempt is accepted: the time of the last failure plus its delay.
	until time.Time
}

// forgotten reports whether the failures are forgotten at now.
func (f *failures) forgotten(now time.Time) bool {
	return now.After(f.until.Add(forgetFailuresAfter))
}

// failureCounters holds the failures of the logins, or of the client IPs, delayed from limit
// failures on.
type failureCounters[K comparable] struct {
	limit int
	byKey map[K]*failures
}

func (c failureCounters[K]) wait(key K, now time.Time) time.Duration {
	f, ok := c.byKey[key]
	if !ok || f.forgotten(now) {
		return 0
	}

	return max(f.until.Sub(now), 0)
}

func (c failureCounters[K]) fail(key K, now time.Time) {
	f, ok := c.byKey[key]
	if !ok || f.forgotten(now) {
		f = &failures{}
		c.byKey[key] = f
	}

	f.count++
	f.until = now.Add(delayAfter(f.count, c.limit))
}

func (c failureCounters[K]) sweep(now time.Time) {
	for key, f := range c.byKey {
		if f.forgotten(now) {
			delete(c.byKey, key)
		}
	}
}

// delayAfter returns the delay after count failures, for a limit of failures: none below it,
// firstLoginDelay at it, then doubling up to maxLoginDelay.
func delayAfter(count, limit int) time.Duration {
	if count < limit {
		return 0
	}

	// Past this many doublings, the delay is above maxLoginDelay, and shifting further could
	// overflow.
	const maxDoublings = 10

	doublings := count - limit
	if doublings >= maxDoublings {
		return maxLoginDelay
	}

	return min(firstLoginDelay<<doublings, maxLoginDelay)
}
