package applexml_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/ingest/applexml"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
	"github.com/gauthier-se/verve/internal/synthexport"
	"github.com/gauthier-se/verve/internal/workouts"
	"github.com/gauthier-se/verve/internal/workouts/route"
)

// importTime is the current time of the store of the pipeline tests, when their imports are
// created.
var importTime = time.Date(2026, 11, 16, 20, 0, 0, 0, time.UTC)

// importer runs the fixture, written as a zip, through a pipeline with the duplicate detector, on a
// migrated store in a fresh temporary directory, closed at the end of the test.
type importer struct {
	store    store.Store
	user     store.User
	pipeline *ingest.Pipeline
	// export is the path of the zip of the fixture.
	export string
}

// newImporter returns an importer of a new user.
func newImporter(t *testing.T) *importer {
	t.Helper()

	ctx := t.Context()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "verve.db"))
	require.NoError(t, err)

	_, err = sqlite.Migrate(ctx, db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	s := sqlite.New(db, clock.NewFake(importTime))
	t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

	cat, err := catalog.Shipped()
	require.NoError(t, err)
	require.NoError(t, s.MetricTypes().Sync(ctx, cat))

	activities, err := catalog.ShippedActivities()
	require.NoError(t, err)

	user, err := s.Users().Create(ctx, store.NewUser{Username: "alex", Role: store.RoleMember})
	require.NoError(t, err)

	enc, err := zstd.NewWriter(nil)
	require.NoError(t, err)

	dec, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(64<<20))
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, enc.Close())
		dec.Close()
	})

	return &importer{store: s, user: user, export: writeExport(t, t.TempDir(), synthexport.Fixture()), pipeline: ingest.NewPipeline(ingest.Config{
		Store:      s,
		Catalog:    cat,
		Activities: activities,
		Codec:      route.NewCodec(enc, dec),
		Duplicates: workouts.NewDetector(activities),
	})}
}

// run imports the zip of the fixture in a new running XML import, and returns the import
// once the run is over.
func (im *importer) run(t *testing.T) store.Import {
	t.Helper()

	ctx := t.Context()

	imp, err := im.store.Imports().Create(ctx, store.NewImport{UserID: im.user.ID, Origin: store.OriginXML})
	require.NoError(t, err)

	imp, err = im.store.Imports().SetStatus(ctx, im.user.ID, imp.ID, store.ImportRunning)
	require.NoError(t, err)

	f, err := os.Open(im.export)
	require.NoError(t, err)

	defer func() { require.NoError(t, f.Close()) }()

	export, err := applexml.Open(ctx, f)
	require.NoError(t, err)

	require.NoError(t, im.pipeline.Run(ctx, im.user.ID, imp.ID, export.Records(ctx)))

	imp, err = im.store.Imports().Get(ctx, im.user.ID, imp.ID)
	require.NoError(t, err)

	return imp
}

// workouts returns every stored workout of the user, the duplicates included.
func (im *importer) workouts(t *testing.T) []store.Workout {
	t.Helper()

	all, err := im.store.Workouts().List(t.Context(), im.user.ID, store.WorkoutsQuery{
		From:           time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC),
		To:             time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		Limit:          1000,
		WithDuplicates: true,
	})
	require.NoError(t, err)

	return all
}

// Given the fixture imported twice through the pipeline, then the second run inserts no workout,
// and the third-party copy of the run is stored apart, the duplicate flag of #250 marking it.
func TestRecords_FixtureImportedTwice_InsertsNoWorkoutTheSecondTime(t *testing.T) {
	t.Parallel()

	im := newImporter(t)

	first := im.run(t)
	require.Equal(t, int64(5), first.Report.Workouts.Received)
	require.Equal(t, int64(5), first.Report.Workouts.Inserted)
	require.Zero(t, first.Report.Workouts.Invalid)

	stored := im.workouts(t)
	require.Len(t, stored, 5)

	second := im.run(t)
	require.Equal(t, store.ImportCounters{Received: 5, Duplicates: 5}, second.Report.Workouts)
	require.Equal(t, stored, im.workouts(t), "the second run changes no workout")

	var watchRun, stravaRun store.Workout

	for _, w := range stored {
		require.Empty(t, w.ExternalID, "a workout of export.xml has no uuid")

		switch {
		case w.Start.Equal(time.Date(2025, 12, 30, 6, 0, 0, 0, time.UTC)):
			watchRun = w
		case w.Start.Equal(time.Date(2025, 12, 30, 6, 0, 5, 0, time.UTC)):
			stravaRun = w
		}
	}

	require.NotZero(t, watchRun.ID, "the run of the watch is stored")
	require.NotZero(t, stravaRun.ID, "the copy of Strava is stored apart")
	require.Equal(t, watchRun.ID, stravaRun.DuplicateOf, "the copy of Strava duplicates the run of the watch")
	require.Zero(t, watchRun.DuplicateOf)
	require.NotEqual(t, watchRun.SourceID, stravaRun.SourceID)

	for _, w := range stored {
		if w.ID != stravaRun.ID {
			require.Zero(t, w.DuplicateOf, "workout %d duplicates nothing", w.ID)
		}
	}

	require.Zero(t, first.Report.Workouts.Warnings, "every route of the zip is whole")

	rt, err := im.store.Workouts().GetRoute(t.Context(), im.user.ID, watchRun.ID)
	require.NoError(t, err)
	require.Equal(t, 25, rt.PointCount, "the route of the run is stored with it")
}
