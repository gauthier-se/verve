package applexml

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"os"

	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
)

// KindImport is the job of an XML import (Ingestion pipeline, Data and API notes).
const KindImport = "import.xml"

// Job returns the import.xml job of the import of importID of userID. Its payload has path, the
// absolute path of the file of a CLI import, or no path for an import that reads its inbox file.
// Its serial key runs the imports of the user one at a time, in arrival order (FR-4 of Ingestion
// pipeline).
func Job(userID, importID int64, path string) (store.NewJob, error) {
	return ingest.NewImportJob(KindImport, userID, ingest.JobPayload{ImportID: importID, Path: path})
}

// Register adds the import.xml job to registry. The job reads the export of its import, a zip or a
// bare export.xml, with the lifecycle of every import job (see ingest.RegisterImport): a file that
// is not an export, a truncated document, malformed XML or a corrupted zip fails the import at
// once, with the fixed message of its cause as its sample error message.
func Register(registry *jobs.Registry, deps ingest.ImportJobDeps) {
	ingest.RegisterImport(registry, KindImport, deps, openRecords)
}

// openRecords opens the export of f, stores its size on imp, which its progress is computed
// against, and returns its records.
func openRecords(ctx context.Context, imports store.ImportsRepository, imp store.Import, f *os.File) (iter.Seq2[ingest.Record, error], error) {
	export, err := Open(ctx, f)
	if err != nil {
		return nil, reject(err)
	}

	if size := export.Size(); size > 0 {
		if err := imports.SetExportSize(ctx, imp.UserID, imp.ID, size); err != nil {
			return nil, fmt.Errorf("store export size of import %d: %w", imp.ID, err)
		}
	}

	return func(yield func(ingest.Record, error) bool) {
		for r, err := range export.Records(ctx) {
			if err != nil {
				err = reject(err)
			}

			if !yield(r, err) {
				return
			}
		}
	}, nil
}

// reject returns err as the error of an export that fails as a whole, with the fixed message of its
// cause, and err otherwise, such as a read error of the disk, which is retried.
func reject(err error) error {
	for _, cause := range []error{ErrNotAnExport, ErrTruncated, ErrNotWellFormed, ErrCorruptedZip} {
		if errors.Is(err, cause) {
			return ingest.Reject(cause.Error(), err)
		}
	}

	return err
}
