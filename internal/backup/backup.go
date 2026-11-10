// Package backup creates the backups of the instance: zstd-compressed tar archives of a snapshot of
// the database and of the secret key, see FR-1 and FR-4 of backups.
package backup

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/klauspost/compress/zstd"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// Reason is why a backup was made. It ends the name of the archive.
type Reason string

// ReasonManual is a backup asked for by an operator.
const ReasonManual Reason = "manual"

// The names of the files of an archive.
const (
	dbEntry        = "verve.db"
	secretKeyEntry = "secret.key"
)

// entryPerm is the permission of the files of an archive, and of the archive itself.
const entryPerm = 0o600

// timestampLayout is the UTC time in the name of an archive, without the colons that Windows
// refuses in file names.
const timestampLayout = "20060102T150405Z"

// tmpPrefix starts the name of the temporary directory of a backup in progress. The next backups
// remove those of the backups that crashed.
const tmpPrefix = ".verve-backup-"

// tmpLockFile is the lock of a temporary directory, held by its backup while it runs.
const tmpLockFile = "lock"

// Config locates what a backup copies and where it goes.
type Config struct {
	// Dir is the directory of the archives, which exists.
	Dir string
	// DB is the database file, whose size, with its WAL, estimates the size of a backup.
	DB string
	// SecretKey is the secret key file, or empty when the key is configured and has no file. A file
	// that does not exist yet is left out of the archive, like an empty SecretKey.
	SecretKey string
	// Version is the version of Verve, written in the name of the archives.
	Version string
	// FreeSpace returns the space available in a directory, in bytes.
	FreeSpace func(dir string) (uint64, error)
}

// Backups creates the backups of Config.
type Backups struct {
	cfg   Config
	clock clock.Clock
}

// New returns the backups of cfg, timed by clk.
func New(cfg Config, clk clock.Clock) *Backups {
	return &Backups{cfg: cfg, clock: clk}
}

// Create backs up s for reason and returns the path of the archive. It runs next to the writes of
// s, which it never holds off, and next to other backups. On failure it leaves no archive and no
// temporary file. It first removes the temporary files that crashed backups left.
func (b *Backups) Create(ctx context.Context, s store.Store, reason Reason) (string, error) {
	if err := removeCrashed(b.cfg.Dir); err != nil {
		return "", fmt.Errorf("back up: %w", err)
	}

	path, err := b.create(ctx, s, reason)
	if err != nil {
		return "", fmt.Errorf("back up: %w", err)
	}

	return path, nil
}

func (b *Backups) create(ctx context.Context, s store.Store, reason Reason) (path string, err error) {
	if err := b.checkSpace(); err != nil {
		return "", err
	}

	tmpDir, unlock, err := createTmpDir(b.cfg.Dir)
	if err != nil {
		return "", err
	}

	defer func() { err = errors.Join(err, unlock(), os.RemoveAll(tmpDir)) }()

	// The time of the backup is the one of the snapshot, the state it holds.
	now := b.clock.Now().UTC()

	snapshot := filepath.Join(tmpDir, dbEntry)
	if err := s.Snapshot(ctx, snapshot); err != nil {
		return "", err
	}

	archive := filepath.Join(tmpDir, "archive")

	if err := b.writeArchive(archive, snapshot, now); err != nil {
		return "", err
	}

	name := fmt.Sprintf("verve-%s-%s-%s.tar.zst", b.cfg.Version, now.Format(timestampLayout), reason)
	path = filepath.Join(b.cfg.Dir, name)

	if err := os.Rename(archive, path); err != nil {
		return "", fmt.Errorf("move archive: %w", err)
	}

	if err := audit.Record(ctx, s.AuditEvents(), audit.BackupCreated(string(reason), name)); err != nil {
		return "", errors.Join(err, os.Remove(path))
	}

	return path, nil
}

// writeArchive writes to path the archive of the database snapshot and of the secret key, its files
// dated now.
func (b *Backups) writeArchive(path, snapshot string, now time.Time) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, entryPerm) //nolint:gosec // G304: the path is in the backups directory
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}

	defer func() { err = errors.Join(err, f.Close()) }()

	// One goroutine, so that small hardware keeps its other cores for the server.
	zw, err := zstd.NewWriter(f, zstd.WithEncoderConcurrency(1))
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}

	tw := tar.NewWriter(zw)

	if err := addFile(tw, dbEntry, snapshot, now); err != nil {
		return errors.Join(err, zw.Close())
	}

	if b.cfg.SecretKey != "" {
		err := addFile(tw, secretKeyEntry, b.cfg.SecretKey, now)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return errors.Join(err, zw.Close())
		}
	}

	if err := errors.Join(tw.Close(), zw.Close(), f.Sync()); err != nil {
		return fmt.Errorf("write archive: %w", err)
	}

	return nil
}

// addFile streams the regular file at path into tw as name.
func addFile(tw *tar.Writer, name, path string, now time.Time) (err error) {
	f, err := os.Open(path) //nolint:gosec // G304: the path comes from the configured data directory
	if err != nil {
		return fmt.Errorf("archive %s: %w", name, err)
	}

	defer func() { err = errors.Join(err, f.Close()) }()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("archive %s: %w", name, err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("archive %s: %s is not a regular file", name, path)
	}

	header := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Mode:     entryPerm,
		Size:     info.Size(),
		ModTime:  now,
		Format:   tar.FormatPAX,
	}

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("archive %s: %w", name, err)
	}

	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("archive %s: %w", name, err)
	}

	return nil
}

// createTmpDir creates the temporary directory of a backup in dir, with the permissions 0700 of
// os.MkdirTemp, since the snapshot has those of a new file. It returns the directory and the
// function that releases its lock, held until then so that other backups keep it.
func createTmpDir(dir string) (string, func() error, error) {
	tmpDir, err := os.MkdirTemp(dir, tmpPrefix+"*")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary directory: %w", err)
	}

	lock := flock.New(filepath.Join(tmpDir, tmpLockFile))

	locked, err := lock.TryLock()
	if err == nil && !locked {
		err = errors.New("taken by another backup")
	}

	if err != nil {
		return "", nil, errors.Join(fmt.Errorf("lock temporary directory: %w", err), os.RemoveAll(tmpDir))
	}

	return tmpDir, lock.Unlock, nil
}

// removeCrashed removes the temporary directories of dir whose backup no longer runs: the operating
// system released their lock when the process ended.
func removeCrashed(dir string) error {
	tmpDirs, err := filepath.Glob(filepath.Join(dir, tmpPrefix+"*"))
	if err != nil {
		return fmt.Errorf("find temporary files: %w", err)
	}

	for _, tmpDir := range tmpDirs {
		lock := flock.New(filepath.Join(tmpDir, tmpLockFile))

		locked, err := lock.TryLock()
		if err != nil {
			return fmt.Errorf("remove temporary files %s: %w", tmpDir, err)
		}

		if !locked {
			continue
		}

		if err := errors.Join(lock.Unlock(), os.RemoveAll(tmpDir)); err != nil {
			return fmt.Errorf("remove temporary files %s: %w", tmpDir, err)
		}
	}

	return nil
}
