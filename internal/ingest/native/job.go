package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"iter"

	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
)

// KindImport is the job of a native push (Ingestion pipeline, Data and API notes).
const KindImport = "import.native"

// ImportPayload is the payload of the import.native job.
type ImportPayload struct {
	ImportID int64 `json:"importId"`
}

// Pipeline runs the records of an import, as *ingest.Pipeline does.
type Pipeline interface {
	Run(ctx context.Context, userID, importID int64, records iter.Seq2[ingest.Record, error]) error
}

// JobDeps is what the import.native job needs.
type JobDeps struct {
	Store    store.Store
	Pipeline Pipeline
	// Inbox holds the payloads of the pushes.
	Inbox *ingest.Inbox
}

// errMissingPayload is the error of an import whose inbox file is gone. It is a fixed text, the
// sample error message of the import.
var errMissingPayload = errors.New("the payload of the import is missing from the inbox")

// Job returns the import.native job of the import of importID of userID. Its serial key runs the
// imports of the user one at a time, in arrival order (FR-4 of Ingestion pipeline).
func Job(userID, importID int64) (store.NewJob, error) {
	payload, err := json.Marshal(ImportPayload{ImportID: importID})
	if err != nil {
		return store.NewJob{}, fmt.Errorf("encode payload of job kind %q: %w", KindImport, err)
	}

	return store.NewJob{Kind: KindImport, Payload: payload, UserID: userID, SerialKey: ingest.SerialKey(userID)}, nil
}

// Register adds the import.native job to registry. The job runs the payload of the inbox file of its
// import through the pipeline, and ends the import:
//   - completed once every record is processed, its file deleted,
//   - failed, without retry, for a payload that fails as a whole or a file that is gone, with the
//     reason as its sample error message, its file kept for a diagnosis,
//   - cancelled at the next batch boundary once its job is cancelled, its file deleted.
//
// The batches committed before the end stay. A job stopped otherwise, such as by a shutdown, leaves
// the import running, and its next run resumes it from the stored position (FR-5).
func Register(registry *jobs.Registry, deps JobDeps) {
	jobs.Register(registry, KindImport, jobs.KindOptions{}, func(ctx context.Context, job jobs.Job[ImportPayload]) error {
		return runImport(ctx, deps, job.UserID, job.Payload.ImportID)
	})
}

// runImport runs the import of importID of userID.
func runImport(ctx context.Context, deps JobDeps, userID, importID int64) error {
	imports := deps.Store.Imports()

	imp, err := imports.Get(ctx, userID, importID)
	if errors.Is(err, store.ErrNotFound) {
		return jobs.Permanent(err)
	}

	if err != nil {
		return err
	}

	if imp.Status.Finished() {
		// Ended without its job, such as an import cancelled while queued: a completed or cancelled
		// import no longer needs its file.
		if imp.Status != store.ImportFailed {
			return deps.Inbox.Remove(imp.InboxFile)
		}

		return nil
	}

	imp, err = imports.SetStatus(ctx, userID, importID, store.ImportRunning)
	if errors.Is(err, store.ErrConflict) {
		// Ended since it was read.
		return nil
	}

	if err != nil {
		return fmt.Errorf("start import %d: %w", importID, err)
	}

	err = process(ctx, deps, imp)

	switch {
	case err == nil:
		return finish(ctx, deps, imp, store.ImportCompleted)
	case jobs.Cancelled(ctx):
		// The writes that end the import run once the handler context is done.
		return errors.Join(err, finish(context.WithoutCancel(ctx), deps, imp, store.ImportCancelled))
	case errors.Is(err, ErrInvalidPayload), errors.Is(err, errMissingPayload):
		return jobs.Permanent(errors.Join(err, fail(ctx, deps.Store, imp, err)))
	default:
		return err
	}
}

// process reads the payload of the inbox file of imp, stores its client, and runs its records
// through the pipeline.
func process(ctx context.Context, deps JobDeps, imp store.Import) (err error) {
	f, err := deps.Inbox.Open(imp.InboxFile)
	if errors.Is(err, fs.ErrNotExist) {
		return errMissingPayload
	}

	if err != nil {
		return err
	}

	defer func() { err = errors.Join(err, f.Close()) }()

	// Its error is the sample error message of a payload that fails as a whole: it names the reason,
	// and never the import, which the report belongs to.
	payload, err := Open(ctx, f)
	if err != nil {
		return err
	}

	if err := deps.Store.Imports().SetClient(ctx, imp.UserID, imp.ID, payload.Client().metadata()); err != nil {
		return fmt.Errorf("store client of import %d: %w", imp.ID, err)
	}

	return deps.Pipeline.Run(ctx, imp.UserID, imp.ID, payload.Records(ctx))
}

// finish ends imp with status, completed or cancelled, and removes its inbox file, which it no
// longer needs.
func finish(ctx context.Context, deps JobDeps, imp store.Import, status store.ImportStatus) error {
	if _, err := deps.Store.Imports().SetStatus(ctx, imp.UserID, imp.ID, status); err != nil {
		return fmt.Errorf("end import %d %s: %w", imp.ID, status, err)
	}

	return deps.Inbox.Remove(imp.InboxFile)
}

// fail ends imp failed, with the message of cause as a sample error message, in one transaction. Its
// inbox file stays, for a retry or a diagnosis.
func fail(ctx context.Context, s store.Store, imp store.Import, cause error) error {
	err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		report := imp.Report
		report.Errors = append(report.Errors, cause.Error())

		progress := store.ImportProgress{Position: imp.Position, Report: report}
		if err := tx.Imports().SaveProgress(ctx, imp.UserID, imp.ID, progress); err != nil {
			return err
		}

		_, err := tx.Imports().SetStatus(ctx, imp.UserID, imp.ID, store.ImportFailed)

		return err
	})
	if err != nil {
		return fmt.Errorf("fail import %d: %w", imp.ID, err)
	}

	return nil
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
