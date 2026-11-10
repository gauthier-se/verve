package backup

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ErrArchiveNotFound is returned by Open for a name that is not the one of an archive of the
// backups directory.
var ErrArchiveNotFound = errors.New("backup archive not found")

// Archive is a backup archive of the backups directory.
type Archive struct {
	// Name is the file name of the archive.
	Name string
	// CreatedAt is the time of the snapshot, read from the name, in UTC.
	CreatedAt time.Time
	Reason    Reason
	// Size is the size of the archive, in bytes.
	Size int64
}

// parseListedName reads name as parseArchiveName does, and also reports false for a name that holds
// another character than an ASCII letter, a digit, '.', '-', '_' or '+'. Create writes no other,
// and the others could be path separators, or break the header that names a download.
func parseListedName(name string) (archiveName, bool) {
	unsafe := func(r rune) bool {
		return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-_+", r)
	}

	if strings.ContainsFunc(name, unsafe) {
		return archiveName{}, false
	}

	return parseArchiveName(name)
}

// List returns the archives of the backups directory, newest first by the timestamp of their name.
// It leaves out the directories, the links and the files whose name does not parse (see
// parseListedName), such as the temporary files of a backup in progress.
func (b *Backups) List(ctx context.Context) ([]Archive, error) {
	entries, err := os.ReadDir(b.cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}

	archives := []Archive{}

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("list backups: %w", err)
		}

		parsed, ok := parseListedName(entry.Name())
		if !ok || !entry.Type().IsRegular() {
			continue
		}

		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			// Removed since the directory was read, by retention for example.
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("list backups: %w", err)
		}

		archives = append(archives, Archive{Name: parsed.name, CreatedAt: parsed.time, Reason: parsed.reason, Size: info.Size()})
	}

	// Newest first. The name breaks the tie between archives of different versions.
	slices.SortFunc(archives, func(a, b Archive) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), strings.Compare(b.Name, a.Name))
	})

	return archives, nil
}

// Open opens the archive of name in the backups directory for reading, and returns it with what its
// name tells. It returns ErrArchiveNotFound for a name that List would leave out, such as one that
// holds a path separator or that is not the name of an archive, and for a file of that name that
// does not exist, is not a regular file or is a link. The caller closes the file.
func (b *Backups) Open(name string) (*os.File, Archive, error) {
	parsed, ok := parseListedName(name)
	if !ok {
		return nil, Archive{}, ErrArchiveNotFound
	}

	// The name holds no separator, so it stays in the backups directory.
	info, err := os.Lstat(filepath.Join(b.cfg.Dir, name))
	if errors.Is(err, fs.ErrNotExist) || err == nil && !info.Mode().IsRegular() {
		return nil, Archive{}, ErrArchiveNotFound
	}

	if err != nil {
		return nil, Archive{}, fmt.Errorf("open backup: %w", err)
	}

	// The root refuses a link out of the backups directory that replaced the file since.
	f, err := os.OpenInRoot(b.cfg.Dir, name)
	if err != nil {
		return nil, Archive{}, fmt.Errorf("open backup: %w", err)
	}

	info, err = f.Stat()
	if err != nil {
		return nil, Archive{}, errors.Join(fmt.Errorf("open backup: %w", err), f.Close())
	}

	return f, Archive{Name: parsed.name, CreatedAt: parsed.time, Reason: parsed.reason, Size: info.Size()}, nil
}
