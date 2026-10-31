package jobs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Given the attempts of a job, then the delay starts at 30 seconds, doubles per attempt and is
// capped at 1 hour.
func TestBackoff_StartsAt30SecondsDoublesAndCapsAtOneHour(t *testing.T) {
	t.Parallel()

	tests := []struct {
		attempts int64
		want     time.Duration
	}{
		{attempts: 1, want: 30 * time.Second},
		{attempts: 2, want: time.Minute},
		{attempts: 3, want: 2 * time.Minute},
		{attempts: 4, want: 4 * time.Minute},
		{attempts: 7, want: 32 * time.Minute},
		{attempts: 8, want: time.Hour},
		{attempts: 1000, want: time.Hour},
	}

	for _, tt := range tests {
		require.Equal(t, tt.want, backoff(tt.attempts), "%d attempts", tt.attempts)
	}
}
