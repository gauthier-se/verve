package native

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

// KindImport is the job of a native push (Ingestion pipeline, Data and API notes).
const KindImport = "import.native"

// JobDeps is what the import.native job needs.
type JobDeps = ingest.ImportJobDeps

// Job returns the import.native job of the import of importID of userID. Its serial key runs the
// imports of the user one at a time, in arrival order (FR-4 of Ingestion pipeline).
func Job(userID, importID int64) (store.NewJob, error) {
	return ingest.NewImportJob(KindImport, userID, ingest.JobPayload{ImportID: importID})
}

// Register adds the import.native job to registry. The job runs the payload of the inbox file of its
// import through the pipeline, with the lifecycle of every import job (see ingest.RegisterImport): a
// payload that fails as a whole fails the import at once, with the reason as its sample error
// message.
func Register(registry *jobs.Registry, deps JobDeps) {
	ingest.RegisterImport(registry, KindImport, deps, openRecords)
}

// openRecords reads the payload of f, stores the client of imp, and returns its records.
func openRecords(ctx context.Context, imports store.ImportsRepository, imp store.Import, f *os.File) (iter.Seq2[ingest.Record, error], error) {
	payload, err := Open(ctx, f)
	if errors.Is(err, ErrInvalidPayload) {
		// Its message names the reason, and never the import, which the report belongs to.
		return nil, ingest.Reject(err.Error(), err)
	}

	if err != nil {
		return nil, err
	}

	if err := imports.SetClient(ctx, imp.UserID, imp.ID, payload.Client().metadata()); err != nil {
		return nil, fmt.Errorf("store client of import %d: %w", imp.ID, err)
	}

	return payload.Records(ctx), nil
}

// metadata returns the client metadata of the import of a payload sent by c, without the fields
// that c leaves out.
func (c Client) metadata() map[string]string {
	client := map[string]string{"name": c.Name, "version": c.Version}

	if c.DeviceModel != "" {
		client["deviceModel"] = c.DeviceModel
	}

	if c.OSVersion != "" {
		client["osVersion"] = c.OSVersion
	}

	return client
}
