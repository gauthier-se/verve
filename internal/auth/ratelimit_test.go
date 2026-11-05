package auth

import (
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
)

var (
	// limiterTestNow is when the failures of the limiter tests start.
	limiterTestNow = time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)

	phoneIP  = netip.MustParseAddr("203.0.113.7")
	laptopIP = netip.MustParseAddr("198.51.100.20")
)

// newTestLimiter returns a limiter on a fake clock at limiterTestNow, and the clock.
func newTestLimiter() (*LoginLimiter, *clock.Fake) {
	clk := clock.NewFake(limiterTestNow)

	return NewLoginLimiter(clk), clk
}

// failTimes records count failures of login from ip, without waiting between them.
func failTimes(l *LoginLimiter, count int, login string, ip netip.Addr) {
	for range count {
		l.Fail(login, ip)
	}
}

// Given 4 failures for one login, then the next attempt is accepted. Given the 5th, then the next
// attempt waits 1 second, from any client IP. The login counts whether or not a user has it.
func TestLoginLimiter_FiveFailuresForALoginDelayItsNextAttempt(t *testing.T) {
	t.Parallel()

	l, _ := newTestLimiter()

	failTimes(l, 4, "alice", phoneIP)
	require.Zero(t, l.Wait("alice", phoneIP))

	l.Fail("alice", phoneIP)
	require.Equal(t, time.Second, l.Wait("alice", phoneIP))
	require.Equal(t, time.Second, l.Wait("alice", laptopIP))
	require.Zero(t, l.Wait("bob", laptopIP))
}

// Given failures for one login, each made as soon as the previous delay passed, then the delay
// doubles from 1 second and stays at 15 minutes.
func TestLoginLimiter_DelayDoublesUpTo15Minutes(t *testing.T) {
	t.Parallel()

	l, clk := newTestLimiter()
	failTimes(l, 4, "alice", phoneIP)

	want := []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second,
		32 * time.Second, 64 * time.Second, 128 * time.Second, 256 * time.Second, 512 * time.Second,
		15 * time.Minute, 15 * time.Minute, 15 * time.Minute,
	}

	for i, delay := range want {
		l.Fail("alice", phoneIP)
		require.Equal(t, delay, l.Wait("alice", phoneIP), "failure %d", i+5)

		clk.Advance(delay)
		require.Zero(t, l.Wait("alice", phoneIP), "failure %d", i+5)
	}
}

// Given 19 failures from one client IP, each for another login, then the next attempt is accepted.
// Given the 20th, then the next attempt from that IP waits 1 second, whatever the login, and the
// other IPs are not delayed.
func TestLoginLimiter_TwentyFailuresFromAnIPDelayItsNextAttempt(t *testing.T) {
	t.Parallel()

	l, _ := newTestLimiter()

	for i := range 19 {
		l.Fail(fmt.Sprintf("user%d", i), phoneIP)
	}

	require.Zero(t, l.Wait("zoe", phoneIP))

	l.Fail("yann", phoneIP)
	require.Equal(t, time.Second, l.Wait("zoe", phoneIP))
	require.Zero(t, l.Wait("zoe", laptopIP))
}

// Given a delayed login, then the wait shrinks as time passes, and an attempt is accepted once the
// delay has passed.
func TestLoginLimiter_AttemptsAreAcceptedOnceTheDelayHasPassed(t *testing.T) {
	t.Parallel()

	l, clk := newTestLimiter()
	failTimes(l, 6, "alice", phoneIP)
	require.Equal(t, 2*time.Second, l.Wait("alice", phoneIP))

	clk.Advance(1500 * time.Millisecond)
	require.Equal(t, 500*time.Millisecond, l.Wait("alice", phoneIP))

	clk.Advance(500 * time.Millisecond)
	require.Zero(t, l.Wait("alice", phoneIP))
}

// Given a delayed login, when it succeeds, then its counter starts over: 4 new failures do not
// delay it. The counter of the client IP stays, so that logging in to one's own account does not
// reset it.
func TestLoginLimiter_ResetStartsTheLoginOverButNotTheIP(t *testing.T) {
	t.Parallel()

	l, _ := newTestLimiter()
	failTimes(l, 5, "alice", phoneIP)
	failTimes(l, 18, "bob", laptopIP)
	l.Fail("alice", laptopIP)

	l.Reset("alice")

	failTimes(l, 4, "alice", phoneIP)
	require.Zero(t, l.Wait("alice", phoneIP))

	l.Fail("carol", laptopIP)
	require.Equal(t, time.Second, l.Wait("dave", laptopIP))
}

// Given logins that differ only by case, in any alphabet, then they share a counter, as they name
// the same user.
func TestLoginLimiter_LoginsDifferingOnlyByCaseShareACounter(t *testing.T) {
	t.Parallel()

	l, _ := newTestLimiter()

	for _, login := range []string{"élodie", "Élodie", "ÉLODIE", "élodiE", "ÉlOdIe"} {
		l.Fail(login, phoneIP)
	}

	require.Equal(t, time.Second, l.Wait("élodie", laptopIP))

	l.Reset("ÉLODIE")
	require.Zero(t, l.Wait("élodie", laptopIP))
}

// Given failures followed by an hour without failure after the end of their delay, then they are
// forgotten: the next failure counts from zero. Below the limit, the hour counts from the last
// failure.
func TestLoginLimiter_FailuresAreForgottenAnHourAfterTheirDelay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		failures int
		// quiet is how long the login stays without a failure after the last one.
		quiet time.Duration
		// want is the wait after one more failure.
		want time.Duration
	}{
		{name: "BelowTheLimitAfterAnHour", failures: 4, quiet: time.Hour, want: time.Second},
		{name: "BelowTheLimitAfterMore", failures: 4, quiet: time.Hour + time.Millisecond},
		{name: "DelayedAfterAnHourPastTheDelay", failures: 6, quiet: 2*time.Second + time.Hour, want: 4 * time.Second},
		{name: "DelayedAfterMore", failures: 6, quiet: 2*time.Second + time.Hour + time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			l, clk := newTestLimiter()
			failTimes(l, tt.failures, "alice", phoneIP)

			clk.Advance(tt.quiet)
			l.Fail("alice", phoneIP)

			require.Equal(t, tt.want, l.Wait("alice", phoneIP))
		})
	}
}

// Given counters forgotten for a minute, then the next call removes them from memory, so that the
// logins that an attacker tries do not pile up.
func TestLoginLimiter_ForgottenCountersAreRemovedFromMemory(t *testing.T) {
	t.Parallel()

	l, clk := newTestLimiter()

	for i := range 100 {
		l.Fail(fmt.Sprintf("user%d", i), netip.AddrFrom4([4]byte{192, 0, 2, byte(i)}))
	}

	clk.Advance(time.Hour)
	l.Fail("alice", phoneIP)

	clk.Advance(time.Minute)
	require.Zero(t, l.Wait("bob", laptopIP))
	require.Equal(t, 2, l.counters(), "the failure of alice, from the phone, is not forgotten yet")
}
