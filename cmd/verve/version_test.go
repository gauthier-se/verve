package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVersionCommand checks that `verve version` prints the stamped version and
// does so without opening a database or creating the data dir — a released
// binary must report its version even with no writable VERVE_DATA_DIR.
func TestVersionCommand(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	var out bytes.Buffer

	if err := run(context.Background(), testLogger(), os.Stdin, &out, []string{"-data-dir=" + dir, "version"}); err != nil {
		t.Fatalf("run version: %v", err)
	}

	if got := strings.TrimSpace(out.String()); got != version {
		t.Errorf("version output = %q, want %q", got, version)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("data dir %q was created; version must not touch the filesystem", dir)
	}
}
