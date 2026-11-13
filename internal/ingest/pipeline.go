package ingest

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/workouts/route"
)

// DefaultBatchSize is the most records that a batch writes in one transaction (FR-2 stage 7 of
// Ingestion pipeline), so that the single writer stays available for API requests.
const DefaultBatchSize = 5000

// Config configures a Pipeline.
type Config struct {
	Store store.Store
	// Catalog is the metric type catalog, synchronized into Store.
	Catalog *catalog.Catalog
	// Activities is the activity type catalog.
	Activities *catalog.Activities
	// Codec encodes the routes of workouts.
	Codec *route.Codec
	// BlockRules drops the records that a block rule matches, NoBlockRules when nil.
	BlockRules BlockRules
	// DirtyRanges records the ranges whose aggregates a batch changed, NoDirtyRanges when nil.
	DirtyRanges DirtyRanges
	// BatchSize is the most records of a transaction, DefaultBatchSize when 0.
	BatchSize int
}

// Pipeline runs the records of imports through the stages of FR-2 of Ingestion pipeline. It is
// safe for concurrent use.
type Pipeline struct {
	cfg Config
}

// NewPipeline returns a pipeline of cfg.
func NewPipeline(cfg Config) *Pipeline {
	if cfg.BatchSize == 0 {
		cfg.BatchSize = DefaultBatchSize
	}

	if cfg.BlockRules == nil {
		cfg.BlockRules = NoBlockRules{}
	}

	if cfg.DirtyRanges == nil {
		cfg.DirtyRanges = NoDirtyRanges{}
	}

	return &Pipeline{cfg: cfg}
}

// Run runs records through the pipeline, for the running import of importID of userID. It skips the
// records before the position stored on the import, and writes the others in batches of at most
// BatchSize records. The transaction of each batch stores its samples, workouts and routes with
// the position of the next record and the report, so that a run stopped at any point resumes from
// its last batch with the report of an uninterrupted run (FR-5).
//
// It returns the error of records, which stops the run, and the error of ctx once it is done, at
// the next batch boundary: the batches committed before stay.
func (p *Pipeline) Run(ctx context.Context, userID, importID int64, records iter.Seq2[Record, error]) error {
	imp, err := p.cfg.Store.Imports().Get(ctx, userID, importID)
	if err != nil {
		return fmt.Errorf("get import %d: %w", importID, err)
	}

	report := imp.Report
	batch := make([]Record, 0, p.cfg.BatchSize)

	for r, err := range records {
		if err != nil {
			return fmt.Errorf("read records of import %d: %w", importID, err)
		}

		if r.Position < imp.Position {
			continue
		}

		batch = append(batch, r)
		if len(batch) < p.cfg.BatchSize {
			continue
		}

		if report, err = p.write(ctx, imp, report, batch); err != nil {
			return err
		}

		batch = batch[:0]
	}

	if len(batch) > 0 {
		if _, err := p.write(ctx, imp, report, batch); err != nil {
			return err
		}
	}

	return nil
}

// write runs batch through the stages in one transaction, and returns the report of the import
// once the batch is committed.
func (p *Pipeline) write(ctx context.Context, imp store.Import, report store.ImportReport, batch []Record) (store.ImportReport, error) {
	if err := ctx.Err(); err != nil {
		return store.ImportReport{}, fmt.Errorf("stop import %d at position %d: %w", imp.ID, batch[0].Position, err)
	}

	var next store.ImportReport

	err := p.cfg.Store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		b := &batchRun{p: p, tx: tx, imp: imp, report: cloneReport(report)}
		if err := b.run(ctx, batch); err != nil {
			return err
		}

		progress := store.ImportProgress{Position: batch[len(batch)-1].Position + 1, Report: b.report}
		if err := tx.Imports().SaveProgress(ctx, imp.UserID, imp.ID, progress); err != nil {
			return fmt.Errorf("save progress of import %d: %w", imp.ID, err)
		}

		next = b.report

		return nil
	})
	if err != nil {
		return store.ImportReport{}, fmt.Errorf("write batch of import %d at position %d: %w", imp.ID, batch[0].Position, err)
	}

	return next, nil
}

// batchRun is the run of one batch, in its transaction.
type batchRun struct {
	p      *Pipeline
	tx     store.Repositories
	imp    store.Import
	report store.ImportReport
	// types are the types of the batch, by identifier.
	types map[string]mappedType
	// keys are the keys of the types of the samples that the batch removed, by type id.
	keys map[int64]string
}

// mappedType is what type mapping found for an identifier.
type mappedType struct {
	status catalog.Status
	// entry is the type of the catalog of a known identifier.
	entry catalog.Type
	// stored is the stored type, for an identifier that is not unsupported.
	stored store.MetricType
}

// item is a record of a batch once mapped and normalized.
type item struct {
	Record

	// counter is where the record counts in the report.
	counter counter
	// reason is the reason of an invalid record, empty for a valid one.
	reason string
	// unsupported is true for a record of an unsupported type.
	unsupported bool

	times Times
	// sample is the sample of a quantity or category record, without its source and natural key.
	sample store.Sample
	// workout is the workout of a workout record, without its source and natural key, and route
	// its encoded route, nil when it has none.
	workout store.Workout
	route   *store.WorkoutRoute
	// warnings name the details that a stored record lost.
	warnings []string
}

// suspiciousReason is the sample error message of a suspicious record.
const suspiciousReason = "start is before 2000 or more than one day after the import was created, stored as suspicious"

// run maps and normalizes the records of batch, resolves their sources, stores them, and then
// applies its deletions.
func (b *batchRun) run(ctx context.Context, batch []Record) error {
	b.types = make(map[string]mappedType)
	b.keys = make(map[int64]string)

	items := make([]item, len(batch))
	for i, r := range batch {
		it, err := b.prepare(ctx, r)
		if err != nil {
			return err
		}

		items[i] = it
	}

	sources, err := b.resolveSources(ctx, items)
	if err != nil {
		return err
	}

	var (
		samples   = newSampleGroups()
		workouts  []*item
		deletions []string
	)

	for i := range items {
		it := &items[i]

		if it.deletion() {
			if uuid, ok := b.filterDeletion(it); ok {
				deletions = append(deletions, uuid)
			}

			continue
		}

		keep, err := b.filter(ctx, it, sources)
		if err != nil {
			return err
		}

		switch {
		case !keep:
		case it.Workout != nil:
			workouts = append(workouts, it)
		default:
			samples.add(it)
		}
	}

	if err := b.storeSamples(ctx, samples); err != nil {
		return err
	}

	for _, it := range workouts {
		if err := b.storeWorkout(ctx, it); err != nil {
			return err
		}
	}

	return b.applyDeletions(ctx, deletions)
}

// deletion reports whether the record of it is a deletion, valid or not.
func (it *item) deletion() bool {
	return it.Deletion != nil || (it.Invalid != nil && it.Invalid.Of == DeletionRecord)
}

// filterDeletion returns the uuid of the deletion of it, or counts it as invalid in the total
// only: a deletion is not a sample or workout received.
func (b *batchRun) filterDeletion(it *item) (string, bool) {
	if it.reason != "" {
		b.report.Total.Invalid++
		b.addError(it.Record, it.reason)

		return "", false
	}

	return it.Deletion.UUID, true
}

// applyDeletions removes, for each uuid of uuids, the sample of that uuid whatever its origin and
// the native workout of that uuid with its route (FR-8 of Ingestion pipeline), after the samples
// and workouts of the batch, so that a record sent and deleted in one payload ends up deleted. Each
// record removed counts as deleted, and a uuid that removes nothing counts as not found in the
// total only. The ranges of the samples removed are recorded as dirty.
func (b *batchRun) applyDeletions(ctx context.Context, uuids []string) error {
	var dirty []DirtyRange

	for _, uuid := range uuids {
		found := false

		sample, err := b.tx.Samples().DeleteByKey(ctx, b.imp.UserID, sampleKey(store.Sample{UUID: uuid}, false))

		switch {
		case err == nil:
			key, err := b.typeKey(ctx, sample.TypeID)
			if err != nil {
				return err
			}

			found = true
			counter{key: key}.add(&b.report, func(c *store.ImportCounters) { c.Deleted++ })
			dirty = append(dirty, DirtyRange{TypeID: sample.TypeID, From: sample.Start, To: sample.End})
		case !errors.Is(err, store.ErrNotFound):
			return fmt.Errorf("delete sample: %w", err)
		}

		_, err = b.tx.Workouts().DeleteByKey(ctx, b.imp.UserID, workoutKey(store.Workout{Origin: store.OriginNative, ExternalID: uuid}))

		switch {
		case err == nil:
			found = true
			counter{workout: true}.add(&b.report, func(c *store.ImportCounters) { c.Deleted++ })
		case !errors.Is(err, store.ErrNotFound):
			return fmt.Errorf("delete workout: %w", err)
		}

		if !found {
			b.report.Total.NotFound++
		}
	}

	if len(dirty) == 0 {
		return nil
	}

	if err := b.p.cfg.DirtyRanges.Record(ctx, b.tx, b.imp.UserID, dirty); err != nil {
		return fmt.Errorf("record dirty ranges: %w", err)
	}

	return nil
}

// typeKey returns the key of the stored type of typeID, read once per batch.
func (b *batchRun) typeKey(ctx context.Context, typeID int64) (string, error) {
	if key, ok := b.keys[typeID]; ok {
		return key, nil
	}

	mt, err := b.tx.MetricTypes().Get(ctx, typeID)
	if err != nil {
		return "", fmt.Errorf("get type %d: %w", typeID, err)
	}

	b.keys[typeID] = mt.Key

	return mt.Key, nil
}

// filter counts it as received, then drops it when it is unsupported, invalid or blocked, counting
// why. A record that it keeps gets its source and natural key, and is counted suspicious or under
// warnings when it is.
func (b *batchRun) filter(ctx context.Context, it *item, sources map[store.SourceInfo]store.Source) (bool, error) {
	it.counter.add(&b.report, func(c *store.ImportCounters) { c.Received++ })

	switch {
	case it.unsupported:
		it.counter.add(&b.report, func(c *store.ImportCounters) { c.Unsupported++ })

		return false, nil
	case it.reason != "":
		it.counter.add(&b.report, func(c *store.ImportCounters) { c.Invalid++ })
		b.addError(it.Record, it.reason)

		return false, nil
	}

	candidate := it.identify(sources[it.source()].ID)

	blocked, err := b.p.cfg.BlockRules.Blocks(ctx, b.tx, b.imp.UserID, candidate)
	if err != nil {
		return false, fmt.Errorf("apply block rules: %w", err)
	}

	if blocked {
		it.counter.add(&b.report, func(c *store.ImportCounters) { c.Blocked++ })

		return false, nil
	}

	if it.times.Suspicious {
		it.counter.add(&b.report, func(c *store.ImportCounters) { c.Suspicious++ })
		b.addError(it.Record, suspiciousReason)
	}

	if len(it.warnings) > 0 {
		it.counter.add(&b.report, func(c *store.ImportCounters) { c.Warnings++ })
		b.addError(it.Record, strings.Join(it.warnings, "; "))
	}

	return true, nil
}

// identify sets the source of the record of it and its natural key (FR-2 stage 6), and returns it
// as a candidate of the block rules.
func (it *item) identify(sourceID int64) BlockCandidate {
	if it.Workout != nil {
		it.workout.SourceID = sourceID
		it.workout.NaturalKey = workoutKey(it.workout)

		return BlockCandidate{SourceID: sourceID, Start: it.workout.Start, UUID: it.workout.ExternalID, NaturalKey: it.workout.NaturalKey}
	}

	it.sample.SourceID = sourceID
	it.sample.NaturalKey = sampleKey(it.sample, it.preAggregated())

	return BlockCandidate{
		TypeID:     it.sample.TypeID,
		SourceID:   sourceID,
		Start:      it.sample.Start,
		UUID:       it.sample.UUID,
		NaturalKey: it.sample.NaturalKey,
	}
}

// preAggregated reports whether the record of it is a pre-aggregated point.
func (it *item) preAggregated() bool {
	return it.Quantity != nil && it.Quantity.PreAggregated
}

// sampleGroups are the samples of a batch to store, by type in the order of their first sample.
type sampleGroups struct {
	order    []int64
	samples  map[int64][]store.NewSample
	counters map[int64]counter
}

// newSampleGroups returns empty groups.
func newSampleGroups() *sampleGroups {
	return &sampleGroups{samples: make(map[int64][]store.NewSample), counters: make(map[int64]counter)}
}

// add adds the sample of it to the group of its type.
func (g *sampleGroups) add(it *item) {
	typeID := it.sample.TypeID

	if _, ok := g.samples[typeID]; !ok {
		g.order = append(g.order, typeID)
		g.counters[typeID] = it.counter
	}

	g.samples[typeID] = append(g.samples[typeID], store.NewSample{
		Sample:     it.sample,
		OnConflict: sampleRule(it.sample.UUID, it.preAggregated()),
	})
}

// storeSamples upserts the samples of groups, one upsert per type so that each type gets its
// counters, and records the dirty ranges of the types whose samples were inserted or updated.
func (b *batchRun) storeSamples(ctx context.Context, groups *sampleGroups) error {
	var dirty []DirtyRange

	for _, typeID := range groups.order {
		samples := groups.samples[typeID]

		result, err := b.tx.Samples().Upsert(ctx, b.imp.UserID, samples)
		if err != nil {
			return fmt.Errorf("upsert samples: %w", err)
		}

		groups.counters[typeID].add(&b.report, func(c *store.ImportCounters) {
			c.Inserted += int64(result.Inserted)
			c.Updated += int64(result.Updated)
			c.Duplicates += int64(result.Duplicates)
		})

		if result.Inserted+result.Updated > 0 {
			dirty = append(dirty, dirtyRanges(samples)...)
		}
	}

	if len(dirty) == 0 {
		return nil
	}

	if err := b.p.cfg.DirtyRanges.Record(ctx, b.tx, b.imp.UserID, dirty); err != nil {
		return fmt.Errorf("record dirty ranges: %w", err)
	}

	return nil
}

// dirtyRanges returns the time ranges of samples, all of one type, sorted by start, with the
// overlapping ones merged.
func dirtyRanges(samples []store.NewSample) []DirtyRange {
	if len(samples) == 0 {
		return nil
	}

	ranges := make([]DirtyRange, 0, len(samples))
	for _, s := range samples {
		ranges = append(ranges, DirtyRange{TypeID: s.TypeID, From: s.Start, To: s.End})
	}

	slices.SortFunc(ranges, func(a, b DirtyRange) int { return a.From.Compare(b.From) })

	merged := ranges[:1]

	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]

		switch {
		case r.From.After(last.To):
			merged = append(merged, r)
		case r.To.After(last.To):
			last.To = r.To
		}
	}

	return merged
}

// storeWorkout upserts the workout of it, counts it, and stores its route: always for a workout
// inserted or updated, and for a duplicate only when the stored workout has no route yet.
func (b *batchRun) storeWorkout(ctx context.Context, it *item) error {
	upsert, err := b.tx.Workouts().Upsert(ctx, b.imp.UserID, store.NewWorkout{Workout: it.workout, OnConflict: workoutRule(it.workout)})
	if err != nil {
		return fmt.Errorf("upsert workout: %w", err)
	}

	it.counter.add(&b.report, func(c *store.ImportCounters) {
		switch upsert.Outcome {
		case store.OutcomeInserted:
			c.Inserted++
		case store.OutcomeUpdated:
			c.Updated++
		case store.OutcomeDuplicate:
			c.Duplicates++
		}
	})

	if it.route == nil {
		return nil
	}

	if upsert.Outcome == store.OutcomeDuplicate {
		_, err := b.tx.Workouts().GetRoute(ctx, b.imp.UserID, upsert.ID)

		switch {
		case err == nil:
			return nil
		case !errors.Is(err, store.ErrNotFound):
			return fmt.Errorf("get route of workout %d: %w", upsert.ID, err)
		}
	}

	if err := b.tx.Workouts().SetRoute(ctx, b.imp.UserID, upsert.ID, *it.route); err != nil {
		return fmt.Errorf("set route of workout %d: %w", upsert.ID, err)
	}

	return nil
}

// prepare maps the type of r and normalizes it. It returns an error only when the store fails: an
// invalid record is an item with a reason.
func (b *batchRun) prepare(ctx context.Context, r Record) (item, error) {
	it, err := b.prepareRecord(ctx, r)
	if err != nil {
		return item{}, err
	}

	if it.reason != "" || it.unsupported || it.deletion() {
		return it, nil
	}

	switch m := r.metadata(); {
	case r.source().Name == "":
		it.reason = "source name is missing"
	case m != nil && !isJSONObject(m):
		it.reason = "metadata is not a JSON object"
	}

	return it, nil
}

// prepareRecord runs the stages of prepare that depend on the kind of r.
func (b *batchRun) prepareRecord(ctx context.Context, r Record) (item, error) {
	it := item{Record: r}

	switch {
	case r.Quantity != nil:
		s := r.Quantity

		mt, ok, err := b.sampleType(ctx, &it, s.Type, catalog.KindQuantity, s.Unit)
		if err != nil || !ok {
			return it, err
		}

		n, err := NormalizeQuantity(*s, mt.stored, b.imp.CreatedAt)
		if err != nil {
			it.reason = err.Error()

			return it, nil
		}

		it.times = n.Times
		it.sample = b.newSample(mt.stored, n.Times, s.UUID, s.Metadata)
		it.sample.Value, it.sample.Min, it.sample.Max = n.Value, n.Min, n.Max
	case r.Category != nil:
		s := r.Category

		mt, ok, err := b.sampleType(ctx, &it, s.Type, catalog.KindCategory, categoryUnit)
		if err != nil || !ok {
			return it, err
		}

		times, err := NormalizeCategory(*s, b.imp.CreatedAt)
		if err != nil {
			it.reason = err.Error()

			return it, nil
		}

		if s.Value == "" {
			it.reason = "category value is missing"

			return it, nil
		}

		it.times = times
		it.sample = b.newSample(mt.stored, times, s.UUID, s.Metadata)
		it.sample.Value, it.sample.Category = 1, categoryKey(mt.entry, s.Value)
	case r.Workout != nil:
		it.counter = counter{workout: true}

		if err := b.prepareWorkout(&it); err != nil {
			it.reason = err.Error()
		}
	case r.Deletion != nil:
		if r.Deletion.UUID == "" {
			it.reason = "uuid is missing"
		}
	case r.Invalid != nil:
		if err := b.prepareInvalid(ctx, &it); err != nil {
			return item{}, err
		}
	default:
		return item{}, fmt.Errorf("record at position %d holds no sample or workout", r.Position)
	}

	return it, nil
}

// prepareInvalid sets the reason of the record of it, which its adapter found invalid, and where it
// counts: a sample under the key of its type when that type is stored, without creating it, and
// as unsupported under its identifier for an unsupported type, since type mapping comes first.
func (b *batchRun) prepareInvalid(ctx context.Context, it *item) error {
	invalid := it.Invalid
	it.reason = invalid.Reason

	switch invalid.Of {
	case WorkoutRecord:
		it.counter = counter{workout: true}

		return nil
	case SampleRecord:
		if invalid.Type == "" {
			return nil
		}
	default:
		return nil
	}

	if _, status := b.p.cfg.Catalog.Lookup(invalid.Type); status == catalog.Unsupported {
		it.counter, it.unsupported = counter{key: invalid.Type}, true

		return nil
	}

	mt, err := b.tx.MetricTypes().GetByIdentifier(ctx, invalid.Type)

	switch {
	case err == nil:
		it.counter = counter{key: mt.Key}
	case !errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("get type %q: %w", invalid.Type, err)
	}

	return nil
}

// prepareWorkout normalizes the workout of it, maps its activity type, and encodes its route. It
// returns the reason of an invalid workout.
func (b *batchRun) prepareWorkout(it *item) error {
	w := it.Workout

	n, err := NormalizeWorkout(*w, b.imp.CreatedAt)
	if err != nil {
		return err
	}

	it.times, it.warnings = n.Times, n.Warnings

	activity, ok := b.p.cfg.Activities.ByIdentifier(w.ActivityType)
	if !ok {
		activity = catalog.Activity{Key: otherActivity}
		it.warnings = append(it.warnings, fmt.Sprintf("activity type %q is not a known activity type, stored as %s", w.ActivityType, otherActivity))
	}

	it.workout = store.Workout{
		Origin:        b.imp.Origin,
		ExternalID:    w.UUID,
		Activity:      activity.Key,
		Start:         n.Start,
		End:           n.End,
		UTCOffset:     n.UTCOffset,
		Duration:      n.Duration,
		Distance:      n.Distance,
		ActiveEnergy:  n.ActiveEnergy,
		TotalEnergy:   n.TotalEnergy,
		ElevationGain: n.ElevationGain,
		ElevationLoss: n.ElevationLoss,
		AvgHeartRate:  n.AvgHeartRate,
		MaxHeartRate:  n.MaxHeartRate,
		Indoor:        w.Indoor,
		LocationType:  locationType(w.Indoor),
		Events:        n.Events,
		Metadata:      w.Metadata,
		ImportID:      b.imp.ID,
	}

	if n.Route != nil {
		if it.route, err = b.encodeRoute(n.Route); err != nil {
			return err
		}
	}

	return nil
}

// otherActivity is the key of the activity type of the workouts whose activity type the catalog
// does not list (FR-4 of Storage and aggregates).
const otherActivity = "other"

// locationType returns the location type of a workout of the indoor flag indoor, unknown when the
// flag is nil.
func locationType(indoor *bool) store.LocationType {
	switch {
	case indoor == nil:
		return store.LocationUnknown
	case *indoor:
		return store.LocationIndoor
	default:
		return store.LocationOutdoor
	}
}

// encodeRoute encodes points with what is stored with them: bounding box, total distance and
// simplified polyline. Its error is the reason of an invalid workout, which never quotes a value,
// unlike those of the codec.
func (b *batchRun) encodeRoute(points []route.Point) (*store.WorkoutRoute, error) {
	if len(points) > route.MaxPoints {
		return nil, fmt.Errorf("route has more than %d points", route.MaxPoints)
	}

	encoded, err := b.p.cfg.Codec.Encode(points)
	if err != nil {
		return nil, errors.New("route has a value that is not finite")
	}

	box, _ := route.Bounds(points)
	simplified, _ := route.Simplify(points)

	return &store.WorkoutRoute{
		Encoded:    encoded,
		Box:        store.RouteBox{MinLat: box.MinLat, MinLon: box.MinLon, MaxLat: box.MaxLat, MaxLon: box.MaxLon},
		Distance:   route.Distance(points),
		PointCount: len(points),
		Polyline:   route.Polyline(simplified),
	}, nil
}

// sampleType maps identifier, the type of the sample of it, and sets where it counts. It returns
// false, with the reason of an invalid sample when there is one, for a sample that it cannot
// store: without type, of an unsupported type, or of a type of another kind.
func (b *batchRun) sampleType(ctx context.Context, it *item, identifier string, kind catalog.Kind, unit string) (mappedType, bool, error) {
	if identifier == "" {
		it.reason = "type is missing"

		return mappedType{}, false, nil
	}

	mt, err := b.mapType(ctx, identifier, kind, unit)
	if err != nil {
		return mappedType{}, false, err
	}

	if mt.status == catalog.Unsupported {
		it.counter, it.unsupported = counter{key: identifier}, true

		return mappedType{}, false, nil
	}

	it.counter = counter{key: mt.stored.Key}

	if mt.stored.Kind != kind {
		it.reason = fmt.Sprintf("type is a %s type", mt.stored.Kind)

		return mappedType{}, false, nil
	}

	return mt, true, nil
}

// categoryUnit is the unit of the uncurated category types, as of the category types of the
// catalog.
const categoryUnit = "count"

// categoryKey returns the key of the category value of identifier listed by entry, or identifier
// when entry does not list it, as for a value of an uncurated type or one that a new iOS release
// adds (FR-3 of Storage and aggregates).
func categoryKey(entry catalog.Type, identifier string) string {
	for _, v := range entry.Values {
		if v.Identifier == identifier {
			return v.Key
		}
	}

	return identifier
}

// newSample returns a sample of the import, of type mt, without its source, value and natural key.
func (b *batchRun) newSample(mt store.MetricType, times Times, uuid string, metadata []byte) store.Sample {
	return store.Sample{
		TypeID:    mt.ID,
		Start:     times.Start,
		End:       times.End,
		UTCOffset: times.UTCOffset,
		Origin:    b.imp.Origin,
		ImportID:  b.imp.ID,
		UUID:      uuid,
		Metadata:  metadata,
	}
}

// mapType maps identifier to its type (FR-2 stage 1), once per batch: the stored type of an
// identifier of the catalog, nothing for an unsupported one, and for another one its uncurated
// type, created of kind and unit when it does not exist.
func (b *batchRun) mapType(ctx context.Context, identifier string, kind catalog.Kind, unit string) (mappedType, error) {
	if mt, ok := b.types[identifier]; ok {
		return mt, nil
	}

	entry, status := b.p.cfg.Catalog.Lookup(identifier)
	mt := mappedType{status: status, entry: entry}

	var err error

	switch status {
	case catalog.Known:
		mt.stored, err = b.tx.MetricTypes().GetByIdentifier(ctx, identifier)
	case catalog.Unknown:
		mt.stored, err = b.tx.MetricTypes().EnsureUncurated(ctx, identifier, kind, unit)
	}

	if err != nil {
		return mappedType{}, fmt.Errorf("map type %q: %w", identifier, err)
	}

	b.types[identifier] = mt

	return mt, nil
}

// resolveSources resolves the source of the valid records of items (FR-2 stage 4), once per
// distinct source, seen at the latest start of its records.
func (b *batchRun) resolveSources(ctx context.Context, items []item) (map[store.SourceInfo]store.Source, error) {
	seen := make(map[store.SourceInfo]time.Time)

	for _, it := range items {
		if it.reason != "" || it.unsupported || it.deletion() {
			continue
		}

		info := it.source()
		if at, ok := seen[info]; !ok || it.times.Start.After(at) {
			seen[info] = it.times.Start
		}
	}

	sources := make(map[store.SourceInfo]store.Source, len(seen))

	for _, info := range slices.SortedFunc(maps.Keys(seen), compareSourceInfo) {
		source, err := b.tx.Sources().Resolve(ctx, b.imp.UserID, info, DetectKind(info), seen[info])
		if err != nil {
			return nil, fmt.Errorf("resolve source: %w", err)
		}

		sources[info] = source
	}

	return sources, nil
}

// source returns the source of r.
func (r Record) source() store.SourceInfo {
	switch {
	case r.Quantity != nil:
		return r.Quantity.Source
	case r.Category != nil:
		return r.Category.Source
	default:
		return r.Workout.Source
	}
}

// compareSourceInfo orders sources by their fields, so that a batch resolves them in the same order
// on every run.
func compareSourceInfo(a, b store.SourceInfo) int {
	return cmp.Or(
		cmp.Compare(a.Name, b.Name),
		cmp.Compare(a.DeviceModel, b.DeviceModel),
		cmp.Compare(a.BundleID, b.BundleID),
		cmp.Compare(a.DeviceName, b.DeviceName),
		cmp.Compare(a.Manufacturer, b.Manufacturer),
		cmp.Compare(a.HardwareVersion, b.HardwareVersion),
		cmp.Compare(a.SoftwareVersion, b.SoftwareVersion),
	)
}

// addError adds a sample error message for r while the report keeps fewer than the most it can.
func (b *batchRun) addError(r Record, reason string) {
	if len(b.report.Errors) >= store.ImportErrorsLimit {
		return
	}

	message := fmt.Sprintf("record %d: %s", r.Position, reason)
	if name := r.typeName(); name != "" {
		message = fmt.Sprintf("record %d (%s): %s", r.Position, name, reason)
	}

	b.report.Errors = append(b.report.Errors, message)
}

// typeName returns what a sample error message names as the type of r, empty for a sample without
// type.
func (r Record) typeName() string {
	switch {
	case r.Quantity != nil:
		return r.Quantity.Type
	case r.Category != nil:
		return r.Category.Type
	case r.Deletion != nil:
		return string(DeletionRecord)
	case r.Invalid != nil && r.Invalid.Of == SampleRecord:
		return r.Invalid.Type
	case r.Invalid != nil:
		return string(r.Invalid.Of)
	default:
		return string(WorkoutRecord)
	}
}

// counter is where a record counts in a report: in the total, and under its type key or the
// workouts. A record without a usable type counts in the total only.
type counter struct {
	key     string
	workout bool
}

// add applies inc to the counters of c in report.
func (c counter) add(report *store.ImportReport, inc func(*store.ImportCounters)) {
	inc(&report.Total)

	switch {
	case c.workout:
		inc(&report.Workouts)
	case c.key != "":
		if report.Types == nil {
			report.Types = make(map[string]store.ImportCounters)
		}

		counters := report.Types[c.key]
		inc(&counters)
		report.Types[c.key] = counters
	}
}

// cloneReport copies report, so that a batch rolled back leaves it as it was.
func cloneReport(report store.ImportReport) store.ImportReport {
	report.Types = maps.Clone(report.Types)
	report.Errors = slices.Clone(report.Errors)

	return report
}

// metadata returns the metadata of r, nil when it has none.
func (r Record) metadata() json.RawMessage {
	switch {
	case r.Quantity != nil:
		return r.Quantity.Metadata
	case r.Category != nil:
		return r.Category.Metadata
	case r.Workout != nil:
		return r.Workout.Metadata
	default:
		return nil
	}
}

// isJSONObject reports whether data is a valid JSON object, as the store requires of metadata.
func isJSONObject(data []byte) bool {
	return json.Valid(data) && bytes.HasPrefix(bytes.TrimSpace(data), []byte("{"))
}
