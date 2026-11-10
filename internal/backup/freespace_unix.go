//go:build unix

package backup

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// FreeSpace returns the space available to the process in the file system of dir, in bytes.
func FreeSpace(dir string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", dir, err)
	}

	return uint64(st.Bavail) * uint64(st.Bsize), nil //nolint:gosec,unconvert // G115: both are positive, and their types depend on the system
}
