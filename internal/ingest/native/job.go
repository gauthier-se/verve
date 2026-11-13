package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"iter"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
)

// KindImport is the job of a native push (Ingestion pipeline, Data and API notes).
const KindImport = "import.native"

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

// errLastAttempt is the sample error message of an import whose job failed its last attempt. It is a
// fixed text: the error of the attempt is the last error of the job, which admins see.
var errLastAttempt = errors.New("the import stopped after the last attempt of its job failed")

// Job returns the import.native job of the import of importID of userID. Its serial key runs the
// imports of the user one at a time, in arrival order (FR-4 of Ingestion pipeline).
func Job(userID, importID int64) (store.NewJob, error) {
	payload, err := json.Marshal(ingest.JobPayload{ImportID: importID})
	if err != nil {
		return store.NewJob{}, fmt.Errorf("encode payload of job kind %q: %w", KindImport, err)
	}

	return store.NewJob{Kind: KindImport, Payload: payload, UserID: userID, SerialKey: ingest.SerialKey(userID)}, nil
}

// Register adds the import.native job to registry. The job runs the payload of the inbox file of its
// import through the pipeline, and ends the import:
//   - completed once every record is processed, its file deleted,
//   - failed, without retry, for a payload that fails as a whole or a file that is gone, with the
//     reason as its sample error message, its file kept for a diagnosis, and once the last attempt
//     of its job fails,
//   - cancelled at the next batch boundary once its job is cancelled, its file deleted.
//
// The batches committed before the end stay. A job stopped otherwise, such as by a shutdown, leaves
// the import running, and its next run resumes it from the stored position (FR-5).
func Register(registry *jobs.Registry, deps JobDeps) {
	jobs.Register(registry, KindImport, jobs.KindOptions{}, func(ctx context.Context, job jobs.Job[ingest.JobPayload]) error {
		return runImport(ctx, deps, job)
	})
}

// runImport runs the import of job.
func runImport(ctx context.Context, deps JobDeps, job jobs.Job[ingest.JobPayload]) error {
	imports := deps.Store.Imports()
	userID, importID := job.UserID, job.Payload.ImportID

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
			return deps.Inbox.Remove(ctx, imp.InboxFile)
		}

		return nil
	}

	imp, err = start(ctx, deps.Store, userID, importID)
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
	case job.LastAttempt && ctx.Err() == nil:
		// The job fails for good: the import would otherwise stay running.
		return errors.Join(err, fail(ctx, deps.Store, imp, errLastAttempt))
	default:
		return err
	}
}

// process reads the payload of the inbox file of imp, stores its client, and runs its records
// through the pipeline.
func process(ctx context.Context, deps JobDeps, imp store.Import) (err error) {
	f, err := deps.Inbox.Open(ctx, imp.InboxFile)
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

// start moves the import of importID of userID to running, and returns it. A queued import records
// import.started in the same transaction, so that a resumed import, already running, records it
// once. It returns store.ErrConflict for an import that ended.
func start(ctx context.Context, s store.Store, userID, importID int64) (store.Import, error) {
	var started store.Import

	err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		imp, err := tx.Imports().Get(ctx, userID, importID)
		if err != nil {
			return err
		}

		started, err = tx.Imports().SetStatus(ctx, userID, importID, store.ImportRunning)
		if err != nil {
			return err
		}

		if imp.Status != store.ImportQueued {
			return nil
		}

		return audit.Record(ctx, tx.AuditEvents(), audit.ImportStarted(userID, importID, imp.Origin))
	})

	return started, err
}

// finish ends imp with status, completed or cancelled, recording its end in the same transaction,
// and removes its inbox file, which it no longer needs.
func finish(ctx context.Context, deps JobDeps, imp store.Import, status store.ImportStatus) error {
	err := deps.Store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		ended, err := tx.Imports().SetStatus(ctx, imp.UserID, imp.ID, status)
		if err != nil {
			return err
		}

		return audit.Record(ctx, tx.AuditEvents(), endEvent(ended))
	})
	if err != nil {
		return fmt.Errorf("end import %d %s: %w", imp.ID, status, err)
	}

	return deps.Inbox.Remove(ctx, imp.InboxFile)
}

// fail ends imp failed, with the message of cause as a sample error message, and records its end,
// in one transaction. The position and the counters of the batches committed since imp was read
// stay. Its inbox file stays, for a retry or a diagnosis.
func fail(ctx context.Context, s store.Store, imp store.Import, cause error) error {
	err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		current, err := tx.Imports().Get(ctx, imp.UserID, imp.ID)
		if err != nil {
			return err
		}

		report := current.Report
		report.Errors = append(report.Errors, cause.Error())

		progress := store.ImportProgress{Position: current.Position, Report: report}
		if err := tx.Imports().SaveProgress(ctx, imp.UserID, imp.ID, progress); err != nil {
			return err
		}

		ended, err := tx.Imports().SetStatus(ctx, imp.UserID, imp.ID, store.ImportFailed)
		if err != nil {
			return err
		}

		return audit.Record(ctx, tx.AuditEvents(), endEvent(ended))
	})
	if err != nil {
		return fmt.Errorf("fail import %d: %w", imp.ID, err)
	}

	return nil
}

// endEvent returns the audit event of the end of imp, which ended completed, failed or cancelled,
// with the total counters of its report.
func endEvent(imp store.Import) audit.Event {
	switch imp.Status {
	case store.ImportCompleted:
		return audit.ImportCompleted(imp.UserID, imp.ID, imp.Origin, imp.Report.Total)
	case store.ImportFailed:
		return audit.ImportFailed(imp.UserID, imp.ID, imp.Origin, imp.Report.Total)
	default:
		return audit.ImportCancelled(imp.UserID, imp.ID, imp.Origin, imp.Report.Total)
	}
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
