// Command synthexport writes an invented Apple Health export. It writes the fixture of the tests
// unless flags change it: a year at density 30 is as dense as a real Apple Watch, about 1 GB of XML.
//
//	go run ./tools/synthexport -o export.zip -days 365 -density 30
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/gauthier-se/verve/internal/synthexport"
)

func main() {
	// A large export takes minutes: an interrupt still removes the temporary file.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:], os.Stderr)

	stop()

	if err != nil {
		fmt.Fprintln(os.Stderr, "synthexport:", err)
		os.Exit(1)
	}
}

// run reads the command line and writes the export.
func run(ctx context.Context, args []string, stderr io.Writer) error {
	opts := synthexport.Fixture()

	flags := flag.NewFlagSet("synthexport", flag.ContinueOnError)
	flags.SetOutput(stderr)

	path := flags.String("o", "", "path of the zip to write (required)")
	flags.Uint64Var(&opts.Seed, "seed", opts.Seed, "selects the values")
	flags.IntVar(&opts.Days, "days", opts.Days, "days of history")
	flags.IntVar(&opts.Density, "density", opts.Density, "samples per hour of the continuous series, route points per minute")

	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("read flags: %w", err)
	}

	if *path == "" || flags.NArg() > 0 {
		flags.Usage()

		return errors.New("expected -o <path> and no other argument")
	}

	return writeExport(ctx, *path, opts)
}

// writeExport replaces path only with a complete export: a failure leaves it as it was.
func writeExport(ctx context.Context, path string, opts synthexport.Options) error {
	f, err := os.CreateTemp(filepath.Dir(path), "synthexport-*.zip")
	if err != nil {
		return fmt.Errorf("create export: %w", err)
	}

	defer func() { _ = os.Remove(f.Name()) }() // fails once the file has its final name

	if err := synthexport.Write(ctx, f, opts); err != nil {
		_ = f.Close()

		return fmt.Errorf("write export: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close export: %w", err)
	}

	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("move export: %w", err)
	}

	return nil
}
