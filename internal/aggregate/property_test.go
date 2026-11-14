package aggregate_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/gauthier-se/verve/internal/aggregate"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/preferences"
	"github.com/gauthier-se/verve/internal/store"
)

// propertyStart is the start of the samples of the property test: their ten days hold the end of
// daylight saving time in Europe/Paris and the end of a month.
var propertyStart = time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)

// propertyZones are the timezones of the property test: whole hour offsets with and without
// daylight saving time, and offsets of 30 and 45 minutes.
var propertyZones = []string{"", "UTC", "Europe/Paris", "Asia/Kolkata", "Asia/Kathmandu"}

// sleepAnalysis is the category type of the property test, and its values.
const sleepAnalysis = "HKCategoryTypeIdentifierSleepAnalysis"

var sleepValues = []string{"HKCategoryValueSleepAnalysisAsleepCore", "HKCategoryValueSleepAnalysisAwake"}

// propertySources are the sources of the samples of the property test, with the kinds they are
// created with.
var propertySources = []struct {
	info store.SourceInfo
	kind store.SourceKind
}{
	{info: watch, kind: store.SourceWatch},
	{info: store.SourceInfo{Name: "iPhone", DeviceModel: "iPhone"}, kind: store.SourcePhone},
	{info: store.SourceInfo{Name: "Pedometer"}, kind: store.SourceApp},
}

// sent is a sample with a uuid that the property test imported and has not deleted.
type sent struct {
	record ingest.Record
}

// propertyRun is one sequence of the property test: a user of its own on the store of the job
// tests, and the samples it holds.
type propertyRun struct {
	e     *jobEnv
	ctx   context.Context
	user  store.User
	sent  []sent
	count int
}

// newPropertyRun creates the user of run n, with a drawn timezone and drawn priorities: a source
// ignored or not for the steps, and combine mode or not for heart rate.
func newPropertyRun(ctx context.Context, rt *rapid.T, e *jobEnv, n int) *propertyRun {
	user, err := e.store.Users().Create(ctx, store.NewUser{Username: fmt.Sprintf("user%d", n), Role: store.RoleMember})
	require.NoError(rt, err)

	if zone := rapid.SampledFrom(propertyZones).Draw(rt, "zone"); zone != "" {
		_, err := e.store.Preferences().SetTimezone(ctx, user.ID, zone)
		require.NoError(rt, err)
	}

	sources := make([]int64, 0, len(propertySources))

	for _, s := range propertySources {
		source, err := e.store.Sources().Resolve(ctx, user.ID, s.info, s.kind, propertyStart)
		require.NoError(rt, err)

		sources = append(sources, source.ID)
	}

	stepsPriorities := store.SourcePriorities{Order: rapid.Permutation(sources).Draw(rt, "steps order")}
	if rapid.Bool().Draw(rt, "ignore a steps source") {
		stepsPriorities.Ignored, stepsPriorities.Order = stepsPriorities.Order[:1], stepsPriorities.Order[1:]
	}

	require.NoError(rt, e.store.SourcePriorities().Set(ctx, user.ID, e.typeOfRT(rt, stepCount).ID, stepsPriorities))
	require.NoError(rt, e.store.SourcePriorities().Set(ctx, user.ID, e.typeOfRT(rt, heartRate).ID,
		store.SourcePriorities{Combine: rapid.Bool().Draw(rt, "combine heart rate")}))

	return &propertyRun{e: e, ctx: ctx, user: user}
}

// importSamples imports a batch of drawn samples, each with a new uuid.
func (p *propertyRun) importSamples(rt *rapid.T) {
	records := make([]ingest.Record, rapid.IntRange(1, 6).Draw(rt, "samples"))

	for i := range records {
		p.count++

		uuid := fmt.Sprintf("%08X-0000-4000-8000-%012X", p.user.ID, p.count)
		source := rapid.SampledFrom(propertySources).Draw(rt, "source").info
		start := propertyStart.Add(time.Duration(rapid.IntRange(0, 10*24*60).Draw(rt, "start")) * time.Minute)

		switch rapid.IntRange(0, 2).Draw(rt, "type") {
		case 0:
			end := start.Add(time.Duration(rapid.IntRange(0, 6*60).Draw(rt, "steps minutes")) * time.Minute)
			records[i] = ingest.Record{Quantity: &ingest.QuantitySample{
				UUID: uuid, Type: stepCount, Start: start.Format(time.RFC3339), End: end.Format(time.RFC3339),
				Value: float64(rapid.IntRange(0, 2000).Draw(rt, "steps")), Unit: "count", Source: source,
			}}
		case 1:
			end := start.Add(time.Duration(rapid.IntRange(0, 2).Draw(rt, "heart rate minutes")) * time.Minute)
			records[i] = ingest.Record{Quantity: &ingest.QuantitySample{
				UUID: uuid, Type: heartRate, Start: start.Format(time.RFC3339), End: end.Format(time.RFC3339),
				Value: float64(rapid.IntRange(40, 180).Draw(rt, "heart rate")), Unit: "count/min", Source: source,
			}}
		default:
			end := start.Add(time.Duration(rapid.IntRange(0, 5*60).Draw(rt, "sleep minutes")) * time.Minute)
			records[i] = ingest.Record{Category: &ingest.CategorySample{
				UUID: uuid, Type: sleepAnalysis, Start: start.Format(time.RFC3339), End: end.Format(time.RFC3339),
				Value: rapid.SampledFrom(sleepValues).Draw(rt, "sleep value"), Source: source,
			}}
		}

		p.sent = append(p.sent, sent{record: records[i]})
	}

	p.e.importRecordsOf(p.ctx, rt, p.user.ID, records...)
}

// reimport imports a sample sent before again, with another value, which updates it.
func (p *propertyRun) reimport(rt *rapid.T) {
	if len(p.sent) == 0 {
		rt.Skip("no sample to import again")
	}

	i := rapid.IntRange(0, len(p.sent)-1).Draw(rt, "reimported")
	record := p.sent[i].record

	if record.Quantity != nil {
		quantity := *record.Quantity
		quantity.Value += float64(rapid.IntRange(1, 50).Draw(rt, "increase"))
		record.Quantity = &quantity
	} else {
		category := *record.Category
		category.Value = sleepValues[(len(category.Value)+1)%len(sleepValues)]
		record.Category = &category
	}

	p.sent[i].record = record
	p.e.importRecordsOf(p.ctx, rt, p.user.ID, record)
}

// deleteSample deletes a sample sent before by its uuid.
func (p *propertyRun) deleteSample(rt *rapid.T) {
	if len(p.sent) == 0 {
		rt.Skip("no sample to delete")
	}

	i := rapid.IntRange(0, len(p.sent)-1).Draw(rt, "deleted")

	uuid := ""
	if q := p.sent[i].record.Quantity; q != nil {
		uuid = q.UUID
	} else {
		uuid = p.sent[i].record.Category.UUID
	}

	p.sent = append(p.sent[:i], p.sent[i+1:]...)
	p.e.importRecordsOf(p.ctx, rt, p.user.ID, ingest.Record{Deletion: &ingest.Deletion{UUID: uuid}})
}

// changeTimezone sets a drawn timezone, which recomputes the aggregates when it changes.
func (p *propertyRun) changeTimezone(rt *rapid.T) {
	zone := rapid.SampledFrom(propertyZones[1:]).Draw(rt, "new zone")

	_, err := preferences.SetTimezone(p.ctx, p.e.store, p.e.recorder, p.user.ID, zone)
	require.NoError(rt, err)
}

// runJob runs the oldest due job, if any.
func (p *propertyRun) runJob(rt *rapid.T) {
	_, err := p.e.worker.Tick(p.ctx)
	require.NoError(rt, err)
}

// aggregates are the hourly and daily rows of a user, by type identifier.
type aggregates map[string][2][]store.AggregateRow

// rows returns the rows of the user of p.
func (p *propertyRun) rows(rt *rapid.T) aggregates {
	got := make(aggregates)

	for _, identifier := range []string{stepCount, heartRate, sleepAnalysis} {
		typeID := p.e.typeOfRT(rt, identifier).ID

		hourly, err := p.e.store.Aggregates().ListHourly(p.ctx, p.user.ID, typeID, time.Unix(0, 0), jobTime.AddDate(10, 0, 0))
		require.NoError(rt, err)

		daily, err := p.e.store.Aggregates().ListDaily(p.ctx, p.user.ID, typeID, time.Unix(0, 0), jobTime.AddDate(10, 0, 0))
		require.NoError(rt, err)

		got[identifier] = [2][]store.AggregateRow{hourly, daily}
	}

	return got
}

// Given any sequence of imports, updates, deletions, timezone changes and job runs, when the jobs
// that are left run, then the aggregates equal those of a full recomputation of every sample of the
// user, and no dirty range or job is left.
func TestRecompute_AggregatesAfterAnySequenceEqualAFullRecomputation(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t)
	ctx := t.Context()
	runs := 0

	rapid.Check(t, func(rt *rapid.T) {
		runs++
		p := newPropertyRun(ctx, rt, e, runs)

		rt.Repeat(map[string]func(*rapid.T){
			"import":   p.importSamples,
			"reimport": p.reimport,
			"delete":   p.deleteSample,
			"timezone": p.changeTimezone,
			"job":      p.runJob,
		})

		e.runJobsOn(ctx, rt)

		ranges, err := e.store.Aggregates().ListDirty(ctx, p.user.ID)
		require.NoError(rt, err)
		require.Empty(rt, ranges)

		active, err := e.store.Jobs().ActiveJobs(ctx, aggregate.SerialKey(p.user.ID))
		require.NoError(rt, err)
		require.Empty(rt, active)

		incremental := p.rows(rt)

		require.NoError(rt, e.store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			if err := tx.Aggregates().DeleteAll(ctx, p.user.ID); err != nil {
				return err
			}

			return tx.Aggregates().RecordAllDirty(ctx, p.user.ID)
		}))
		require.NoError(rt, aggregate.Recompute(ctx, e.store, p.user.ID))

		require.Equal(rt, p.rows(rt), incremental)
	})
}
