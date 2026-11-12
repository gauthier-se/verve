package storetest

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// samplesTime has sub-millisecond digits and a non-UTC zone, so that the suite checks that the
// times of a sample are truncated to the millisecond and returned in UTC.
var samplesTime = time.Date(2026, 11, 12, 8, 30, 0, 123_456_789, time.FixedZone("CET", 3600))

// samplesUTC is samplesTime truncated to the millisecond, in UTC.
var samplesUTC = time.Date(2026, 11, 12, 7, 30, 0, 123_000_000, time.UTC)

// allTime is a range that holds every sample of the suite.
var allTime = store.SamplesQuery{From: time.Unix(0, 0), To: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)}

// samplesTest holds a store with two users, a source of each, and a quantity and a category type.
type samplesTest struct {
	store    store.Store
	user     store.User
	other    store.User
	source   store.Source
	foreign  store.Source
	heart    store.MetricType
	standing store.MetricType
}

func newSamplesTest(t *testing.T, newStore NewStoreFunc) samplesTest {
	t.Helper()

	s := newStore(t, clock.NewFake(samplesTime))
	ctx := t.Context()
	user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))
	other := mustCreateUser(ctx, t, s.Users(), newUser("dominique", "dominique@example.com"))

	heart, err := s.MetricTypes().EnsureUncurated(ctx, "HKQuantityTypeIdentifierHeartRate", catalog.KindQuantity, "count/min")
	require.NoError(t, err)

	standing, err := s.MetricTypes().EnsureUncurated(ctx, "HKCategoryTypeIdentifierAppleStandHour", catalog.KindCategory, "count")
	require.NoError(t, err)

	return samplesTest{
		store:    s,
		user:     user,
		other:    other,
		source:   mustResolve(ctx, t, s.Sources(), user.ID, watchInfo, store.SourceWatch, samplesTime),
		foreign:  mustResolve(ctx, t, s.Sources(), other.ID, watchInfo, store.SourceWatch, samplesTime),
		heart:    heart,
		standing: standing,
	}
}

// heartRate returns a raw heart rate sample of the user of st, keyed by key, starting at start and
// lasting a minute, to store with rule.
func (st samplesTest) heartRate(key string, start time.Time, value float64, rule store.ConflictRule) store.NewSample {
	return store.NewSample{
		Sample: store.Sample{
			TypeID:     st.heart.ID,
			SourceID:   st.source.ID,
			Start:      start,
			End:        start.Add(time.Minute),
			UTCOffset:  3600,
			Value:      value,
			Origin:     store.OriginXML,
			ImportID:   42,
			NaturalKey: []byte(key),
		},
		OnConflict: rule,
	}
}

// foreignHeartRate is heartRate for the other user of st.
func (st samplesTest) foreignHeartRate(key string, start time.Time, value float64) store.NewSample {
	sample := st.heartRate(key, start, value, store.ConflictIgnore)
	sample.SourceID = st.foreign.ID

	return sample
}

func (st samplesTest) mustUpsert(ctx context.Context, t *testing.T, userID int64, samples ...store.NewSample) store.UpsertResult {
	t.Helper()

	got, err := st.store.Samples().Upsert(ctx, userID, samples)
	require.NoError(t, err)

	return got
}

// mustListAll returns every sample of userID and of typeID, read in pages of pageSize.
func (st samplesTest) mustListAll(ctx context.Context, t *testing.T, userID, typeID int64, pageSize int) []store.Sample {
	t.Helper()

	query := allTime
	query.TypeID = typeID
	query.Limit = pageSize

	var all []store.Sample

	for {
		page, err := st.store.Samples().List(ctx, userID, query)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page), pageSize)

		all = append(all, page...)

		if len(page) < pageSize {
			return all
		}

		query.After = page[len(page)-1].Cursor()
	}
}

func (st samplesTest) mustStats(ctx context.Context, t *testing.T, userID int64) []store.SampleStats {
	t.Helper()

	got, err := st.store.Samples().Stats(ctx, userID)
	require.NoError(t, err)

	return got
}

// stored returns sample as the store returns it for userID, with id.
func stored(sample store.NewSample, userID, id int64) store.Sample {
	got := sample.Sample
	got.ID = id
	got.UserID = userID
	got.Start = got.Start.UTC().Truncate(time.Millisecond)
	got.End = got.End.UTC().Truncate(time.Millisecond)

	return got
}

func ptr(v float64) *float64 {
	return &v
}

// RunSamples runs the SamplesRepository contract against the stores returned by newStore.
func RunSamples(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	runSamplesUpsert(t, newStore)
	runSamplesConflicts(t, newStore)
	runSamplesInvalid(t, newStore)
	runSamplesStats(t, newStore)
	runSamplesList(t, newStore)
	runSamplesDelete(t, newStore)
}

// runSamplesUpsert runs the cases of the samples stored by an upsert.
func runSamplesUpsert(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a batch of a pre-aggregated point with a uuid and metadata and of a category sample,
	// when Upsert runs, then both are inserted with every field, their times in UTC with millisecond
	// precision.
	t.Run("UpsertStoresEveryFieldOfEachSample", func(t *testing.T) {
		t.Parallel()

		st := newSamplesTest(t, newStore)
		ctx := t.Context()

		point := st.heartRate("point", samplesTime, 72.5, store.ConflictReplace)
		point.End = samplesTime.Add(time.Hour)
		point.UTCOffset = -16200
		point.Min = ptr(0)
		point.Max = ptr(131.25)
		point.Origin = store.OriginHAE
		point.ImportID = 7
		point.UUID = "4D2A8E3C-1F0B-4C6E-9A7D-2B5E8C1F3A90"
		point.NaturalKey = []byte{0x00, 0xff, 0x10, 0x80}
		point.Metadata = json.RawMessage(`{"HKMetadataKeyHeartRateMotionContext":1}`)

		stood := store.NewSample{
			Sample: store.Sample{
				TypeID:     st.standing.ID,
				SourceID:   st.source.ID,
				Start:      samplesTime,
				End:        samplesTime.Add(time.Hour),
				Value:      1,
				Category:   "stood",
				Origin:     store.OriginNative,
				NaturalKey: []byte("stood"),
			},
			OnConflict: store.ConflictUpdateIfChanged,
		}

		got := st.mustUpsert(ctx, t, st.user.ID, point, stood)
		require.Equal(t, store.UpsertResult{Inserted: 2}, got)

		points := st.mustListAll(ctx, t, st.user.ID, st.heart.ID, 10)
		require.Len(t, points, 1)
		require.NotZero(t, points[0].ID)
		require.Equal(t, samplesUTC, points[0].Start)
		require.Equal(t, stored(point, st.user.ID, points[0].ID), points[0])

		stoods := st.mustListAll(ctx, t, st.user.ID, st.standing.ID, 10)
		require.Len(t, stoods, 1)
		require.Equal(t, stored(stood, st.user.ID, stoods[0].ID), stoods[0])
		require.Nil(t, stoods[0].Min)
		require.Nil(t, stoods[0].Max)
		require.Nil(t, stoods[0].Metadata)
	})

	// Given a batch with a new sample and one of each rule on stored samples, of which the update if
	// changed one changed, when Upsert runs, then the result counts them: one inserted, two updated
	// and one duplicate.
	t.Run("UpsertCountsInsertedUpdatedAndDuplicateSamples", func(t *testing.T) {
		t.Parallel()

		st := newSamplesTest(t, newStore)
		ctx := t.Context()

		st.mustUpsert(ctx, t, st.user.ID,
			st.heartRate("a", samplesTime, 60, store.ConflictIgnore),
			st.heartRate("b", samplesTime, 61, store.ConflictIgnore),
			st.heartRate("c", samplesTime, 62, store.ConflictIgnore),
		)

		got := st.mustUpsert(ctx, t, st.user.ID,
			st.heartRate("a", samplesTime, 70, store.ConflictIgnore),
			st.heartRate("b", samplesTime, 71, store.ConflictUpdateIfChanged),
			st.heartRate("c", samplesTime, 62, store.ConflictReplace),
			st.heartRate("d", samplesTime, 63, store.ConflictIgnore),
		)
		require.Equal(t, store.UpsertResult{Inserted: 1, Updated: 2, Duplicates: 1}, got)

		require.Len(t, st.mustListAll(ctx, t, st.user.ID, st.heart.ID, 10), 4)
	})

	// Given two samples of one natural key in one batch, then the second one meets the first one as
	// stored, by its rule.
	t.Run("UpsertAppliesTheRuleToAnEarlierSampleOfTheBatch", func(t *testing.T) {
		t.Parallel()

		st := newSamplesTest(t, newStore)
		ctx := t.Context()

		got := st.mustUpsert(ctx, t, st.user.ID,
			st.heartRate("a", samplesTime, 60, store.ConflictIgnore),
			st.heartRate("a", samplesTime, 60, store.ConflictIgnore),
			st.heartRate("b", samplesTime, 61, store.ConflictReplace),
			st.heartRate("b", samplesTime, 65, store.ConflictReplace),
		)
		require.Equal(t, store.UpsertResult{Inserted: 2, Updated: 1, Duplicates: 1}, got)

		all := st.mustListAll(ctx, t, st.user.ID, st.heart.ID, 10)
		require.Len(t, all, 2)
		require.InDelta(t, 65, all[1].Value, 0)
	})

	// Given an empty batch, then Upsert counts nothing.
	t.Run("UpsertOfAnEmptyBatchCountsNothing", func(t *testing.T) {
		t.Parallel()

		st := newSamplesTest(t, newStore)

		require.Equal(t, store.UpsertResult{}, st.mustUpsert(t.Context(), t, st.user.ID))
	})

	// Given the natural key of a sample of another user, then the sample is inserted, and each user
	// lists only their own.
	t.Run("UpsertOfTheNaturalKeyOfAnotherUserInserts", func(t *testing.T) {
		t.Parallel()

		st := newSamplesTest(t, newStore)
		ctx := t.Context()

		st.mustUpsert(ctx, t, st.other.ID, st.foreignHeartRate("a", samplesTime, 60))

		mine := st.heartRate("a", samplesTime, 80, store.ConflictIgnore)
		require.Equal(t, store.UpsertResult{Inserted: 1}, st.mustUpsert(ctx, t, st.user.ID, mine))

		own := st.mustListAll(ctx, t, st.user.ID, st.heart.ID, 10)
		require.Len(t, own, 1)
		require.Equal(t, stored(mine, st.user.ID, own[0].ID), own[0])

		foreign := st.mustListAll(ctx, t, st.other.ID, st.heart.ID, 10)
		require.Len(t, foreign, 1)
		require.InDelta(t, 60, foreign[0].Value, 0)
		require.Equal(t, st.other.ID, foreign[0].UserID)
	})
}

// runSamplesConflicts runs the cases of the conflict rules.
func runSamplesConflicts(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given the rule ignore and a natural key already stored, then the sample is a duplicate and the
	// stored one is unchanged, even with other values.
	t.Run("IgnoreKeepsTheStoredSample", func(t *testing.T) {
		t.Parallel()

		st := newSamplesTest(t, newStore)
		ctx := t.Context()

		first := st.heartRate("a", samplesTime, 60, store.ConflictIgnore)
		st.mustUpsert(ctx, t, st.user.ID, first)
		before := st.mustListAll(ctx, t, st.user.ID, st.heart.ID, 10)

		again := st.heartRate("a", samplesTime.Add(time.Hour), 99, store.ConflictIgnore)
		again.Min = ptr(1)
		again.Max = ptr(2)
		again.ImportID = 43
		again.Metadata = json.RawMessage(`{"other":true}`)

		require.Equal(t, store.UpsertResult{Duplicates: 1}, st.mustUpsert(ctx, t, st.user.ID, again))
		require.Equal(t, before, st.mustListAll(ctx, t, st.user.ID, st.heart.ID, 10))
	})

	// Given the rule update if changed and a stored sample, then the same value, min, max and
	// category value count as a duplicate and leave it unchanged, and a change of one of them counts
	// as updated and replaces all four, the other fields being kept.
	t.Run("UpdateIfChangedUpdatesOnlyAChangedValue", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name   string
			change func(*store.NewSample)
			want   store.UpsertResult
		}{
			{"SameValue", func(*store.NewSample) {}, store.UpsertResult{Duplicates: 1}},
			{"OtherFieldsOnly", func(s *store.NewSample) {
				s.ImportID = 43
				s.Metadata = json.RawMessage(`{"other":true}`)
			}, store.UpsertResult{Duplicates: 1}},
			{"NewValue", func(s *store.NewSample) { s.Value = 64 }, store.UpsertResult{Updated: 1}},
			{"NewMin", func(s *store.NewSample) { s.Min = ptr(51) }, store.UpsertResult{Updated: 1}},
			{"NoMax", func(s *store.NewSample) { s.Max = nil }, store.UpsertResult{Updated: 1}},
			{"NewCategory", func(s *store.NewSample) { s.Category = "idle" }, store.UpsertResult{Updated: 1}},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				st := newSamplesTest(t, newStore)
				ctx := t.Context()

				first := st.heartRate("a", samplesTime, 63, store.ConflictUpdateIfChanged)
				first.Min = ptr(50)
				first.Max = ptr(80)
				first.Category = "stood"
				first.UUID = "8F14E45F-CEEA-467A-A866-051F0A5BBA11"
				first.Metadata = json.RawMessage(`{"first":true}`)
				st.mustUpsert(ctx, t, st.user.ID, first)
				before := st.mustListAll(ctx, t, st.user.ID, st.heart.ID, 10)

				again := first
				tc.change(&again)

				require.Equal(t, tc.want, st.mustUpsert(ctx, t, st.user.ID, again))

				want := before[0]
				want.Value, want.Min, want.Max, want.Category = again.Value, again.Min, again.Max, again.Category
				require.Equal(t, []store.Sample{want}, st.mustListAll(ctx, t, st.user.ID, st.heart.ID, 10))
			})
		}
	})

	// Given the rule replace and a stored sample, then its value, min and max are replaced, and the
	// sample counts as updated, also with the same values. The other fields are kept.
	t.Run("ReplaceReplacesValueMinAndMax", func(t *testing.T) {
		t.Parallel()

		st := newSamplesTest(t, newStore)
		ctx := t.Context()

		first := st.heartRate("a", samplesTime, 70, store.ConflictReplace)
		first.Min = ptr(55)
		first.Max = ptr(90)
		first.Origin = store.OriginHAE
		st.mustUpsert(ctx, t, st.user.ID, first)
		before := st.mustListAll(ctx, t, st.user.ID, st.heart.ID, 10)

		again := st.heartRate("a", samplesTime, 72, store.ConflictReplace)
		again.Min = ptr(54)
		again.Max = nil
		again.Origin = store.OriginNative
		again.Category = "ignored"

		require.Equal(t, store.UpsertResult{Updated: 1}, st.mustUpsert(ctx, t, st.user.ID, again))

		want := before[0]
		want.Value, want.Min, want.Max = 72, ptr(54), nil
		require.Equal(t, []store.Sample{want}, st.mustListAll(ctx, t, st.user.ID, st.heart.ID, 10))

		require.Equal(t, store.UpsertResult{Updated: 1}, st.mustUpsert(ctx, t, st.user.ID, again), "the same values")
		require.Equal(t, []store.Sample{want}, st.mustListAll(ctx, t, st.user.ID, st.heart.ID, 10))
	})
}

// runSamplesInvalid runs the cases of the batches that Upsert refuses.
func runSamplesInvalid(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a batch whose last sample is invalid, then Upsert answers ErrInvalid and writes nothing
	// of the batch: neither its samples nor their data availability.
	cases := []struct {
		name  string
		spoil func(st samplesTest, s *store.NewSample)
	}{
		{"UnknownType", func(st samplesTest, s *store.NewSample) { s.TypeID = st.heart.ID + st.standing.ID + 1 }},
		{"SourceOfAnotherUser", func(st samplesTest, s *store.NewSample) { s.SourceID = st.foreign.ID }},
		{"UnknownSource", func(st samplesTest, s *store.NewSample) { s.SourceID = st.source.ID + st.foreign.ID + 1 }},
		{"EmptyNaturalKey", func(_ samplesTest, s *store.NewSample) { s.NaturalKey = nil }},
		{"UnknownRule", func(_ samplesTest, s *store.NewSample) { s.OnConflict = "merge" }},
		{"UnknownOrigin", func(_ samplesTest, s *store.NewSample) { s.Origin = "csv" }},
		{"EndBeforeStart", func(_ samplesTest, s *store.NewSample) { s.End = s.Start.Add(-time.Millisecond) }},
		{"MetadataNotJSON", func(_ samplesTest, s *store.NewSample) { s.Metadata = json.RawMessage(`{"a":`) }},
		{"MetadataNotAnObject", func(_ samplesTest, s *store.NewSample) { s.Metadata = json.RawMessage(`[1]`) }},
	}

	for _, tc := range cases {
		t.Run("UpsertRefusesABatchWith"+tc.name, func(t *testing.T) {
			t.Parallel()

			st := newSamplesTest(t, newStore)
			ctx := t.Context()

			bad := st.heartRate("bad", samplesTime.Add(time.Hour), 70, store.ConflictIgnore)
			tc.spoil(st, &bad)
			batch := []store.NewSample{st.heartRate("good", samplesTime, 60, store.ConflictIgnore), bad}

			_, err := st.store.Samples().Upsert(ctx, st.user.ID, batch)
			require.ErrorIs(t, err, store.ErrInvalid)

			// A caller that commits its transaction despite the error still writes nothing of the
			// batch.
			require.NoError(t, st.store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
				_, err := tx.Samples().Upsert(ctx, st.user.ID, batch)
				require.ErrorIs(t, err, store.ErrInvalid)

				return nil
			}))

			require.Empty(t, st.mustListAll(ctx, t, st.user.ID, st.heart.ID, 10))
			require.Empty(t, st.mustStats(ctx, t, st.user.ID))
		})
	}

	// Given an unknown user, then Upsert answers ErrInvalid, since no source belongs to them.
	t.Run("UpsertRefusesAnUnknownUser", func(t *testing.T) {
		t.Parallel()

		st := newSamplesTest(t, newStore)

		_, err := st.store.Samples().Upsert(t.Context(), st.user.ID+st.other.ID+1,
			[]store.NewSample{st.heartRate("a", samplesTime, 60, store.ConflictIgnore)})
		require.ErrorIs(t, err, store.ErrInvalid)
	})
}

// runSamplesStats runs the cases of the data availability.
func runSamplesStats(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given samples of two types, then Stats returns, per type sorted by id, the first and last
	// start and the count of the samples of the user. Duplicates and updates count nothing, and
	// another user has their own.
	t.Run("StatsGivesTheFirstAndLastStartAndTheCountPerType", func(t *testing.T) {
		t.Parallel()

		st := newSamplesTest(t, newStore)
		ctx := t.Context()

		require.Empty(t, st.mustStats(ctx, t, st.user.ID))

		stood := st.heartRate("stood", samplesTime.Add(2*time.Hour), 1, store.ConflictIgnore)
		stood.TypeID = st.standing.ID

		st.mustUpsert(ctx, t, st.user.ID,
			st.heartRate("a", samplesTime.Add(time.Hour), 60, store.ConflictIgnore),
			st.heartRate("b", samplesTime, 61, store.ConflictIgnore),
			stood,
		)
		st.mustUpsert(ctx, t, st.user.ID,
			st.heartRate("c", samplesTime.Add(-time.Hour), 62, store.ConflictIgnore),
			st.heartRate("a", samplesTime.Add(-2*time.Hour), 70, store.ConflictUpdateIfChanged),
			st.heartRate("b", samplesTime.Add(3*time.Hour), 71, store.ConflictReplace),
		)
		st.mustUpsert(ctx, t, st.other.ID, st.foreignHeartRate("a", samplesTime.Add(-24*time.Hour), 60))

		require.Equal(t, []store.SampleStats{
			{TypeID: st.heart.ID, FirstAt: samplesUTC.Add(-time.Hour), LastAt: samplesUTC.Add(time.Hour), Count: 3},
			{TypeID: st.standing.ID, FirstAt: samplesUTC.Add(2 * time.Hour), LastAt: samplesUTC.Add(2 * time.Hour), Count: 1},
		}, st.mustStats(ctx, t, st.user.ID))

		require.Equal(t, []store.SampleStats{
			{TypeID: st.heart.ID, FirstAt: samplesUTC.Add(-24 * time.Hour), LastAt: samplesUTC.Add(-24 * time.Hour), Count: 1},
		}, st.mustStats(ctx, t, st.other.ID))
	})

	// Given any sequence of random batches of two users, over a few natural keys, both types and
	// every rule, then after each batch Stats equals a computation over the samples that List
	// returns.
	for seed := range uint64(8) {
		t.Run(fmt.Sprintf("StatsMatchTheSamplesAfterRandomBatches/Seed%d", seed), func(t *testing.T) {
			t.Parallel()

			st := newSamplesTest(t, newStore)
			ctx := t.Context()
			rng := rand.New(rand.NewPCG(seed, seed^0x5eed)) //nolint:gosec // G404: a reproducible test sequence
			rules := []store.ConflictRule{store.ConflictIgnore, store.ConflictUpdateIfChanged, store.ConflictReplace}

			for batch := range 12 {
				userID := st.user.ID
				if rng.IntN(3) == 0 {
					userID = st.other.ID
				}

				samples := make([]store.NewSample, rng.IntN(9))
				for i := range samples {
					sample := st.heartRate(fmt.Sprintf("key-%d", rng.IntN(15)),
						samplesTime.Add(time.Duration(rng.IntN(48*60)-24*60)*time.Minute), float64(rng.IntN(3)),
						rules[rng.IntN(len(rules))])
					if rng.IntN(2) == 0 {
						sample.TypeID = st.standing.ID
					}

					if userID == st.other.ID {
						sample.SourceID = st.foreign.ID
					}

					samples[i] = sample
				}

				st.mustUpsert(ctx, t, userID, samples...)

				for _, user := range []int64{st.user.ID, st.other.ID} {
					require.Equal(t, st.statsOfTable(ctx, t, user), st.mustStats(ctx, t, user), "batch %d", batch)
				}
			}
		})
	}
}

// statsOfTable computes the data availability of userID from the samples that List returns.
func (st samplesTest) statsOfTable(ctx context.Context, t *testing.T, userID int64) []store.SampleStats {
	t.Helper()

	stats := []store.SampleStats{}

	for _, typeID := range []int64{st.heart.ID, st.standing.ID} {
		samples := st.mustListAll(ctx, t, userID, typeID, 7)
		if len(samples) == 0 {
			continue
		}

		typeStats := store.SampleStats{TypeID: typeID, FirstAt: samples[0].Start, LastAt: samples[0].Start}
		for _, sample := range samples {
			if sample.Start.Before(typeStats.FirstAt) {
				typeStats.FirstAt = sample.Start
			}

			if sample.Start.After(typeStats.LastAt) {
				typeStats.LastAt = sample.Start
			}

			typeStats.Count++
		}

		stats = append(stats, typeStats)
	}

	slices.SortFunc(stats, func(a, b store.SampleStats) int { return cmp.Compare(a.TypeID, b.TypeID) })

	return stats
}

// runSamplesList runs the cases of List.
func runSamplesList(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given samples of a user inserted out of order, of two types, and samples of another user,
	// when List reads a type and a range in pages, then it returns the samples of that user, type
	// and range, ordered by start then id, each once.
	t.Run("ListReturnsTheSamplesOfTheUserTypeAndRangeByStartInPages", func(t *testing.T) {
		t.Parallel()

		st := newSamplesTest(t, newStore)
		ctx := t.Context()

		at := func(hours int) time.Time { return samplesTime.Add(time.Duration(hours) * time.Hour) }
		stood := st.heartRate("stood", at(2), 1, store.ConflictIgnore)
		stood.TypeID = st.standing.ID

		st.mustUpsert(ctx, t, st.user.ID,
			st.heartRate("before", at(-1), 1, store.ConflictIgnore),
			st.heartRate("third", at(3), 1, store.ConflictIgnore),
			st.heartRate("first-a", at(0), 1, store.ConflictIgnore),
			st.heartRate("second", at(1), 1, store.ConflictIgnore),
			st.heartRate("first-b", at(0), 1, store.ConflictIgnore),
			st.heartRate("to", at(4), 1, store.ConflictIgnore),
			stood,
		)
		st.mustUpsert(ctx, t, st.other.ID, st.foreignHeartRate("foreign", at(1), 1))

		query := store.SamplesQuery{TypeID: st.heart.ID, From: at(0), To: at(4), Limit: 2}

		var keys []string

		for range 3 {
			page, err := st.store.Samples().List(ctx, st.user.ID, query)
			require.NoError(t, err)
			require.LessOrEqual(t, len(page), 2)

			for _, sample := range page {
				require.Equal(t, st.user.ID, sample.UserID)
				keys = append(keys, string(sample.NaturalKey))
			}

			if len(page) > 0 {
				query.After = page[len(page)-1].Cursor()
			}
		}

		require.Equal(t, []string{"first-a", "first-b", "second", "third"}, keys)
	})

	// Given a limit below 1 or a range whose end is not after its start, then List answers
	// ErrInvalid.
	t.Run("ListRefusesAnInvalidQuery", func(t *testing.T) {
		t.Parallel()

		st := newSamplesTest(t, newStore)
		ctx := t.Context()

		for _, query := range []store.SamplesQuery{
			{TypeID: st.heart.ID, From: samplesTime, To: samplesTime.Add(time.Hour), Limit: 0},
			{TypeID: st.heart.ID, From: samplesTime, To: samplesTime, Limit: 10},
		} {
			_, err := st.store.Samples().List(ctx, st.user.ID, query)
			require.ErrorIs(t, err, store.ErrInvalid)
		}
	})
}

// runSamplesDelete runs the cases of DeleteByUser.
func runSamplesDelete(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given three samples of two types of a user, and a sample of another user, when DeleteByUser
	// runs with a limit of two, then each call removes at most two rows, the samples first and then
	// the data availability, and the other user keeps theirs.
	t.Run("DeleteByUserRemovesTheSamplesThenTheStatsInBatches", func(t *testing.T) {
		t.Parallel()

		st := newSamplesTest(t, newStore)
		ctx := t.Context()

		stood := st.heartRate("stood", samplesTime, 1, store.ConflictIgnore)
		stood.TypeID = st.standing.ID

		st.mustUpsert(ctx, t, st.user.ID,
			st.heartRate("a", samplesTime, 1, store.ConflictIgnore),
			st.heartRate("b", samplesTime, 1, store.ConflictIgnore),
			stood,
		)
		st.mustUpsert(ctx, t, st.other.ID, st.foreignHeartRate("a", samplesTime, 1))

		var removed []int64

		for {
			deleted, err := st.store.Samples().DeleteByUser(ctx, st.user.ID, 2)
			require.NoError(t, err)

			removed = append(removed, deleted)

			if deleted < 2 {
				break
			}

			if len(removed) == 1 {
				require.Len(t, st.mustStats(ctx, t, st.user.ID), 2, "the stats stay while samples are left")
			}
		}

		require.Equal(t, []int64{2, 2, 1}, removed, "three samples and two stats rows")
		require.Empty(t, st.mustListAll(ctx, t, st.user.ID, st.heart.ID, 10))
		require.Empty(t, st.mustListAll(ctx, t, st.user.ID, st.standing.ID, 10))
		require.Empty(t, st.mustStats(ctx, t, st.user.ID))

		require.Len(t, st.mustListAll(ctx, t, st.other.ID, st.heart.ID, 10), 1)
		require.Len(t, st.mustStats(ctx, t, st.other.ID), 1)

		_, err := st.store.Samples().DeleteByUser(ctx, st.user.ID, 0)
		require.ErrorIs(t, err, store.ErrInvalid)
	})
}
