package main

import (
	"go/build"
	"testing"

	"github.com/stretchr/testify/require"
)

// Given the binary, then it embeds the timezone database, so that valid timezone names still load on
// a system without one, such as Windows: time.LoadLocation falls back on the embedded database. The
// test process reads the database of its own system first, so the test checks the import of the
// binary, outside its test files.
func TestMain_EmbedsTheTimezoneDatabase(t *testing.T) {
	t.Parallel()

	pkg, err := build.ImportDir(".", 0)
	require.NoError(t, err)
	require.Contains(t, pkg.Imports, "time/tzdata")
}
