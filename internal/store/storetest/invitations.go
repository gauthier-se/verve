package storetest

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// invitationsTime has sub-millisecond digits and a non-UTC zone, so that the suite checks that the
// times of an invitation are truncated to the millisecond and returned in UTC.
var invitationsTime = time.Date(2026, 11, 9, 10, 30, 15, 123_456_789, time.FixedZone("CET", 3600))

// invitationsUTC is invitationsTime truncated to the millisecond, in UTC.
var invitationsUTC = time.Date(2026, 11, 9, 9, 30, 15, 123_000_000, time.UTC)

// tokenHash returns the hash of the link token token.
func tokenHash(token string) []byte {
	hash := sha256.Sum256([]byte(token))

	return hash[:]
}

// newInvitation returns an invitation of username as a member, created by createdBy, for the link
// token "secret", which expires in 72 hours.
func newInvitation(createdBy int64, username string) store.NewInvitation {
	return store.NewInvitation{
		Kind:      store.InvitationInvite,
		TokenHash: tokenHash("secret"),
		Username:  username,
		Email:     username + "@example.com",
		Role:      store.RoleMember,
		ExpiresAt: invitationsTime.Add(72 * time.Hour),
		CreatedBy: createdBy,
	}
}

func mustCreateInvitation(ctx context.Context, t *testing.T, invitations store.InvitationsRepository, invitation store.NewInvitation) store.Invitation {
	t.Helper()

	got, err := invitations.Create(ctx, invitation)
	require.NoError(t, err)

	return got
}

// RunInvitations runs the InvitationsRepository contract against the stores returned by newStore.
func RunInvitations(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a created invitation, then GetByTokenHash by the hash of its token returns it, with the
	// clock's now (millisecond precision, UTC) as CreatedAt, and unused.
	t.Run("CreateStoresEveryFieldAndGetByTokenHashReturnsIt", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(invitationsTime))
		ctx := t.Context()
		admin := mustCreateUser(ctx, t, s.Users(), newUser("admin", ""))

		created := mustCreateInvitation(ctx, t, s.Invitations(), store.NewInvitation{
			Kind:      store.InvitationInvite,
			TokenHash: tokenHash("secret"),
			Username:  "Bob",
			Email:     "bob@example.com",
			Role:      store.RoleAdmin,
			ExpiresAt: time.Date(2026, 11, 12, 10, 30, 15, 987_654_321, time.FixedZone("CET", 3600)),
			CreatedBy: admin.ID,
		})

		want := store.Invitation{
			ID:        created.ID,
			Kind:      store.InvitationInvite,
			Username:  "Bob",
			Email:     "bob@example.com",
			Role:      store.RoleAdmin,
			ExpiresAt: time.Date(2026, 11, 12, 9, 30, 15, 987_000_000, time.UTC),
			CreatedBy: admin.ID,
			CreatedAt: invitationsUTC,
		}
		require.NotZero(t, created.ID)
		require.Equal(t, want, created)

		got, err := s.Invitations().GetByTokenHash(ctx, tokenHash("secret"))
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	// Given an invitation without an email, then its email stays empty.
	t.Run("EmailIsOptional", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(invitationsTime))
		ctx := t.Context()
		admin := mustCreateUser(ctx, t, s.Users(), newUser("admin", ""))

		invitation := newInvitation(admin.ID, "bob")
		invitation.Email = ""
		mustCreateInvitation(ctx, t, s.Invitations(), invitation)

		got, err := s.Invitations().GetByTokenHash(ctx, tokenHash("secret"))
		require.NoError(t, err)
		require.Empty(t, got.Email)
	})

	// Given a hash that no invitation has, then GetByTokenHash returns store.ErrNotFound.
	t.Run("GetByTokenHashOnUnknownHashReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(invitationsTime))
		ctx := t.Context()
		admin := mustCreateUser(ctx, t, s.Users(), newUser("admin", ""))
		mustCreateInvitation(ctx, t, s.Invitations(), newInvitation(admin.ID, "bob"))

		_, err := s.Invitations().GetByTokenHash(ctx, tokenHash("other"))
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	// Given an invitation, when another one has the same token hash, then Create returns
	// store.ErrConflict.
	t.Run("CreateWithATakenTokenHashReturnsErrConflict", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(invitationsTime))
		ctx := t.Context()
		admin := mustCreateUser(ctx, t, s.Users(), newUser("admin", ""))
		mustCreateInvitation(ctx, t, s.Invitations(), newInvitation(admin.ID, "bob"))

		_, err := s.Invitations().Create(ctx, newInvitation(admin.ID, "carol"))
		require.ErrorIs(t, err, store.ErrConflict)
	})

	// Given an unused invitation, when MarkUsed marks it used by a user, then it has the clock's
	// now as UsedAt and that user as UserID. Marking it used again returns store.ErrNotFound, as
	// for an unknown invitation.
	t.Run("MarkUsedMarksOnce", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(invitationsTime)
		s := newStore(t, clk)
		ctx := t.Context()
		admin := mustCreateUser(ctx, t, s.Users(), newUser("admin", ""))
		invitation := mustCreateInvitation(ctx, t, s.Invitations(), newInvitation(admin.ID, "bob"))
		bob := mustCreateUser(ctx, t, s.Users(), newUser("bob", ""))

		clk.Advance(time.Hour)
		require.NoError(t, s.Invitations().MarkUsed(ctx, invitation.ID, bob.ID))

		got, err := s.Invitations().GetByTokenHash(ctx, tokenHash("secret"))
		require.NoError(t, err)
		require.Equal(t, invitationsUTC.Add(time.Hour), got.UsedAt)
		require.Equal(t, bob.ID, got.UserID)

		require.ErrorIs(t, s.Invitations().MarkUsed(ctx, invitation.ID, bob.ID), store.ErrNotFound)
		require.ErrorIs(t, s.Invitations().MarkUsed(ctx, invitation.ID+1, bob.ID), store.ErrNotFound)
	})

	// Given an unused invitation, when many transactions mark it used concurrently, then exactly
	// one of them succeeds and the others get store.ErrNotFound.
	t.Run("MarkUsedSucceedsOnceUnderConcurrency", func(t *testing.T) {
		t.Parallel()

		const markers = 8

		s := newStore(t, clock.NewFake(invitationsTime))
		ctx := t.Context()
		admin := mustCreateUser(ctx, t, s.Users(), newUser("admin", ""))
		invitation := mustCreateInvitation(ctx, t, s.Invitations(), newInvitation(admin.ID, "bob"))

		var wg sync.WaitGroup

		errs := make(chan error, markers)

		for range markers {
			wg.Go(func() {
				errs <- s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
					return tx.Invitations().MarkUsed(ctx, invitation.ID, admin.ID)
				})
			})
		}

		wg.Wait()
		close(errs)

		var succeeded int

		for err := range errs {
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, store.ErrNotFound):
			default:
				require.NoError(t, err)
			}
		}

		require.Equal(t, 1, succeeded)
	})
}

// RunPasswordResetLinks runs the InvitationsRepository contract of the password reset links
// against the stores returned by newStore.
func RunPasswordResetLinks(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a created reset link, then GetByTokenHash returns it with its user, and without the
	// username, email and role that only an invitation has.
	t.Run("ResetLinkStoresItsUser", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(invitationsTime))
		ctx := t.Context()
		admin := mustCreateUser(ctx, t, s.Users(), newUser("admin", ""))
		bob := mustCreateUser(ctx, t, s.Users(), newUser("bob", ""))

		created := mustCreateInvitation(ctx, t, s.Invitations(), newResetLink(admin.ID, bob.ID, "secret"))

		want := store.Invitation{
			ID:        created.ID,
			Kind:      store.InvitationReset,
			ExpiresAt: invitationsUTC.Add(24 * time.Hour),
			UserID:    bob.ID,
			CreatedBy: admin.ID,
			CreatedAt: invitationsUTC,
		}
		require.Equal(t, want, created)

		got, err := s.Invitations().GetByTokenHash(ctx, tokenHash("secret"))
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	// Given unused and used reset links of a user, unused reset links of another user and an
	// invitation, when DeleteUnusedResets deletes those of the user, then only their unused reset
	// links are gone.
	t.Run("DeleteUnusedResetsDeletesOnlyTheUnusedResetLinksOfTheUser", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(invitationsTime))
		ctx := t.Context()
		admin := mustCreateUser(ctx, t, s.Users(), newUser("admin", ""))
		bob := mustCreateUser(ctx, t, s.Users(), newUser("bob", ""))
		carol := mustCreateUser(ctx, t, s.Users(), newUser("carol", ""))

		mustCreateInvitation(ctx, t, s.Invitations(), newResetLink(admin.ID, bob.ID, "bob first"))
		mustCreateInvitation(ctx, t, s.Invitations(), newResetLink(admin.ID, bob.ID, "bob second"))
		used := mustCreateInvitation(ctx, t, s.Invitations(), newResetLink(admin.ID, bob.ID, "bob used"))
		require.NoError(t, s.Invitations().MarkUsed(ctx, used.ID, bob.ID))
		mustCreateInvitation(ctx, t, s.Invitations(), newResetLink(admin.ID, carol.ID, "carol"))

		invitation := newInvitation(admin.ID, "dave")
		invitation.TokenHash = tokenHash("dave")
		mustCreateInvitation(ctx, t, s.Invitations(), invitation)

		require.NoError(t, s.Invitations().DeleteUnusedResets(ctx, bob.ID))

		for token, kept := range map[string]bool{
			"bob first": false, "bob second": false, "bob used": true, "carol": true, "dave": true,
		} {
			_, err := s.Invitations().GetByTokenHash(ctx, tokenHash(token))
			if kept {
				require.NoError(t, err, token)
			} else {
				require.ErrorIs(t, err, store.ErrNotFound, token)
			}
		}
	})
}

// newResetLink returns a reset link of the user of userID, created by createdBy, for the link token
// token, which expires in 24 hours.
func newResetLink(createdBy, userID int64, token string) store.NewInvitation {
	return store.NewInvitation{
		Kind:      store.InvitationReset,
		TokenHash: tokenHash(token),
		UserID:    userID,
		ExpiresAt: invitationsTime.Add(24 * time.Hour),
		CreatedBy: createdBy,
	}
}
