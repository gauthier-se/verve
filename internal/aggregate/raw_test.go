package aggregate_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"runtime/metrics"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/aggregate"
	"github.com/gauthier-se/verve/internal/store"
)

// rawFrom is the start of the ranges of the raw read tests.
var rawFrom = time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)

// Given more samples than three batches of a downsampled read, some at the same start, when ReadRaw
// downsamples their range, then it returns the downsampling of every sample of the range, sorted by
// start then id, as Downsample keeps them.
func TestReadRaw_DownsampledReadReturnsTheDownsamplingOfEverySample(t *testing.T) {
	t.Parallel()

	e := newSeriesEnv(t)
	ctx := t.Context()
	heartRate := e.mustType(t, "heart_rate")

	const count = 12_345

	samples := make([]store.NewSample, 0, count)
	for i := range count {
		// Two samples at each start, and a value with peaks that the downsampling must keep.
		start := rawFrom.Add(time.Duration(i/2) * 30 * time.Second)
		samples = append(samples, store.NewSample{
			Sample: store.Sample{
				TypeID: heartRate.ID, SourceID: e.source.ID, Start: start, End: start,
				Value: 60 + 40*math.Abs(math.Sin(float64(i)/97)) + float64(i%13), Origin: store.OriginNative,
				NaturalKey: fmt.Appendf(nil, "hr-%d", i),
			},
			OnConflict: store.ConflictIgnore,
		})
	}

	_, err := e.store.Samples().Upsert(ctx, e.user.ID, samples)
	require.NoError(t, err)

	to := rawFrom.AddDate(0, 0, 7)

	all, err := e.store.Samples().List(ctx, e.user.ID, store.SamplesQuery{
		TypeID: heartRate.ID, From: rawFrom, To: to, Limit: count + 1,
	})
	require.NoError(t, err)
	require.Len(t, all, count)

	want := aggregate.Downsample(all, 300, func(s store.Sample) (float64, float64) {
		return float64(s.Start.UnixMilli()), s.Value
	})

	page, err := aggregate.ReadRaw(ctx, e.store, e.user.ID, heartRate, time.UTC, aggregate.RawQuery{
		From: rawFrom, To: to, MaxDays: 7, MaxPoints: 300,
	})
	require.NoError(t, err)
	require.False(t, page.More)

	got := make([]store.Sample, 0, len(page.Samples))
	for _, sample := range page.Samples {
		got = append(got, sample.Sample)
	}

	require.Len(t, got, 300)
	require.Equal(t, want, got)
}

// Given a range of half a million samples, when ReadRaw downsamples it, then the memory it holds
// stays below 16 MiB, a small part of the samples of the range, and the first and last samples are
// kept.
//
// It does not run in parallel, so that the live heap that it measures is its own.
func TestReadRaw_DownsampledReadHoldsBoundedMemory(t *testing.T) {
	e := newSeriesEnv(t)
	heartRate := e.mustType(t, "heart_rate")
	repos := &generatedStore{Store: e.store, samples: &generatedSamples{typeID: heartRate.ID, count: 500_000}}

	page, held := readGenerated(t, repos, heartRate, 1000)

	require.Len(t, page.Samples, 1000)
	require.Equal(t, rawFrom, page.Samples[0].Start)
	require.Equal(t, repos.samples.start(499_999), page.Samples[999].Start)
	require.Less(t, held, uint64(16<<20), "a downsampled read held %d MiB", held>>20)
}

// Given samples added at the end of the range or removed from it after ReadRaw counted them, when it
// downsamples the range, then it keeps the first and last samples it lists, sorted and at most
// maxPoints.
func TestReadRaw_DownsampledReadKeepsTheFirstAndLastSamplesOfARangeThatChanges(t *testing.T) {
	t.Parallel()

	e := newSeriesEnv(t)
	heartRate := e.mustType(t, "heart_rate")

	for _, tc := range []struct {
		count, drift, maxPoints int
	}{
		{count: 12_000, drift: 7, maxPoints: 300},
		{count: 12_000, drift: -7, maxPoints: 300},
		{count: 12_000, drift: -11_000, maxPoints: 300},
		{count: 12_000, drift: -11_999, maxPoints: 300},
		{count: 12_000, drift: 7, maxPoints: 3},
		{count: 12_000, drift: -7, maxPoints: 3},
		{count: 12_000, drift: 7, maxPoints: 2},
		{count: 12_000, drift: -7, maxPoints: 2},
		{count: 250, drift: 100, maxPoints: 300},
	} {
		t.Run(fmt.Sprintf("count=%d,drift=%d,maxPoints=%d", tc.count, tc.drift, tc.maxPoints), func(t *testing.T) {
			t.Parallel()

			samples := &generatedSamples{typeID: heartRate.ID, count: tc.count, drift: tc.drift}

			page, err := aggregate.ReadRaw(t.Context(), &generatedStore{Store: e.store, samples: samples}, 1, heartRate,
				time.UTC, aggregate.RawQuery{From: rawFrom, To: rawFrom.AddDate(0, 0, 366), MaxDays: 366, MaxPoints: tc.maxPoints})
			require.NoError(t, err)

			got := page.Samples
			require.LessOrEqual(t, len(got), tc.maxPoints)
			require.GreaterOrEqual(t, len(got), min(2, tc.count+tc.drift))
			require.Equal(t, rawFrom, got[0].Start)
			require.Equal(t, samples.start(tc.count+tc.drift-1), got[len(got)-1].Start)

			for i := 1; i < len(got); i++ {
				require.True(t, got[i].Start.After(got[i-1].Start), "sample %d is not after the one before", i)
			}
		})
	}
}

// BenchmarkReadRaw_Downsampled measures a downsampled read of ranges of millions of samples, and
// reports the most memory it held, which stays the same whatever the number of samples.
func BenchmarkReadRaw_Downsampled(b *testing.B) {
	e := newSeriesEnv(b)
	heartRate := e.mustType(b, "heart_rate")

	for _, count := range []int{1_000_000, 4_000_000} {
		b.Run(fmt.Sprintf("samples=%d", count), func(b *testing.B) {
			repos := &generatedStore{Store: e.store, samples: &generatedSamples{typeID: heartRate.ID, count: count}}

			var held uint64

			for b.Loop() {
				_, h := readGenerated(b, repos, heartRate, 1000)
				held = max(held, h)
			}

			b.ReportMetric(float64(held), "held-B")
		})
	}
}

// readGenerated downsamples to maxPoints the samples that repos generates, and returns them with the
// most live heap, above the one before the read, that the read held when it listed a batch.
func readGenerated(tb testing.TB, repos *generatedStore, metricType store.MetricType, maxPoints int) (aggregate.RawPage, uint64) {
	tb.Helper()

	runtime.GC()

	baseline := liveHeap()
	repos.samples.peak = baseline

	page, err := aggregate.ReadRaw(tb.Context(), repos, 1, metricType, time.UTC, aggregate.RawQuery{
		From: rawFrom, To: rawFrom.AddDate(0, 0, 366), MaxDays: 366, MaxPoints: maxPoints,
	})
	require.NoError(tb, err)

	return page, repos.samples.peak - baseline
}

// liveHeap returns the heap that the last garbage collection found live.
func liveHeap() uint64 {
	sample := []metrics.Sample{{Name: "/gc/heap/live:bytes"}}
	metrics.Read(sample)

	return sample[0].Value.Uint64()
}

// generatedStore is a store whose samples are generatedSamples, so that a read can range over
// millions of samples without storing them. The rest is a real store.
type generatedStore struct {
	store.Store

	samples *generatedSamples
}

func (s *generatedStore) Samples() store.SamplesRepository {
	return s.samples
}

// generatedSamples generates count samples of the type of typeID, one every 2 seconds from rawFrom,
// with a uuid, a natural key and metadata as a real sample has, and records the most live heap seen
// when a batch is listed. Its other methods are those of no repository and panic.
type generatedSamples struct {
	store.SamplesRepository

	typeID int64
	count  int
	// drift is the number of samples that List lists beyond those that Count counts, as samples added
	// at the end of the range after the count, or below 0 removed from it.
	drift int
	peak  uint64
}

// start returns the start of the sample i.
func (g *generatedSamples) start(i int) time.Time {
	return rawFrom.Add(time.Duration(i) * 2 * time.Second)
}

// index returns the index of the first sample that starts at or after at.
func (g *generatedSamples) index(at time.Time) int {
	if !at.After(rawFrom) {
		return 0
	}

	step := 2 * time.Second

	return int((at.Sub(rawFrom) + step - 1) / step)
}

func (g *generatedSamples) Count(_ context.Context, _, typeID int64, from, to time.Time) (int64, error) {
	if typeID != g.typeID {
		return 0, nil
	}

	return int64(min(g.index(to), g.count) - min(g.index(from), g.count)), nil
}

func (g *generatedSamples) List(_ context.Context, userID int64, query store.SamplesQuery) ([]store.Sample, error) {
	g.peak = max(g.peak, liveHeap())

	if query.TypeID != g.typeID {
		return nil, nil
	}

	first := g.index(query.From)
	if query.After != (store.Cursor{}) {
		first = max(first, int(query.After.ID))
	}

	last := min(g.index(query.To), g.count+g.drift, first+query.Limit)

	batch := make([]store.Sample, 0, max(last-first, 0))
	for i := first; i < last; i++ {
		start := g.start(i)
		batch = append(batch, store.Sample{
			// Ids from 1, so that the id of a sample is the index of the next one.
			ID: int64(i + 1), UserID: userID, TypeID: g.typeID, SourceID: 1, Start: start, End: start,
			Value: 60 + 40*math.Abs(math.Sin(float64(i)/97)), Origin: store.OriginXML, ImportID: 1,
			UUID:       fmt.Sprintf("00000000-0000-4000-8000-%012d", i),
			NaturalKey: fmt.Appendf(nil, "%032d", i),
			Metadata:   json.RawMessage(`{"HKMetadataKeyHeartRateMotionContext":1}`),
		})
	}

	return batch, nil
}
