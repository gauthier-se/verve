package sqlite

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// Given priorities that order and ignore sources, when a source of each section is deleted, as a
// merge does once its last batch is committed, then they leave the priorities, which keep the other
// sources in their order. No store method deletes a source yet, so the test deletes them in SQL.
func TestSourcePriorities_DeletedSourceLeavesThePriorities(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	s, db := newTestStore(t, clock.NewFake(now))
	ctx := t.Context()

	user, err := s.Users().Create(ctx, store.NewUser{Username: "camille", Role: store.RoleMember})
	require.NoError(t, err)

	weight, err := s.MetricTypes().EnsureUncurated(ctx, "HKQuantityTypeIdentifierBodyMass", catalog.KindQuantity, "kg")
	require.NoError(t, err)

	var ids []int64

	for _, name := range []string{"Balance", "Ancienne balance", "Course", "Montre", "Ancienne montre"} {
		source, err := s.Sources().Resolve(ctx, user.ID, store.SourceInfo{Name: name}, store.SourceUnknown, now)
		require.NoError(t, err)

		ids = append(ids, source.ID)
	}

	require.NoError(t, s.SourcePriorities().Set(ctx, user.ID, weight.ID, store.SourcePriorities{
		Order:   []int64{ids[0], ids[1], ids[2]},
		Ignored: []int64{ids[3], ids[4]},
		Combine: true,
	}))

	for _, id := range []int64{ids[1], ids[4]} {
		_, err := db.Write().ExecContext(ctx, `DELETE FROM sources WHERE id = ?`, id)
		require.NoError(t, err)
	}

	got, err := s.SourcePriorities().Get(ctx, user.ID, weight.ID)
	require.NoError(t, err)
	require.Equal(t, store.SourcePriorities{Order: []int64{ids[0], ids[2]}, Ignored: []int64{ids[3]}, Combine: true}, got)
}
