package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
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

// requireEventsRemoved checks that none of the appended events exists any more.
func requireEventsRemoved(ctx context.Context, t *testing.T, events store.AuditEventsRepository, appended ...store.AuditEvent) {
	t.Helper()

	for _, event := range appended {
		_, err := events.Get(ctx, event.ID)
		require.ErrorIs(t, err, store.ErrNotFound, "event %d", event.ID)
	}
}

// requireEventsKept checks that each of the appended events still exists.
func requireEventsKept(ctx context.Context, t *testing.T, events store.AuditEventsRepository, appended ...store.AuditEvent) {
	t.Helper()

	for _, event := range appended {
		mustGetEvent(ctx, t, events, event.ID)
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
	// is added to this list on purpose, after checking that it keeps events append-only: PurgeBefore
	// removes events by age only.
	t.Run("RepositoryHasNoMethodThatEditsOrDeletesAnEvent", func(t *testing.T) {
		t.Parallel()

		repo := reflect.TypeFor[store.AuditEventsRepository]()

		methods := make([]string, 0, repo.NumMethod())
		for i := range repo.NumMethod() {
			methods = append(methods, repo.Method(i).Name)
		}

		require.ElementsMatch(t, []string{"Append", "Get", "List", "PurgeBefore"}, methods)
	})

	runAuditEventsList(t, newStore)
	runAuditEventsPurge(t, newStore)
}

// listEvents returns the events that List returns for filter and page.
func listEvents(ctx context.Context, t *testing.T, events store.AuditEventsRepository, filter store.AuditEventsFilter, page store.PageRequest) []store.AuditEvent {
	t.Helper()

	got, err := events.List(ctx, filter, page)
	require.NoError(t, err)

	return got
}

// eventIDs returns the ids of events, in order.
func eventIDs(events ...store.AuditEvent) []int64 {
	ids := make([]int64, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}

	return ids
}

// eventBy returns an event of actorID about subjectID, of eventType.
func eventBy(actorID, subjectID int64, eventType string) store.NewAuditEvent {
	return store.NewAuditEvent{
		ActorID:   actorID,
		SubjectID: subjectID,
		Type:      eventType,
		Payload:   json.RawMessage(`{}`),
	}
}

// filterCase is a filter of List and the events it keeps, newest first.
type filterCase struct {
	name   string
	filter store.AuditEventsFilter
	want   []store.AuditEvent
}

// runFilterCases runs each case as a parallel subtest, which checks the events that List keeps for
// its filter.
func runFilterCases(t *testing.T, events store.AuditEventsRepository, cases []filterCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := listEvents(t.Context(), t, events, tc.filter, store.PageRequest{Limit: 10})
			require.Equal(t, eventIDs(tc.want...), eventIDs(got...))
		})
	}
}

func runAuditEventsList(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given events at different times, two of them at the same time and one appended after the clock
	// moved back, then List returns them newest first by OccurredAt, then by ID, with every field.
	t.Run("ListReturnsEveryEventNewestFirst", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(auditTime)
		events := newStore(t, clk).AuditEvents()
		ctx := t.Context()

		clk.Set(auditTime.Add(time.Second))
		first := mustAppendEvent(ctx, t, events, auditEvent())

		clk.Set(auditTime.Add(2 * time.Second))
		second := mustAppendEvent(ctx, t, events, auditEvent())
		sameTime := mustAppendEvent(ctx, t, events, auditEvent())

		clk.Set(auditTime.Add(500 * time.Millisecond))
		older := mustAppendEvent(ctx, t, events, auditEvent())

		got := listEvents(ctx, t, events, store.AuditEventsFilter{}, store.PageRequest{Limit: 10})

		require.Equal(t, eventIDs(sameTime, second, first, older), eventIDs(got...))
		require.Equal(t, mustGetEvent(ctx, t, events, sameTime.ID), got[0])
	})

	// Given an empty audit log, then List returns no event.
	t.Run("ListOnAnEmptyLogReturnsNoEvent", func(t *testing.T) {
		t.Parallel()

		events := newStore(t, clock.NewFake(auditTime)).AuditEvents()

		require.Empty(t, listEvents(t.Context(), t, events, store.AuditEventsFilter{}, store.PageRequest{Limit: 10}))
	})

	// Given events of several actors, subjects and types, then each filter keeps the matching events,
	// newest first, and filters combine.
	t.Run("ListKeepsTheEventsThatMatchTheFilter", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(auditTime)
		events := newStore(t, clk).AuditEvents()
		ctx := t.Context()

		appended := make([]store.AuditEvent, 0, 5)
		for _, event := range []store.NewAuditEvent{
			eventBy(7, 9, "admin.user_disabled"),
			eventBy(9, 0, "auth.logout"),
			eventBy(0, 7, "auth.session_revoked"),
			eventBy(8, 8, "admin.user_disabled"),
			eventBy(0, 0, "system.backup_created"),
		} {
			clk.Advance(time.Second)
			appended = append(appended, mustAppendEvent(ctx, t, events, event))
		}

		byAlice, byBob, aboutAlice, byCarolAboutCarol, bySystem := appended[0], appended[1], appended[2], appended[3], appended[4]

		runFilterCases(t, events, []filterCase{
			{name: "none", filter: store.AuditEventsFilter{}, want: []store.AuditEvent{bySystem, byCarolAboutCarol, aboutAlice, byBob, byAlice}},
			{name: "user as actor or subject", filter: store.AuditEventsFilter{UserID: 7}, want: []store.AuditEvent{aboutAlice, byAlice}},
			{name: "user as subject of another actor", filter: store.AuditEventsFilter{UserID: 9}, want: []store.AuditEvent{byBob, byAlice}},
			{name: "user as actor and subject at once", filter: store.AuditEventsFilter{UserID: 8}, want: []store.AuditEvent{byCarolAboutCarol}},
			{name: "user without events", filter: store.AuditEventsFilter{UserID: 42}, want: []store.AuditEvent{}},
			{name: "actor", filter: store.AuditEventsFilter{ActorID: 7}, want: []store.AuditEvent{byAlice}},
			{name: "subject", filter: store.AuditEventsFilter{SubjectID: 7}, want: []store.AuditEvent{aboutAlice}},
			{name: "type", filter: store.AuditEventsFilter{Type: "admin.user_disabled"}, want: []store.AuditEvent{byCarolAboutCarol, byAlice}},
			{name: "type and actor", filter: store.AuditEventsFilter{Type: "admin.user_disabled", ActorID: 8}, want: []store.AuditEvent{byCarolAboutCarol}},
			{name: "actor and subject", filter: store.AuditEventsFilter{ActorID: 7, SubjectID: 7}, want: []store.AuditEvent{}},
		})
	})

	// Given events one second apart, then From keeps the events at or after it and To the events
	// before it.
	t.Run("ListKeepsTheEventsOfTheTimeRange", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(auditTime)
		events := newStore(t, clk).AuditEvents()
		ctx := t.Context()

		appended := appendEvery(ctx, t, events, clk, time.Second, 3, auditEvent())
		first, second, third := appended[0], appended[1], appended[2]

		runFilterCases(t, events, []filterCase{
			{name: "from", filter: store.AuditEventsFilter{From: second.OccurredAt}, want: []store.AuditEvent{third, second}},
			{name: "to", filter: store.AuditEventsFilter{To: second.OccurredAt}, want: []store.AuditEvent{first}},
			{name: "from and to", filter: store.AuditEventsFilter{From: second.OccurredAt, To: third.OccurredAt}, want: []store.AuditEvent{second}},
			{name: "empty range", filter: store.AuditEventsFilter{From: second.OccurredAt, To: second.OccurredAt}, want: []store.AuditEvent{}},
		})
	})

	// Given an aggregated row whose first event is before From, then List keeps it while its last
	// event is at or after From. Given To, then List keeps it while its first event is before To.
	t.Run("ListKeepsAnAggregatedRowWithAnEventInTheTimeRange", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(auditTime)
		events := newStore(t, clk).AuditEvents()
		ctx := t.Context()

		burst := appendEvery(ctx, t, events, clk, 10*time.Second, 21, failedLogin("203.0.113.7"))
		latest := burst[len(burst)-1]
		require.Equal(t, int64(2), latest.Count)

		runFilterCases(t, events, []filterCase{
			{name: "from its last event", filter: store.AuditEventsFilter{From: latest.LastAt}, want: []store.AuditEvent{latest}},
			{name: "from after its last event", filter: store.AuditEventsFilter{From: latest.LastAt.Add(time.Millisecond)}, want: []store.AuditEvent{}},
			{name: "to after its first event", filter: store.AuditEventsFilter{From: latest.LastAt, To: latest.OccurredAt.Add(time.Millisecond)}, want: []store.AuditEvent{latest}},
			{name: "to at its first event", filter: store.AuditEventsFilter{From: latest.LastAt, To: latest.OccurredAt}, want: []store.AuditEvent{}},
		})
	})

	// Given more events than the limit, some of them at the same time, then List returns at most the
	// limit, and following the position of the last event of each page lists every event once, in
	// order.
	t.Run("ListReturnsAtMostTheLimitAfterTheCursor", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(auditTime)
		events := newStore(t, clk).AuditEvents()
		ctx := t.Context()

		appended := slices.Concat(
			appendEvery(ctx, t, events, clk, 0, 4, auditEvent()),
			appendEvery(ctx, t, events, clk, time.Second, 3, auditEvent()),
		)

		var listed []store.AuditEvent

		page := store.PageRequest{Limit: 2}

		for range len(appended) {
			got := listEvents(ctx, t, events, store.AuditEventsFilter{}, page)
			require.LessOrEqual(t, len(got), 2)

			if len(got) == 0 {
				break
			}

			listed = append(listed, got...)
			page.After = got[len(got)-1].Cursor()
		}

		want := eventIDs(appended...)
		slices.Reverse(want)
		require.Equal(t, want, eventIDs(listed...))
	})

	// Given a page listed, when new events arrive and an event of a later page is aggregated, then
	// following the cursor lists each event of the log once: the new events are newer than the
	// cursor, and an aggregated row keeps its place.
	t.Run("FollowingTheCursorListsEachEventOnceWhileEventsArrive", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(auditTime)
		events := newStore(t, clk).AuditEvents()
		ctx := t.Context()

		burst := appendEvery(ctx, t, events, clk, time.Second, 20, failedLogin("203.0.113.7"))
		appended := slices.Concat(burst, appendEvery(ctx, t, events, clk, time.Second, 5, auditEvent()))
		want := eventIDs(appended...)
		slices.Reverse(want)

		filter := store.AuditEventsFilter{}
		listed := listEvents(ctx, t, events, filter, store.PageRequest{Limit: 3})
		require.Equal(t, want[:3], eventIDs(listed...))

		mustAppendEvent(ctx, t, events, auditEvent())
		aggregated := mustAppendEvent(ctx, t, events, failedLogin("203.0.113.7"))
		require.Equal(t, burst[len(burst)-1].ID, aggregated.ID)

		for range len(appended) {
			got := listEvents(ctx, t, events, filter, store.PageRequest{After: listed[len(listed)-1].Cursor(), Limit: 3})
			if len(got) == 0 {
				break
			}

			listed = append(listed, got...)
		}

		require.Equal(t, want, eventIDs(listed...))
	})

	// Given a limit below 1, then List returns store.ErrInvalid.
	t.Run("ListWithALimitBelowOneReturnsErrInvalid", func(t *testing.T) {
		t.Parallel()

		events := newStore(t, clock.NewFake(auditTime)).AuditEvents()
		ctx := t.Context()

		mustAppendEvent(ctx, t, events, auditEvent())

		for _, limit := range []int{0, -1} {
			_, err := events.List(ctx, store.AuditEventsFilter{}, store.PageRequest{Limit: limit})
			require.ErrorIs(t, err, store.ErrInvalid, "limit %d", limit)
		}
	})
}

func runAuditEventsPurge(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given events before, at and after a cutoff, then PurgeBefore removes only the event before the
	// cutoff, and returns how many it removed.
	t.Run("PurgeBeforeRemovesOnlyTheEventsBeforeTheCutoff", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(auditTime)
		events := newStore(t, clk).AuditEvents()
		ctx := t.Context()

		appended := appendEvery(ctx, t, events, clk, time.Millisecond, 3, auditEvent())
		before, at, after := appended[0], appended[1], appended[2]

		purged, err := events.PurgeBefore(ctx, at.OccurredAt, 10)
		require.NoError(t, err)
		require.Equal(t, int64(1), purged)

		requireEventsRemoved(ctx, t, events, before)
		requireEventsKept(ctx, t, events, at, after)
	})

	// Given more events before the cutoff than the batch size, then PurgeBefore removes the oldest
	// batch only, and the next calls remove the rest, until none is left.
	t.Run("PurgeBeforeRemovesAtMostTheBatchSizeOldestFirst", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(auditTime)
		events := newStore(t, clk).AuditEvents()
		ctx := t.Context()

		appended := appendEvery(ctx, t, events, clk, time.Second, 3, auditEvent())
		cutoff := clk.Now().Add(time.Second)

		for i, want := range []int64{2, 1, 0} {
			purged, err := events.PurgeBefore(ctx, cutoff, 2)
			require.NoError(t, err)
			require.Equal(t, want, purged, "call %d", i)

			if i == 0 {
				requireEventsRemoved(ctx, t, events, appended[:2]...)
				requireEventsKept(ctx, t, events, appended[2])
			}
		}

		requireEventsRemoved(ctx, t, events, appended...)
	})

	// Given an aggregated row whose first event is before the cutoff and whose last event is not, then
	// PurgeBefore keeps it, since it holds a recent event. Once its last event is before the cutoff,
	// the row is removed.
	t.Run("PurgeBeforeKeepsAnAggregatedRowUntilItsLastEventIsBeforeTheCutoff", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(auditTime)
		events := newStore(t, clk).AuditEvents()
		ctx := t.Context()

		burst := appendEvery(ctx, t, events, clk, 10*time.Second, 21, failedLogin("203.0.113.7"))
		latest := burst[len(burst)-1]
		require.Equal(t, int64(2), latest.Count)
		require.True(t, latest.OccurredAt.Before(latest.LastAt))

		purged, err := events.PurgeBefore(ctx, latest.LastAt, 100)
		require.NoError(t, err)
		require.Equal(t, int64(19), purged)
		requireEventsKept(ctx, t, events, latest)

		purged, err = events.PurgeBefore(ctx, latest.LastAt.Add(time.Millisecond), 100)
		require.NoError(t, err)
		require.Equal(t, int64(1), purged)
		requireEventsRemoved(ctx, t, events, latest)
	})

	// Given no event before the cutoff, then PurgeBefore removes nothing.
	t.Run("PurgeBeforeWithoutOldEventsRemovesNothing", func(t *testing.T) {
		t.Parallel()

		events := newStore(t, clock.NewFake(auditTime)).AuditEvents()
		ctx := t.Context()

		event := mustAppendEvent(ctx, t, events, auditEvent())

		purged, err := events.PurgeBefore(ctx, event.OccurredAt, 10)
		require.NoError(t, err)
		require.Zero(t, purged)
		requireEventsKept(ctx, t, events, event)
	})
}
