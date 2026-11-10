package backup

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The start and the end of the name of an archive, around its version, timestamp and reason.
const (
	archivePrefix = "verve-"
	archiveSuffix = ".tar.zst"
)

// archiveName is what the name of an archive tells.
type archiveName struct {
	name   string
	time   time.Time
	reason Reason
}

// parseArchiveName reads the timestamp and the reason from the name of an archive,
// verve-<version>-<timestamp>-<reason>.tar.zst. It reports false for a name that Create does not
// write.
func parseArchiveName(name string) (archiveName, bool) {
	rest, hasPrefix := strings.CutPrefix(name, archivePrefix)
	rest, hasSuffix := strings.CutSuffix(rest, archiveSuffix)

	if !hasPrefix || !hasSuffix {
		return archiveName{}, false
	}

	// The version and the reason can hold dashes, the timestamp cannot, and no reason holds a
	// timestamp: the last part that is one is the timestamp.
	parts := strings.Split(rest, "-")

	for i := len(parts) - 2; i > 0; i-- {
		at, err := time.Parse(timestampLayout, parts[i])
		if err != nil || at.Format(timestampLayout) != parts[i] {
			continue
		}

		if strings.Join(parts[:i], "-") == "" {
			return archiveName{}, false
		}

		return archiveName{name: name, time: at, reason: Reason(strings.Join(parts[i+1:], "-"))}, true
	}

	return archiveName{}, false
}

// ApplyRetention removes the scheduled archives of the backups directory but the keep newest, by
// the timestamp of their name. It never removes another archive, or a file whose name does not
// parse.
func (b *Backups) ApplyRetention(keep int) error {
	entries, err := os.ReadDir(b.cfg.Dir)
	if err != nil {
		return fmt.Errorf("apply retention: %w", err)
	}

	var scheduled []archiveName

	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}

		if archive, ok := parseArchiveName(entry.Name()); ok && archive.reason == ReasonScheduled {
			scheduled = append(scheduled, archive)
		}
	}

	// Newest first. The name breaks the tie between archives of different versions.
	slices.SortFunc(scheduled, func(a, b archiveName) int {
		return cmp.Or(b.time.Compare(a.time), strings.Compare(b.name, a.name))
	})

	var errs []error

	for _, archive := range scheduled[min(keep, len(scheduled)):] {
		err := os.Remove(filepath.Join(b.cfg.Dir, archive.name))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("apply retention: %w", err)
	}

	return nil
}
