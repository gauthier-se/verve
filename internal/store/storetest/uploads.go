package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// uploadsTime has sub-millisecond digits and a non-UTC zone, so that the suite checks that the
// times of an upload are truncated to the millisecond and returned in UTC.
var uploadsTime = time.Date(2026, 11, 17, 10, 0, 0, 456_789_123, time.FixedZone("CET", 3600))

// uploadsUTC is uploadsTime truncated to the millisecond, in UTC.
var uploadsUTC = time.Date(2026, 11, 17, 9, 0, 0, 456_000_000, time.UTC)

// uploadsTest holds a store on a fake clock with two users.
type uploadsTest struct {
	store store.Store
	clk   *clock.Fake
	user  store.User
	other store.User
}

func newUploadsTest(t *testing.T, newStore NewStoreFunc) uploadsTest {
	t.Helper()

	clk := clock.NewFake(uploadsTime)
	s := newStore(t, clk)
	ctx := t.Context()

	return uploadsTest{
		store: s,
		clk:   clk,
		user:  mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com")),
		other: mustCreateUser(ctx, t, s.Users(), newUser("dominique", "dominique@example.com")),
	}
}

// exportUpload returns the upload id of an export.zip of 1000 bytes of userID, which expires 24
// hours after uploadsTime.
func exportUpload(userID int64, id string) store.NewUpload {
	return store.NewUpload{
		ID:        id,
		UserID:    userID,
		FileName:  "export.zip",
		Length:    1000,
		ExpiresAt: uploadsTime.Add(24 * time.Hour),
	}
}

func mustCreateUpload(ctx context.Context, t *testing.T, uploads store.UploadsRepository, upload store.NewUpload) store.Upload {
	t.Helper()

	got, err := uploads.Create(ctx, upload)
	require.NoError(t, err)

	return got
}

// RunUploads runs the UploadsRepository contract against the stores returned by newStore.
func RunUploads(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a created upload, then Get returns it at offset 0, with the clock's now as CreatedAt,
	// its times truncated to the millisecond in UTC.
	t.Run("CreateStoresEveryFieldAndGetReturnsIt", func(t *testing.T) {
		t.Parallel()

		ut := newUploadsTest(t, newStore)
		ctx := t.Context()

		created := mustCreateUpload(ctx, t, ut.store.Uploads(), store.NewUpload{
			ID:        "QK4ZM2T7X3N5B6V7C8D9F2G3H4",
			UserID:    ut.user.ID,
			FileName:  "Exportación.zip",
			Length:    5_000_000_000,
			ExpiresAt: time.Date(2026, 11, 18, 10, 0, 0, 987_654_321, time.FixedZone("CET", 3600)),
		})

		want := store.Upload{
			ID:        "QK4ZM2T7X3N5B6V7C8D9F2G3H4",
			UserID:    ut.user.ID,
			FileName:  "Exportación.zip",
			Length:    5_000_000_000,
			CreatedAt: uploadsUTC,
			ExpiresAt: time.Date(2026, 11, 18, 9, 0, 0, 987_000_000, time.UTC),
		}
		require.Equal(t, want, created)

		got, err := ut.store.Uploads().Get(ctx, ut.user.ID, created.ID)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	// Given an upload without file name, then its file name stays empty.
	t.Run("FileNameIsOptional", func(t *testing.T) {
		t.Parallel()

		ut := newUploadsTest(t, newStore)
		ctx := t.Context()

		upload := exportUpload(ut.user.ID, "upload-1")
		upload.FileName = ""
		mustCreateUpload(ctx, t, ut.store.Uploads(), upload)

		got, err := ut.store.Uploads().Get(ctx, ut.user.ID, "upload-1")
		require.NoError(t, err)
		require.Empty(t, got.FileName)
	})

	// Given an empty id, a length below 1, a taken id or an unknown user, then Create refuses the
	// upload.
	t.Run("CreateRefusesAnInvalidUpload", func(t *testing.T) {
		t.Parallel()

		ut := newUploadsTest(t, newStore)
		ctx := t.Context()
		mustCreateUpload(ctx, t, ut.store.Uploads(), exportUpload(ut.user.ID, "taken"))

		tests := []struct {
			name   string
			change func(*store.NewUpload)
			want   error
		}{
			{name: "empty id", change: func(u *store.NewUpload) { u.ID = "" }, want: store.ErrInvalid},
			{name: "length 0", change: func(u *store.NewUpload) { u.Length = 0 }, want: store.ErrInvalid},
			{name: "taken id", change: func(u *store.NewUpload) { u.ID = "taken"; u.UserID = ut.other.ID }, want: store.ErrConflict},
			{name: "unknown user", change: func(u *store.NewUpload) { u.UserID = ut.other.ID + 100 }, want: store.ErrNotFound},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				upload := exportUpload(ut.user.ID, "upload-1")
				tt.change(&upload)

				_, err := ut.store.Uploads().Create(ctx, upload)
				require.ErrorIs(t, err, tt.want)
			})
		}
	})

	// Given an upload of a user, then Get by another user or by an unknown id returns
	// store.ErrNotFound.
	t.Run("GetOfAnotherUserOrUnknownReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		ut := newUploadsTest(t, newStore)
		ctx := t.Context()
		mustCreateUpload(ctx, t, ut.store.Uploads(), exportUpload(ut.user.ID, "upload-1"))

		_, err := ut.store.Uploads().Get(ctx, ut.other.ID, "upload-1")
		require.ErrorIs(t, err, store.ErrNotFound)

		_, err = ut.store.Uploads().Get(ctx, ut.user.ID, "unknown")
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	// Given an upload at offset 0, when Advance moves it from 0 to 400 with a new expiry, then Get
	// returns the new offset and expiry, and an advance to the length is accepted.
	t.Run("AdvanceMovesTheOffsetAndTheExpiry", func(t *testing.T) {
		t.Parallel()

		ut := newUploadsTest(t, newStore)
		ctx := t.Context()
		mustCreateUpload(ctx, t, ut.store.Uploads(), exportUpload(ut.user.ID, "upload-1"))

		expires := uploadsTime.Add(25 * time.Hour)

		advanced, err := ut.store.Uploads().Advance(ctx, ut.user.ID, "upload-1", 0, 400, expires)
		require.NoError(t, err)
		require.Equal(t, int64(400), advanced.Offset)
		require.Equal(t, uploadsUTC.Add(25*time.Hour), advanced.ExpiresAt)

		got, err := ut.store.Uploads().Get(ctx, ut.user.ID, "upload-1")
		require.NoError(t, err)
		require.Equal(t, advanced, got)

		advanced, err = ut.store.Uploads().Advance(ctx, ut.user.ID, "upload-1", 400, 1000, expires)
		require.NoError(t, err)
		require.Equal(t, int64(1000), advanced.Offset)
	})

	// Given an upload at offset 400, then Advance from another offset returns store.ErrConflict,
	// to an offset below the current one or past the length store.ErrInvalid, and for another user
	// or an unknown id store.ErrNotFound, each leaving the upload as it was.
	t.Run("AdvanceRefusesAnotherOffsetOrUser", func(t *testing.T) {
		t.Parallel()

		ut := newUploadsTest(t, newStore)
		ctx := t.Context()
		mustCreateUpload(ctx, t, ut.store.Uploads(), exportUpload(ut.user.ID, "upload-1"))

		before, err := ut.store.Uploads().Advance(ctx, ut.user.ID, "upload-1", 0, 400, uploadsTime.Add(24*time.Hour))
		require.NoError(t, err)

		later := uploadsTime.Add(48 * time.Hour)

		tests := []struct {
			name     string
			userID   int64
			id       string
			from, to int64
			want     error
		}{
			{name: "another offset", userID: ut.user.ID, id: "upload-1", from: 0, to: 500, want: store.ErrConflict},
			{name: "below the offset", userID: ut.user.ID, id: "upload-1", from: 400, to: 300, want: store.ErrInvalid},
			{name: "past the length", userID: ut.user.ID, id: "upload-1", from: 400, to: 1001, want: store.ErrInvalid},
			{name: "another user", userID: ut.other.ID, id: "upload-1", from: 400, to: 500, want: store.ErrNotFound},
			{name: "unknown id", userID: ut.user.ID, id: "unknown", from: 400, to: 500, want: store.ErrNotFound},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := ut.store.Uploads().Advance(ctx, tt.userID, tt.id, tt.from, tt.to, later)
				require.ErrorIs(t, err, tt.want)

				got, err := ut.store.Uploads().Get(ctx, ut.user.ID, "upload-1")
				require.NoError(t, err)
				require.Equal(t, before, got)
			})
		}
	})

	// Given an upload, then Delete by another user returns store.ErrNotFound and keeps it, and
	// Delete by its user removes it, so that Get and a second Delete return store.ErrNotFound.
	t.Run("DeleteRemovesTheUploadOfItsUserOnly", func(t *testing.T) {
		t.Parallel()

		ut := newUploadsTest(t, newStore)
		ctx := t.Context()
		mustCreateUpload(ctx, t, ut.store.Uploads(), exportUpload(ut.user.ID, "upload-1"))

		require.ErrorIs(t, ut.store.Uploads().Delete(ctx, ut.other.ID, "upload-1"), store.ErrNotFound)

		_, err := ut.store.Uploads().Get(ctx, ut.user.ID, "upload-1")
		require.NoError(t, err)

		require.NoError(t, ut.store.Uploads().Delete(ctx, ut.user.ID, "upload-1"))

		_, err = ut.store.Uploads().Get(ctx, ut.user.ID, "upload-1")
		require.ErrorIs(t, err, store.ErrNotFound)
		require.ErrorIs(t, ut.store.Uploads().Delete(ctx, ut.user.ID, "upload-1"), store.ErrNotFound)
	})

	// Given uploads of two users, one expiring before now, one at now and one after, then
	// DeleteExpired removes the first two, whatever their user, and returns their ids, and IDs
	// returns the ids of the uploads that remain.
	t.Run("DeleteExpiredRemovesTheUploadsExpiredAtNow", func(t *testing.T) {
		t.Parallel()

		ut := newUploadsTest(t, newStore)
		ctx := t.Context()

		for _, upload := range []struct {
			id      string
			userID  int64
			expires time.Time
		}{
			{id: "expired-before", userID: ut.user.ID, expires: uploadsTime.Add(time.Hour)},
			{id: "expired-at-now", userID: ut.other.ID, expires: uploadsTime.Add(2 * time.Hour)},
			{id: "kept", userID: ut.user.ID, expires: uploadsTime.Add(2*time.Hour + time.Millisecond)},
		} {
			created := exportUpload(upload.userID, upload.id)
			created.ExpiresAt = upload.expires
			mustCreateUpload(ctx, t, ut.store.Uploads(), created)
		}

		ids, err := ut.store.Uploads().IDs(ctx)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"expired-before", "expired-at-now", "kept"}, ids)

		ut.clk.Advance(2 * time.Hour)

		deleted, err := ut.store.Uploads().DeleteExpired(ctx)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"expired-before", "expired-at-now"}, deleted)

		ids, err = ut.store.Uploads().IDs(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"kept"}, ids)

		deleted, err = ut.store.Uploads().DeleteExpired(ctx)
		require.NoError(t, err)
		require.Empty(t, deleted)
	})

	// Given uploads of two users, when the account of one is deleted, then its uploads are gone and
	// those of the other user stay.
	t.Run("DeletingTheUserRemovesTheirUploads", func(t *testing.T) {
		t.Parallel()

		ut := newUploadsTest(t, newStore)
		ctx := t.Context()
		mustCreateUpload(ctx, t, ut.store.Uploads(), exportUpload(ut.user.ID, "of-camille"))
		mustCreateUpload(ctx, t, ut.store.Uploads(), exportUpload(ut.other.ID, "of-dominique"))

		require.NoError(t, ut.store.Users().Delete(ctx, ut.user.ID))

		_, err := ut.store.Uploads().Get(ctx, ut.user.ID, "of-camille")
		require.ErrorIs(t, err, store.ErrNotFound)

		ids, err := ut.store.Uploads().IDs(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"of-dominique"}, ids)
	})
}
