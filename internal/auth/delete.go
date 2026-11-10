package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/store"
)

// KindDeleteAccount is the kind of the job that removes an account and its data (FR-13 of accounts
// and administration). The job has no user of its own, so that it does not remove itself.
const KindDeleteAccount = "account.delete"

// deleteBatchSize is the most rows of a table that one transaction of an account deletion removes,
// so that it never holds the writer for long.
const deleteBatchSize = 1000

var (
	// ErrConfirmationMismatch is returned when the confirmation of an admin deletion is not the exact
	// username of the account. Its message is the stable code that the API answers with.
	ErrConfirmationMismatch = errors.New("confirmation_mismatch")
	// ErrUserNotDeleting is returned by the deletion job for an account that no deletion was
	// requested for. No retry fixes it.
	ErrUserNotDeleting = errors.New("account not being deleted")
	// ErrJobsStillRunning is returned by the deletion job while jobs of the account have not stopped
	// since their cancellation. A later run of the job finds them stopped.
	ErrJobsStillRunning = errors.New("jobs of the account still running")
)

// DeleteAccountPayload is the payload of the account.delete job.
type DeleteAccountPayload struct {
	UserID int64 `json:"userId"`
}

// JobQueue enqueues a job through repos, the store or the repositories of a WithTx, so that the job
// commits with the action that queues it.
type JobQueue interface {
	Enqueue(ctx context.Context, repos store.Repositories, job store.NewJob) (store.Job, error)
}

// storeQueue writes jobs to the store without waking a worker, which finds them at its next poll.
type storeQueue struct{}

func (storeQueue) Enqueue(ctx context.Context, repos store.Repositories, job store.NewJob) (store.Job, error) {
	return repos.Jobs().Enqueue(ctx, job)
}

// OwnDeletion is a user deleting their own account, with their password.
type OwnDeletion struct {
	UserID   int64
	Password string
}

// AdminDeletion is an admin deleting an account, with its exact username as confirmation.
type AdminDeletion struct {
	ActorID      int64
	UserID       int64
	Confirmation string
}

// Deletions requests the deletion of accounts (FR-11 to FR-13 of accounts and administration): the
// account is disabled at once, and the account.delete job, which DeleteAccount runs, removes it
// with its data.
type Deletions struct {
	passwords Passwords
	sessions  Sessions
	queue     JobQueue
}

// NewDeletions returns the deletions service, which verifies passwords with passwords, revokes
// sessions with sessions and queues the deletion job on queue. A nil queue writes the job to the
// store without waking a worker.
func NewDeletions(passwords Passwords, sessions Sessions, queue JobQueue) Deletions {
	if queue == nil {
		queue = storeQueue{}
	}

	return Deletions{passwords: passwords, sessions: sessions, queue: queue}
}

// DeleteOwn requests the deletion of the account of deletion through st, once its password is
// verified as Passwords.Change does, for the client of ctx. See request for what it changes.
//
// It returns ErrInvalidCredentials for a wrong password, which counts as a failed login of the
// user, a *RateLimitedError while the user or the client IP is delayed, ErrLastAdmin for the last
// active admin, and store.ErrNotFound for a user who no longer exists.
func (d Deletions) DeleteOwn(ctx context.Context, st store.Store, deletion OwnDeletion) error {
	if err := d.passwords.confirm(ctx, st, deletion.UserID, deletion.Password); err != nil {
		return err
	}

	_, err := d.request(ctx, st, deletion.UserID, deletion.UserID, func(store.User) error { return nil })

	return err
}

// DeleteAsAdmin requests, for the admin of deletion, the deletion of the account of deletion
// through st, and returns the account, being deleted. See request for what it changes.
//
// It returns ErrConfirmationMismatch when the confirmation is not the exact username of the
// account, ErrLastAdmin for the last active admin, ErrUserDeleting for an account already being
// deleted, and store.ErrNotFound for an unknown user.
func (d Deletions) DeleteAsAdmin(ctx context.Context, st store.Store, deletion AdminDeletion) (store.User, error) {
	return d.request(ctx, st, deletion.ActorID, deletion.UserID, func(user store.User) error {
		if deletion.Confirmation != user.Username {
			return ErrConfirmationMismatch
		}

		return nil
	})
}

// request requests the deletion of the account of userID through st, for the user of actorID, once
// check accepts the account. In one transaction, it marks the account deleting, which refuses its
// logins and sessions and keeps its username taken, revokes its sessions, cancels its jobs, queues
// the account.delete job and records the audit event, with the client of ctx. The last active admin
// is never deleted, checked in the transaction so that of concurrent deletions, one keeps an admin.
func (d Deletions) request(ctx context.Context, st store.Store, actorID, userID int64, check func(store.User) error) (store.User, error) {
	job, err := deleteAccountJob(userID)
	if err != nil {
		return store.User{}, err
	}

	var deleting store.User

	err = st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		user, err := changeableUser(ctx, tx.Users(), userID)
		if err != nil {
			return err
		}

		if err := check(user); err != nil {
			return err
		}

		if err := keepAnActiveAdmin(ctx, tx.Users(), user); err != nil {
			return err
		}

		if _, err := d.sessions.RevokeAll(ctx, tx, user.ID); err != nil {
			return err
		}

		if _, err := tx.Jobs().CancelByUser(ctx, user.ID); err != nil {
			return err
		}

		deleting, err = tx.Users().SetStatus(ctx, user.ID, store.UserDeleting)
		if err != nil {
			return err
		}

		if _, err := d.queue.Enqueue(ctx, tx, job); err != nil {
			return err
		}

		return audit.Record(ctx, tx.AuditEvents(), audit.UserDeletionRequested(actorID, user.ID))
	})

	return deleting, err
}

// deleteAccountJob returns the account.delete job of the user of userID.
func deleteAccountJob(userID int64) (store.NewJob, error) {
	payload, err := json.Marshal(DeleteAccountPayload{UserID: userID})
	if err != nil {
		return store.NewJob{}, fmt.Errorf("encode payload of job kind %q: %w", KindDeleteAccount, err)
	}

	return store.NewJob{Kind: KindDeleteAccount, Payload: payload}, nil
}

// DeleteAccount is the handler of the account.delete job of the user of userID: it removes the
// account and every row of the user through st, and records the completion. Running it again once
// it succeeded, or after it stopped halfway, is safe: it finishes the work and records nothing more.
//
// It cancels the jobs of the user first, and returns ErrJobsStillRunning while some of them have
// not stopped, without removing anything. It returns ErrUserNotDeleting for an account whose
// deletion was not requested.
func DeleteAccount(ctx context.Context, st store.Store, userID int64) error {
	return deleteAccount(ctx, st, userID, deleteBatchSize)
}

// deleteAccount is DeleteAccount with the batches of batchSize rows.
func deleteAccount(ctx context.Context, st store.Store, userID int64, batchSize int) error {
	user, err := st.Users().Get(ctx, userID)

	switch {
	case errors.Is(err, store.ErrNotFound):
		// The user was removed by an earlier run, which may have stopped before their jobs.
		return deleteJobsOf(ctx, st.Jobs(), userID, batchSize)
	case err != nil:
		return fmt.Errorf("delete account %d: %w", userID, err)
	case user.Status != store.UserDeleting:
		return fmt.Errorf("delete account %d: %w", userID, ErrUserNotDeleting)
	}

	running, err := st.Jobs().CancelByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("delete account %d: %w", userID, err)
	}

	if running > 0 {
		return fmt.Errorf("delete account %d: %w", userID, ErrJobsStillRunning)
	}

	// The large tables of later milestones go here, removed in batches before the user, whose foreign
	// keys remove the small tables in the transaction that records the completion.
	err = st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		if err := tx.Users().Delete(ctx, userID); err != nil {
			return err
		}

		return audit.Record(ctx, tx.AuditEvents(), audit.UserDeletionCompleted(userID))
	})
	if err != nil {
		return fmt.Errorf("delete account %d: %w", userID, err)
	}

	// The jobs come last: jobs.user_id has no foreign key, and a run that stops before them finds
	// the user gone and removes them.
	return deleteJobsOf(ctx, st.Jobs(), userID, batchSize)
}

// deleteJobsOf removes the jobs of the user of userID from jobs, in batches of batchSize. Each batch
// is a transaction of its own only when jobs is the repository of the store, never the one of a
// WithTx.
func deleteJobsOf(ctx context.Context, jobs store.JobsRepository, userID int64, batchSize int) error {
	for {
		deleted, err := jobs.DeleteByUser(ctx, userID, batchSize)
		if err != nil {
			return fmt.Errorf("delete account %d: %w", userID, err)
		}

		if deleted < int64(batchSize) {
			return nil
		}
	}
}
