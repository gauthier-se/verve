package backup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// checkSpace fails unless the free space of the backups directory holds the estimated size of a
// backup: the size of the database and of its WAL, which bounds the snapshot, the largest file of a
// backup (FR-10 of backups).
func (b *Backups) checkSpace() error {
	needed, err := estimateSize(b.cfg.DB)
	if err != nil {
		return err
	}

	available, err := b.cfg.FreeSpace(b.cfg.Dir)
	if err != nil {
		return fmt.Errorf("check free space in %s: %w", b.cfg.Dir, err)
	}

	if available < needed {
		return fmt.Errorf("not enough free space: the backup needs about %s, %s available in %s",
			formatSize(needed), formatSize(available), b.cfg.Dir)
	}

	return nil
}

// estimateSize returns the size of the database at db and of its WAL, when it has one.
func estimateSize(db string) (uint64, error) {
	var size uint64

	for _, path := range []string{db, db + "-wal"} {
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) && path != db {
			continue
		}

		if err != nil {
			return 0, fmt.Errorf("estimate backup size: %w", err)
		}

		size += uint64(info.Size()) //nolint:gosec // G115: a file size is never negative
	}

	return size, nil
}

// formatSize returns size in bytes with a decimal unit and one decimal, for example "4.5 MB".
func formatSize(size uint64) string {
	const unit = 1000

	if size < unit {
		return fmt.Sprintf("%d B", size)
	}

	value := float64(size) / unit
	prefix := 0

	for value >= unit && prefix < len("MGTPE")-1 {
		value /= unit
		prefix++
	}

	return fmt.Sprintf("%.1f %cB", value, "kMGTPE"[prefix])
}
