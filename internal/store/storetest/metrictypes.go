package storetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// metricTypesTime has sub-millisecond digits and a non-UTC zone, so that the suite checks that
// CreatedAt is truncated to the millisecond and returned in UTC.
var metricTypesTime = time.Date(2026, 11, 11, 8, 30, 0, 456_789_123, time.FixedZone("CET", 3600))

// metricTypesCreatedAt is metricTypesTime as the store returns it.
var metricTypesCreatedAt = time.Date(2026, 11, 11, 7, 30, 0, 456_000_000, time.UTC)

// twoTypes is a catalog with one type of each kind.
const twoTypes = `
types:
  - key: heart_rate
    identifier: HKQuantityTypeIdentifierHeartRate
    kind: quantity
    unit: count/min
    aggregation: discrete
    group: heart
  - key: stand_hour
    identifier: HKCategoryTypeIdentifierAppleStandHour
    kind: category
    unit: count
    aggregation: discrete
    group: activity
    values:
      - {key: stood, identifier: HKCategoryValueAppleStandHourStood}
`

// parseCatalog parses a catalog in the layout of catalog.yaml.
func parseCatalog(t *testing.T, data string) *catalog.Catalog {
	t.Helper()

	c, err := catalog.Parse([]byte(data))
	require.NoError(t, err)

	return c
}

// mustSync synchronizes the types of c.
func mustSync(ctx context.Context, t *testing.T, s store.Store, c *catalog.Catalog) {
	t.Helper()

	require.NoError(t, s.MetricTypes().Sync(ctx, c))
}

// mustListMetricTypes returns every type.
func mustListMetricTypes(ctx context.Context, t *testing.T, s store.Store) []store.MetricType {
	t.Helper()

	types, err := s.MetricTypes().List(ctx)
	require.NoError(t, err)

	return types
}

// withoutIDs returns types with zero ids, for comparisons with types built by the suite.
func withoutIDs(types []store.MetricType) []store.MetricType {
	out := make([]store.MetricType, 0, len(types))
	for _, mt := range types {
		mt.ID = 0
		out = append(out, mt)
	}

	return out
}

// RunMetricTypes runs the MetricTypesRepository contract against the stores returned by newStore.
func RunMetricTypes(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a start, then every type of the catalog is stored with its attributes, curated, with the
	// clock's now (millisecond precision, UTC) as CreatedAt, in the order of the catalog.
	t.Run("SyncStoresEveryTypeOfTheCatalog", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(metricTypesTime))
		ctx := t.Context()

		mustSync(ctx, t, s, parseCatalog(t, twoTypes))

		require.Equal(t, []store.MetricType{
			{
				Key:         "heart_rate",
				Identifier:  "HKQuantityTypeIdentifierHeartRate",
				Kind:        catalog.KindQuantity,
				Unit:        "count/min",
				Aggregation: catalog.AggregationDiscrete,
				Group:       catalog.GroupHeart,
				Curated:     true,
				CreatedAt:   metricTypesCreatedAt,
			},
			{
				Key:         "stand_hour",
				Identifier:  "HKCategoryTypeIdentifierAppleStandHour",
				Kind:        catalog.KindCategory,
				Unit:        "count",
				Aggregation: catalog.AggregationDiscrete,
				Group:       catalog.GroupActivity,
				Curated:     true,
				CreatedAt:   metricTypesCreatedAt,
			},
		}, withoutIDs(mustListMetricTypes(ctx, t, s)))
	})

	// Given the shipped catalog, then Sync stores each of its types.
	t.Run("SyncStoresTheShippedCatalog", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(metricTypesTime))
		ctx := t.Context()

		shipped, err := catalog.Shipped()
		require.NoError(t, err)

		mustSync(ctx, t, s, shipped)

		stored := mustListMetricTypes(ctx, t, s)
		require.Len(t, stored, len(shipped.Types()))

		for i, want := range shipped.Types() {
			got := stored[i]
			require.Equal(t, want.Key, got.Key)
			require.Equal(t, want.Identifier, got.Identifier)
			require.Equal(t, want.Kind, got.Kind)
			require.Equal(t, want.Unit, got.Unit)
			require.Equal(t, want.Aggregation, got.Aggregation)
			require.Equal(t, want.Group, got.Group)
			require.True(t, got.Curated, want.Key)
		}
	})

	// Given a second start, later, then nothing changes, CreatedAt included.
	t.Run("SyncAgainChangesNothing", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(metricTypesTime)
		s := newStore(t, clk)
		ctx := t.Context()
		c := parseCatalog(t, twoTypes)

		mustSync(ctx, t, s, c)
		first := mustListMetricTypes(ctx, t, s)

		clk.Advance(time.Hour)
		mustSync(ctx, t, s, c)

		require.Equal(t, first, mustListMetricTypes(ctx, t, s))
	})

	// Given a catalog whose attributes of a type changed, key included, then the row is updated and
	// keeps its id and CreatedAt.
	t.Run("SyncUpdatesAChangedTypeAndKeepsItsID", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(metricTypesTime)
		s := newStore(t, clk)
		ctx := t.Context()

		mustSync(ctx, t, s, parseCatalog(t, twoTypes))
		before := mustListMetricTypes(ctx, t, s)

		clk.Advance(time.Hour)
		mustSync(ctx, t, s, parseCatalog(t, `
types:
  - key: pulse
    identifier: HKQuantityTypeIdentifierHeartRate
    kind: quantity
    unit: count/min
    aggregation: cumulative
    group: vitals
  - key: stand_hour
    identifier: HKCategoryTypeIdentifierAppleStandHour
    kind: category
    unit: count
    aggregation: discrete
    group: activity
    values:
      - {key: stood, identifier: HKCategoryValueAppleStandHourStood}
`))

		changed := before[0]
		changed.Key = "pulse"
		changed.Aggregation = catalog.AggregationCumulative
		changed.Group = catalog.GroupVitals

		require.Equal(t, []store.MetricType{changed, before[1]}, mustListMetricTypes(ctx, t, s))
	})

	// Given a catalog in which two types swap their keys, then each row takes its new key.
	t.Run("SyncSwapsTheKeysOfTwoTypes", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(metricTypesTime))
		ctx := t.Context()

		mustSync(ctx, t, s, parseCatalog(t, twoTypes))
		before := mustListMetricTypes(ctx, t, s)

		mustSync(ctx, t, s, parseCatalog(t, `
types:
  - key: stand_hour
    identifier: HKQuantityTypeIdentifierHeartRate
    kind: quantity
    unit: count/min
    aggregation: discrete
    group: heart
  - key: heart_rate
    identifier: HKCategoryTypeIdentifierAppleStandHour
    kind: category
    unit: count
    aggregation: discrete
    group: activity
    values:
      - {key: stood, identifier: HKCategoryValueAppleStandHourStood}
`))

		before[0].Key, before[1].Key = "stand_hour", "heart_rate"
		require.Equal(t, before, mustListMetricTypes(ctx, t, s))
	})

	// Given a type that a later catalog no longer lists, then it stays with its id and attributes,
	// uncurated.
	t.Run("SyncKeepsATypeThatTheCatalogDroppedAsUncurated", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(metricTypesTime))
		ctx := t.Context()

		mustSync(ctx, t, s, parseCatalog(t, twoTypes))
		before := mustListMetricTypes(ctx, t, s)

		mustSync(ctx, t, s, parseCatalog(t, `
types:
  - key: heart_rate
    identifier: HKQuantityTypeIdentifierHeartRate
    kind: quantity
    unit: count/min
    aggregation: discrete
    group: heart
`))

		before[1].Curated = false
		require.Equal(t, before, mustListMetricTypes(ctx, t, s))
	})

	// Given a type that a later catalog no longer lists and whose key it gives to another type, then
	// the dropped type becomes uncurated, keyed from its identifier by the rule of FR-2.
	t.Run("SyncRekeysADroppedTypeWhoseKeyTheCatalogTakes", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(metricTypesTime))
		ctx := t.Context()

		mustSync(ctx, t, s, parseCatalog(t, twoTypes))
		before := mustListMetricTypes(ctx, t, s)

		mustSync(ctx, t, s, parseCatalog(t, `
types:
  - key: heart_rate
    identifier: HKQuantityTypeIdentifierHeartRate
    kind: quantity
    unit: count/min
    aggregation: discrete
    group: heart
  - key: stand_hour
    identifier: HKQuantityTypeIdentifierAppleStandTime
    kind: quantity
    unit: s
    aggregation: cumulative
    group: activity
`))

		dropped, err := s.MetricTypes().Get(ctx, before[1].ID)
		require.NoError(t, err)

		before[1].Key, before[1].Curated = "apple_stand_hour", false
		require.Equal(t, before[1], dropped)

		taker, err := s.MetricTypes().GetByKey(ctx, "stand_hour")
		require.NoError(t, err)
		require.Equal(t, "HKQuantityTypeIdentifierAppleStandTime", taker.Identifier)
	})

	// Given a Sync through the repositories of WithTx, when the transaction rolls back, then no type
	// is stored.
	t.Run("RolledBackSyncStoresNothing", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(metricTypesTime))
		ctx := t.Context()
		errRollback := errors.New("roll back")

		err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			require.NoError(t, tx.MetricTypes().Sync(ctx, parseCatalog(t, twoTypes)))

			return errRollback
		})
		require.ErrorIs(t, err, errRollback)

		require.Empty(t, mustListMetricTypes(ctx, t, s))
	})

	runMetricTypesUncurated(t, newStore)
	runMetricTypesGet(t, newStore)
}

// mustEnsureUncurated returns the type of identifier, created uncurated if needed.
func mustEnsureUncurated(ctx context.Context, t *testing.T, s store.Store, identifier string, kind catalog.Kind,
	unit string,
) store.MetricType {
	t.Helper()

	mt, err := s.MetricTypes().EnsureUncurated(ctx, identifier, kind, unit)
	require.NoError(t, err)

	return mt
}

// runMetricTypesUncurated runs the contract of EnsureUncurated.
func runMetricTypesUncurated(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given an unknown identifier, when EnsureUncurated runs with a kind and a unit, then it returns
	// a new type, uncurated, with that unit, discrete, in the group other, keyed by the rule of FR-2
	// and with the clock's now as CreatedAt. Given the same identifier again, later, then it returns
	// the same type.
	t.Run("EnsureUncuratedCreatesATypeOnce", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(metricTypesTime)
		s := newStore(t, clk)
		ctx := t.Context()
		mustSync(ctx, t, s, parseCatalog(t, twoTypes))

		created := mustEnsureUncurated(ctx, t, s, "HKQuantityTypeIdentifierUVExposure", catalog.KindQuantity, "count")
		require.NotZero(t, created.ID)

		want := store.MetricType{
			ID:          created.ID,
			Key:         "uv_exposure",
			Identifier:  "HKQuantityTypeIdentifierUVExposure",
			Kind:        catalog.KindQuantity,
			Unit:        "count",
			Aggregation: catalog.AggregationDiscrete,
			Group:       catalog.GroupOther,
			Curated:     false,
			CreatedAt:   metricTypesCreatedAt,
		}
		require.Equal(t, want, created)

		clk.Advance(time.Hour)
		require.Equal(t, want, mustEnsureUncurated(ctx, t, s, "HKQuantityTypeIdentifierUVExposure", catalog.KindQuantity, "count"))

		category := mustEnsureUncurated(ctx, t, s, "Mood Swings", catalog.KindCategory, "count")
		require.Equal(t, "mood_swings", category.Key)
		require.Equal(t, catalog.KindCategory, category.Kind)

		require.Len(t, mustListMetricTypes(ctx, t, s), 4)
	})

	// Given the identifier of a type of the catalog, then EnsureUncurated returns that type unchanged.
	t.Run("EnsureUncuratedReturnsATypeOfTheCatalog", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(metricTypesTime))
		ctx := t.Context()
		mustSync(ctx, t, s, parseCatalog(t, twoTypes))
		heartRate := mustListMetricTypes(ctx, t, s)[0]

		require.Equal(t, heartRate,
			mustEnsureUncurated(ctx, t, s, "HKQuantityTypeIdentifierHeartRate", catalog.KindCategory, "bpm"))
	})

	// Given a derived key that is already taken, then the new key gets a numeric suffix, the first
	// one that is free. Given an identifier that leaves an empty key, then it is keyed uncurated, with
	// the same suffixes.
	t.Run("EnsureUncuratedSuffixesATakenKey", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(metricTypesTime))
		ctx := t.Context()
		mustSync(ctx, t, s, parseCatalog(t, twoTypes))

		tests := []struct {
			identifier string
			want       string
		}{
			{"Heart Rate", "heart_rate_2"},
			{"heart-rate", "heart_rate_3"},
			{"HKCategoryTypeIdentifierHeartRate", "heart_rate_4"},
			{"HKQuantityTypeIdentifier", "uncurated"},
			{"--", "uncurated_2"},
		}

		for _, tt := range tests {
			got := mustEnsureUncurated(ctx, t, s, tt.identifier, catalog.KindQuantity, "count")
			require.Equal(t, tt.want, got.Key, tt.identifier)
		}
	})

	// Given an empty identifier or an unknown kind, then EnsureUncurated returns store.ErrInvalid and
	// stores nothing.
	t.Run("EnsureUncuratedRefusesAnInvalidType", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(metricTypesTime))
		ctx := t.Context()

		_, err := s.MetricTypes().EnsureUncurated(ctx, "", catalog.KindQuantity, "count")
		require.ErrorIs(t, err, store.ErrInvalid)

		_, err = s.MetricTypes().EnsureUncurated(ctx, "Foo", catalog.Kind("workout"), "count")
		require.ErrorIs(t, err, store.ErrInvalid)

		require.Empty(t, mustListMetricTypes(ctx, t, s))
	})

	// Given concurrent calls for the same identifier, then one type exists and every call returns it.
	t.Run("ConcurrentEnsureUncuratedCreatesOneType", func(t *testing.T) {
		t.Parallel()

		const callers = 8

		s := newStore(t, clock.NewFake(metricTypesTime))
		ctx := t.Context()

		var wg sync.WaitGroup

		got := make([]store.MetricType, callers)
		errs := make([]error, callers)

		for i := range callers {
			wg.Go(func() {
				got[i], errs[i] = s.MetricTypes().EnsureUncurated(ctx, "Blood Oxygen", catalog.KindQuantity, "fraction")
			})
		}

		wg.Wait()

		for i := range callers {
			require.NoError(t, errs[i])
			require.Equal(t, got[0], got[i])
		}

		require.Equal(t, []store.MetricType{got[0]}, mustListMetricTypes(ctx, t, s))
	})

	// Given an uncurated type that a later catalog includes, then Sync makes it curated with the
	// attributes of the catalog and the same id and CreatedAt, so that its samples stay attached.
	t.Run("SyncCuratesAnUncuratedTypeAndKeepsItsID", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(metricTypesTime)
		s := newStore(t, clk)
		ctx := t.Context()
		mustSync(ctx, t, s, parseCatalog(t, twoTypes))

		uncurated := mustEnsureUncurated(ctx, t, s, "HKQuantityTypeIdentifierUVExposure", catalog.KindQuantity, "count")

		clk.Advance(time.Hour)
		mustSync(ctx, t, s, parseCatalog(t, twoTypes+`
  - key: uv_index
    identifier: HKQuantityTypeIdentifierUVExposure
    kind: quantity
    unit: count
    aggregation: cumulative
    group: vitals
`))

		got, err := s.MetricTypes().GetByIdentifier(ctx, "HKQuantityTypeIdentifierUVExposure")
		require.NoError(t, err)
		require.Equal(t, store.MetricType{
			ID:          uncurated.ID,
			Key:         "uv_index",
			Identifier:  "HKQuantityTypeIdentifierUVExposure",
			Kind:        catalog.KindQuantity,
			Unit:        "count",
			Aggregation: catalog.AggregationCumulative,
			Group:       catalog.GroupVitals,
			Curated:     true,
			CreatedAt:   metricTypesCreatedAt,
		}, got)
	})

	// Given an uncurated type whose key a later catalog gives to another type, then Sync stores the
	// type of the catalog and gives the uncurated type the next free key.
	t.Run("SyncRekeysAnUncuratedTypeWhoseKeyTheCatalogTakes", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(metricTypesTime))
		ctx := t.Context()
		mustSync(ctx, t, s, parseCatalog(t, twoTypes))

		uncurated := mustEnsureUncurated(ctx, t, s, "Blood Oxygen", catalog.KindQuantity, "fraction")
		require.Equal(t, "blood_oxygen", uncurated.Key)

		mustSync(ctx, t, s, parseCatalog(t, twoTypes+`
  - key: blood_oxygen
    identifier: HKQuantityTypeIdentifierOxygenSaturation
    kind: quantity
    unit: fraction
    aggregation: discrete
    group: vitals
`))

		curated, err := s.MetricTypes().GetByKey(ctx, "blood_oxygen")
		require.NoError(t, err)
		require.Equal(t, "HKQuantityTypeIdentifierOxygenSaturation", curated.Identifier)
		require.True(t, curated.Curated)

		uncurated.Key = "blood_oxygen_2"
		got, err := s.MetricTypes().Get(ctx, uncurated.ID)
		require.NoError(t, err)
		require.Equal(t, uncurated, got)
	})
}

// runMetricTypesGet runs the contract of Get, GetByKey, GetByIdentifier and List.
func runMetricTypesGet(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given Get by id, by key or by identifier, then it returns the type, or store.ErrNotFound.
	// Given List, then it returns every type, curated or not, sorted by id.
	t.Run("GetReturnsTheTypeOrErrNotFound", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(metricTypesTime))
		ctx := t.Context()
		mustSync(ctx, t, s, parseCatalog(t, twoTypes))

		uncurated := mustEnsureUncurated(ctx, t, s, "Blood Oxygen", catalog.KindQuantity, "fraction")
		all := mustListMetricTypes(ctx, t, s)
		require.Len(t, all, 3)
		require.Equal(t, uncurated, all[2])

		for _, want := range all {
			got, err := s.MetricTypes().Get(ctx, want.ID)
			require.NoError(t, err)
			require.Equal(t, want, got)

			got, err = s.MetricTypes().GetByKey(ctx, want.Key)
			require.NoError(t, err)
			require.Equal(t, want, got)

			got, err = s.MetricTypes().GetByIdentifier(ctx, want.Identifier)
			require.NoError(t, err)
			require.Equal(t, want, got)
		}

		_, err := s.MetricTypes().Get(ctx, 42)
		require.ErrorIs(t, err, store.ErrNotFound)

		_, err = s.MetricTypes().GetByKey(ctx, "blood_pressure")
		require.ErrorIs(t, err, store.ErrNotFound)

		_, err = s.MetricTypes().GetByIdentifier(ctx, "HKQuantityTypeIdentifierBloodPressure")
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	// Given no type, then List returns an empty list.
	t.Run("ListWithoutTypesIsEmpty", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(metricTypesTime))

		require.Empty(t, mustListMetricTypes(t.Context(), t, s))
	})
}
