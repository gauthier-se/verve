package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// auditTime has sub-millisecond digits and a non-UTC zone, so that the suite checks that the times
// of an event are truncated to the millisecond and returned in UTC.
var auditTime = time.Date(2026, 10, 28, 10, 30, 15, 123_456_789, time.FixedZone("CET", 3600))

// auditEvent returns a complete event of user 7 about user 9.
func auditEvent() store.NewAuditEvent {
	return store.NewAuditEvent{
		ActorID:   7,
		SubjectID: 9,
		Type:      "admin.user_disabled",
		ClientIP:  "203.0.113.7",
		UserAgent: "curl/8.0",
		Payload:   json.RawMessage(`{"reason":"left"}`),
	}
}

func mustAppendEvent(ctx context.Context, t *testing.T, events store.AuditEventsRepository, event store.NewAuditEvent) store.AuditEvent {
	t.Helper()

	got, err := events.Append(ctx, event)
	require.NoError(t, err)

	return got
}

func mustGetEvent(ctx context.Context, t *testing.T, events store.AuditEventsRepository, id int64) store.AuditEvent {
	t.Helper()

	got, err := events.Get(ctx, id)
	require.NoError(t, err)

	return got
}

// RunAuditEvents runs the AuditEventsRepository contract against the stores returned by newStore.
func RunAuditEvents(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given an appended event, then it stores its time, actor, subject, type, client IP, user agent
	// and payload, at the clock's now (millisecond precision, UTC).
	t.Run("AppendStoresEveryField", func(t *testing.T) {
		t.Parallel()

		events := newStore(t, clock.NewFake(auditTime)).AuditEvents()
		ctx := t.Context()

		appended := mustAppendEvent(ctx, t, events, auditEvent())
		at := time.Date(2026, 10, 28, 9, 30, 15, 123_000_000, time.UTC)

		got := mustGetEvent(ctx, t, events, appended.ID)
		require.JSONEq(t, `{"reason":"left"}`, string(got.Payload))

		got.Payload = nil
		require.Equal(t, store.AuditEvent{
			ID:         appended.ID,
			OccurredAt: at,
			ActorID:    7,
			SubjectID:  9,
			Type:       "admin.user_disabled",
			ClientIP:   "203.0.113.7",
			UserAgent:  "curl/8.0",
			Count:      1,
			LastAt:     at,
		}, got)
	})

	// Given an event of the system, from a job or a CLI command, then its actor, subject, client IP
	// and user agent stay empty.
	t.Run("AppendWithoutActorKeepsTheOptionalFieldsEmpty", func(t *testing.T) {
		t.Parallel()

		events := newStore(t, clock.NewFake(auditTime)).AuditEvents()
		ctx := t.Context()

		appended := mustAppendEvent(ctx, t, events, store.NewAuditEvent{
			Type:    "system.backup_created",
			Payload: json.RawMessage(`{}`),
		})

		got := mustGetEvent(ctx, t, events, appended.ID)
		require.Zero(t, got.ActorID)
		require.Zero(t, got.SubjectID)
		require.Empty(t, got.ClientIP)
		require.Empty(t, got.UserAgent)
	})

	// Given events appended in sequence, then their ids grow.
	t.Run("AppendGivesEachEventAGrowingID", func(t *testing.T) {
		t.Parallel()

		events := newStore(t, clock.NewFake(auditTime)).AuditEvents()
		ctx := t.Context()

		first := mustAppendEvent(ctx, t, events, auditEvent())
		second := mustAppendEvent(ctx, t, events, auditEvent())

		require.Greater(t, second.ID, first.ID)
	})

	// Given an event recorded through the repositories of WithTx, when the transaction rolls back,
	// then no event exists.
	t.Run("RolledBackTransactionLeavesNoEvent", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(auditTime))
		ctx := t.Context()
		errRollback := errors.New("roll back")

		var id int64

		err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			appended, err := tx.AuditEvents().Append(ctx, auditEvent())
			if err != nil {
				return err
			}

			id = appended.ID

			return errRollback
		})
		require.ErrorIs(t, err, errRollback)
		require.NotZero(t, id)

		_, err = s.AuditEvents().Get(ctx, id)
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	// Given an event recorded through the repositories of WithTx, when the transaction commits, then
	// the event exists.
	t.Run("CommittedTransactionKeepsTheEvent", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(auditTime))
		ctx := t.Context()

		var id int64

		err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			appended, err := tx.AuditEvents().Append(ctx, auditEvent())
			id = appended.ID

			return err
		})
		require.NoError(t, err)

		mustGetEvent(ctx, t, s.AuditEvents(), id)
	})

	// Given Get on a missing id, then the error satisfies errors.Is(err, store.ErrNotFound).
	t.Run("GetOnMissingIDReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		events := newStore(t, clock.NewFake(auditTime)).AuditEvents()

		_, err := events.Get(t.Context(), 42)
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	// Given user ids that no account has, as after an account deletion, then the event is stored and
	// keeps them: no constraint ties an event to its users. The store has no account yet, so this
	// suite creates none to delete.
	t.Run("EventsKeepTheIDsOfUsersThatDoNotExist", func(t *testing.T) {
		t.Parallel()

		events := newStore(t, clock.NewFake(auditTime)).AuditEvents()
		ctx := t.Context()

		appended := mustAppendEvent(ctx, t, events, auditEvent())

		got := mustGetEvent(ctx, t, events, appended.ID)
		require.Equal(t, int64(7), got.ActorID)
		require.Equal(t, int64(9), got.SubjectID)
	})

	// Given the repository, then it has no method that edits or deletes an event by id. A new method
	// is added to this list on purpose, after checking that it keeps events append-only.
	t.Run("RepositoryHasNoMethodThatEditsOrDeletesAnEvent", func(t *testing.T) {
		t.Parallel()

		repo := reflect.TypeFor[store.AuditEventsRepository]()

		methods := make([]string, 0, repo.NumMethod())
		for i := range repo.NumMethod() {
			methods = append(methods, repo.Method(i).Name)
		}

		require.ElementsMatch(t, []string{"Append", "Get"}, methods)
	})
}
