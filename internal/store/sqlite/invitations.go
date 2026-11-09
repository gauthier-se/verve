package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// invitations implements store.InvitationsRepository. Its errors never hold a hash: the hash of a
// token stands for the token in the database.
type invitations struct {
	q   queries
	clk clock.Clock
}

func (r invitations) Create(ctx context.Context, invitation store.NewInvitation) (store.Invitation, error) {
	var row sqlcgen.Invitation

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		row, err = q.CreateInvitation(ctx, sqlcgen.CreateInvitationParams{
			Kind:      string(invitation.Kind),
			TokenHash: invitation.TokenHash,
			Username:  sql.NullString{String: invitation.Username, Valid: invitation.Username != ""},
			Email:     sql.NullString{String: invitation.Email, Valid: invitation.Email != ""},
			Role:      sql.NullString{String: string(invitation.Role), Valid: invitation.Role != ""},
			UserID:    sql.NullInt64{Int64: invitation.UserID, Valid: invitation.UserID != 0},
			ExpiresAt: toMillis(invitation.ExpiresAt),
			CreatedBy: sql.NullInt64{Int64: invitation.CreatedBy, Valid: invitation.CreatedBy != 0},
			CreatedAt: toMillis(r.clk.Now()),
		})

		// The foreign keys of a new link are its creator and the user of a reset link.
		if isForeignKeyError(err) {
			return store.ErrNotFound
		}

		return constraintError(err)
	})
	if err != nil {
		return store.Invitation{}, fmt.Errorf("create invitation: %w", err)
	}

	return invitationOf(row), nil
}

func (r invitations) GetByTokenHash(ctx context.Context, tokenHash []byte) (store.Invitation, error) {
	row, err := r.q.read().GetInvitationByTokenHash(ctx, tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.Invitation{}, fmt.Errorf("get invitation: %w", err)
	}

	return invitationOf(row), nil
}

func (r invitations) MarkUsed(ctx context.Context, id, userID int64) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		updated, err := q.MarkInvitationUsed(ctx, sqlcgen.MarkInvitationUsedParams{
			UsedAt: nullMillis(r.clk.Now()),
			UserID: sql.NullInt64{Int64: userID, Valid: true},
			ID:     id,
		})
		if isForeignKeyError(err) {
			return store.ErrNotFound
		}

		if err != nil {
			return err
		}

		if updated == 0 {
			return store.ErrNotFound
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("mark invitation %d used: %w", id, err)
	}

	return nil
}

func (r invitations) DeleteUnusedResets(ctx context.Context, userID int64) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		return q.DeleteUnusedResets(ctx, sql.NullInt64{Int64: userID, Valid: true})
	})
	if err != nil {
		return fmt.Errorf("delete unused reset links of user %d: %w", userID, err)
	}

	return nil
}

func invitationOf(row sqlcgen.Invitation) store.Invitation {
	return store.Invitation{
		ID:        row.ID,
		Kind:      store.InvitationKind(row.Kind),
		Username:  row.Username.String,
		Email:     row.Email.String,
		Role:      store.UserRole(row.Role.String),
		ExpiresAt: fromMillis(row.ExpiresAt),
		UsedAt:    fromNullMillis(row.UsedAt),
		UserID:    row.UserID.Int64,
		CreatedBy: row.CreatedBy.Int64,
		CreatedAt: fromMillis(row.CreatedAt),
	}
}
