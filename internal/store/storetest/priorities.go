package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// prioritiesTime is when the sources of the suite are seen.
var prioritiesTime = time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)

// prioritiesTypes is a catalog with a discrete quantity, a cumulative quantity and a category.
const prioritiesTypes = `
types:
  - key: body_mass
    identifier: HKQuantityTypeIdentifierBodyMass
    kind: quantity
    unit: kg
    aggregation: discrete
    group: body
  - key: step_count
    identifier: HKQuantityTypeIdentifierStepCount
    kind: quantity
    unit: count
    aggregation: cumulative
    group: activity
  - key: stand_hour
    identifier: HKCategoryTypeIdentifierAppleStandHour
    kind: category
    unit: count
    aggregation: discrete
    group: activity
    values:
      - {key: stood, identifier: HKCategoryValueAppleStandHourStood}
`

// prioritiesFixture holds two users, the types of prioritiesTypes and sources of both users.
type prioritiesFixture struct {
	s          store.Store
	user       store.User
	other      store.User
	discrete   store.MetricType
	cumulative store.MetricType
	category   store.MetricType
	// watch, scale and app are sources of user, and foreign the source of other.
	watch   store.Source
	scale   store.Source
	app     store.Source
	foreign store.Source
}

func newPrioritiesFixture(ctx context.Context, t *testing.T, newStore NewStoreFunc) prioritiesFixture {
	t.Helper()

	s := newStore(t, clock.NewFake(prioritiesTime))
	mustSync(ctx, t, s, parseCatalog(t, prioritiesTypes))

	typeOf := func(key string) store.MetricType {
		t.Helper()

		got, err := s.MetricTypes().GetByKey(ctx, key)
		require.NoError(t, err)

		return got
	}

	user := mustCreateUser(ctx, t, s.Users(), newUser("camille", ""))
	other := mustCreateUser(ctx, t, s.Users(), newUser("dominique", ""))

	return prioritiesFixture{
		s:          s,
		user:       user,
		other:      other,
		discrete:   typeOf("body_mass"),
		cumulative: typeOf("step_count"),
		category:   typeOf("stand_hour"),
		watch:      mustResolve(ctx, t, s.Sources(), user.ID, watchInfo, store.SourceWatch, prioritiesTime),
		scale:      mustResolve(ctx, t, s.Sources(), user.ID, sourceInfo("Balance", ""), store.SourceScale, prioritiesTime),
		app:        mustResolve(ctx, t, s.Sources(), user.ID, sourceInfo("Course", ""), store.SourceApp, prioritiesTime),
		foreign:    mustResolve(ctx, t, s.Sources(), other.ID, watchInfo, store.SourceWatch, prioritiesTime),
	}
}

// requirePriorities checks that userID has the priorities want for typeID.
func requirePriorities(ctx context.Context, t *testing.T, priorities store.SourcePrioritiesRepository, userID, typeID int64,
	want store.SourcePriorities,
) {
	t.Helper()

	got, err := priorities.Get(ctx, userID, typeID)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// requireNoPriorities checks that userID set no priorities for typeID.
func requireNoPriorities(ctx context.Context, t *testing.T, priorities store.SourcePrioritiesRepository, userID, typeID int64) {
	t.Helper()

	_, err := priorities.Get(ctx, userID, typeID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// RunSourcePriorities runs the SourcePrioritiesRepository contract against the stores returned by
// newStore.
func RunSourcePriorities(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	runSourcePrioritiesSet(t, newStore)
	runSourcePrioritiesRefused(t, newStore)
	runSourcePrioritiesCombine(t, newStore)
}

// runSourcePrioritiesSet runs the cases of the priorities stored by Set.
func runSourcePrioritiesSet(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a type without stored priorities, then Get returns store.ErrNotFound.
	t.Run("GetWithoutStoredPrioritiesReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		f := newPrioritiesFixture(ctx, t, newStore)

		requireNoPriorities(ctx, t, f.s.SourcePriorities(), f.user.ID, f.discrete.ID)
	})

	// Given priorities set for a user and a type, then Get returns their order, ignored sources and
	// combine flag, and the other types and users have none. Given Set again, then the priorities
	// are replaced, the sources left out included.
	t.Run("SetStoresThePrioritiesOfTheUserForTheType", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		f := newPrioritiesFixture(ctx, t, newStore)
		priorities := f.s.SourcePriorities()

		set := store.SourcePriorities{Order: []int64{f.app.ID, f.watch.ID}, Ignored: []int64{f.scale.ID}}
		require.NoError(t, priorities.Set(ctx, f.user.ID, f.cumulative.ID, set))

		requirePriorities(ctx, t, priorities, f.user.ID, f.cumulative.ID, set)
		requireNoPriorities(ctx, t, priorities, f.user.ID, f.discrete.ID)
		requireNoPriorities(ctx, t, priorities, f.other.ID, f.cumulative.ID)

		replaced := store.SourcePriorities{Order: []int64{f.watch.ID}}
		require.NoError(t, priorities.Set(ctx, f.user.ID, f.cumulative.ID, replaced))

		requirePriorities(ctx, t, priorities, f.user.ID, f.cumulative.ID, replaced)
	})

	// Given priorities without ordered or ignored sources, then they are stored, so that Get tells
	// them from priorities never set.
	t.Run("SetStoresEmptyPriorities", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		f := newPrioritiesFixture(ctx, t, newStore)

		require.NoError(t, f.s.SourcePriorities().Set(ctx, f.user.ID, f.discrete.ID, store.SourcePriorities{}))

		requirePriorities(ctx, t, f.s.SourcePriorities(), f.user.ID, f.discrete.ID, store.SourcePriorities{})
	})

	// Given priorities set through the repositories of WithTx, when the transaction rolls back, then
	// the user has none.
	t.Run("RolledBackTransactionLeavesNoPriorities", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		f := newPrioritiesFixture(ctx, t, newStore)
		errRollback := errors.New("roll back")

		err := f.s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			set := store.SourcePriorities{Order: []int64{f.watch.ID}}
			require.NoError(t, tx.SourcePriorities().Set(ctx, f.user.ID, f.discrete.ID, set))
			requirePriorities(ctx, t, tx.SourcePriorities(), f.user.ID, f.discrete.ID, set)

			return errRollback
		})
		require.ErrorIs(t, err, errRollback)

		requireNoPriorities(ctx, t, f.s.SourcePriorities(), f.user.ID, f.discrete.ID)
	})
}

// runSourcePrioritiesRefused runs the cases of the priorities that Set refuses.
func runSourcePrioritiesRefused(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given priorities that hold a source of another user, a source that does not exist, or a
	// source both ordered and ignored or listed twice, when they are set, then Set returns
	// store.ErrInvalid and nothing is stored: the earlier priorities of the type stay, and a type
	// without priorities keeps none.
	t.Run("SetRefusesInvalidSources", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			priorities func(f prioritiesFixture) store.SourcePriorities
		}{
			{"OrderedSourceOfAnotherUser", func(f prioritiesFixture) store.SourcePriorities {
				return store.SourcePriorities{Order: []int64{f.watch.ID, f.foreign.ID}}
			}},
			{"IgnoredSourceOfAnotherUser", func(f prioritiesFixture) store.SourcePriorities {
				return store.SourcePriorities{Order: []int64{f.watch.ID}, Ignored: []int64{f.foreign.ID}}
			}},
			{"OrderedSourceThatDoesNotExist", func(f prioritiesFixture) store.SourcePriorities {
				return store.SourcePriorities{Order: []int64{f.watch.ID, f.foreign.ID + 100}}
			}},
			{"IgnoredSourceThatDoesNotExist", func(f prioritiesFixture) store.SourcePriorities {
				return store.SourcePriorities{Ignored: []int64{f.foreign.ID + 100}}
			}},
			{"SourceOrderedAndIgnored", func(f prioritiesFixture) store.SourcePriorities {
				return store.SourcePriorities{Order: []int64{f.watch.ID, f.app.ID}, Ignored: []int64{f.app.ID}}
			}},
			{"SourceOrderedTwice", func(f prioritiesFixture) store.SourcePriorities {
				return store.SourcePriorities{Order: []int64{f.watch.ID, f.app.ID, f.watch.ID}}
			}},
			{"SourceIgnoredTwice", func(f prioritiesFixture) store.SourcePriorities {
				return store.SourcePriorities{Ignored: []int64{f.scale.ID, f.scale.ID}}
			}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				ctx := t.Context()
				f := newPrioritiesFixture(ctx, t, newStore)
				priorities := f.s.SourcePriorities()

				earlier := store.SourcePriorities{Order: []int64{f.scale.ID}, Ignored: []int64{f.app.ID}}
				require.NoError(t, priorities.Set(ctx, f.user.ID, f.discrete.ID, earlier))

				err := priorities.Set(ctx, f.user.ID, f.discrete.ID, tt.priorities(f))
				require.ErrorIs(t, err, store.ErrInvalid)
				requirePriorities(ctx, t, priorities, f.user.ID, f.discrete.ID, earlier)

				err = priorities.Set(ctx, f.user.ID, f.cumulative.ID, tt.priorities(f))
				require.ErrorIs(t, err, store.ErrInvalid)
				requireNoPriorities(ctx, t, priorities, f.user.ID, f.cumulative.ID)
			})
		}
	})

	// Given a user id that no account has, or a type id that no type has, then Set returns
	// store.ErrNotFound and stores nothing.
	t.Run("SetOnAMissingUserOrTypeReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		f := newPrioritiesFixture(ctx, t, newStore)
		priorities := f.s.SourcePriorities()
		missingUser := f.other.ID + 100
		missingType := f.category.ID + 100

		err := priorities.Set(ctx, missingUser, f.discrete.ID, store.SourcePriorities{})
		require.ErrorIs(t, err, store.ErrNotFound)
		requireNoPriorities(ctx, t, priorities, missingUser, f.discrete.ID)

		err = priorities.Set(ctx, f.user.ID, missingType, store.SourcePriorities{Order: []int64{f.watch.ID}})
		require.ErrorIs(t, err, store.ErrNotFound)
		requireNoPriorities(ctx, t, priorities, f.user.ID, missingType)
	})
}

// runSourcePrioritiesCombine runs the cases of the combine mode.
func runSourcePrioritiesCombine(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given the combine mode on a cumulative or a category type, when it is set, then Set returns
	// store.ErrCombineNotDiscrete, whose message is the stable combine_not_discrete, and nothing is
	// stored.
	t.Run("SetRefusesCombineOnACumulativeOrCategoryType", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		f := newPrioritiesFixture(ctx, t, newStore)
		priorities := f.s.SourcePriorities()
		combine := store.SourcePriorities{Order: []int64{f.watch.ID}, Combine: true}

		require.Equal(t, "combine_not_discrete", store.ErrCombineNotDiscrete.Error())

		for _, metricType := range []store.MetricType{f.cumulative, f.category} {
			err := priorities.Set(ctx, f.user.ID, metricType.ID, combine)
			require.ErrorIs(t, err, store.ErrCombineNotDiscrete, metricType.Key)
			requireNoPriorities(ctx, t, priorities, f.user.ID, metricType.ID)

			require.NoError(t, priorities.Set(ctx, f.user.ID, metricType.ID, store.SourcePriorities{Order: combine.Order}))
		}
	})

	// Given the combine mode on a discrete type, when it is set, then it is stored and read back,
	// and setting it off again stores it off.
	t.Run("SetStoresCombineOnADiscreteType", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		f := newPrioritiesFixture(ctx, t, newStore)
		priorities := f.s.SourcePriorities()

		combine := store.SourcePriorities{Order: []int64{f.scale.ID, f.app.ID}, Ignored: []int64{f.watch.ID}, Combine: true}
		require.NoError(t, priorities.Set(ctx, f.user.ID, f.discrete.ID, combine))
		requirePriorities(ctx, t, priorities, f.user.ID, f.discrete.ID, combine)

		combine.Combine = false
		require.NoError(t, priorities.Set(ctx, f.user.ID, f.discrete.ID, combine))
		requirePriorities(ctx, t, priorities, f.user.ID, f.discrete.ID, combine)
	})
}
