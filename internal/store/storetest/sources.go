package storetest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// sourcesTime has sub-millisecond digits and a non-UTC zone, so that the suite checks that the
// times of a source are truncated to the millisecond and returned in UTC.
var sourcesTime = time.Date(2026, 11, 12, 10, 15, 30, 987_654_321, time.FixedZone("CET", 3600))

// sourcesUTC is sourcesTime truncated to the millisecond, in UTC.
var sourcesUTC = time.Date(2026, 11, 12, 9, 15, 30, 987_000_000, time.UTC)

// watchInfo is the source of a record of an invented Apple Watch, as the XML export gives it.
var watchInfo = store.SourceInfo{
	Name:            "Montre de Camille",
	BundleID:        "com.apple.health.81C2A1F0",
	DeviceName:      "Apple Watch",
	DeviceModel:     "Watch",
	HardwareVersion: "Watch7,3",
	SoftwareVersion: "11.1",
}

// summarizedInfo is the synthetic source of the summarized Health Auto Export points.
var summarizedInfo = store.SourceInfo{Name: "Health Auto Export (summarized)"}

// sourceInfo returns the source of a record named name of the device model model, without details.
func sourceInfo(name, model string) store.SourceInfo {
	return store.SourceInfo{Name: name, DeviceModel: model}
}

// sourceIdentity returns the identity of name and model.
func sourceIdentity(name, model string) store.SourceIdentity {
	return store.SourceIdentity{Name: name, DeviceModel: model}
}

func mustResolve(ctx context.Context, t *testing.T, sources store.SourcesRepository, userID int64,
	info store.SourceInfo, kind store.SourceKind, seenAt time.Time,
) store.Source {
	t.Helper()

	got, err := sources.Resolve(ctx, userID, info, kind, seenAt)
	require.NoError(t, err)

	return got
}

func mustListSources(ctx context.Context, t *testing.T, sources store.SourcesRepository, userID int64) []store.Source {
	t.Helper()

	got, err := sources.List(ctx, userID)
	require.NoError(t, err)

	return got
}

// RunSources runs the SourcesRepository contract against the stores returned by newStore.
func RunSources(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given the source of a record and a kind, when Resolve runs, then it returns a new source of
	// that identity, with every detail of the record, the name as display name, that kind, and the
	// given time (millisecond precision, UTC) as first and last seen. Get and List return it.
	t.Run("ResolveCreatesTheSourceOfANewIdentity", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sourcesTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))

		got := mustResolve(ctx, t, s.Sources(), user.ID, watchInfo, store.SourceWatch, sourcesTime)

		require.NotZero(t, got.ID)
		require.Equal(t, store.Source{
			ID:              got.ID,
			UserID:          user.ID,
			Name:            "Montre de Camille",
			DeviceModel:     "Watch",
			DisplayName:     "Montre de Camille",
			BundleID:        "com.apple.health.81C2A1F0",
			DeviceName:      "Apple Watch",
			HardwareVersion: "Watch7,3",
			SoftwareVersion: "11.1",
			Kind:            store.SourceWatch,
			KindSetByUser:   false,
			FirstSeenAt:     sourcesUTC,
			LastSeenAt:      sourcesUTC,
		}, got)

		stored, err := s.Sources().Get(ctx, user.ID, got.ID)
		require.NoError(t, err)
		require.Equal(t, got, stored)

		require.Equal(t, []store.Source{got}, mustListSources(ctx, t, s.Sources(), user.ID))
	})

	runSourcesIdentity(t, newStore)
	runSourcesSeen(t, newStore)
	runSourcesAliases(t, newStore)
	runSourcesCorrections(t, newStore)
	runSourcesOfUsers(t, newStore)
}

// runSourcesOfUsers runs the cases of the isolation of the sources of each user.
func runSourcesOfUsers(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given the synthetic source of summarized Health Auto Export points, of kind aggregated, when
	// two users resolve it twice, then each user has one source of that kind.
	t.Run("ResolveOfASyntheticSourceTwiceMakesOneSourcePerUser", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sourcesTime))
		ctx := t.Context()
		camille := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))
		dominique := mustCreateUser(ctx, t, s.Users(), newUser("dominique", "dominique@example.com"))

		for _, user := range []store.User{camille, dominique} {
			first := mustResolve(ctx, t, s.Sources(), user.ID, summarizedInfo, store.SourceAggregated, sourcesTime)
			second := mustResolve(ctx, t, s.Sources(), user.ID, summarizedInfo, store.SourceAggregated, sourcesTime.Add(time.Hour))

			require.Equal(t, first.ID, second.ID)
			require.Equal(t, store.SourceAggregated, second.Kind)
			require.Equal(t, []store.Source{second}, mustListSources(ctx, t, s.Sources(), user.ID))
		}
	})

	// Given the same identity for two users, then they get two sources. Given a source of another
	// user, then Get, Rename and SetKind answer ErrNotFound and leave it unchanged, and List never
	// returns it.
	t.Run("SourcesOfAnotherUserAreNeverReturned", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sourcesTime))
		ctx := t.Context()
		camille := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))
		dominique := mustCreateUser(ctx, t, s.Users(), newUser("dominique", "dominique@example.com"))

		own := mustResolve(ctx, t, s.Sources(), camille.ID, watchInfo, store.SourceWatch, sourcesTime)
		foreign := mustResolve(ctx, t, s.Sources(), dominique.ID, watchInfo, store.SourceWatch, sourcesTime)
		require.NotEqual(t, own.ID, foreign.ID)
		require.Equal(t, dominique.ID, foreign.UserID)

		_, err := s.Sources().Get(ctx, camille.ID, foreign.ID)
		require.ErrorIs(t, err, store.ErrNotFound)

		_, err = s.Sources().Rename(ctx, camille.ID, foreign.ID, "Ma montre")
		require.ErrorIs(t, err, store.ErrNotFound)

		_, err = s.Sources().SetKind(ctx, camille.ID, foreign.ID, store.SourcePhone)
		require.ErrorIs(t, err, store.ErrNotFound)

		require.Equal(t, []store.Source{own}, mustListSources(ctx, t, s.Sources(), camille.ID))
		require.Equal(t, []store.Source{foreign}, mustListSources(ctx, t, s.Sources(), dominique.ID))
		require.Empty(t, mustListSources(ctx, t, s.Sources(), dominique.ID+camille.ID+1))
	})

	// Given an empty name or an unknown kind, then Resolve answers ErrInvalid, and given an unknown
	// user, ErrNotFound. No source is created.
	t.Run("ResolveRefusesInvalidRecordsAndUnknownUsers", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sourcesTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))

		_, err := s.Sources().Resolve(ctx, user.ID, sourceInfo("", "Watch"), store.SourceWatch, sourcesTime)
		require.ErrorIs(t, err, store.ErrInvalid)

		_, err = s.Sources().Resolve(ctx, user.ID, watchInfo, "tablet", sourcesTime)
		require.ErrorIs(t, err, store.ErrInvalid)

		_, err = s.Sources().Resolve(ctx, user.ID+1, watchInfo, store.SourceWatch, sourcesTime)
		require.ErrorIs(t, err, store.ErrNotFound)

		require.Empty(t, mustListSources(ctx, t, s.Sources(), user.ID))
	})

	// Given concurrent Resolve calls of one new identity, then they all return one source.
	t.Run("ConcurrentResolvesOfANewIdentityMakeOneSource", func(t *testing.T) {
		t.Parallel()

		const callers = 8

		s := newStore(t, clock.NewFake(sourcesTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))

		var wg sync.WaitGroup

		ids := make(chan int64, callers)
		errs := make(chan error, callers)

		for range callers {
			wg.Go(func() {
				source, err := s.Sources().Resolve(ctx, user.ID, watchInfo, store.SourceWatch, sourcesTime)
				if err != nil {
					errs <- err

					return
				}

				ids <- source.ID
			})
		}

		wg.Wait()
		close(ids)
		close(errs)

		for err := range errs {
			require.NoError(t, err)
		}

		stored := mustListSources(ctx, t, s.Sources(), user.ID)
		require.Len(t, stored, 1)

		for id := range ids {
			require.Equal(t, stored[0].ID, id)
		}
	})
}

// runSourcesCorrections runs the cases of the kind of a source and of the corrections of the user.
func runSourcesCorrections(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a renamed source, then its display name changes and its identity does not: Resolve of
	// that identity returns it, with the new display name.
	t.Run("RenameChangesTheDisplayNameAndKeepsTheIdentity", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sourcesTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))
		created := mustResolve(ctx, t, s.Sources(), user.ID, watchInfo, store.SourceWatch, sourcesTime)

		got, err := s.Sources().Rename(ctx, user.ID, created.ID, "Ma montre")
		require.NoError(t, err)

		want := created
		want.DisplayName = "Ma montre"
		require.Equal(t, want, got)

		resolved := mustResolve(ctx, t, s.Sources(), user.ID, watchInfo, store.SourceWatch, sourcesTime)
		require.Equal(t, want, resolved)

		_, err = s.Sources().Rename(ctx, user.ID, created.ID, "")
		require.ErrorIs(t, err, store.ErrInvalid)
	})

	// Given a source whose kind the user did not set, when a later record is resolved with another
	// kind, then the source takes that kind, unless it is unknown. An earlier record only replaces
	// an unknown kind.
	t.Run("ResolveTakesTheKindOfTheLatestRecord", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sourcesTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))
		info := sourceInfo("Balance", "")

		steps := []struct {
			kind   store.SourceKind
			seenAt time.Time
			want   store.SourceKind
		}{
			{store.SourceUnknown, sourcesTime, store.SourceUnknown},
			{store.SourceApp, sourcesTime.Add(-time.Hour), store.SourceApp},
			{store.SourceScale, sourcesTime.Add(-time.Hour), store.SourceApp},
			{store.SourceScale, sourcesTime, store.SourceScale},
			{store.SourceUnknown, sourcesTime.Add(time.Hour), store.SourceScale},
		}

		for i, step := range steps {
			got := mustResolve(ctx, t, s.Sources(), user.ID, info, step.kind, step.seenAt)
			require.Equal(t, step.want, got.Kind, "step %d", i)
			require.False(t, got.KindSetByUser, "step %d", i)
		}
	})

	// Given a kind set by the user, then a later Resolve keeps it instead of the kind it is given.
	t.Run("SetKindIsKeptByLaterResolves", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sourcesTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))
		created := mustResolve(ctx, t, s.Sources(), user.ID, sourceInfo("Pesée", ""), store.SourceApp, sourcesTime)

		got, err := s.Sources().SetKind(ctx, user.ID, created.ID, store.SourceScale)
		require.NoError(t, err)

		want := created
		want.Kind = store.SourceScale
		want.KindSetByUser = true
		require.Equal(t, want, got)

		resolved := mustResolve(ctx, t, s.Sources(), user.ID, sourceInfo("Pesée", ""), store.SourceApp, sourcesTime.Add(time.Hour))
		require.Equal(t, store.SourceScale, resolved.Kind)
		require.True(t, resolved.KindSetByUser)

		_, err = s.Sources().SetKind(ctx, user.ID, created.ID, "tablet")
		require.ErrorIs(t, err, store.ErrInvalid)
	})
}

// runSourcesAliases runs the cases of the aliases of merged sources.
func runSourcesAliases(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given an alias from an identity to a source, when a record of that identity is resolved, then
	// Resolve returns that source, seen at the time of the record, and creates none.
	t.Run("ResolveOfAnAliasReturnsItsSource", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sourcesTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))
		target := mustResolve(ctx, t, s.Sources(), user.ID, watchInfo, store.SourceWatch, sourcesTime)

		require.NoError(t, s.Sources().AddAlias(ctx, user.ID, store.SourceIdentity{Name: "Ancienne montre", DeviceModel: "Watch"}, target.ID))

		got := mustResolve(ctx, t, s.Sources(), user.ID, sourceInfo("Ancienne montre", "Watch"), store.SourceWatch,
			sourcesTime.Add(time.Hour))

		want := target
		want.LastSeenAt = sourcesUTC.Add(time.Hour)

		require.Equal(t, want, got)
		require.Equal(t, []store.Source{want}, mustListSources(ctx, t, s.Sources(), user.ID))
	})

	// Given an alias without model, when a record of its name with a model is resolved, then it
	// returns the source of the alias, whose model does not change, and the alias takes the model:
	// a record of another model then creates a source.
	t.Run("ResolveMatchesAnAliasWithoutModelWhichTakesTheModel", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sourcesTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))
		target := mustResolve(ctx, t, s.Sources(), user.ID, sourceInfo("Montre", ""), store.SourceWatch, sourcesTime)

		require.NoError(t, s.Sources().AddAlias(ctx, user.ID, store.SourceIdentity{Name: "Ancienne montre"}, target.ID))

		got := mustResolve(ctx, t, s.Sources(), user.ID, sourceInfo("Ancienne montre", "Watch"), store.SourceWatch, sourcesTime)
		require.Equal(t, target, got)

		other := mustResolve(ctx, t, s.Sources(), user.ID, sourceInfo("Ancienne montre", "iPhone"), store.SourcePhone, sourcesTime)
		require.NotEqual(t, target.ID, other.ID)
		require.Equal(t, store.SourceIdentity{Name: "Ancienne montre", DeviceModel: "iPhone"},
			store.SourceIdentity{Name: other.Name, DeviceModel: other.DeviceModel})
	})

	// Given an alias with a model, when a record of its name without model is resolved, then it
	// returns the source of the alias, the only identity of that name.
	t.Run("ResolveWithoutModelMatchesTheOnlyAliasOfItsName", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sourcesTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))
		target := mustResolve(ctx, t, s.Sources(), user.ID, watchInfo, store.SourceWatch, sourcesTime)

		require.NoError(t, s.Sources().AddAlias(ctx, user.ID, store.SourceIdentity{Name: "Ancienne montre", DeviceModel: "Watch"}, target.ID))

		got := mustResolve(ctx, t, s.Sources(), user.ID, sourceInfo("Ancienne montre", ""), store.SourceWatch, sourcesTime)
		require.Equal(t, target, got)
	})

	// Given an identity that a source or an alias already has, an empty name, or a source that the
	// user does not have, then AddAlias refuses it.
	t.Run("AddAliasRefusesTakenIdentitiesAndUnknownSources", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sourcesTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))
		other := mustCreateUser(ctx, t, s.Users(), newUser("dominique", "dominique@example.com"))
		target := mustResolve(ctx, t, s.Sources(), user.ID, watchInfo, store.SourceWatch, sourcesTime)
		phone := mustResolve(ctx, t, s.Sources(), user.ID, sourceInfo("iPhone de Camille", "iPhone"), store.SourcePhone, sourcesTime)
		foreign := mustResolve(ctx, t, s.Sources(), other.ID, watchInfo, store.SourceWatch, sourcesTime)

		alias := store.SourceIdentity{Name: "Ancienne montre", DeviceModel: "Watch"}
		require.NoError(t, s.Sources().AddAlias(ctx, user.ID, alias, target.ID))

		cases := []struct {
			name     string
			identity store.SourceIdentity
			sourceID int64
			want     error
		}{
			{"IdentityOfASource", store.SourceIdentity{Name: "iPhone de Camille", DeviceModel: "iPhone"}, target.ID, store.ErrConflict},
			{"IdentityOfAnAlias", alias, phone.ID, store.ErrConflict},
			{"EmptyName", store.SourceIdentity{DeviceModel: "Watch"}, target.ID, store.ErrInvalid},
			{"UnknownSource", store.SourceIdentity{Name: "Montre"}, phone.ID + foreign.ID + 1, store.ErrNotFound},
			{"SourceOfAnotherUser", store.SourceIdentity{Name: "Montre"}, foreign.ID, store.ErrNotFound},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := s.Sources().AddAlias(ctx, user.ID, tc.identity, tc.sourceID)
				require.ErrorIs(t, err, tc.want)
			})
		}

		got := mustResolve(ctx, t, s.Sources(), user.ID, sourceInfo("Ancienne montre", "Watch"), store.SourceWatch, sourcesTime)
		require.Equal(t, target.ID, got.ID)
	})
}

// runSourcesSeen runs the cases of the first and last seen times and of the details of a source.
func runSourcesSeen(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a source, when a later record of it is resolved, then last seen moves to its time and
	// the source takes its bundle id, device name and versions. First seen does not move.
	t.Run("ResolveOfALaterRecordMovesLastSeenAndTakesItsDetails", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sourcesTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))
		created := mustResolve(ctx, t, s.Sources(), user.ID, watchInfo, store.SourceWatch, sourcesTime)

		later := watchInfo
		later.BundleID = "com.apple.health.5D0E77B2"
		later.DeviceName = "Apple Watch Ultra"
		later.HardwareVersion = "Watch7,5"
		later.SoftwareVersion = "11.2"

		got := mustResolve(ctx, t, s.Sources(), user.ID, later, store.SourceWatch, sourcesTime.Add(time.Hour))

		want := created
		want.BundleID = "com.apple.health.5D0E77B2"
		want.DeviceName = "Apple Watch Ultra"
		want.HardwareVersion = "Watch7,5"
		want.SoftwareVersion = "11.2"
		want.LastSeenAt = sourcesUTC.Add(time.Hour)

		require.Equal(t, want, got)
		require.Equal(t, []store.Source{want}, mustListSources(ctx, t, s.Sources(), user.ID))
	})

	// Given a source, when an earlier record of it is resolved, then neither first nor last seen
	// moves, and its details only fill those that the source lacks.
	t.Run("ResolveOfAnEarlierRecordMovesNoTimeAndOnlyFillsMissingDetails", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sourcesTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))

		first := sourceInfo("Montre de Camille", "Watch")
		first.SoftwareVersion = "11.1"
		created := mustResolve(ctx, t, s.Sources(), user.ID, first, store.SourceWatch, sourcesTime)

		earlier := sourceInfo("Montre de Camille", "Watch")
		earlier.BundleID = "com.apple.health.81C2A1F0"
		earlier.SoftwareVersion = "10.0"

		got := mustResolve(ctx, t, s.Sources(), user.ID, earlier, store.SourceWatch, sourcesTime.Add(-24*time.Hour))

		want := created
		want.BundleID = "com.apple.health.81C2A1F0"

		require.Equal(t, want, got)
	})

	// Given a source with details, when a later record without details is resolved, as the name
	// only payloads of Health Auto Export, then the source keeps its model and details, and last
	// seen moves.
	t.Run("ResolveOfALaterRecordWithoutDetailsKeepsThoseOfTheSource", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sourcesTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))
		created := mustResolve(ctx, t, s.Sources(), user.ID, watchInfo, store.SourceWatch, sourcesTime)

		got := mustResolve(ctx, t, s.Sources(), user.ID, sourceInfo("Montre de Camille", ""), store.SourceWatch,
			sourcesTime.Add(time.Hour))

		want := created
		want.LastSeenAt = sourcesUTC.Add(time.Hour)

		require.Equal(t, want, got)
	})
}

// runSourcesIdentity runs the cases of the identity rule of FR-2.
func runSourcesIdentity(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given records with and without device model resolved in order, then each lands in the source
	// numbered by want, sources being numbered in the order of their creation, and the sources end
	// with the identities of identities.
	cases := []struct {
		name       string
		records    []store.SourceInfo
		want       []int
		identities []store.SourceIdentity
	}{
		{
			name:       "SameNameAndModelMatchTheSameSource",
			records:    []store.SourceInfo{sourceInfo("Watch", "Watch"), sourceInfo("Watch", "Watch")},
			want:       []int{0, 0},
			identities: []store.SourceIdentity{sourceIdentity("Watch", "Watch")},
		},
		{
			name:       "SameNameOnTwoModelsMakesTwoSources",
			records:    []store.SourceInfo{sourceInfo("Camille", "Watch"), sourceInfo("Camille", "iPhone")},
			want:       []int{0, 1},
			identities: []store.SourceIdentity{sourceIdentity("Camille", "Watch"), sourceIdentity("Camille", "iPhone")},
		},
		{
			name:       "NameIsComparedAsReceived",
			records:    []store.SourceInfo{sourceInfo("Strava", ""), sourceInfo("strava", ""), sourceInfo("Strava ", "")},
			want:       []int{0, 1, 2},
			identities: []store.SourceIdentity{sourceIdentity("Strava", ""), sourceIdentity("strava", ""), sourceIdentity("Strava ", "")},
		},
		{
			name:       "RecordWithModelMatchesTheSourceWithoutModelWhichTakesTheModel",
			records:    []store.SourceInfo{sourceInfo("Camille", ""), sourceInfo("Camille", "Watch"), sourceInfo("Camille", "Watch")},
			want:       []int{0, 0, 0},
			identities: []store.SourceIdentity{sourceIdentity("Camille", "Watch")},
		},
		{
			name: "RecordWithAnotherModelCreatesASourceOnceTheSourceWithoutModelTookOne",
			records: []store.SourceInfo{
				sourceInfo("Camille", ""), sourceInfo("Camille", "Watch"), sourceInfo("Camille", "iPhone"),
			},
			want:       []int{0, 0, 1},
			identities: []store.SourceIdentity{sourceIdentity("Camille", "Watch"), sourceIdentity("Camille", "iPhone")},
		},
		{
			name:       "RecordWithoutModelMatchesTheOnlySourceOfItsName",
			records:    []store.SourceInfo{sourceInfo("Camille", "Watch"), sourceInfo("Camille", "")},
			want:       []int{0, 0},
			identities: []store.SourceIdentity{sourceIdentity("Camille", "Watch")},
		},
		{
			name: "RecordWithoutModelAmongSourcesWithModelsCreatesASourceWithoutModel",
			records: []store.SourceInfo{
				sourceInfo("Camille", "Watch"), sourceInfo("Camille", "iPhone"), sourceInfo("Camille", ""),
				sourceInfo("Camille", ""),
			},
			want:       []int{0, 1, 2, 2},
			identities: []store.SourceIdentity{sourceIdentity("Camille", "Watch"), sourceIdentity("Camille", "iPhone"), sourceIdentity("Camille", "")},
		},
		{
			name: "RecordWithModelPrefersItsIdentityToTheSourceWithoutModel",
			records: []store.SourceInfo{
				sourceInfo("Camille", "Watch"), sourceInfo("Camille", "iPhone"), sourceInfo("Camille", ""),
				sourceInfo("Camille", "Watch"), sourceInfo("Camille", "iPad"),
			},
			want:       []int{0, 1, 2, 0, 2},
			identities: []store.SourceIdentity{sourceIdentity("Camille", "Watch"), sourceIdentity("Camille", "iPhone"), sourceIdentity("Camille", "iPad")},
		},
	}

	for _, tc := range cases {
		t.Run("Resolve"+tc.name, func(t *testing.T) {
			t.Parallel()

			s := newStore(t, clock.NewFake(sourcesTime))
			ctx := t.Context()
			user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))

			numbers := make(map[int64]int)
			got := make([]int, 0, len(tc.records))

			for _, record := range tc.records {
				source := mustResolve(ctx, t, s.Sources(), user.ID, record, store.SourceUnknown, sourcesTime)

				if _, ok := numbers[source.ID]; !ok {
					numbers[source.ID] = len(numbers)
				}

				got = append(got, numbers[source.ID])
			}

			require.Equal(t, tc.want, got)

			identities := make([]store.SourceIdentity, 0, len(tc.identities))
			for _, source := range mustListSources(ctx, t, s.Sources(), user.ID) {
				identities = append(identities, store.SourceIdentity{Name: source.Name, DeviceModel: source.DeviceModel})
			}

			require.Equal(t, tc.identities, identities)
		})
	}
}
