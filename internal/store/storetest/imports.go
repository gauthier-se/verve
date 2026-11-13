package storetest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// importsTime has sub-millisecond digits and a non-UTC zone, so that the suite checks that the
// times of an import are truncated to the millisecond and returned in UTC.
var importsTime = time.Date(2026, 11, 13, 10, 0, 0, 456_789_123, time.FixedZone("CET", 3600))

// importsUTC is importsTime truncated to the millisecond, in UTC.
var importsUTC = time.Date(2026, 11, 13, 9, 0, 0, 456_000_000, time.UTC)

// importsTest holds a store on a fake clock with two users, a source of the first one and a
// quantity type, for the samples that an import writes.
type importsTest struct {
	store  store.Store
	clk    *clock.Fake
	user   store.User
	other  store.User
	source store.Source
	heart  store.MetricType
}

func newImportsTest(t *testing.T, newStore NewStoreFunc) importsTest {
	t.Helper()

	clk := clock.NewFake(importsTime)
	s := newStore(t, clk)
	ctx := t.Context()
	user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))
	other := mustCreateUser(ctx, t, s.Users(), newUser("dominique", "dominique@example.com"))

	heart, err := s.MetricTypes().EnsureUncurated(ctx, "HKQuantityTypeIdentifierHeartRate", catalog.KindQuantity, "count/min")
	require.NoError(t, err)

	return importsTest{
		store:  s,
		clk:    clk,
		user:   user,
		other:  other,
		source: mustResolve(ctx, t, s.Sources(), user.ID, watchInfo, store.SourceWatch, importsTime),
		heart:  heart,
	}
}

// nativePush returns a native import of userID pushed by the token of id 7 from an iOS client.
func nativePush(userID int64) store.NewImport {
	return store.NewImport{
		UserID:  userID,
		Origin:  store.OriginNative,
		TokenID: 7,
		Client: map[string]string{
			"name":        "Verve for iOS",
			"version":     "1.0.0",
			"deviceModel": "iPhone17,1",
			"osVersion":   "27.1",
		},
	}
}

// xmlUpload returns an XML import of userID, without token or client.
func xmlUpload(userID int64) store.NewImport {
	return store.NewImport{UserID: userID, Origin: store.OriginXML}
}

func mustCreateImport(ctx context.Context, t *testing.T, imports store.ImportsRepository, imp store.NewImport) store.Import {
	t.Helper()

	got, err := imports.Create(ctx, imp)
	require.NoError(t, err)

	return got
}

func mustGetImport(ctx context.Context, t *testing.T, imports store.ImportsRepository, userID, id int64) store.Import {
	t.Helper()

	got, err := imports.Get(ctx, userID, id)
	require.NoError(t, err)

	return got
}

func mustSetImportStatus(ctx context.Context, t *testing.T, imports store.ImportsRepository, userID, id int64,
	status store.ImportStatus,
) store.Import {
	t.Helper()

	got, err := imports.SetStatus(ctx, userID, id, status)
	require.NoError(t, err)

	return got
}

// mustCreateFinished creates an import of imp, runs it and ends it with status.
func mustCreateFinished(ctx context.Context, t *testing.T, imports store.ImportsRepository, imp store.NewImport,
	status store.ImportStatus,
) store.Import {
	t.Helper()

	created := mustCreateImport(ctx, t, imports, imp)
	mustSetImportStatus(ctx, t, imports, imp.UserID, created.ID, store.ImportRunning)

	return mustSetImportStatus(ctx, t, imports, imp.UserID, created.ID, status)
}

// mustListAllImports returns every import of userID, read in pages of pageSize.
func mustListAllImports(ctx context.Context, t *testing.T, imports store.ImportsRepository, userID int64, pageSize int) []store.Import {
	t.Helper()

	page := store.PageRequest{Limit: pageSize}

	var all []store.Import

	for {
		got, err := imports.List(ctx, userID, page)
		require.NoError(t, err)
		require.LessOrEqual(t, len(got), pageSize)

		all = append(all, got...)

		if len(got) < pageSize {
			return all
		}

		page.After = got[len(got)-1].Cursor()
	}
}

// importIDs returns the ids of imports, in order.
func importIDs(imports ...store.Import) []int64 {
	ids := make([]int64, 0, len(imports))
	for _, imp := range imports {
		ids = append(ids, imp.ID)
	}

	return ids
}

// fullReport returns a report with every counter set, each to a distinct value, in total, for two
// type keys and for the workouts, and two error messages.
func fullReport() store.ImportReport {
	return store.ImportReport{
		Total: store.ImportCounters{
			Received: 1201, Inserted: 1180, Updated: 3, Duplicates: 18, Blocked: 4, Invalid: 2,
			Unsupported: 1, Suspicious: 5, Warnings: 6, Deleted: 7, NotFound: 8,
		},
		Types: map[string]store.ImportCounters{
			"heart_rate":                            {Received: 99, Inserted: 97, Invalid: 2, Deleted: 1},
			"HKDataTypeIdentifierElectrocardiogram": {Received: 1, Unsupported: 1},
		},
		Workouts: store.ImportCounters{Received: 1, Inserted: 1, Warnings: 1},
		Errors: []string{
			`record 17 (HKQuantityTypeIdentifierHeartRate): unit "bpm" is not a unit of count/min`,
			"record 18 (HKQuantityTypeIdentifierHeartRate): end before start",
		},
	}
}

// heartRate returns a heart rate sample of the user of st, keyed by key, stored by the import of
// importID.
func (st importsTest) heartRate(key string, importID int64) store.NewSample {
	return store.NewSample{
		Sample: store.Sample{
			TypeID:     st.heart.ID,
			SourceID:   st.source.ID,
			Start:      importsTime,
			End:        importsTime,
			Value:      60,
			Origin:     store.OriginNative,
			ImportID:   importID,
			NaturalKey: []byte(key),
		},
		OnConflict: store.ConflictIgnore,
	}
}

// samplesOf returns the heart rate samples of the user of st.
func (st importsTest) samplesOf(ctx context.Context, t *testing.T) []store.Sample {
	t.Helper()

	query := allTime
	query.TypeID = st.heart.ID
	query.Limit = 100

	got, err := st.store.Samples().List(ctx, st.user.ID, query)
	require.NoError(t, err)

	return got
}

// RunImports runs the contract of ImportsRepository against the stores returned by newStore.
func RunImports(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	runImportsCreate(t, newStore)
	runImportsSaveProgress(t, newStore)
	runImportsSetStatus(t, newStore)
	runImportsList(t, newStore)
	runImportsPurge(t, newStore)
	runImportsLastByOrigin(t, newStore)
	runImportsDeleteByUser(t, newStore)
}

func runImportsCreate(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a new import, when Create runs, then it is stored queued with its user, origin, token id,
	// client metadata and creation time, and returned with its id.
	t.Run("CreateStoresAQueuedImportWithItsUserOriginTokenAndClient", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()

		created := mustCreateImport(ctx, t, st.store.Imports(), nativePush(st.user.ID))
		require.Positive(t, created.ID)
		require.Equal(t, store.Import{
			ID:        created.ID,
			UserID:    st.user.ID,
			Origin:    store.OriginNative,
			TokenID:   7,
			Client:    nativePush(st.user.ID).Client,
			Status:    store.ImportQueued,
			CreatedAt: importsUTC,
		}, created)
		require.Equal(t, created, mustGetImport(ctx, t, st.store.Imports(), st.user.ID, created.ID))
	})

	// Given a new import without token or client, when Create runs, then it is stored with no token
	// id and no client metadata.
	t.Run("CreateStoresAnImportWithoutTokenOrClient", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()

		first := mustCreateImport(ctx, t, st.store.Imports(), xmlUpload(st.user.ID))
		second := mustCreateImport(ctx, t, st.store.Imports(), xmlUpload(st.user.ID))
		require.Greater(t, second.ID, first.ID)

		got := mustGetImport(ctx, t, st.store.Imports(), st.user.ID, first.ID)
		require.Equal(t, store.OriginXML, got.Origin)
		require.Zero(t, got.TokenID)
		require.Nil(t, got.Client)
	})

	// Given an unknown origin, when Create runs, then it answers ErrInvalid and stores nothing.
	t.Run("CreateOfAnUnknownOriginReturnsErrInvalid", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()

		for _, origin := range []store.Origin{"", "csv", "XML"} {
			_, err := st.store.Imports().Create(ctx, store.NewImport{UserID: st.user.ID, Origin: origin})
			require.ErrorIs(t, err, store.ErrInvalid, "origin %q", origin)
		}

		require.Empty(t, mustListAllImports(ctx, t, st.store.Imports(), st.user.ID, 10))
	})

	// Given an unknown user, when Create runs, then it answers ErrNotFound.
	t.Run("CreateForAnUnknownUserReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)

		_, err := st.store.Imports().Create(t.Context(), xmlUpload(st.other.ID+100))
		require.ErrorIs(t, err, store.ErrNotFound)
	})
}

func runImportsSaveProgress(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a running import, when SaveProgress runs, then the position and every counter, in total,
	// per type key and for the workouts, and the error messages are stored together, and a second
	// call replaces them.
	t.Run("SaveProgressStoresThePositionAndEveryCounterOfTheReport", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()
		imports := st.store.Imports()

		created := mustCreateImport(ctx, t, imports, nativePush(st.user.ID))
		mustSetImportStatus(ctx, t, imports, st.user.ID, created.ID, store.ImportRunning)

		first := store.ImportProgress{Position: 5000, Report: store.ImportReport{
			Total:  store.ImportCounters{Received: 5000, Inserted: 5000},
			Types:  map[string]store.ImportCounters{"step_count": {Received: 5000, Inserted: 5000}},
			Errors: []string{"record 3 (HKQuantityTypeIdentifierStepCount): missing source"},
		}}
		require.NoError(t, imports.SaveProgress(ctx, st.user.ID, created.ID, first))

		got := mustGetImport(ctx, t, imports, st.user.ID, created.ID)
		require.Equal(t, first.Position, got.Position)
		require.Equal(t, first.Report, got.Report)

		second := store.ImportProgress{Position: 8_123_456_789, Report: fullReport()}
		require.NoError(t, imports.SaveProgress(ctx, st.user.ID, created.ID, second))

		got = mustGetImport(ctx, t, imports, st.user.ID, created.ID)
		require.Equal(t, second.Position, got.Position)
		require.Equal(t, fullReport(), got.Report)
		require.Equal(t, store.ImportRunning, got.Status)

		listed := mustListAllImports(ctx, t, imports, st.user.ID, 10)
		require.Equal(t, []store.Import{got}, listed, "List returns the whole report")
	})

	// Given a report without type keys or errors, then they come back nil.
	t.Run("SaveProgressOfAnEmptyReportReturnsNilTypesAndErrors", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()
		imports := st.store.Imports()

		created := mustCreateImport(ctx, t, imports, nativePush(st.user.ID))
		mustSetImportStatus(ctx, t, imports, st.user.ID, created.ID, store.ImportRunning)

		require.NoError(t, imports.SaveProgress(ctx, st.user.ID, created.ID, store.ImportProgress{
			Position: 1,
			Report: store.ImportReport{
				Total:  store.ImportCounters{Received: 1, NotFound: 1},
				Types:  map[string]store.ImportCounters{},
				Errors: []string{},
			},
		}))

		got := mustGetImport(ctx, t, imports, st.user.ID, created.ID)
		require.Equal(t, store.ImportReport{Total: store.ImportCounters{Received: 1, NotFound: 1}}, got.Report)
	})

	// Given 101 error messages, when SaveProgress runs, then the first 100 are kept and the 101st
	// is dropped.
	t.Run("SaveProgressKeepsAtMostOneHundredErrorMessages", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()
		imports := st.store.Imports()

		created := mustCreateImport(ctx, t, imports, nativePush(st.user.ID))
		mustSetImportStatus(ctx, t, imports, st.user.ID, created.ID, store.ImportRunning)

		messages := make([]string, 0, 101)
		for i := range 101 {
			messages = append(messages, fmt.Sprintf("record %d (HKQuantityTypeIdentifierHeartRate): missing source", i))
		}

		require.NoError(t, imports.SaveProgress(ctx, st.user.ID, created.ID, store.ImportProgress{
			Position: 101,
			Report:   store.ImportReport{Total: store.ImportCounters{Received: 101, Invalid: 101}, Errors: messages},
		}))

		got := mustGetImport(ctx, t, imports, st.user.ID, created.ID)
		require.Len(t, got.Report.Errors, store.ImportErrorsLimit)
		require.Equal(t, messages[:100], got.Report.Errors)
		require.Len(t, messages, 101, "the caller's messages are left alone")
	})

	// Given a running import, when a batch commits through SaveProgress in the transaction of its
	// writes, then the writes, the position and the counters are stored together, and a rollback
	// stores none of them.
	t.Run("SaveProgressCommitsAndRollsBackWithTheWritesOfItsBatch", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()

		created := mustCreateImport(ctx, t, st.store.Imports(), nativePush(st.user.ID))
		mustSetImportStatus(ctx, t, st.store.Imports(), st.user.ID, created.ID, store.ImportRunning)

		batch := func(key string, position int64) func(ctx context.Context, tx store.Repositories) error {
			return func(ctx context.Context, tx store.Repositories) error {
				if _, err := tx.Samples().Upsert(ctx, st.user.ID, []store.NewSample{st.heartRate(key, created.ID)}); err != nil {
					return err
				}

				return tx.Imports().SaveProgress(ctx, st.user.ID, created.ID, store.ImportProgress{
					Position: position,
					Report: store.ImportReport{
						Total: store.ImportCounters{Received: position, Inserted: position},
						Types: map[string]store.ImportCounters{"heart_rate": {Received: position, Inserted: position}},
					},
				})
			}
		}

		require.NoError(t, st.store.WithTx(ctx, batch("committed", 1)))

		errRollback := errors.New("roll back")
		err := st.store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			if err := batch("rolled back", 2)(ctx, tx); err != nil {
				return err
			}

			return errRollback
		})
		require.ErrorIs(t, err, errRollback)

		got := mustGetImport(ctx, t, st.store.Imports(), st.user.ID, created.ID)
		require.Equal(t, int64(1), got.Position, "the rolled back batch stores no position")
		require.Equal(t, store.ImportCounters{Received: 1, Inserted: 1}, got.Report.Total)
		require.Equal(t, map[string]store.ImportCounters{"heart_rate": {Received: 1, Inserted: 1}}, got.Report.Types)

		samples := st.samplesOf(ctx, t)
		require.Len(t, samples, 1, "the rolled back batch stores no sample")
		require.Equal(t, []byte("committed"), samples[0].NaturalKey)
	})

	// Given a queued or a finished import, when SaveProgress runs, then it answers ErrConflict and
	// stores nothing.
	t.Run("SaveProgressOfAnImportThatIsNotRunningReturnsErrConflict", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()
		imports := st.store.Imports()

		queued := mustCreateImport(ctx, t, imports, nativePush(st.user.ID))
		completed := mustCreateFinished(ctx, t, imports, nativePush(st.user.ID), store.ImportCompleted)

		for _, imp := range []store.Import{queued, completed} {
			err := imports.SaveProgress(ctx, st.user.ID, imp.ID, store.ImportProgress{Position: 3, Report: fullReport()})
			require.ErrorIs(t, err, store.ErrConflict, "import %s", imp.Status)
			require.Equal(t, imp, mustGetImport(ctx, t, imports, st.user.ID, imp.ID))
		}
	})

	// Given an import of another user, or an unknown id, when SaveProgress runs, then it answers
	// ErrNotFound and stores nothing.
	t.Run("SaveProgressOfAnImportOfAnotherUserReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()
		imports := st.store.Imports()

		foreign := mustCreateImport(ctx, t, imports, nativePush(st.other.ID))
		foreign = mustSetImportStatus(ctx, t, imports, st.other.ID, foreign.ID, store.ImportRunning)

		err := imports.SaveProgress(ctx, st.user.ID, foreign.ID, store.ImportProgress{Position: 3, Report: fullReport()})
		require.ErrorIs(t, err, store.ErrNotFound)

		err = imports.SaveProgress(ctx, st.other.ID, foreign.ID+1, store.ImportProgress{Position: 3})
		require.ErrorIs(t, err, store.ErrNotFound)

		require.Equal(t, foreign, mustGetImport(ctx, t, imports, st.other.ID, foreign.ID))
	})

	// Given a negative position, when SaveProgress runs, then it answers ErrInvalid and stores
	// nothing.
	t.Run("SaveProgressOfANegativePositionReturnsErrInvalid", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()
		imports := st.store.Imports()

		created := mustCreateImport(ctx, t, imports, nativePush(st.user.ID))
		running := mustSetImportStatus(ctx, t, imports, st.user.ID, created.ID, store.ImportRunning)

		err := imports.SaveProgress(ctx, st.user.ID, created.ID, store.ImportProgress{Position: -1, Report: fullReport()})
		require.ErrorIs(t, err, store.ErrInvalid)
		require.Equal(t, running, mustGetImport(ctx, t, imports, st.user.ID, created.ID))
	})
}

func runImportsSetStatus(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a queued import, when it runs, runs again on resume, then completes, then the time it
	// first ran and the time it ended are stored.
	t.Run("SetStatusStoresTheTimeOfRunningAndOfTheEnd", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()
		imports := st.store.Imports()

		created := mustCreateImport(ctx, t, imports, nativePush(st.user.ID))

		st.clk.Advance(time.Second)

		running := mustSetImportStatus(ctx, t, imports, st.user.ID, created.ID, store.ImportRunning)
		require.Equal(t, store.ImportRunning, running.Status)
		require.Equal(t, importsUTC.Add(time.Second), running.StartedAt)
		require.Zero(t, running.FinishedAt)
		require.Equal(t, running, mustGetImport(ctx, t, imports, st.user.ID, created.ID))

		st.clk.Advance(time.Second)

		resumed := mustSetImportStatus(ctx, t, imports, st.user.ID, created.ID, store.ImportRunning)
		require.Equal(t, running, resumed, "a resumed import keeps the time it first ran")

		st.clk.Advance(time.Second)

		completed := mustSetImportStatus(ctx, t, imports, st.user.ID, created.ID, store.ImportCompleted)
		require.Equal(t, store.ImportCompleted, completed.Status)
		require.Equal(t, importsUTC.Add(time.Second), completed.StartedAt)
		require.Equal(t, importsUTC.Add(3*time.Second), completed.FinishedAt)
		require.Equal(t, importsUTC, completed.CreatedAt)
		require.Equal(t, completed, mustGetImport(ctx, t, imports, st.user.ID, created.ID))
	})

	// Given a queued or a running import, when it fails or is cancelled, then the time it ended is
	// stored, and a queued import that never ran has no start.
	t.Run("SetStatusEndsAQueuedOrARunningImport", func(t *testing.T) {
		t.Parallel()

		for _, status := range []store.ImportStatus{store.ImportCompleted, store.ImportFailed, store.ImportCancelled} {
			st := newImportsTest(t, newStore)
			ctx := t.Context()
			imports := st.store.Imports()

			queued := mustCreateImport(ctx, t, imports, nativePush(st.user.ID))
			running := mustCreateImport(ctx, t, imports, nativePush(st.user.ID))
			mustSetImportStatus(ctx, t, imports, st.user.ID, running.ID, store.ImportRunning)

			st.clk.Advance(time.Minute)

			ended := mustSetImportStatus(ctx, t, imports, st.user.ID, queued.ID, status)
			require.Equal(t, status, ended.Status)
			require.Zero(t, ended.StartedAt, "%s queued import", status)
			require.Equal(t, importsUTC.Add(time.Minute), ended.FinishedAt, "%s queued import", status)

			ended = mustSetImportStatus(ctx, t, imports, st.user.ID, running.ID, status)
			require.Equal(t, status, ended.Status)
			require.Equal(t, importsUTC, ended.StartedAt, "%s running import", status)
			require.Equal(t, importsUTC.Add(time.Minute), ended.FinishedAt, "%s running import", status)
		}
	})

	// Given a finished import, when its status changes again, then it answers ErrConflict and the
	// import keeps its status and its times.
	t.Run("SetStatusOfAFinishedImportReturnsErrConflict", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()
		imports := st.store.Imports()

		finished := []store.ImportStatus{store.ImportCompleted, store.ImportFailed, store.ImportCancelled}

		for _, status := range finished {
			ended := mustCreateFinished(ctx, t, imports, nativePush(st.user.ID), status)

			st.clk.Advance(time.Second)

			for _, next := range append([]store.ImportStatus{store.ImportRunning}, finished...) {
				_, err := imports.SetStatus(ctx, st.user.ID, ended.ID, next)
				require.ErrorIs(t, err, store.ErrConflict, "%s import set %s", status, next)
			}

			require.Equal(t, ended, mustGetImport(ctx, t, imports, st.user.ID, ended.ID))
		}
	})

	// Given the status queued or an unknown one, when SetStatus runs, then it answers ErrInvalid and
	// the import keeps its status.
	t.Run("SetStatusToQueuedOrAnUnknownStatusReturnsErrInvalid", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()
		imports := st.store.Imports()

		created := mustCreateImport(ctx, t, imports, nativePush(st.user.ID))
		running := mustSetImportStatus(ctx, t, imports, st.user.ID, created.ID, store.ImportRunning)

		for _, status := range []store.ImportStatus{store.ImportQueued, "", "done", "Completed"} {
			_, err := imports.SetStatus(ctx, st.user.ID, created.ID, status)
			require.ErrorIs(t, err, store.ErrInvalid, "status %q", status)
		}

		require.Equal(t, running, mustGetImport(ctx, t, imports, st.user.ID, created.ID))
	})

	// Given an import of another user, or an unknown id, when SetStatus runs, then it answers
	// ErrNotFound and the import keeps its status.
	t.Run("SetStatusOfAnImportOfAnotherUserReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()
		imports := st.store.Imports()

		foreign := mustCreateImport(ctx, t, imports, nativePush(st.other.ID))

		_, err := imports.SetStatus(ctx, st.user.ID, foreign.ID, store.ImportRunning)
		require.ErrorIs(t, err, store.ErrNotFound)

		_, err = imports.SetStatus(ctx, st.other.ID, foreign.ID+1, store.ImportCancelled)
		require.ErrorIs(t, err, store.ErrNotFound)

		require.Equal(t, foreign, mustGetImport(ctx, t, imports, st.other.ID, foreign.ID))
	})
}

func runImportsList(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given imports of two users, some created at the same time, then List returns the imports of
	// the user newest first by creation then id, in pages that following the cursor lists once each.
	t.Run("ListReturnsTheImportsOfTheUserNewestFirstInPages", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()
		imports := st.store.Imports()

		var created []store.Import

		for i := range 7 {
			// Three imports share each second, so that pages break between imports of one time.
			if i%3 == 0 {
				st.clk.Advance(time.Second)
			}

			created = append(created, mustCreateImport(ctx, t, imports, nativePush(st.user.ID)))
			mustCreateImport(ctx, t, imports, nativePush(st.other.ID))
		}

		// The first import is created first and finished last: List sorts by creation.
		mustSetImportStatus(ctx, t, imports, st.user.ID, created[0].ID, store.ImportCancelled)

		want := importIDs(created...)
		slices.Reverse(want)

		for _, pageSize := range []int{1, 2, 3, 10} {
			require.Equal(t, want, importIDs(mustListAllImports(ctx, t, imports, st.user.ID, pageSize)...), "pages of %d", pageSize)
		}

		got, err := imports.List(ctx, st.user.ID, store.PageRequest{Limit: 1})
		require.NoError(t, err)
		require.Equal(t, []store.Import{mustGetImport(ctx, t, imports, st.user.ID, created[6].ID)}, got)
	})

	// Given a user without imports, then List returns none.
	t.Run("ListWithoutImportsReturnsNone", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()

		mustCreateImport(ctx, t, st.store.Imports(), nativePush(st.other.ID))

		require.Empty(t, mustListAllImports(ctx, t, st.store.Imports(), st.user.ID, 10))
	})

	// Given a limit below 1, then List answers ErrInvalid.
	t.Run("ListWithALimitBelowOneReturnsErrInvalid", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)

		for _, limit := range []int{0, -1} {
			_, err := st.store.Imports().List(t.Context(), st.user.ID, store.PageRequest{Limit: limit})
			require.ErrorIs(t, err, store.ErrInvalid, "limit %d", limit)
		}
	})

	// Given an import of another user, or an unknown id, then Get answers ErrNotFound.
	t.Run("GetOfAnImportOfAnotherUserReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()

		foreign := mustCreateImport(ctx, t, st.store.Imports(), nativePush(st.other.ID))

		_, err := st.store.Imports().Get(ctx, st.user.ID, foreign.ID)
		require.ErrorIs(t, err, store.ErrNotFound)

		_, err = st.store.Imports().Get(ctx, st.other.ID, foreign.ID+1)
		require.ErrorIs(t, err, store.ErrNotFound)
	})
}

func runImportsPurge(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// seedHistory stores, in one transaction, a queued and a running import of the user of st, then
	// count finished imports, a second apart, and a finished import of the other user before them.
	// It returns the queued, the running and the finished imports of the user, oldest first.
	seedHistory := func(ctx context.Context, t *testing.T, st importsTest, count int) (store.Import, store.Import, []store.Import) {
		t.Helper()

		var (
			queued, running store.Import
			finished        []store.Import
		)

		require.NoError(t, st.store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			mustCreateFinished(ctx, t, tx.Imports(), nativePush(st.other.ID), store.ImportCompleted)

			queued = mustCreateImport(ctx, t, tx.Imports(), nativePush(st.user.ID))
			running = mustCreateImport(ctx, t, tx.Imports(), nativePush(st.user.ID))
			running = mustSetImportStatus(ctx, t, tx.Imports(), st.user.ID, running.ID, store.ImportRunning)

			statuses := []store.ImportStatus{store.ImportCompleted, store.ImportFailed, store.ImportCancelled}

			for i := range count {
				st.clk.Advance(time.Second)
				finished = append(finished, mustCreateFinished(ctx, t, tx.Imports(), xmlUpload(st.user.ID), statuses[i%len(statuses)]))
			}

			return nil
		}))

		return queued, running, finished
	}

	// Given a user with a queued, a running and 200 finished imports, when an import is created,
	// then the oldest finished one is purged, the queued and running ones stay although they are
	// older, and so do the imports of another user and the samples of the purged import, with its
	// id.
	t.Run("CreatePurgesTheOldestFinishedImportBeyondTheHistory", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()
		imports := st.store.Imports()

		queued, running, finished := seedHistory(ctx, t, st, store.ImportHistoryLimit)
		require.Len(t, mustListAllImports(ctx, t, imports, st.user.ID, 500), store.ImportHistoryLimit+2,
			"no import is purged while the finished ones are among the 200 newest")

		oldest := finished[0]
		_, err := st.store.Samples().Upsert(ctx, st.user.ID, []store.NewSample{st.heartRate("stored by the oldest", oldest.ID)})
		require.NoError(t, err)

		st.clk.Advance(time.Second)

		created := mustCreateImport(ctx, t, imports, xmlUpload(st.user.ID))

		_, err = imports.Get(ctx, st.user.ID, oldest.ID)
		require.ErrorIs(t, err, store.ErrNotFound, "the oldest finished import is purged")

		want := append([]store.Import{created}, finished[1:]...)
		slices.Reverse(want[1:])
		want = append(want, running, queued)

		got := mustListAllImports(ctx, t, imports, st.user.ID, 500)
		require.Equal(t, importIDs(want...), importIDs(got...))
		require.Len(t, mustListAllImports(ctx, t, imports, st.other.ID, 10), 1, "the imports of the other user stay")

		samples := st.samplesOf(ctx, t)
		require.Len(t, samples, 1, "the samples of a purged import stay")
		require.Equal(t, oldest.ID, samples[0].ImportID)
	})

	// Given a user with 200 finished imports, when the transaction that creates an import rolls
	// back, then no import is purged.
	t.Run("CreatePurgesInItsTransaction", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()

		_, _, finished := seedHistory(ctx, t, st, store.ImportHistoryLimit)

		errRollback := errors.New("roll back")
		err := st.store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			mustCreateImport(ctx, t, tx.Imports(), xmlUpload(st.user.ID))

			_, err := tx.Imports().Get(ctx, st.user.ID, finished[0].ID)
			require.ErrorIs(t, err, store.ErrNotFound, "purged inside the transaction")

			return errRollback
		})
		require.ErrorIs(t, err, errRollback)

		require.Equal(t, finished[0], mustGetImport(ctx, t, st.store.Imports(), st.user.ID, finished[0].ID))
		require.Len(t, mustListAllImports(ctx, t, st.store.Imports(), st.user.ID, 500), store.ImportHistoryLimit+2)
	})
}

func runImportsLastByOrigin(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given imports of two users of several origins and statuses, then LastByOrigin returns the end
	// of the last completed import of the user per origin, and leaves out the origins without one.
	t.Run("LastByOriginReturnsTheEndOfTheLastCompletedImportPerOrigin", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()
		imports := st.store.Imports()

		// The XML import created first ends last.
		lastXML := mustCreateImport(ctx, t, imports, xmlUpload(st.user.ID))
		mustSetImportStatus(ctx, t, imports, st.user.ID, lastXML.ID, store.ImportRunning)

		st.clk.Advance(time.Minute)
		mustCreateFinished(ctx, t, imports, xmlUpload(st.user.ID), store.ImportCompleted)

		st.clk.Advance(time.Minute)
		mustCreateFinished(ctx, t, imports, store.NewImport{UserID: st.user.ID, Origin: store.OriginHAE}, store.ImportCompleted)

		st.clk.Advance(time.Minute)
		mustSetImportStatus(ctx, t, imports, st.user.ID, lastXML.ID, store.ImportCompleted)

		st.clk.Advance(time.Minute)
		mustCreateFinished(ctx, t, imports, xmlUpload(st.user.ID), store.ImportFailed)
		mustCreateFinished(ctx, t, imports, nativePush(st.user.ID), store.ImportCancelled)
		mustCreateImport(ctx, t, imports, store.NewImport{UserID: st.user.ID, Origin: store.OriginCLI})
		mustCreateFinished(ctx, t, imports, store.NewImport{UserID: st.other.ID, Origin: store.OriginCLI}, store.ImportCompleted)

		got, err := imports.LastByOrigin(ctx, st.user.ID)
		require.NoError(t, err)
		require.Equal(t, map[store.Origin]time.Time{
			store.OriginXML: importsUTC.Add(3 * time.Minute),
			store.OriginHAE: importsUTC.Add(2 * time.Minute),
		}, got)
	})

	// Given a user without completed imports, then LastByOrigin returns an empty map.
	t.Run("LastByOriginWithoutCompletedImportsReturnsNone", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()

		mustCreateFinished(ctx, t, st.store.Imports(), xmlUpload(st.user.ID), store.ImportFailed)

		got, err := st.store.Imports().LastByOrigin(ctx, st.user.ID)
		require.NoError(t, err)
		require.Empty(t, got)
	})
}

func runImportsDeleteByUser(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given imports of every status of two users, when DeleteByUser runs in batches, then it removes
	// at most the limit each time until none of the user is left, and leaves those of the other user.
	t.Run("DeleteByUserRemovesTheImportsOfTheUserInBatches", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)
		ctx := t.Context()
		imports := st.store.Imports()

		mustCreateImport(ctx, t, imports, nativePush(st.user.ID))
		running := mustCreateImport(ctx, t, imports, nativePush(st.user.ID))
		mustSetImportStatus(ctx, t, imports, st.user.ID, running.ID, store.ImportRunning)
		mustCreateFinished(ctx, t, imports, xmlUpload(st.user.ID), store.ImportCompleted)
		foreign := mustCreateImport(ctx, t, imports, nativePush(st.other.ID))

		var batches []int64

		for range 3 {
			deleted, err := imports.DeleteByUser(ctx, st.user.ID, 2)
			require.NoError(t, err)

			batches = append(batches, deleted)
		}

		require.Equal(t, []int64{2, 1, 0}, batches)
		require.Empty(t, mustListAllImports(ctx, t, imports, st.user.ID, 10))
		require.Equal(t, []int64{foreign.ID}, importIDs(mustListAllImports(ctx, t, imports, st.other.ID, 10)...))
	})

	// Given a limit below 1, then DeleteByUser answers ErrInvalid.
	t.Run("DeleteByUserWithALimitBelowOneReturnsErrInvalid", func(t *testing.T) {
		t.Parallel()

		st := newImportsTest(t, newStore)

		_, err := st.store.Imports().DeleteByUser(t.Context(), st.user.ID, 0)
		require.ErrorIs(t, err, store.ErrInvalid)
	})
}
