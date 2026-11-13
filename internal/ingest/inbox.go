package ingest

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

const (
	// InboxFailedRetention is how long the inbox file of a failed import is kept, for a retry or a
	// diagnosis (Ingestion pipeline, Data and API notes).
	InboxFailedRetention = 7 * 24 * time.Hour
	// InboxOrphanAge is the age after which an inbox file that no import needs is removed. A push
	// writes its file before it commits its import, so a younger file may still get one.
	InboxOrphanAge = time.Hour
)

// inboxTempPattern names the temporary files that Write renames once they are complete.
const inboxTempPattern = ".push-*"

// errInboxName is the error of a name that is not the one of a file of the inbox.
var errInboxName = errors.New("not the name of an inbox file")

// Inbox holds the bodies of pushes, each in a file of one directory, from the request that accepts
// a push until its import no longer needs it. It is safe for concurrent use.
type Inbox struct {
	dir string
	clk clock.Clock
	// wrap wraps the writer of each file that Write writes, so that a test can fill the disk.
	wrap func(io.Writer) io.Writer
}

// NewInbox returns the inbox of the directory dir, which exists.
func NewInbox(dir string, clk clock.Clock) *Inbox {
	return &Inbox{dir: dir, clk: clk, wrap: func(w io.Writer) io.Writer { return w }}
}

// Write stores body durably in a new file of the inbox, and returns its name, a random one: it writes
// a temporary file, syncs it, renames it and syncs the directory, so that a crash never leaves a
// partial file under a final name. It stops when ctx is done. On any failure it removes what it
// wrote and returns the error, which wraps the error of body when body fails, and syscall.ENOSPC
// when the disk is full.
func (i *Inbox) Write(ctx context.Context, body io.Reader) (name string, err error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("write inbox file: %w", err)
	}

	f, err := os.CreateTemp(i.dir, inboxTempPattern)
	if err != nil {
		return "", fmt.Errorf("create inbox file: %w", err)
	}

	temp := f.Name()

	defer func() {
		if err != nil {
			err = errors.Join(err, removeIfExists(temp))
		}
	}()

	if _, err := io.Copy(i.wrap(f), contextReader{ctx: ctx, r: body}); err != nil {
		return "", errors.Join(fmt.Errorf("write inbox file: %w", err), f.Close())
	}

	if err := f.Sync(); err != nil {
		return "", errors.Join(fmt.Errorf("sync inbox file: %w", err), f.Close())
	}

	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close inbox file: %w", err)
	}

	name = rand.Text()
	final := filepath.Join(i.dir, name)

	if err := os.Rename(temp, final); err != nil {
		return "", fmt.Errorf("rename inbox file: %w", err)
	}

	if err := syncDir(i.dir); err != nil {
		return "", errors.Join(err, removeIfExists(final))
	}

	return name, nil
}

// Open opens the inbox file of name for reading.
func (i *Inbox) Open(ctx context.Context, name string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("open inbox file: %w", err)
	}

	path, err := i.path(name)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path) //nolint:gosec // G304: path checks that name is a file of the inbox
	if err != nil {
		return nil, fmt.Errorf("open inbox file: %w", err)
	}

	return f, nil
}

// Remove removes the inbox file of name. A file already gone is not an error, so that removing a
// file twice is harmless.
func (i *Inbox) Remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("remove inbox file: %w", err)
	}

	path, err := i.path(name)
	if err != nil {
		return err
	}

	return removeIfExists(path)
}

// Cleanup removes the files of the imports that failed more than InboxFailedRetention ago, and the
// files without import, temporary ones included, once they are older than InboxOrphanAge. It keeps
// the files of the other imports. It is the handler of the daily job cleanup.inbox.
func (i *Inbox) Cleanup(ctx context.Context, imports store.ImportsRepository) error {
	now := i.clk.Now()

	names, err := imports.InboxFilesKept(ctx, now.Add(-InboxFailedRetention))
	if err != nil {
		return err
	}

	kept := make(map[string]bool, len(names))
	for _, name := range names {
		kept[name] = true
	}

	entries, err := os.ReadDir(i.dir)
	if err != nil {
		return fmt.Errorf("list inbox: %w", err)
	}

	var errs []error

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}

		if !entry.Type().IsRegular() || kept[entry.Name()] {
			continue
		}

		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}

		if err != nil {
			errs = append(errs, fmt.Errorf("stat inbox file: %w", err))

			continue
		}

		if now.Sub(info.ModTime()) < InboxOrphanAge {
			continue
		}

		if err := removeIfExists(filepath.Join(i.dir, entry.Name())); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// path returns the path of the inbox file of name, or errInboxName when name is not the one of a
// file of the inbox directory.
func (i *Inbox) path(name string) (string, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return "", fmt.Errorf("inbox file %q: %w", name, errInboxName)
	}

	return filepath.Join(i.dir, name), nil
}

// removeIfExists removes the file at path, and succeeds when it is already gone.
func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove inbox file: %w", err)
	}

	return nil
}

// syncDir syncs the directory dir, so that a rename in it survives a crash.
func syncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // G304: the inbox directory
	if err != nil {
		return fmt.Errorf("open inbox: %w", err)
	}

	if err := d.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync inbox: %w", err), d.Close())
	}

	if err := d.Close(); err != nil {
		return fmt.Errorf("close inbox: %w", err)
	}

	return nil
}

// contextReader reads r until ctx is done.
type contextReader struct {
	ctx context.Context //nolint:containedctx // a reader that io.Copy calls has no other way to get it
	r   io.Reader
}

func (c contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}

	return c.r.Read(p)
}
