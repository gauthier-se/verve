package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// dataDirPerm is the permission of the data directory and its subdirectories when Verve creates them.
const dataDirPerm = 0o750

// dbFile is the name of the database file in the data directory.
const dbFile = "verve.db"

// Layout holds the paths of the data directory content.
type Layout struct {
	Root    string
	DB      string
	Lock    string
	Uploads string // in-progress resumable uploads
	Inbox   string // raw push payloads waiting for processing
	Exports string // generated user exports
	Backups string // database backups
	Map     string // optional PMTiles and assets
}

func newLayout(root string) Layout {
	return Layout{
		Root:    root,
		DB:      filepath.Join(root, dbFile),
		Lock:    filepath.Join(root, lockFile),
		Uploads: filepath.Join(root, "uploads"),
		Inbox:   filepath.Join(root, "inbox"),
		Exports: filepath.Join(root, "exports"),
		Backups: filepath.Join(root, "backups"),
		Map:     filepath.Join(root, "map"),
	}
}

// EnsureDataDir creates the data directory at root and its subdirectories when they do not
// exist, after checking that root is writable.
func EnsureDataDir(root string) (Layout, error) {
	layout := newLayout(root)

	if err := os.MkdirAll(root, dataDirPerm); err != nil {
		return Layout{}, fmt.Errorf("data directory %s is not writable: %w", root, err)
	}

	if err := checkWritable(root); err != nil {
		return Layout{}, fmt.Errorf("data directory %s is not writable: %w", root, err)
	}

	for _, dir := range []string{layout.Uploads, layout.Inbox, layout.Exports, layout.Backups, layout.Map} {
		if err := os.MkdirAll(dir, dataDirPerm); err != nil {
			return Layout{}, fmt.Errorf("create data directory: %w", err)
		}
	}

	return layout, nil
}

// checkWritable creates and removes a temporary file in dir.
func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return err
	}

	return errors.Join(f.Close(), os.Remove(f.Name()))
}
