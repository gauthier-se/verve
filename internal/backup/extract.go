package backup

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
)

// Extracted locates the files of an archive written by Extract.
type Extracted struct {
	// DB is the database.
	DB string
	// SecretKey is the secret key, or empty when the archive holds none.
	SecretKey string
}

// CheckDB checks the database file at path, extracted from an archive.
type CheckDB func(ctx context.Context, path string) error

// Extract writes the database and the secret key of archive to dir, with the permissions 0600, then
// checks the database with check. It refuses an archive that does not read to its end, an entry
// that is not a regular file named verve.db or secret.key, an entry that appears twice, and an
// archive without database. The names of the entries are never joined with dir: an entry with a
// path is refused, so that nothing is written outside dir. On failure, the files already written
// stay in dir.
func Extract(ctx context.Context, archive, dir string, check CheckDB) (Extracted, error) {
	extracted, err := extract(ctx, archive, dir)
	if err != nil {
		return Extracted{}, fmt.Errorf("invalid archive %s: %w", filepath.Base(archive), err)
	}

	if err := check(ctx, extracted.DB); err != nil {
		return Extracted{}, fmt.Errorf("invalid archive %s: check %s: %w", filepath.Base(archive), dbEntry, err)
	}

	return extracted, nil
}

// extract writes the entries of archive to dir.
func extract(ctx context.Context, archive, dir string) (extracted Extracted, err error) {
	f, err := os.Open(archive) //nolint:gosec // G304: the archive is the one the operator names
	if err != nil {
		return Extracted{}, err
	}

	defer func() { err = errors.Join(err, f.Close()) }()

	zr, err := zstd.NewReader(f, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return Extracted{}, fmt.Errorf("read archive: %w", err)
	}

	defer zr.Close()

	tr := tar.NewReader(zr)

	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return Extracted{}, fmt.Errorf("read archive: %w", err)
		}

		target, err := entryTarget(header, &extracted, dir)
		if err != nil {
			return Extracted{}, err
		}

		if err := writeEntry(ctx, tr, target); err != nil {
			return Extracted{}, fmt.Errorf("read archive: %s: %w", header.Name, err)
		}
	}

	// The end of the tar archive may come before the end of the file: what follows must be valid
	// compressed data too, so that data appended to an archive is refused.
	if _, err := io.Copy(io.Discard, zr); err != nil {
		return Extracted{}, fmt.Errorf("read archive: %w", err)
	}

	if extracted.DB == "" {
		return Extracted{}, fmt.Errorf("archive holds no %s", dbEntry)
	}

	return extracted, nil
}

// entryTarget returns the path in dir of the file of header, and records it in extracted.
func entryTarget(header *tar.Header, extracted *Extracted, dir string) (string, error) {
	var (
		slot *string
		name string
	)

	switch header.Name {
	case dbEntry:
		slot, name = &extracted.DB, dbEntry
	case secretKeyEntry:
		slot, name = &extracted.SecretKey, secretKeyEntry
	default:
		return "", fmt.Errorf("unexpected entry %q: an archive holds only %s and %s", header.Name, dbEntry, secretKeyEntry)
	}

	if header.Typeflag != tar.TypeReg {
		return "", fmt.Errorf("entry %s is not a regular file", header.Name)
	}

	if *slot != "" {
		return "", fmt.Errorf("archive holds %s twice", header.Name)
	}

	// The constant name, never the one of the archive.
	*slot = filepath.Join(dir, name)

	return *slot, nil
}

// writeEntry writes the current entry of tr to a new file at path, until ctx is done.
func writeEntry(ctx context.Context, tr *tar.Reader, path string) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, entryPerm) //nolint:gosec // G304: the path is a constant name in dir
	if err != nil {
		return err
	}

	defer func() { err = errors.Join(err, f.Close()) }()

	if _, err := io.Copy(f, ctxReader{ctx: ctx, r: tr}); err != nil {
		return err
	}

	return f.Sync()
}
