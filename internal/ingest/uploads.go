package ingest

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
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
}

// NewUploads returns the uploads of the directory dir, which exists on the file system of the
// directory of inbox, so that a completed upload moves into the inbox by a rename.
func NewUploads(dir string, inbox *Inbox, clk clock.Clock) *Uploads {
	return &Uploads{dir: dir, inbox: inbox, clk: clk}
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
