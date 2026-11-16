package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"os"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
)

// ImportPipeline runs the records of an import, as *Pipeline does.
type ImportPipeline interface {
	Run(ctx context.Context, userID, importID int64, records iter.Seq2[Record, error]) error
}

// ImportJobDeps is what an import job needs, whatever its kind.
type ImportJobDeps struct {
	Store    store.Store
	Pipeline ImportPipeline
	// Inbox holds the payloads of the imports that have an inbox file.
	Inbox *Inbox
}

// OpenRecords opens the records of the running import imp from f, the file of its payload, which
// stays open while they are read. It may store on imp, through imports, what the payload tells,
// such as its client. An error that Reject returns, from OpenRecords or from its records, fails the
// import at once, without retry.
type OpenRecords func(ctx context.Context, imports store.ImportsRepository, imp store.Import, f *os.File) (iter.Seq2[Record, error], error)

// Reject returns err as the error of a payload that fails as a whole, such as a file in another
// format or a truncated one: it fails its import at once, without retry, with reason as its sample
// error message. The reason is a fixed text, which never quotes a value.
func Reject(reason string, err error) error {
	return &rejectedError{reason: reason, err: err}
}

// rejectedError is the error of a payload that fails as a whole.
type rejectedError struct {
	reason string
	err    error
}

func (e *rejectedError) Error() string { return e.err.Error() }

func (e *rejectedError) Unwrap() error { return e.err }

// errMissingPayload is the error of an import whose inbox file is gone. It is a fixed text, the
// sample error message of the import.
var errMissingPayload = errors.New("the payload of the import is missing from the inbox")

// errMissingFile is the error of an import whose file, at the path of its job, is gone. It is a
// fixed text, which never quotes the path.
var errMissingFile = errors.New("the file of the import is missing")

// errLastAttempt is the sample error message of an import whose job failed its last attempt. It is a
// fixed text: the error of the attempt is the last error of the job, which admins see.
var errLastAttempt = errors.New("the import stopped after the last attempt of its job failed")

// errAbandoned is the sample error message of an import whose job was abandoned after its lost
// leases (FR-4 of Job worker). It is a fixed text, as errLastAttempt is.
var errAbandoned = errors.New("the import stopped after its job was abandoned: its process stopped 5 times while running it")

// NewImportJob returns the job of kind of an import of userID, with payload. Its serial key runs
// the imports of the user one at a time, in arrival order (FR-4 of Ingestion pipeline).
func NewImportJob(kind string, userID int64, payload JobPayload) (store.NewJob, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return store.NewJob{}, fmt.Errorf("encode payload of job kind %q: %w", kind, err)
	}

	return store.NewJob{Kind: kind, Payload: encoded, UserID: userID, SerialKey: SerialKey(userID)}, nil
}

// RegisterImport adds the import job of kind to registry. The job opens the file of its import,
// the file at the path of its payload when it has one and its inbox file otherwise, runs the
// records that open reads from it through the pipeline, and ends the import:
//   - completed once every record is processed, its inbox file deleted,
//   - failed, without retry, for a payload that open rejects or a file that is gone, with the
//     reason as its sample error message, its inbox file kept for a diagnosis, and once the last
//     attempt of its job fails,
//   - cancelled at the next batch boundary once its job is cancelled, its inbox file deleted, also
//     when the run fails or stops, or the import has not started, before the heartbeat delivers
//     the cancellation.
//
// A claim that ends the job without a handler ends the import too, in the same transaction: failed
// once the job is abandoned after its lost leases, its inbox file kept, and cancelled for a
// cancellation that no handler can confirm, its inbox file deleted. The batches committed before
// the end stay. A job stopped otherwise, such as by a shutdown, leaves the import running, and its
// next run resumes it from the stored position (FR-5). The file at the path of a job is never
// removed.
func RegisterImport(registry *jobs.Registry, kind string, deps ImportJobDeps, open OpenRecords) {
	run := importRun{deps: deps, open: open}

	k := jobs.Register(registry, kind, jobs.KindOptions{}, run.run)
	jobs.OnEndedByClaim(registry, k, run.endedByClaim)
}

// importRun runs the import jobs of a kind.
type importRun struct {
	deps ImportJobDeps
	open OpenRecords
}

// run runs the import of job.
func (r importRun) run(ctx context.Context, job jobs.Job[JobPayload]) error {
	s := r.deps.Store
	userID, importID := job.UserID, job.Payload.ImportID

	imp, err := s.Imports().Get(ctx, userID, importID)
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
			return r.removeInboxFile(ctx, imp)
		}

		return nil
	}

	started, err := start(ctx, s, userID, importID)
	if errors.Is(err, store.ErrConflict) {
		// Ended since it was read.
		return nil
	}

	if err != nil {
		err = fmt.Errorf("start import %d: %w", importID, err)

		if cancelRequested(ctx, s, job.ID) {
			// Cancelled before it started, such as by a cancellation that the heartbeat delivered.
			return errors.Join(err, r.finish(context.WithoutCancel(ctx), imp, store.ImportCancelled))
		}

		return err
	}

	imp = started
	err = r.process(ctx, imp, job.Payload.Path)

	var rejected *rejectedError

	switch {
	case err == nil:
		return r.finish(ctx, imp, store.ImportCompleted)
	case cancelRequested(ctx, s, job.ID):
		// The writes that end the import run once the handler context is done.
		return errors.Join(err, r.finish(context.WithoutCancel(ctx), imp, store.ImportCancelled))
	case errors.As(err, &rejected):
		return jobs.Permanent(errors.Join(err, fail(ctx, s, imp, errors.New(rejected.reason))))
	case errors.Is(err, errMissingPayload), errors.Is(err, errMissingFile):
		return jobs.Permanent(errors.Join(err, fail(ctx, s, imp, err)))
	case job.LastAttempt && ctx.Err() == nil:
		// The job fails for good: the import would otherwise stay running.
		return errors.Join(err, fail(ctx, s, imp, errLastAttempt))
	default:
		return err
	}
}

// process opens the file of imp, at path when it is set and its inbox file otherwise, opens its
// records and runs them through the pipeline.
func (r importRun) process(ctx context.Context, imp store.Import, path string) (err error) {
	f, err := r.openFile(ctx, imp, path)
	if err != nil {
		return err
	}

	defer func() { err = errors.Join(err, f.Close()) }()

	records, err := r.open(ctx, r.deps.Store.Imports(), imp, f)
	if err != nil {
		return fmt.Errorf("open records of import %d: %w", imp.ID, err)
	}

	return r.deps.Pipeline.Run(ctx, imp.UserID, imp.ID, records)
}

// openFile opens the file at path when it is set, and the inbox file of imp otherwise. It returns
// errMissingFile or errMissingPayload for a file that is gone.
func (r importRun) openFile(ctx context.Context, imp store.Import, path string) (*os.File, error) {
	if path != "" {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("open file of import %d: %w", imp.ID, err)
		}

		f, err := os.Open(path) //nolint:gosec // G304: the path of a CLI import, given by the operator
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errMissingFile
		}

		if err != nil {
			return nil, fmt.Errorf("open file of import %d: %w", imp.ID, err)
		}

		return f, nil
	}

	if imp.InboxFile == "" {
		return nil, errMissingPayload
	}

	f, err := r.deps.Inbox.Open(ctx, imp.InboxFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errMissingPayload
	}

	return f, err
}

// removeInboxFile removes the inbox file of imp, when it has one.
func (r importRun) removeInboxFile(ctx context.Context, imp store.Import) error {
	if imp.InboxFile == "" {
		return nil
	}

	return r.deps.Inbox.Remove(ctx, imp.InboxFile)
}

// cancelRequested reports whether the cancellation of the job of jobID was requested: delivered to
// the handler context by the heartbeat, or requested since its last heartbeat. A handler that stops
// otherwise, by a failure or a shutdown, then ends its import itself: its job, queued again, would
// be cancelled without a handler, and the import would stay unfinished. A failed read counts as no
// cancellation, the import then resuming at the next run.
func cancelRequested(ctx context.Context, s store.Store, jobID int64) bool {
	if jobs.Cancelled(ctx) {
		return true
	}

	job, err := s.Jobs().Get(context.WithoutCancel(ctx), jobID)

	return err == nil && job.CancelRequested
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

// endedByClaim ends the import of job, whose job a claim ended with status without a handler,
// through tx: failed with a sample error message once its job is abandoned after its lost leases,
// its inbox file kept for a diagnosis, and cancelled for a cancellation that no handler can
// confirm, its inbox file deleted once committed. An import that ended or is gone changes nothing.
func (r importRun) endedByClaim(ctx context.Context, tx store.Repositories, job jobs.Job[JobPayload], status store.JobStatus) error {
	importID := job.Payload.ImportID

	imp, err := tx.Imports().Get(ctx, job.UserID, importID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("get import %d: %w", importID, err)
	}

	if imp.Status.Finished() {
		return nil
	}

	switch status {
	case store.JobFailed:
		// A report only changes while its import runs: an import whose job never started it, its
		// process dying first, runs before it fails, as its job did.
		if imp.Status == store.ImportQueued {
			if _, err := tx.Imports().SetStatus(ctx, job.UserID, importID, store.ImportRunning); err != nil {
				return fmt.Errorf("start import %d: %w", importID, err)
			}
		}

		if err := failInTx(ctx, tx, imp, errAbandoned); err != nil {
			return fmt.Errorf("fail import %d: %w", importID, err)
		}
	case store.JobCancelled:
		if err := endInTx(ctx, tx, imp, store.ImportCancelled); err != nil {
			return fmt.Errorf("end import %d %s: %w", importID, store.ImportCancelled, err)
		}

		tx.AfterCommit(func() {
			// A file that the removal leaves behind is removed by cleanup.inbox: a cancelled import
			// needs it no more.
			_ = r.removeInboxFile(context.WithoutCancel(ctx), imp)
		})
	}

	return nil
}

// finish ends imp with status, completed or cancelled, recording its end in the same transaction,
// and removes its inbox file, which it no longer needs.
func (r importRun) finish(ctx context.Context, imp store.Import, status store.ImportStatus) error {
	err := r.deps.Store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		return endInTx(ctx, tx, imp, status)
	})
	if err != nil {
		return fmt.Errorf("end import %d %s: %w", imp.ID, status, err)
	}

	return r.removeInboxFile(ctx, imp)
}

// endInTx ends imp with status, completed or cancelled, and records its end, through tx.
func endInTx(ctx context.Context, tx store.Repositories, imp store.Import, status store.ImportStatus) error {
	ended, err := tx.Imports().SetStatus(ctx, imp.UserID, imp.ID, status)
	if err != nil {
		return err
	}

	return audit.Record(ctx, tx.AuditEvents(), endEvent(ended))
}

// fail ends imp failed, with the message of cause as a sample error message, and records its end,
// in one transaction (see failInTx).
func fail(ctx context.Context, s store.Store, imp store.Import, cause error) error {
	err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		return failInTx(ctx, tx, imp, cause)
	})
	if err != nil {
		return fmt.Errorf("fail import %d: %w", imp.ID, err)
	}

	return nil
}

// failInTx ends imp failed, with the message of cause as a sample error message, and records its end,
// through tx. The position and the report of the batches committed since imp was read stay. Its
// inbox file stays, for a retry or a diagnosis.
func failInTx(ctx context.Context, tx store.Repositories, imp store.Import, cause error) error {
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
