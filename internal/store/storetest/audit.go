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

// failedLogin returns an aggregatable failed login from ip, keyed on ip.
func failedLogin(ip string) store.NewAuditEvent {
	return store.NewAuditEvent{
		Type:           "auth.login_failed",
		ClientIP:       ip,
		Payload:        json.RawMessage(`{"attempted":"alice"}`),
		AggregationKey: ip,
	}
}

// appendEvery appends event count times, advancing clk by interval before each one, and returns the
// appended events.
func appendEvery(ctx context.Context, t *testing.T, events store.AuditEventsRepository, clk *clock.Fake, interval time.Duration, count int, event store.NewAuditEvent) []store.AuditEvent {
	t.Helper()

	appended := make([]store.AuditEvent, 0, count)

	for range count {
		clk.Advance(interval)
		appended = append(appended, mustAppendEvent(ctx, t, events, event))
	}

	return appended
}

// appendLongBurst appends failed logins from ip, 10 seconds apart, until the latest row of their
// burst holds 20 events, and returns that row.
func appendLongBurst(ctx context.Context, t *testing.T, events store.AuditEventsRepository, clk *clock.Fake, ip string) store.AuditEvent {
	t.Helper()

	burst := appendEvery(ctx, t, events, clk, 10*time.Second, 39, failedLogin(ip))
	latest := burst[len(burst)-1]
	require.Equal(t, int64(20), latest.Count)

	return latest
}

// requireEachAddedARow checks that each of the appended events is a row of its own.
func requireEachAddedARow(t *testing.T, appended []store.AuditEvent) {
	t.Helper()

	for i, event := range appended {
		require.Equal(t, int64(1), event.Count, "event %d", i)

		if i > 0 {
			require.Greater(t, event.ID, appended[i-1].ID, "event %d", i)
		}
	}
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
	// keeps them: no constraint ties an event to its users. No repository deletes an account yet, so
	// this suite creates none.
	t.Run("EventsKeepTheIDsOfUsersThatDoNotExist", func(t *testing.T) {
		t.Parallel()

		events := newStore(t, clock.NewFake(auditTime)).AuditEvents()
		ctx := t.Context()

		appended := mustAppendEvent(ctx, t, events, auditEvent())

		got := mustGetEvent(ctx, t, events, appended.ID)
		require.Equal(t, int64(7), got.ActorID)
		require.Equal(t, int64(9), got.SubjectID)
	})

	// Given an aggregatable event, when fewer than 20 events of the same type and key happened in the
	// last 10 minutes, then a row is added.
	t.Run("AggregatableEventBelowTheThresholdAddsARow", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(auditTime)
		events := newStore(t, clk).AuditEvents()
		ctx := t.Context()

		requireEachAddedARow(t, appendEvery(ctx, t, events, clk, 10*time.Second, 20, failedLogin("203.0.113.7")))
	})

	// Given 20 aggregatable events of the same type and key, when the first one is 10 minutes old,
	// then fewer than 20 happened in the last 10 minutes and the next event adds a row, without a
	// quiet period.
	t.Run("EventsOlderThan10MinutesLeaveTheWindow", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(auditTime)
		events := newStore(t, clk).AuditEvents()
		ctx := t.Context()

		burst := appendEvery(ctx, t, events, clk, 10*time.Second, 20, failedLogin("203.0.113.7"))

		clk.Set(burst[0].OccurredAt.Add(10 * time.Minute))
		next := mustAppendEvent(ctx, t, events, failedLogin("203.0.113.7"))

		require.Greater(t, next.ID, burst[len(burst)-1].ID)
		require.Equal(t, int64(1), next.Count)
	})

	// Given 20 or more aggregatable events of the same type and key in the last 10 minutes, then the
	// latest row has its count incremented and its last_at updated, and no row is added.
	t.Run("AggregatableEventFromTheThresholdIncrementsTheLatestRow", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(auditTime)
		events := newStore(t, clk).AuditEvents()
		ctx := t.Context()

		burst := appendEvery(ctx, t, events, clk, 10*time.Second, 20, failedLogin("203.0.113.7"))
		latest := burst[len(burst)-1]

		aggregated := appendEvery(ctx, t, events, clk, 10*time.Second, 2, failedLogin("203.0.113.7"))
		lastAt := latest.OccurredAt.Add(20 * time.Second)

		for _, event := range aggregated {
			require.Equal(t, latest.ID, event.ID)
		}

		require.Equal(t, int64(3), aggregated[1].Count)
		require.Equal(t, lastAt, aggregated[1].LastAt)

		got := mustGetEvent(ctx, t, events, latest.ID)
		require.Equal(t, latest.OccurredAt, got.OccurredAt)
		require.Equal(t, int64(3), got.Count)
		require.Equal(t, lastAt, got.LastAt)
	})

	// Given a burst whose latest row holds 20 events, when the next event comes after a quiet period
	// of 10 minutes, then it adds a row.
	t.Run("QuietPeriodOf10MinutesEndsTheBurst", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(auditTime)
		events := newStore(t, clk).AuditEvents()
		ctx := t.Context()

		latest := appendLongBurst(ctx, t, events, clk, "203.0.113.7")

		after := appendEvery(ctx, t, events, clk, 10*time.Minute, 1, failedLogin("203.0.113.7"))[0]
		require.Greater(t, after.ID, latest.ID)
		require.Equal(t, int64(1), after.Count)
	})

	// Given a burst whose latest row holds 20 events, when the next events come just under 10 minutes
	// apart, then they go into that row, although the other rows of the burst are older than 10
	// minutes: the events of a row count as recent while its last_at is.
	t.Run("BurstGoesOnWhileItsLatestRowIsRecent", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(auditTime)
		events := newStore(t, clk).AuditEvents()
		ctx := t.Context()

		latest := appendLongBurst(ctx, t, events, clk, "203.0.113.7")

		aggregated := appendEvery(ctx, t, events, clk, 10*time.Minute-time.Millisecond, 6, failedLogin("203.0.113.7"))

		for i, event := range aggregated {
			require.Equal(t, latest.ID, event.ID, "event %d", i)
		}

		require.Equal(t, int64(26), aggregated[len(aggregated)-1].Count)
	})

	// Given a burst from one client IP, then an event of another IP or of another type is not
	// aggregated with it, and the next event of the burst still goes into the burst's latest row.
	t.Run("BurstIsKeyedOnTypeAndKey", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(auditTime)
		events := newStore(t, clk).AuditEvents()
		ctx := t.Context()

		burst := appendEvery(ctx, t, events, clk, 10*time.Second, 20, failedLogin("203.0.113.7"))
		latest := burst[len(burst)-1]

		otherType := failedLogin("203.0.113.7")
		otherType.Type = "auth.token_rejected"

		for name, event := range map[string]store.NewAuditEvent{
			"another IP":   failedLogin("198.51.100.4"),
			"another type": otherType,
		} {
			appended := mustAppendEvent(ctx, t, events, event)
			require.Greater(t, appended.ID, latest.ID, name)
			require.Equal(t, int64(1), appended.Count, name)
		}

		require.Equal(t, latest.ID, mustAppendEvent(ctx, t, events, failedLogin("203.0.113.7")).ID)
	})

	// Given events without an aggregation key, then each adds a row, however many there are.
	t.Run("EventsWithoutAKeyAreNeverAggregated", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(auditTime)
		events := newStore(t, clk).AuditEvents()
		ctx := t.Context()

		event := failedLogin("203.0.113.7")
		event.AggregationKey = ""

		requireEachAddedARow(t, appendEvery(ctx, t, events, clk, time.Second, 25, event))
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
