package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/synthexport"
)

// Given -o and a path, then the export is written there: the fixture, unless flags select another.
func TestRun_WithAPath_WritesTheSelectedExportThere(t *testing.T) {
	t.Parallel()

	selected := synthexport.Fixture()
	selected.Seed, selected.Days, selected.Density = 7, 2, 3

	tests := []struct {
		name  string
		flags []string
		want  synthexport.Options
	}{
		{"no other flag", nil, synthexport.Fixture()},
		{"seed and size", []string{"-seed", "7", "-days", "2", "-density", "3"}, selected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "export.zip")

			require.NoError(t, run(t.Context(), append([]string{"-o", path}, tt.flags...), io.Discard))

			got, err := os.ReadFile(path) //nolint:gosec // G304: the path comes from t.TempDir
			require.NoError(t, err)

			var want bytes.Buffer
			require.NoError(t, synthexport.Write(t.Context(), &want, tt.want))
			require.Equal(t, want.Bytes(), got)
		})
	}
}

// Given an invalid command line, then run fails, which main turns into a non-zero status, and no
// file stays behind.
func TestRun_InvalidArguments_FailAndLeaveNoFile(t *testing.T) {
	t.Parallel()

	const output = "export.zip"

	tests := []struct {
		name string
		args []string
	}{
		{"no output", nil},
		{"unknown flag", []string{"-o", output, "-unknown"}},
		{"no days", []string{"-o", output, "-days", "0"}},
		{"no density", []string{"-o", output, "-density", "0"}},
		{"not a number", []string{"-o", output, "-days", "many"}},
		{"extra operand", []string{"-o", output, "extra"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			args := make([]string, len(tt.args))

			for i, arg := range tt.args {
				args[i] = arg
				if arg == output {
					args[i] = filepath.Join(dir, arg)
				}
			}

			require.Error(t, run(t.Context(), args, io.Discard))

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}

// Given a file at the path and an export that fails, then the file stays as it was.
func TestRun_FailedExport_LeavesAnExistingFileAlone(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "export.zip")
	require.NoError(t, os.WriteFile(path, []byte("previous"), 0o600))

	require.Error(t, run(t.Context(), []string{"-o", path, "-days", "0"}, io.Discard))

	got, err := os.ReadFile(path) //nolint:gosec // G304: the path comes from t.TempDir
	require.NoError(t, err)
	require.Equal(t, "previous", string(got))
}

// Given a path that cannot be created, then run says so.
func TestRun_UnwritablePath_Fails(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing", "export.zip")

	require.ErrorContains(t, run(t.Context(), []string{"-o", path}, io.Discard), "create")
}
