package sqlite

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// Given two users with a timezone, when one user is deleted, then their preferences are deleted with
// them and the other user keeps theirs. No repository deletes a user yet, so the test deletes the
// row as the deletion of accounts will.
func TestPreferences_DeletedWithTheirUser(t *testing.T) {
	t.Parallel()

	s, db := newTestStore(t, clock.NewFake(time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)))
	ctx := t.Context()

	var ids []int64

	for _, username := range []string{"alice", "bob"} {
		user, err := s.Users().Create(ctx, store.NewUser{Username: username, Role: store.RoleMember})
		require.NoError(t, err)

		_, err = s.Preferences().SetTimezone(ctx, user.ID, "Europe/Paris")
		require.NoError(t, err)

		ids = append(ids, user.ID)
	}

	_, err := db.Write().ExecContext(ctx, "DELETE FROM users WHERE id = ?", ids[0])
	require.NoError(t, err)

	for id, want := range map[int64]int{ids[0]: 0, ids[1]: 1} {
		var count int

		err := db.Read().QueryRowContext(ctx, "SELECT count(*) FROM user_preferences WHERE user_id = ?", id).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, want, count, "user %d", id)
	}
}
