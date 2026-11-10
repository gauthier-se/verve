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

// Reasons of a backup.
const (
	// ReasonManual is a backup asked for by an operator.
	ReasonManual Reason = "manual"
	// ReasonScheduled is a backup of the schedule, which retention removes once newer ones replace it.
	ReasonScheduled Reason = "scheduled"
	// ReasonPreMigration is a backup before migrations, which retention removes once the backups of
	// newer migrations replace it.
	ReasonPreMigration Reason = "pre-migration"
)

// preMigrationKeep is the number of pre-migration backups kept: those of the last 3 migrations
// (FR-3 of backups).
const preMigrationKeep = 3

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

// The start and the end of the name of an archive, around its version, timestamp and reason.
const (
	archivePrefix = "verve-"
	archiveSuffix = ".tar.zst"
)

// tmpPrefix starts the name of the temporary directory of a backup in progress. The next backups
// remove those of the backups that crashed.
const tmpPrefix = ".verve-backup-"

// tmpLockFile is the lock of a temporary directory, held by its backup while it runs.
const tmpLockFile = "lock"

// guardLockFile is the lock of the backups directory, held while a backup removes the temporary
// directories of crashed backups and creates its own. Its name does not start with tmpPrefix.
const guardLockFile = ".verve-backup.lock"

// guardRetryDelay is how often a backup tries to take the lock of the backups directory again.
const guardRetryDelay = 10 * time.Millisecond

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

// Snapshotter copies a database, as store.Store does.
type Snapshotter interface {
	// Snapshot writes a consistent copy of the database to a new file at path, see store.Store.
	Snapshot(ctx context.Context, path string) error
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
	path, err := b.create(ctx, s, reason, func(name string) error {
		return audit.Record(ctx, s.AuditEvents(), audit.BackupCreated(string(reason), name))
	})
	if err != nil {
		return "", fmt.Errorf("back up: %w", err)
	}

	return path, nil
}

// CreatePreMigration backs up db before its migrations and returns the path of the archive, like
// Create. It records no audit event and removes no other archive: the caller records the event and
// calls ApplyPreMigrationRetention once the migrations are applied, since the schema of db may not
// hold the event yet, and a migration that fails does not count as one of the last migrations.
func (b *Backups) CreatePreMigration(ctx context.Context, db Snapshotter) (string, error) {
	path, err := b.create(ctx, db, ReasonPreMigration, func(string) error { return nil })
	if err != nil {
		return "", fmt.Errorf("back up: %w", err)
	}

	return path, nil
}

// create backs up db for reason, then calls record with the name of the archive in place. A failure
// of record removes the archive.
func (b *Backups) create(ctx context.Context, db Snapshotter, reason Reason, record func(name string) error) (path string, err error) {
	tmpDir, release, err := startBackup(ctx, b.cfg.Dir)
	if err != nil {
		return "", err
	}

	// Once the archive is in place, a temporary directory that stays does not fail the backup: the
	// next one removes it, as it does for a crash.
	defer func() {
		if cleanupErr := release(); err != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()

	if err := b.checkSpace(); err != nil {
		return "", err
	}

	// The time of the backup is the one of the snapshot, the state it holds.
	now := b.clock.Now().UTC()

	snapshot := filepath.Join(tmpDir, dbEntry)
	if err := db.Snapshot(ctx, snapshot); err != nil {
		return "", err
	}

	archive := filepath.Join(tmpDir, "archive")

	if err := b.writeArchive(ctx, archive, snapshot, now); err != nil {
		return "", err
	}

	name := fmt.Sprintf("%s%s-%s-%s%s", archivePrefix, b.cfg.Version, now.Format(timestampLayout), reason, archiveSuffix)
	path = filepath.Join(b.cfg.Dir, name)

	// A link rather than a rename, which would replace the archive of a backup made in the same
	// second. The temporary name goes with the temporary directory.
	err = os.Link(archive, path)
	if errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("archive %s exists", name)
	}

	if err != nil {
		return "", fmt.Errorf("move archive: %w", err)
	}

	if err := record(name); err != nil {
		return "", errors.Join(err, os.Remove(path))
	}

	return path, nil
}

// writeArchive writes to path the archive of the database snapshot and of the secret key, its files
// dated now.
func (b *Backups) writeArchive(ctx context.Context, path, snapshot string, now time.Time) (err error) {
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

	if err := addFile(ctx, tw, dbEntry, snapshot, now); err != nil {
		return errors.Join(err, zw.Close())
	}

	if b.cfg.SecretKey != "" {
		err := addFile(ctx, tw, secretKeyEntry, b.cfg.SecretKey, now)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return errors.Join(err, zw.Close())
		}
	}

	if err := errors.Join(tw.Close(), zw.Close(), f.Sync()); err != nil {
		return fmt.Errorf("write archive: %w", err)
	}

	return nil
}

// addFile streams the regular file at path into tw as name, until ctx is done.
func addFile(ctx context.Context, tw *tar.Writer, name, path string, now time.Time) (err error) {
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

	if _, err := io.Copy(tw, ctxReader{ctx: ctx, r: f}); err != nil {
		return fmt.Errorf("archive %s: %w", name, err)
	}

	return nil
}

// ctxReader reads r until ctx is done.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}

	return c.r.Read(p)
}

// startBackup removes the temporary directories of crashed backups from dir, then creates the
// temporary directory of a new backup, with the permissions 0700 of os.MkdirTemp, since the
// snapshot has those of a new file, and locks it. It returns the directory and the function that
// unlocks and removes it.
//
// Both steps run under the lock of dir, so that a backup never removes the directory of another
// one that has not locked it yet.
func startBackup(ctx context.Context, dir string) (string, func() error, error) {
	guard := flock.New(filepath.Join(dir, guardLockFile))

	if _, err := guard.TryLockContext(ctx, guardRetryDelay); err != nil {
		return "", nil, fmt.Errorf("lock backups directory: %w", err)
	}

	tmpDir, release, err := createTmpDir(dir)

	if err := errors.Join(err, guard.Unlock()); err != nil {
		if release != nil {
			err = errors.Join(err, release())
		}

		return "", nil, err
	}

	return tmpDir, release, nil
}

// createTmpDir removes the temporary directories of crashed backups from dir and creates the one of
// a new backup, locked.
func createTmpDir(dir string) (string, func() error, error) {
	if err := removeCrashed(dir); err != nil {
		return "", nil, err
	}

	tmpDir, err := os.MkdirTemp(dir, tmpPrefix+"*")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary directory: %w", err)
	}

	lock, err := lockTmpDir(tmpDir)
	if err == nil && lock == nil {
		err = errors.New("taken by another backup")
	}

	if err != nil {
		return "", nil, errors.Join(fmt.Errorf("lock temporary directory: %w", err), os.RemoveAll(tmpDir))
	}

	return tmpDir, func() error { return errors.Join(lock.Unlock(), os.RemoveAll(tmpDir)) }, nil
}

// removeCrashed removes the temporary directories of dir whose backup no longer runs: the operating
// system released their lock when the process ended.
func removeCrashed(dir string) error {
	tmpDirs, err := filepath.Glob(filepath.Join(dir, tmpPrefix+"*"))
	if err != nil {
		return fmt.Errorf("find temporary files: %w", err)
	}

	for _, tmpDir := range tmpDirs {
		lock, err := lockTmpDir(tmpDir)
		if err != nil {
			return fmt.Errorf("remove temporary files %s: %w", tmpDir, err)
		}

		if lock == nil {
			continue
		}

		if err := errors.Join(lock.Unlock(), os.RemoveAll(tmpDir)); err != nil {
			return fmt.Errorf("remove temporary files %s: %w", tmpDir, err)
		}
	}

	return nil
}

// lockTmpDir takes the lock of the temporary directory tmpDir without waiting. It returns nil when
// the backup of tmpDir holds it.
func lockTmpDir(tmpDir string) (*flock.Flock, error) {
	lock := flock.New(filepath.Join(tmpDir, tmpLockFile))

	locked, err := lock.TryLock()
	if err != nil || !locked {
		return nil, err
	}

	return lock, nil
}
