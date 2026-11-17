//go:build linux

// Command importcheck checks the memory and the duration of a large import with the real binary. It
// generates a synthetic export of at least 1 GB of XML, creates a user in a new data directory,
// imports the export with `verve import xml` and fails when the peak resident memory of that process
// exceeds 300 MB, or when it runs longer than 30 minutes. It runs on Linux only, as CI does: the
// peak memory comes from the getrusage of the child. MB means 10^6 bytes.
//
//	go run ./tools/importcheck -verve bin/verve
package main

import (
	"archive/zip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gauthier-se/verve/internal/synthexport"
)

const (
	mb = 1_000_000

	// The density of a real Apple Watch, about 1 GB of XML a year.
	density = 30

	// The user who imports, and a password that the policy accepts.
	username = "importcheck"
	password = "importcheck synthetic export" //nolint:gosec // G101: the user of a temporary data directory
)

func main() {
	// An interrupt still stops the import and removes the temporary directory.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:], os.Stdout, os.Stderr)

	stop()

	if err != nil {
		fmt.Fprintln(os.Stderr, "importcheck:", err)
		os.Exit(1)
	}
}

// limits are what the import must stay within.
type limits struct {
	memoryMB int64
	duration time.Duration
}

// run reads the command line, imports a synthetic export with the binary, prints the figures of the
// import to stdout and the output of the binary to stderr, and fails when the import exceeds a limit.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("importcheck", flag.ContinueOnError)
	flags.SetOutput(stderr)

	verve := flags.String("verve", "", "path of the verve binary to check (required)")
	sizeMB := flags.Int64("size-mb", 1000, "minimum size of the export.xml, in MB")
	lim := limits{}
	flags.Int64Var(&lim.memoryMB, "max-memory-mb", 300, "maximum peak resident memory of the import, in MB")
	flags.DurationVar(&lim.duration, "max-duration", 30*time.Minute, "maximum duration of the import")

	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("read flags: %w", err)
	}

	if *verve == "" || flags.NArg() > 0 {
		flags.Usage()

		return errors.New("expected -verve <path> and no other argument")
	}

	if *sizeMB < 1 || lim.memoryMB < 1 || lim.duration <= 0 {
		return errors.New("-size-mb, -max-memory-mb and -max-duration must be positive")
	}

	dir, err := os.MkdirTemp("", "importcheck-*")
	if err != nil {
		return fmt.Errorf("create temporary directory: %w", err)
	}

	defer func() { _ = os.RemoveAll(dir) }()

	return check(ctx, *verve, dir, *sizeMB*mb, lim, stdout, stderr)
}

// check generates an export of at least minSize bytes of XML in dir, imports it with verve into a new
// data directory in dir, prints the figures of the import and checks them against lim.
func check(ctx context.Context, verve, dir string, minSize int64, lim limits, stdout, stderr io.Writer) error {
	export := filepath.Join(dir, "export.zip")

	days, size, err := writeExport(ctx, export, minSize)
	if err != nil {
		return err
	}

	// The binary writes its output to stderr, which keeps stdout for the figures.
	command := func(ctx context.Context, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, verve, args...) //nolint:gosec // G204: the binary to check
		cmd.Env = append(os.Environ(), "VERVE_DATA_DIR="+filepath.Join(dir, "data"))
		cmd.Stdout = stderr
		cmd.Stderr = stderr

		return cmd
	}

	// The commands other than serve need the schema, which migrate creates with the data directory.
	if err := command(ctx, "migrate").Run(); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	createUser := command(ctx, "user", "create", username, "--password-stdin")
	createUser.Stdin = strings.NewReader(password)

	if err := createUser.Run(); err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	importCtx, cancel := context.WithTimeout(ctx, lim.duration)
	defer cancel()

	imp := command(importCtx, "import", "xml", "--user", username, export)
	start := time.Now()
	runErr := imp.Run()
	took := time.Since(start)

	// The child has ended, even when it failed to start or was killed, unless ProcessState is nil.
	var peak int64

	if imp.ProcessState != nil {
		if usage, ok := imp.ProcessState.SysUsage().(*syscall.Rusage); ok {
			peak = usage.Maxrss * 1024 // in KiB on Linux
		}
	}

	_, _ = fmt.Fprintf(stdout, "Export: %.1f MB of XML, %d days\n", float64(size)/mb, days)
	_, _ = fmt.Fprintf(stdout, "Duration: %s (limit %s)\n", took.Round(time.Millisecond), lim.duration)
	_, _ = fmt.Fprintf(stdout, "Peak memory: %.1f MB (limit %d MB)\n", float64(peak)/mb, lim.memoryMB)

	switch {
	case errors.Is(importCtx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("import xml: stopped at the limit of %s", lim.duration)
	case runErr != nil:
		return fmt.Errorf("import xml: %w", runErr)
	case peak > lim.memoryMB*mb:
		return fmt.Errorf("peak memory of %.1f MB is above the limit of %d MB", float64(peak)/mb, lim.memoryMB)
	default:
		return nil
	}
}

// writeExport writes to path a synthetic export whose export.xml has at least minSize bytes, and
// returns its number of days and that size. A first export of one day gives the size of a day.
func writeExport(ctx context.Context, path string, minSize int64) (days int, size int64, err error) {
	opts := synthexport.Fixture()
	opts.Density = density

	for days = 1; ; {
		opts.Days = days

		size, err = writeOnce(ctx, path, opts)
		if err != nil {
			return 0, 0, err
		}

		if size >= minSize {
			return days, size, nil
		}

		// Days differ a little in size: a margin of 2 percent avoids a third export.
		days = max(days+1, int(float64(days)*float64(minSize)/float64(size)*1.02)+1)
	}
}

// writeOnce writes the export of opts to path and returns the size of its export.xml.
func writeOnce(ctx context.Context, path string, opts synthexport.Options) (int64, error) {
	f, err := os.Create(path) //nolint:gosec // G304: the path is in the temporary directory
	if err != nil {
		return 0, fmt.Errorf("create export: %w", err)
	}

	if err := synthexport.Write(ctx, f, opts); err != nil {
		_ = f.Close()

		return 0, fmt.Errorf("write export: %w", err)
	}

	if err := f.Close(); err != nil {
		return 0, fmt.Errorf("close export: %w", err)
	}

	r, err := zip.OpenReader(path)
	if err != nil {
		return 0, fmt.Errorf("read export: %w", err)
	}

	defer func() { _ = r.Close() }()

	for _, entry := range r.File {
		if entry.Name == "apple_health_export/export.xml" {
			return int64(entry.UncompressedSize64), nil //nolint:gosec // G115: an export is far below 2^63 bytes
		}
	}

	return 0, errors.New("read export: no export.xml")
}
