package app

import (
	"fmt"
	"path/filepath"

	"github.com/gofrs/flock"
)

// lockFile is the name of the lock file in the data directory.
const lockFile = "verve.lock"

// Lock is an exclusive lock on a data directory. The operating system releases it when the
// process exits, so a crash never leaves a stale lock.
type Lock struct {
	fl *flock.Flock
}

// AcquireLock takes the lock on dataDir without waiting. It fails when another process holds it.
func AcquireLock(dataDir string) (*Lock, error) {
	fl := flock.New(filepath.Join(dataDir, lockFile))

	locked, err := fl.TryLock()
	if err != nil {
		return nil, fmt.Errorf("lock data directory %s: %w", dataDir, err)
	}

	if !locked {
		return nil, fmt.Errorf("another Verve process is using %s", dataDir)
	}

	return &Lock{fl: fl}, nil
}

// Release releases the lock.
func (l *Lock) Release() error {
	if err := l.fl.Unlock(); err != nil {
		return fmt.Errorf("unlock data directory: %w", err)
	}

	return nil
}
