package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store"
)

// XMLImport is an import of an Apple Health export from the command line (FR-10 of XML import).
type XMLImport struct {
	// Login is the username of the user who imports, in any case.
	Login string
	// Path is the absolute path of the export, a zip or a bare export.xml.
	Path string
	// Waiting is called once when the import waits for another import of the user to end.
	Waiting func()
	// Progress is called with the progress of the import, from 0 to 1, at most every 5 seconds.
	Progress func(progress float64)
}

// ImportXML imports the export of req for its user, in this process.
func ImportXML(_ context.Context, _ config.Config, _ XMLImport, _ *slog.Logger) (store.Import, error) {
	return store.Import{}, errors.New("not implemented")
}
