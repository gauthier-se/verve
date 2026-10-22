package version

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Given a binary built with version metadata injected through ldflags,
// when the version is formatted, then it prints one line with version,
// commit and build date.
func TestInfoString_WithValues(t *testing.T) {
	t.Parallel()

	info := Info{Version: "0.1.0", Commit: "abc1234", BuildDate: "2026-10-22T10:00:00Z"}

	require.Equal(t, "verve 0.1.0 (commit abc1234, built 2026-10-22T10:00:00Z)", info.String())
}

// Given a binary built without ldflags, when the version is formatted,
// then it falls back to the development defaults.
func TestInfoString_Defaults(t *testing.T) {
	t.Parallel()

	require.Equal(t, "verve dev (commit unknown, built unknown)", Get().String())
}
