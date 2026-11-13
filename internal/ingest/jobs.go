package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/gauthier-se/verve/internal/store"
)

// SerialKey returns the serial key of the import jobs of the user of userID, shared by every import
// kind, so that the imports of a user run one at a time, in arrival order (FR-4 of Ingestion
// pipeline).
func SerialKey(userID int64) string {
	return "import:" + strconv.FormatInt(userID, 10)
}

// JobPayload is the payload of every import job, whatever its kind (Ingestion pipeline, Data and
// API notes).
type JobPayload struct {
	ImportID int64 `json:"importId"`
}

// JobOfImport returns the queued or running job of the import of importID of userID, the job of the
// serial key of the user whose payload names the import, or store.ErrNotFound when the import has
// none, such as an import whose job ended.
func JobOfImport(ctx context.Context, jobs store.JobsRepository, userID, importID int64) (store.Job, error) {
	active, err := jobs.ActiveJobs(ctx, SerialKey(userID))
	if err != nil {
		return store.Job{}, fmt.Errorf("find job of import %d: %w", importID, err)
	}

	for _, job := range active {
		var payload JobPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return store.Job{}, fmt.Errorf("find job of import %d: decode payload of job %d: %w", importID, job.ID, err)
		}

		if payload.ImportID == importID {
			return job, nil
		}
	}

	return store.Job{}, fmt.Errorf("find job of import %d: %w", importID, store.ErrNotFound)
}
