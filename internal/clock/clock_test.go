package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Given a fake clock set to a fixed instant, when it is advanced by one hour,
// then Now returns the instant plus one hour.
func TestFake_Advance(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.October, 22, 10, 0, 0, 0, time.UTC)
	fake := NewFake(start)

	fake.Advance(time.Hour)

	require.Equal(t, start.Add(time.Hour), fake.Now())
}

// Given a fake clock, when it is set to another instant, then Now returns
// that instant.
func TestFake_Set(t *testing.T) {
	t.Parallel()

	fake := NewFake(time.Date(2026, time.October, 22, 10, 0, 0, 0, time.UTC))
	next := time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)

	fake.Set(next)

	require.Equal(t, next, fake.Now())
}

// Given a fake clock shared between goroutines, when they advance and read it
// concurrently, then the final time accounts for every advance (run with -race).
func TestFake_ConcurrentUse(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.October, 22, 10, 0, 0, 0, time.UTC)
	fake := NewFake(start)

	done := make(chan struct{})
	for range 10 {
		go func() {
			defer func() { done <- struct{}{} }()
			fake.Advance(time.Minute)
			_ = fake.Now()
		}()
	}
	for range 10 {
		<-done
	}

	require.Equal(t, start.Add(10*time.Minute), fake.Now())
}

// Given the real clock, when Now is called, then the time is in UTC and
// close to the system time.
func TestReal_ReturnsUTC(t *testing.T) {
	t.Parallel()

	now := Real().Now()

	require.Equal(t, time.UTC, now.Location())
	require.WithinDuration(t, time.Now(), now, time.Second)
}
