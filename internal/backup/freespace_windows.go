//go:build windows

package backup

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// FreeSpace returns the space available to the process in the file system of dir, in bytes.
func FreeSpace(dir string) (uint64, error) {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, fmt.Errorf("free space of %s: %w", dir, err)
	}

	var available uint64
	if err := windows.GetDiskFreeSpaceEx(path, &available, nil, nil); err != nil {
		return 0, fmt.Errorf("free space of %s: %w", dir, err)
	}

	return available, nil
}
