// Command syncdist replaces the content of a directory with a copy of another, keeping the
// .gitignore of the destination, on every operating system. `task build` runs it to embed the web
// app: syncdist web/dist internal/webui/dist.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: syncdist <src> <dst>")
		os.Exit(2)
	}

	if err := syncDist(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "syncdist:", err)
		os.Exit(1)
	}
}

// syncDist replaces the content of dst, except its .gitignore, with a copy of src.
func syncDist(src, dst string) error {
	// Checked before anything is removed from dst.
	if _, err := os.Stat(src); err != nil { //nolint:gosec // G703: the build task chooses the paths
		return fmt.Errorf("read source: %w", err)
	}

	entries, err := os.ReadDir(dst)
	if err != nil {
		return fmt.Errorf("read destination: %w", err)
	}

	for _, e := range entries {
		if e.Name() == ".gitignore" {
			continue
		}

		if err := os.RemoveAll(filepath.Join(dst, e.Name())); err != nil { //nolint:gosec // G703: dst comes from the build task, e from its listing
			return fmt.Errorf("remove stale files: %w", err)
		}
	}

	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}

	return nil
}
