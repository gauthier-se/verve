package audit

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

const day = 24 * time.Hour

// purgeTestNow is when the events of the purge tests are appended.
var purgeTestNow = time.Date(2026, 11, 3, 3, 0, 0, 0, time.UTC)

// newTestStore returns a store on a migrated database in a fresh temporary directory, and its fake
// clock, at purgeTestNow. The store is closed at the end of the test.
func newTestStore(t *testing.T) (store.Store, *clock.Fake) {
	t.Helper()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "verve.db"))
	require.NoError(t, err)

	_, err = sqlite.Migrate(t.Context(), db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	clk := clock.NewFake(purgeTestNow)
	s := sqlite.New(db, clk)

	t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

	return s, clk
}

// testEvent is an audit event of the system.
func testEvent() store.NewAuditEvent {
	return store.NewAuditEvent{Type: "system.backup_created", Payload: json.RawMessage(`{}`)}
}

// oldEvents holds count events older than any limit, and removes them as PurgeBefore asks, or fails
// with err. It records the limit and the result of each call, each of which stands for a
// transaction. Any other method of the repository panics.
type oldEvents struct {
	store.AuditEventsRepository

	count  int64
	limits []int64
	purged []int64
	err    error
}

func (f *oldEvents) PurgeBefore(_ context.Context, _ time.Time, limit int64) (int64, error) {
	f.limits = append(f.limits, limit)

	if f.err != nil {
		return 0, f.err
	}

	purged := min(f.count, limit)
	f.count -= purged
	f.purged = append(f.purged, purged)

	return purged, nil
}

// Given the daily audit.purge job, then only the events older than the retention period are
// removed: an event is kept on the last day of its retention.
func TestPurge_RemovesOnlyTheEventsOlderThanTheRetention(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		retentionDays int
		age           time.Duration
		removed       bool
	}{
		{name: "365DaysOld", retentionDays: 365, age: 365 * day},
		{name: "MoreThan365DaysOld", retentionDays: 365, age: 365*day + time.Millisecond, removed: true},
		{name: "30DaysOldWithARetentionOf30Days", retentionDays: 30, age: 30 * day},
		{name: "MoreThan30DaysOldWithARetentionOf30Days", retentionDays: 30, age: 30*day + time.Millisecond, removed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, clk := newTestStore(t)
			ctx := t.Context()
			events := s.AuditEvents()

			event, err := events.Append(ctx, testEvent())
			require.NoError(t, err)

			clk.Advance(tt.age)
			require.NoError(t, Purge(ctx, events, clk.Now(), tt.retentionDays))

			_, err = events.Get(ctx, event.ID)
			if tt.removed {
				require.ErrorIs(t, err, store.ErrNotFound)

				return
			}

			require.NoError(t, err)
		})
	}
}

// Given many old events, then they are removed in batches of at most 5,000 per transaction, until
// none is left. A batch of exactly 5,000 may be followed by an empty one.
func TestPurge_RemovesManyOldEventsInBatchesOf5000(t *testing.T) {
	t.Parallel()

	tests := []struct {
		count  int64
		purged []int64
	}{
		{count: 12_345, purged: []int64{5000, 5000, 2345}},
		{count: 10_000, purged: []int64{5000, 5000, 0}},
		{count: 4999, purged: []int64{4999}},
	}

	for _, tt := range tests {
		t.Run(strconv.FormatInt(tt.count, 10), func(t *testing.T) {
			t.Parallel()

			events := &oldEvents{count: tt.count}

			require.NoError(t, Purge(t.Context(), events, purgeTestNow, 365))

			require.Zero(t, events.count)
			require.Equal(t, tt.purged, events.purged)

			for i, limit := range events.limits {
				require.Equal(t, int64(5000), limit, "call %d", i)
			}
		})
	}
}

// Given a repository that fails, then Purge returns its error, for the job to be retried.
func TestPurge_ReturnsTheErrorOfTheRepository(t *testing.T) {
	t.Parallel()

	errPurge := errors.New("disk I/O error")
	events := &oldEvents{count: 10_000, err: errPurge}

	err := Purge(t.Context(), events, purgeTestNow, 365)

	require.ErrorIs(t, err, errPurge)
	require.Len(t, events.limits, 1)
}
