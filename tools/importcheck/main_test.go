//go:build linux

package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The behavior of the fake binary, which the script of newFake passes to TestFakeVerve.
const (
	envFakeLog        = "VERVE_TEST_IMPORTCHECK_LOG"
	envFakeAllocMB    = "VERVE_TEST_IMPORTCHECK_ALLOC_MB"
	envFakeSleep      = "VERVE_TEST_IMPORTCHECK_SLEEP"
	envFakeImportExit = "VERVE_TEST_IMPORTCHECK_IMPORT_EXIT"
	envFakeUserExit   = "VERVE_TEST_IMPORTCHECK_USER_EXIT"
)

// call is a run of the fake binary, as it logs it.
type call struct {
	Args    []string `json:"args"`
	DataDir string   `json:"dataDir"`
	Stdin   string   `json:"stdin"`
	// XMLSize is the uncompressed size of the export.xml of the export that an import reads.
	XMLSize uint64 `json:"xmlSize"`
}

// fake is a verve binary written by a test.
type fake struct {
	path string
	log  string
}

// fakeBehavior selects what the fake binary does.
type fakeBehavior struct {
	allocMB    int           // the memory that an import allocates and touches
	sleep      time.Duration // how long an import runs
	importExit int           // the exit status of an import
	userExit   int           // the exit status of a user creation
}

// newFake writes a script that runs this test binary as TestFakeVerve with behavior.
func newFake(t *testing.T, behavior fakeBehavior) fake {
	t.Helper()

	exe, err := os.Executable()
	require.NoError(t, err)

	dir := t.TempDir()
	f := fake{path: filepath.Join(dir, "verve"), log: filepath.Join(dir, "calls.jsonl")}

	script := fmt.Sprintf("#!/bin/sh\nexec env %s='%s' %s=%d %s=%s %s=%d %s=%d '%s' -test.run='^TestFakeVerve$' -- \"$@\"\n",
		envFakeLog, f.log, envFakeAllocMB, behavior.allocMB, envFakeSleep, behavior.sleep, envFakeImportExit,
		behavior.importExit, envFakeUserExit, behavior.userExit, exe)
	require.NoError(t, os.WriteFile(f.path, []byte(script), 0o700)) //nolint:gosec // G306: the script must run

	return f
}

// calls returns the runs of the fake binary, in order.
func (f fake) calls(t *testing.T) []call {
	t.Helper()

	data, err := os.ReadFile(f.log)
	if os.IsNotExist(err) {
		return nil
	}

	require.NoError(t, err)

	var calls []call

	for line := range strings.Lines(string(data)) {
		var c call
		require.NoError(t, json.Unmarshal([]byte(line), &c))

		calls = append(calls, c)
	}

	return calls
}

// TestFakeVerve is the verve binary that the script of newFake runs: it logs its call, and an
// import allocates memory and runs as the script says.
func TestFakeVerve(t *testing.T) {
	t.Parallel()

	log := os.Getenv(envFakeLog)
	if log == "" {
		t.Skip("the process of another test")
	}

	os.Exit(fakeVerve(log, flag.Args())) //nolint:revive // the exit status of the fake binary
}

func fakeVerve(log string, args []string) int {
	stdin, _ := io.ReadAll(os.Stdin)
	c := call{Args: args, DataDir: os.Getenv("VERVE_DATA_DIR"), Stdin: string(stdin)}

	status, _ := strconv.Atoi(os.Getenv(envFakeUserExit))

	if len(args) == 5 && args[0] == "import" {
		c.XMLSize = xmlSize(args[4])

		mb, _ := strconv.Atoi(os.Getenv(envFakeAllocMB))
		allocate(mb)

		sleep, _ := time.ParseDuration(os.Getenv(envFakeSleep))
		time.Sleep(sleep)

		status, _ = strconv.Atoi(os.Getenv(envFakeImportExit))
	}

	line, _ := json.Marshal(c)

	f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // G304: the path of the test
	if err != nil {
		return 3
	}

	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()

	return status
}

// xmlSize returns the uncompressed size of the export.xml of the zip at path, or 0.
func xmlSize(path string) uint64 {
	r, err := zip.OpenReader(path)
	if err != nil {
		return 0
	}

	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		if f.Name == "apple_health_export/export.xml" {
			return f.UncompressedSize64
		}
	}

	return 0
}

// allocate makes mb megabytes resident. They are mapped outside the Go heap, so that the race
// detector does not add its own memory.
func allocate(mb int) {
	if mb == 0 {
		return
	}

	mem, err := syscall.Mmap(-1, 0, mb*1_000_000, syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		panic(err)
	}

	for i := 0; i < len(mem); i += os.Getpagesize() {
		mem[i] = 1
	}
}

// figures are what run prints.
type figures struct {
	xmlMB  float64
	days   int
	took   time.Duration
	peakMB float64
}

var figuresPattern = regexp.MustCompile(`^Export: ([0-9.]+) MB of XML, (\d+) days\n` +
	`Duration: (\S+) \(limit \S+\)\n` +
	`Peak memory: ([0-9.]+) MB \(limit \d+ MB\)\n$`)

func parseFigures(t *testing.T, stdout string) figures {
	t.Helper()

	m := figuresPattern.FindStringSubmatch(stdout)
	require.NotNil(t, m, "figures in %q", stdout)

	var f figures

	var err error

	f.xmlMB, err = strconv.ParseFloat(m[1], 64)
	require.NoError(t, err)
	f.days, err = strconv.Atoi(m[2])
	require.NoError(t, err)
	f.took, err = time.ParseDuration(m[3])
	require.NoError(t, err)
	f.peakMB, err = strconv.ParseFloat(m[4], 64)
	require.NoError(t, err)

	return f
}

// Given -verve and a small export, then run generates the export, creates a user with the binary
// in a new data directory, imports the export for them with the binary, prints the size of the
// export, the duration and the peak memory of the import, and removes what it created.
func TestRun_SmallExport_ImportsItWithTheBinaryAndPrintsItsFigures(t *testing.T) {
	t.Parallel()

	verve := newFake(t, fakeBehavior{allocMB: 20})

	var stdout bytes.Buffer

	require.NoError(t, run(t.Context(), []string{"-verve", verve.path, "-size-mb", "3"}, &stdout, io.Discard))

	calls := verve.calls(t)
	require.Len(t, calls, 2)

	create, imp := calls[0], calls[1]
	require.Len(t, create.Args, 4)
	require.Equal(t, []string{"user", "create"}, create.Args[:2])
	require.Equal(t, "--password-stdin", create.Args[3])
	require.NotEmpty(t, create.Stdin, "the password")

	user := create.Args[2]
	require.Equal(t, []string{"import", "xml", "--user", user}, imp.Args[:4])

	require.NotEmpty(t, create.DataDir)
	require.Equal(t, create.DataDir, imp.DataDir)
	require.NoDirExists(t, create.DataDir, "the data directory is removed")
	require.NoFileExists(t, imp.Args[4], "the export is removed")

	require.GreaterOrEqual(t, imp.XMLSize, uint64(3_000_000))

	got := parseFigures(t, stdout.String())
	require.Equal(t, fmt.Sprintf("%.1f", float64(imp.XMLSize)/1e6), strconv.FormatFloat(got.xmlMB, 'f', 1, 64))
	require.Positive(t, got.days)
	require.Positive(t, got.took)
	require.GreaterOrEqual(t, got.peakMB, 20.0, "the memory that the import touched")
	require.Less(t, got.peakMB, 300.0)
}

// Given a peak resident memory of the import above the limit, 300 MB unless -max-memory-mb changes
// it, then run fails and says so, after printing the figures.
func TestRun_PeakMemoryAboveTheLimit_Fails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		allocMB int
		flags   []string
		limit   string
	}{
		{"default limit", 320, nil, "300 MB"},
		{"lower limit", 120, []string{"-max-memory-mb", "100"}, "100 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			verve := newFake(t, fakeBehavior{allocMB: tt.allocMB})

			var stdout bytes.Buffer

			args := append([]string{"-verve", verve.path, "-size-mb", "1"}, tt.flags...)
			err := run(t.Context(), args, &stdout, io.Discard)
			require.ErrorContains(t, err, "peak memory")
			require.ErrorContains(t, err, tt.limit)

			got := parseFigures(t, stdout.String())
			require.GreaterOrEqual(t, got.peakMB, float64(tt.allocMB))
		})
	}
}

// Given an import that runs longer than -max-duration, then run stops it and fails.
func TestRun_ImportLongerThanTheLimit_Fails(t *testing.T) {
	t.Parallel()

	verve := newFake(t, fakeBehavior{sleep: time.Minute})

	start := time.Now()
	err := run(t.Context(), []string{"-verve", verve.path, "-size-mb", "1", "-max-duration", "500ms"}, io.Discard,
		io.Discard)
	require.ErrorContains(t, err, "500ms")
	require.Less(t, time.Since(start), 30*time.Second, "the import is stopped")
}

// Given an import or a user creation that fails, then run fails with its status.
func TestRun_FailedChild_Fails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		behavior fakeBehavior
		want     string
	}{
		{"import", fakeBehavior{importExit: 1}, "import xml: exit status 1"},
		{"user creation", fakeBehavior{userExit: 2}, "create user: exit status 2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			verve := newFake(t, tt.behavior)

			err := run(t.Context(), []string{"-verve", verve.path, "-size-mb", "1"}, io.Discard, io.Discard)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

// Given an invalid command line, then run fails before it runs the binary, which main turns into a
// non-zero status.
func TestRun_InvalidArguments_Fail(t *testing.T) {
	t.Parallel()

	const binary = "verve"

	tests := []struct {
		name string
		args []string
	}{
		{"no binary", nil},
		{"unknown flag", []string{"-verve", binary, "-unknown"}},
		{"extra operand", []string{"-verve", binary, "extra"}},
		{"no size", []string{"-verve", binary, "-size-mb", "0"}},
		{"no memory", []string{"-verve", binary, "-max-memory-mb", "0"}},
		{"no duration", []string{"-verve", binary, "-max-duration", "0s"}},
		{"not a number", []string{"-verve", binary, "-size-mb", "large"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			verve := newFake(t, fakeBehavior{})
			args := make([]string, len(tt.args))

			for i, arg := range tt.args {
				args[i] = arg
				if arg == binary {
					args[i] = verve.path
				}
			}

			require.Error(t, run(t.Context(), args, io.Discard, io.Discard))
			require.Empty(t, verve.calls(t))
		})
	}
}
