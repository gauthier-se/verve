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

	"github.com/gauthier-se/verve/internal/clock"
)

// errUploadName is the error of a name that is not the one of a file of the uploads.
var errUploadName = errors.New("not the name of an upload file")

// Uploads holds the files of the incomplete resumable uploads (FR-2 of XML import), each in a file
// of one directory named by the id of its upload, until it completes into the inbox. It is safe for
// concurrent use on different files.
type Uploads struct {
	dir   string
	inbox *Inbox
	clk   clock.Clock
	// wrap wraps the writer of each write of Append, so that a test can fill the disk.
	wrap func(io.Writer) io.Writer
}

// NewUploads returns the uploads of the directory dir, which exists on the file system of the
// directory of inbox, so that a completed upload moves into the inbox by a rename.
func NewUploads(dir string, inbox *Inbox, clk clock.Clock) *Uploads {
	return &Uploads{dir: dir, inbox: inbox, clk: clk, wrap: func(w io.Writer) io.Writer { return w }}
}

// Create creates the empty file of a new upload, syncs the directory and returns its name, a random
// one, which the upload takes as its id.
func (u *Uploads) Create(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("create upload file: %w", err)
	}

	name := rand.Text()
	path := filepath.Join(u.dir, name)

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: a random name in the uploads directory
	if err != nil {
		return "", fmt.Errorf("create upload file: %w", err)
	}

	if err := f.Close(); err != nil {
		return "", errors.Join(fmt.Errorf("close upload file: %w", err), removeUpload(path))
	}

	if err := syncDir(u.dir); err != nil {
		return "", errors.Join(err, removeUpload(path))
	}

	return name, nil
}

// Exists reports whether the file of name exists.
func (u *Uploads) Exists(ctx context.Context, name string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("stat upload file: %w", err)
	}

	path, err := u.path(name)
	if err != nil {
		return false, err
	}

	_, err = os.Stat(path)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("stat upload file: %w", err)
	default:
		return true, nil
	}
}

// Append writes body at offset of the file of name, over the bytes past offset that an earlier
// write left, and syncs the file. It returns the number of bytes written and synced, also when it
// fails: when body fails, such as a cut request, or when the disk fills up, its error wraps the error
// of body or syscall.ENOSPC, and the bytes before stay. It returns an error wrapping fs.ErrNotExist
// when the file is gone. It stops when ctx is done.
func (u *Uploads) Append(ctx context.Context, name string, offset int64, body io.Reader) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("append to upload file: %w", err)
	}

	path, err := u.path(name)
	if err != nil {
		return 0, err
	}

	f, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // G304: path checks that name is a file of the uploads
	if err != nil {
		return 0, fmt.Errorf("open upload file: %w", err)
	}

	if err := seekTo(f, offset); err != nil {
		return 0, errors.Join(err, f.Close())
	}

	written, err := io.Copy(u.wrap(f), contextReader{ctx: ctx, r: body})
	if err != nil {
		err = fmt.Errorf("append to upload file: %w", err)
	}

	if syncErr := f.Sync(); syncErr != nil {
		return 0, errors.Join(err, fmt.Errorf("sync upload file: %w", syncErr), f.Close())
	}

	if closeErr := f.Close(); closeErr != nil {
		return 0, errors.Join(err, fmt.Errorf("close upload file: %w", closeErr))
	}

	return written, err
}

// seekTo truncates f at offset and moves to it, so that a write replaces the bytes past offset. It
// refuses a file shorter than offset, which lost bytes that were synced.
func seekTo(f *os.File, offset int64) error {
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat upload file: %w", err)
	}

	if info.Size() < offset {
		return fmt.Errorf("upload file of %d bytes is shorter than its offset %d", info.Size(), offset)
	}

	if err := f.Truncate(offset); err != nil {
		return fmt.Errorf("truncate upload file: %w", err)
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("seek upload file: %w", err)
	}

	return nil
}

// Complete moves the file of name, whose writes are synced, into the inbox under a new random name,
// which it returns, and syncs both directories, so that a crash leaves the file in one of them
// (FR-4 of XML import).
func (u *Uploads) Complete(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("complete upload file: %w", err)
	}

	path, err := u.path(name)
	if err != nil {
		return "", err
	}

	inboxName := rand.Text()

	if err := os.Rename(path, filepath.Join(u.inbox.dir, inboxName)); err != nil {
		return "", fmt.Errorf("move upload file into the inbox: %w", err)
	}

	if err := syncDir(u.inbox.dir); err != nil {
		return "", err
	}

	if err := syncDir(u.dir); err != nil {
		return "", err
	}

	return inboxName, nil
}

// Remove removes the file of name. A file already gone is not an error, so that removing a file
// twice is harmless.
func (u *Uploads) Remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("remove upload file: %w", err)
	}

	path, err := u.path(name)
	if err != nil {
		return err
	}

	return removeUpload(path)
}

// path returns the path of the file of name, or errUploadName when name is not the one of a file of
// the uploads directory.
func (u *Uploads) path(name string) (string, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return "", fmt.Errorf("upload file %q: %w", name, errUploadName)
	}

	return filepath.Join(u.dir, name), nil
}

// removeUpload removes the upload file at path, and succeeds when it is already gone.
func removeUpload(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove upload file: %w", err)
	}

	return nil
}
