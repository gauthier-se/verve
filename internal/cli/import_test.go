package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/app"
	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store"
)

// fakeImport is an import function that records its request, reports what the test sets and returns
// its import, or its error.
type fakeImport struct {
	req      app.XMLImport
	calls    int
	wait     bool
	progress []float64
	imp      store.Import
	err      error
}

func (f *fakeImport) importXML(_ context.Context, _ config.Config, req app.XMLImport, _ *slog.Logger) (store.Import, error) {
	f.req = req
	f.calls++

	if f.wait {
		req.Waiting()
	}

	for _, p := range f.progress {
		req.Progress(p)
	}

	return f.imp, f.err
}

// importDeps returns the dependencies of the command line with fake as the import function.
func importDeps(t *testing.T, fake *fakeImport) Deps {
	t.Helper()

	deps := depsWithEnv(map[string]string{"VERVE_DATA_DIR": t.TempDir()})
	deps.ImportXML = fake.importXML

	return deps
}

// completedImport is an import that ended completed with a report.
func completedImport() store.Import {
	return store.Import{
		ID:     42,
		Origin: store.OriginCLI,
		Status: store.ImportCompleted,
		Report: store.ImportReport{
			Total: store.ImportCounters{
				Received: 1670, Inserted: 1650, Duplicates: 12, Unsupported: 8,
			},
			Period: store.ImportPeriod{
				Start: time.Date(2025, 12, 29, 22, 52, 0, 0, time.UTC),
				End:   time.Date(2026, 1, 4, 23, 0, 0, 0, time.UTC),
			},
			Routes: 2,
		},
	}
}

// Given verve import xml --user alice <absolute path>, when the import runs and completes, then the
// command passes the user and the path to the import, prints its progress as a percentage to stderr,
// prints the total counters, the period and the routes of the report to stdout and exits 0.
func TestImportXML_Completed_PrintsProgressAndReport(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "export.zip")
	fake := &fakeImport{progress: []float64{0, 0.426, 1}, imp: completedImport()}

	res := run(t, importDeps(t, fake), "import", "xml", "--user", "alice", path)

	require.Equal(t, 0, res.code, res.stderr)
	require.Equal(t, 1, fake.calls)
	require.Equal(t, "alice", fake.req.Login)
	require.Equal(t, path, fake.req.Path)
	require.Equal(t, "Imported 0%\nImported 42%\nImported 100%\n", res.stderr)
	require.Equal(t, `Import 42 completed
received     1670
inserted     1650
updated      0
duplicates   12
blocked      0
invalid      0
unsupported  8
suspicious   0
warnings     0
deleted      0
notFound     0
period       2025-12-29T22:52:00Z to 2026-01-04T23:00:00Z
routes       2
`, res.stdout)
}

// Given an import whose report has no period, then the command prints a dash as its period.
func TestImportXML_NoPeriod_PrintsADash(t *testing.T) {
	t.Parallel()

	imp := completedImport()
	imp.Report = store.ImportReport{}
	fake := &fakeImport{imp: imp}

	res := run(t, importDeps(t, fake), "import", "xml", "--user", "alice", filepath.Join(t.TempDir(), "export.xml"))

	require.Equal(t, 0, res.code, res.stderr)
	require.Contains(t, res.stdout, "\nperiod       -\nroutes       0\n")
}

// Given a relative path, then the command passes it to the import as an absolute path.
func TestImportXML_RelativePath_IsMadeAbsolute(t *testing.T) {
	t.Parallel()

	wd, err := os.Getwd()
	require.NoError(t, err)

	fake := &fakeImport{imp: completedImport()}

	res := run(t, importDeps(t, fake), "import", "xml", "--user", "alice", filepath.Join("exports", "export.zip"))

	require.Equal(t, 0, res.code, res.stderr)
	require.Equal(t, filepath.Join(wd, "exports", "export.zip"), fake.req.Path)
	require.True(t, filepath.IsAbs(fake.req.Path))
}

// Given an import of the same user that runs, then the command says on stderr that it waits for it.
func TestImportXML_Waiting_SaysSoOnStderr(t *testing.T) {
	t.Parallel()

	fake := &fakeImport{wait: true, progress: []float64{0.5}, imp: completedImport()}

	res := run(t, importDeps(t, fake), "import", "xml", "--user", "alice", filepath.Join(t.TempDir(), "export.zip"))

	require.Equal(t, 0, res.code, res.stderr)
	require.Equal(t, "Waiting for another import of alice to end\nImported 50%\n", res.stderr)
}

// Given an unknown, disabled or deleted user, or a path that cannot be opened, then the command
// exits 1 with the reason on stderr and prints nothing to stdout.
func TestImportXML_RefusedImport_ExitsOneWithTheReason(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "export.zip")
	openErr := &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}

	tests := []struct {
		name   string
		err    error
		stderr string
	}{
		{name: "unknown user", err: fmt.Errorf("get user: %w", store.ErrNotFound), stderr: "Error: no user \"Alice\"\n"},
		{name: "disabled user", err: fmt.Errorf("check user: %w", auth.ErrUserDisabled), stderr: "Error: Alice is disabled\n"},
		{name: "user being deleted", err: fmt.Errorf("check user: %w", auth.ErrUserDeleting), stderr: "Error: Alice is being deleted\n"},
		{name: "path that cannot be opened", err: openErr, stderr: "Error: open " + path + ": file does not exist\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeImport{err: tt.err}

			res := run(t, importDeps(t, fake), "import", "xml", "--user", "Alice", path)

			require.Equal(t, 1, res.code)
			require.Equal(t, tt.stderr, res.stderr)
			require.Empty(t, res.stdout)
		})
	}
}

// Given an import that ends failed or cancelled, then the command prints its report to stdout, the
// reason to stderr and exits 1.
func TestImportXML_UnsuccessfulImport_ExitsOne(t *testing.T) {
	t.Parallel()

	failed := completedImport()
	failed.Status = store.ImportFailed
	failed.Report.Errors = []string{"record 3: startDate is not a date", "the file is not an Apple Health export"}

	cancelled := completedImport()
	cancelled.Status = store.ImportCancelled

	tests := []struct {
		name   string
		imp    store.Import
		stdout string
		stderr string
	}{
		{
			name:   "failed",
			imp:    failed,
			stdout: "Import 42 failed\n",
			stderr: "Error: import 42 failed: the file is not an Apple Health export\n",
		},
		{
			name:   "cancelled",
			imp:    cancelled,
			stdout: "Import 42 cancelled\n",
			stderr: "Error: import 42 cancelled\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeImport{imp: tt.imp}

			res := run(t, importDeps(t, fake), "import", "xml", "--user", "alice", filepath.Join(t.TempDir(), "export.zip"))

			require.Equal(t, 1, res.code)
			require.Equal(t, tt.stderr, res.stderr)
			require.Contains(t, res.stdout, tt.stdout)
			require.Contains(t, res.stdout, "\nroutes       2\n")
		})
	}
}

// Given an import function that fails without ending its import, then the command exits 1 with the
// error.
func TestImportXML_ImportError_ExitsOne(t *testing.T) {
	t.Parallel()

	fake := &fakeImport{err: errors.New("renew lease of job 3: database is locked")}

	res := run(t, importDeps(t, fake), "import", "xml", "--user", "alice", filepath.Join(t.TempDir(), "export.zip"))

	require.Equal(t, 1, res.code)
	require.Equal(t, "Error: import xml: renew lease of job 3: database is locked\n", res.stderr)
	require.Empty(t, res.stdout)
}

// Given verve import xml without --user, or without exactly one path, then the command exits 1 with
// a usage error and imports nothing.
func TestImportXML_InvalidArguments_FailWithUsage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		err  string
	}{
		{name: "without user", args: []string{"import", "xml", "export.zip"}, err: "Error: --user is required\n"},
		{name: "empty user", args: []string{"import", "xml", "--user", "", "export.zip"}, err: "Error: --user is required\n"},
		{name: "without path", args: []string{"import", "xml", "--user", "alice"}, err: "Error: accepts 1 arg(s), received 0\n"},
		{name: "two paths", args: []string{"import", "xml", "--user", "alice", "a.zip", "b.zip"}, err: "Error: accepts 1 arg(s), received 2\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeImport{imp: completedImport()}

			res := run(t, importDeps(t, fake), tt.args...)

			require.Equal(t, 1, res.code)
			require.Equal(t, tt.err+"Run 'verve --help' for usage.\n", res.stderr)
			require.Zero(t, fake.calls)
		})
	}
}
