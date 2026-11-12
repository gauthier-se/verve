package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/store"
)

// recordingQueue enqueues jobs through the repositories it is given, as the worker does, and
// records them.
type recordingQueue struct {
	queued []store.Job
}

func (q *recordingQueue) Enqueue(ctx context.Context, repos store.Repositories, job store.NewJob) (store.Job, error) {
	queued, err := repos.Jobs().Enqueue(ctx, job)
	if err != nil {
		return store.Job{}, err
	}

	q.queued = append(q.queued, queued)

	return queued, nil
}

// deletionTest is an adminTest with the deletions service, whose jobs go to queue.
type deletionTest struct {
	adminTest

	queue     *recordingQueue
	deletions Deletions
}

func newDeletionTest(t *testing.T) deletionTest {
	t.Helper()

	at := newAdminTest(t)
	queue := &recordingQueue{}

	return deletionTest{
		adminTest: at,
		queue:     queue,
		deletions: NewDeletions(NewPasswords(at.logins, NewPasswordPolicy()), at.sessions, queue),
	}
}

// requireStatus checks that the user of id has status.
func (dt deletionTest) requireStatus(t *testing.T, id int64, status store.UserStatus) {
	t.Helper()

	user, err := dt.store.Users().Get(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, status, user.Status)
}

// requireDeletionQueued checks that one account.delete job is queued for the user of id, without a
// user of its own.
func (dt deletionTest) requireDeletionQueued(t *testing.T, id int64) {
	t.Helper()

	require.Len(t, dt.queue.queued, 1)

	job, err := dt.store.Jobs().Get(t.Context(), dt.queue.queued[0].ID)
	require.NoError(t, err)
	require.Equal(t, KindDeleteAccount, job.Kind)
	require.Equal(t, store.JobQueued, job.Status)
	require.Zero(t, job.UserID, "the job must not belong to the user, or it would delete itself")

	var payload DeleteAccountPayload
	require.NoError(t, json.Unmarshal(job.Payload, &payload))
	require.Equal(t, id, payload.UserID)
}

// mustRequestOwnDeletion requests the deletion of user, with alicePassword.
func (dt deletionTest) mustRequestOwnDeletion(t *testing.T, user store.User) {
	t.Helper()

	require.NoError(t, dt.deletions.DeleteOwn(t.Context(), dt.store, OwnDeletion{UserID: user.ID, Password: alicePassword}))
}

// Given the right password, then DeleteOwn marks the account deleting at once, revokes every
// session of the user, so that their login fails, queues the account.delete job and records the
// request with the user as actor and subject.
func TestDeletions_OwnDeletionWithTheRightPasswordDisablesTheAccountAtOnce(t *testing.T) {
	t.Parallel()

	dt := newDeletionTest(t)
	bob := dt.mustCreateRole(t, "bob", store.RoleMember)
	dt.mustLogIn(t, bob)

	dt.mustRequestOwnDeletion(t, bob)

	dt.requireStatus(t, bob.ID, store.UserDeleting)
	dt.requireSessionCount(t, bob, 0)
	dt.requireDeletionQueued(t, bob.ID)

	_, err := dt.login(t.Context(), phoneIP, "bob", alicePassword)
	require.ErrorIs(t, err, ErrInvalidCredentials)

	events := dt.eventsOfType(t, audit.TypeUserDeletionRequested)
	require.Len(t, events, 1)
	require.Equal(t, bob.ID, events[0].ActorID)
	require.Equal(t, bob.ID, events[0].SubjectID)
}

// Given a wrong password, then DeleteOwn returns ErrInvalidCredentials and changes nothing. Each
// wrong password counts as a failed login of the user, so that the fifth delays the next attempt.
func TestDeletions_OwnDeletionWithAWrongPasswordChangesNothing(t *testing.T) {
	t.Parallel()

	dt := newDeletionTest(t)
	bob := dt.mustCreateRole(t, "bob", store.RoleMember)
	dt.mustLogIn(t, bob)

	for range 5 {
		err := dt.deletions.DeleteOwn(t.Context(), dt.store, OwnDeletion{UserID: bob.ID, Password: wrongPassword})
		require.ErrorIs(t, err, ErrInvalidCredentials)
	}

	var limited *RateLimitedError
	require.ErrorAs(t, dt.deletions.DeleteOwn(t.Context(), dt.store, OwnDeletion{UserID: bob.ID, Password: alicePassword}), &limited)

	dt.requireStatus(t, bob.ID, store.UserActive)
	dt.requireSessionCount(t, bob, 1)
	require.Empty(t, dt.queue.queued)
	require.Empty(t, dt.eventsOfType(t, audit.TypeUserDeletionRequested))
}

// Given an admin and the exact username as confirmation, then DeleteAsAdmin does what DeleteOwn
// does, with the admin as actor. Given a confirmation that is not the exact username, even only by
// case, then ErrConfirmationMismatch and nothing changes.
func TestDeletions_AdminDeletionRequiresTheExactUsername(t *testing.T) {
	t.Parallel()

	dt := newDeletionTest(t)
	admin := dt.mustCreateRole(t, "admin", store.RoleAdmin)
	bob := dt.mustCreateRole(t, "Bob", store.RoleMember)
	dt.mustLogIn(t, bob)

	for _, confirmation := range []string{"", "bob", "Bob "} {
		_, err := dt.deletions.DeleteAsAdmin(t.Context(), dt.store, AdminDeletion{ActorID: admin.ID, UserID: bob.ID, Confirmation: confirmation})
		require.ErrorIs(t, err, ErrConfirmationMismatch, confirmation)
	}

	dt.requireStatus(t, bob.ID, store.UserActive)
	dt.requireSessionCount(t, bob, 1)
	require.Empty(t, dt.queue.queued)

	deleting, err := dt.deletions.DeleteAsAdmin(t.Context(), dt.store, AdminDeletion{ActorID: admin.ID, UserID: bob.ID, Confirmation: "Bob"})
	require.NoError(t, err)
	require.Equal(t, store.UserDeleting, deleting.Status)

	dt.requireStatus(t, bob.ID, store.UserDeleting)
	dt.requireSessionCount(t, bob, 0)
	dt.requireDeletionQueued(t, bob.ID)

	events := dt.eventsOfType(t, audit.TypeUserDeletionRequested)
	require.Len(t, events, 1)
	require.Equal(t, admin.ID, events[0].ActorID)
	require.Equal(t, bob.ID, events[0].SubjectID)
}

// Given the last active admin, then neither DeleteOwn nor DeleteAsAdmin deletes them: both return
// ErrLastAdmin. Given another active admin, then the deletion goes on.
func TestDeletions_LastActiveAdminIsNotDeleted(t *testing.T) {
	t.Parallel()

	dt := newDeletionTest(t)
	admin := dt.mustCreateRole(t, "admin", store.RoleAdmin)

	err := dt.deletions.DeleteOwn(t.Context(), dt.store, OwnDeletion{UserID: admin.ID, Password: alicePassword})
	require.ErrorIs(t, err, ErrLastAdmin)

	_, err = dt.deletions.DeleteAsAdmin(t.Context(), dt.store, AdminDeletion{ActorID: admin.ID, UserID: admin.ID, Confirmation: "admin"})
	require.ErrorIs(t, err, ErrLastAdmin)

	dt.requireStatus(t, admin.ID, store.UserActive)
	require.Empty(t, dt.queue.queued)

	dt.mustCreateRole(t, "other", store.RoleAdmin)
	dt.mustRequestOwnDeletion(t, admin)
	dt.requireStatus(t, admin.ID, store.UserDeleting)
}

// Given an account being deleted, then a second request returns ErrUserDeleting and queues nothing.
// Given an unknown user, then store.ErrNotFound.
func TestDeletions_SecondRequestAndUnknownUserAreRefused(t *testing.T) {
	t.Parallel()

	dt := newDeletionTest(t)
	admin := dt.mustCreateRole(t, "admin", store.RoleAdmin)
	bob := dt.mustCreateRole(t, "bob", store.RoleMember)

	dt.mustRequestOwnDeletion(t, bob)

	_, err := dt.deletions.DeleteAsAdmin(t.Context(), dt.store, AdminDeletion{ActorID: admin.ID, UserID: bob.ID, Confirmation: "bob"})
	require.ErrorIs(t, err, ErrUserDeleting)
	require.Len(t, dt.queue.queued, 1)

	_, err = dt.deletions.DeleteAsAdmin(t.Context(), dt.store, AdminDeletion{ActorID: admin.ID, UserID: 42, Confirmation: "bob"})
	require.ErrorIs(t, err, store.ErrNotFound)
}

// Given an account being deleted whose account.delete job failed for good, then an admin requesting
// its deletion again queues a new job, recorded as a new request, so that the account is not left
// deleting forever.
func TestDeletions_AdminRequestAfterAFailedJobQueuesItAgain(t *testing.T) {
	t.Parallel()

	dt := newDeletionTest(t)
	admin := dt.mustCreateRole(t, "admin", store.RoleAdmin)
	bob := dt.mustCreateRole(t, "bob", store.RoleMember)
	ctx := t.Context()

	dt.mustRequestOwnDeletion(t, bob)

	claimed, _, err := dt.store.Jobs().Claim(ctx, time.Minute)
	require.NoError(t, err)
	require.Equal(t, dt.queue.queued[0].ID, claimed.ID)

	_, err = dt.deletions.DeleteAsAdmin(ctx, dt.store, AdminDeletion{ActorID: admin.ID, UserID: bob.ID, Confirmation: "bob"})
	require.ErrorIs(t, err, ErrUserDeleting, "the job still runs")

	require.NoError(t, dt.store.Jobs().Fail(ctx, claimed.ID, claimed.Claims, "jobs of the account still running"))

	deleting, err := dt.deletions.DeleteAsAdmin(ctx, dt.store, AdminDeletion{ActorID: admin.ID, UserID: bob.ID, Confirmation: "bob"})
	require.NoError(t, err)
	require.Equal(t, store.UserDeleting, deleting.Status)

	require.Len(t, dt.queue.queued, 2)

	job, err := dt.store.Jobs().Get(ctx, dt.queue.queued[1].ID)
	require.NoError(t, err)
	require.Equal(t, store.JobQueued, job.Status)
	require.Equal(t, KindDeleteAccount, job.Kind)

	require.Len(t, dt.eventsOfType(t, audit.TypeUserDeletionRequested), 2)
}

// Given a pending deletion, then the username and the email stay taken and the status is deleting
// until the job has finished, and are free once it has.
func TestDeletions_UsernameStaysTakenUntilTheJobHasFinished(t *testing.T) {
	t.Parallel()

	dt := newDeletionTest(t)
	bob := dt.mustCreateRole(t, "bob", store.RoleMember)

	dt.mustRequestOwnDeletion(t, bob)
	dt.requireStatus(t, bob.ID, store.UserDeleting)

	for _, taken := range []store.NewUser{
		{Username: "BOB", Role: store.RoleMember},
		{Username: "robert", Email: "Bob@Example.com", Role: store.RoleMember},
	} {
		_, err := dt.store.Users().Create(t.Context(), taken)
		require.ErrorIs(t, err, store.ErrConflict, taken.Username)
	}

	require.NoError(t, DeleteAccount(t.Context(), dt.store, bob.ID))

	_, err := dt.store.Users().Create(t.Context(), store.NewUser{Username: "BOB", Email: "Bob@Example.com", Role: store.RoleMember})
	require.NoError(t, err)
}

// deleteBatch is a call of Jobs().DeleteByUser: its limit and how many jobs it removed.
type deleteBatch struct {
	limit   int
	deleted int64
}

// sampleBatch is a call of Samples().DeleteByUser: its limit, how many rows it removed, and whether
// the user still existed when it ran.
type sampleBatch struct {
	limit      int
	deleted    int64
	userExists bool
}

// countingStore records the calls of Jobs().DeleteByUser and Samples().DeleteByUser on the store
// it wraps.
type countingStore struct {
	store.Store

	batches       *[]deleteBatch
	sampleBatches *[]sampleBatch
}

func (s countingStore) Jobs() store.JobsRepository {
	return countingJobs{JobsRepository: s.Store.Jobs(), batches: s.batches}
}

func (s countingStore) Samples() store.SamplesRepository {
	return countingSamples{SamplesRepository: s.Store.Samples(), users: s.Users(), batches: s.sampleBatches}
}

type countingSamples struct {
	store.SamplesRepository

	users   store.UsersRepository
	batches *[]sampleBatch
}

func (r countingSamples) DeleteByUser(ctx context.Context, userID int64, limit int) (int64, error) {
	_, err := r.users.Get(ctx, userID)
	userExists := err == nil

	deleted, err := r.SamplesRepository.DeleteByUser(ctx, userID, limit)
	*r.batches = append(*r.batches, sampleBatch{limit: limit, deleted: deleted, userExists: userExists})

	return deleted, err
}

type countingJobs struct {
	store.JobsRepository

	batches *[]deleteBatch
}

func (j countingJobs) DeleteByUser(ctx context.Context, userID int64, limit int) (int64, error) {
	deleted, err := j.JobsRepository.DeleteByUser(ctx, userID, limit)
	*j.batches = append(*j.batches, deleteBatch{limit: limit, deleted: deleted})

	return deleted, err
}

// Given the account.delete job, then every row of the user is removed, the jobs in batches of at
// most the batch size, the audit events about the user stay and the completion is recorded once.
// Given the same job run again, then it succeeds and changes nothing.
func TestDeleteAccount_RemovesTheRowsInBoundedBatchesAndIsSafeToRunTwice(t *testing.T) {
	t.Parallel()

	dt := newDeletionTest(t)
	alice := dt.mustCreateRole(t, "alice", store.RoleMember)
	bob := dt.mustCreateRole(t, "bob", store.RoleMember)
	dt.mustLogIn(t, alice)
	dt.mustLogIn(t, bob)

	for _, user := range []store.User{alice, bob, bob, bob, bob, bob} {
		_, err := dt.store.Jobs().Enqueue(t.Context(), store.NewJob{Kind: "test.job", Payload: json.RawMessage(`{}`), UserID: user.ID})
		require.NoError(t, err)
	}

	dt.mustRequestOwnDeletion(t, bob)

	var (
		batches       []deleteBatch
		sampleBatches []sampleBatch
	)

	counting := countingStore{Store: dt.store, batches: &batches, sampleBatches: &sampleBatches}

	require.NoError(t, deleteAccount(t.Context(), counting, bob.ID, 2))
	require.Equal(t, []deleteBatch{{limit: 2, deleted: 2}, {limit: 2, deleted: 2}, {limit: 2, deleted: 1}}, batches,
		"five jobs in batches of at most two")

	batches = nil

	require.NoError(t, deleteAccount(t.Context(), counting, bob.ID, 2), "a second run succeeds")
	require.Equal(t, []deleteBatch{{limit: 2, deleted: 0}}, batches)

	_, err := dt.store.Users().Get(t.Context(), bob.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	remaining, err := dt.store.Jobs().DeleteByUser(t.Context(), bob.ID, 100)
	require.NoError(t, err)
	require.Zero(t, remaining, "no job of the deleted user is left")

	dt.requireSessionCount(t, alice, 1)

	stillThere, err := dt.store.Jobs().DeleteByUser(t.Context(), alice.ID, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), stillThere, "the jobs of the other user stay")

	require.Len(t, dt.eventsOfType(t, audit.TypeUserDeletionRequested), 1, "the events about the user stay")

	logins := dt.eventsOfType(t, audit.TypeLoginSucceeded)
	require.Len(t, logins, 2, "the events about the user from before the request stay")
	require.ElementsMatch(t, []int64{alice.ID, bob.ID}, []int64{logins[0].ActorID, logins[1].ActorID})

	completed := dt.eventsOfType(t, audit.TypeUserDeletionCompleted)
	require.Len(t, completed, 1)
	require.Zero(t, completed[0].ActorID, "the system completes the deletion")
	require.Equal(t, bob.ID, completed[0].SubjectID)
}

// mustUpsertHeartRates stores count heart rate samples of user, from one source.
func (dt deletionTest) mustUpsertHeartRates(t *testing.T, user store.User, count int) {
	t.Helper()

	ctx := t.Context()

	heart, err := dt.store.MetricTypes().EnsureUncurated(ctx, "HKQuantityTypeIdentifierHeartRate", catalog.KindQuantity, "count/min")
	require.NoError(t, err)

	source, err := dt.store.Sources().Resolve(ctx, user.ID, store.SourceInfo{Name: "Watch", DeviceModel: "Watch"},
		store.SourceWatch, sessionsTestNow)
	require.NoError(t, err)

	samples := make([]store.NewSample, count)
	for i := range samples {
		start := sessionsTestNow.Add(time.Duration(i) * time.Minute)
		samples[i] = store.NewSample{
			Sample: store.Sample{
				TypeID:     heart.ID,
				SourceID:   source.ID,
				Start:      start,
				End:        start,
				Value:      60,
				Origin:     store.OriginXML,
				NaturalKey: []byte(fmt.Sprintf("heart rate %d of %s", i, user.Username)),
			},
			OnConflict: store.ConflictIgnore,
		}
	}

	_, err = dt.store.Samples().Upsert(ctx, user.ID, samples)
	require.NoError(t, err)
}

// Given samples of two users, when the account.delete job of one of them runs, then it removes
// their samples and data availability in transactions of at most the batch size, all before the
// user, and the other user keeps theirs.
func TestDeleteAccount_RemovesTheSamplesInBoundedBatchesBeforeTheUser(t *testing.T) {
	t.Parallel()

	dt := newDeletionTest(t)
	alice := dt.mustCreateRole(t, "alice", store.RoleMember)
	bob := dt.mustCreateRole(t, "bob", store.RoleMember)
	dt.mustUpsertHeartRates(t, alice, 2)
	dt.mustUpsertHeartRates(t, bob, 5)

	dt.mustRequestOwnDeletion(t, bob)

	var (
		batches       []deleteBatch
		sampleBatches []sampleBatch
	)

	counting := countingStore{Store: dt.store, batches: &batches, sampleBatches: &sampleBatches}

	require.NoError(t, deleteAccount(t.Context(), counting, bob.ID, 2))
	require.Equal(t, []sampleBatch{
		{limit: 2, deleted: 2, userExists: true},
		{limit: 2, deleted: 2, userExists: true},
		{limit: 2, deleted: 2, userExists: true},
		{limit: 2, deleted: 0, userExists: true},
	}, sampleBatches, "five samples and one stats row in batches of at most two, before the user")

	_, err := dt.store.Users().Get(t.Context(), bob.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	stats, err := dt.store.Samples().Stats(t.Context(), alice.ID)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	require.Equal(t, int64(2), stats[0].Count, "the samples of the other user stay")
}

// Given a queued and a running job of the user, then the account.delete job cancels both first: it
// fails with an error to retry while the running one has not stopped, and deletes nothing, then
// deletes the account once it has.
func TestDeleteAccount_CancelsTheJobsOfTheUserFirst(t *testing.T) {
	t.Parallel()

	dt := newDeletionTest(t)
	bob := dt.mustCreateRole(t, "bob", store.RoleMember)
	ctx := t.Context()

	_, err := dt.store.Jobs().Enqueue(ctx, store.NewJob{Kind: "test.import", Payload: json.RawMessage(`{}`), UserID: bob.ID})
	require.NoError(t, err)

	running, _, err := dt.store.Jobs().Claim(ctx, time.Minute)
	require.NoError(t, err)

	queued, err := dt.store.Jobs().Enqueue(ctx, store.NewJob{Kind: "test.import", Payload: json.RawMessage(`{}`), UserID: bob.ID})
	require.NoError(t, err)

	_, err = dt.store.Users().SetStatus(ctx, bob.ID, store.UserDeleting)
	require.NoError(t, err)

	err = DeleteAccount(ctx, dt.store, bob.ID)
	require.ErrorIs(t, err, ErrJobsStillRunning)

	got, err := dt.store.Jobs().Get(ctx, queued.ID)
	require.NoError(t, err)
	require.Equal(t, store.JobCancelled, got.Status)

	got, err = dt.store.Jobs().Get(ctx, running.ID)
	require.NoError(t, err)
	require.True(t, got.CancelRequested)

	dt.requireStatus(t, bob.ID, store.UserDeleting)

	require.NoError(t, dt.store.Jobs().ConfirmCancel(ctx, running.ID, running.Claims))
	require.NoError(t, DeleteAccount(ctx, dt.store, bob.ID))

	_, err = dt.store.Users().Get(ctx, bob.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// Given a user who is not being deleted, then the account.delete job deletes nothing and returns
// ErrUserNotDeleting, which no retry fixes.
func TestDeleteAccount_RefusesAUserWhoIsNotBeingDeleted(t *testing.T) {
	t.Parallel()

	dt := newDeletionTest(t)
	bob := dt.mustCreateRole(t, "bob", store.RoleMember)

	require.ErrorIs(t, DeleteAccount(t.Context(), dt.store, bob.ID), ErrUserNotDeleting)
	dt.requireStatus(t, bob.ID, store.UserActive)
}
