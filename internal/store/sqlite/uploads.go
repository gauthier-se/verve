package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// uploads implements store.UploadsRepository.
type uploads struct {
	q   queries
	clk clock.Clock
}

func (r uploads) Create(ctx context.Context, upload store.NewUpload) (store.Upload, error) {
	if upload.ID == "" || upload.Length < 1 {
		return store.Upload{}, fmt.Errorf("create upload of user %d: empty id or length %d below 1: %w",
			upload.UserID, upload.Length, store.ErrInvalid)
	}

	var row sqlcgen.Upload

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		row, err = q.CreateUpload(ctx, sqlcgen.CreateUploadParams{
			ID:        upload.ID,
			UserID:    upload.UserID,
			FileName:  sql.NullString{String: upload.FileName, Valid: upload.FileName != ""},
			Length:    upload.Length,
			CreatedAt: toMillis(r.clk.Now()),
			ExpiresAt: toMillis(upload.ExpiresAt),
		})

		// The only foreign key of an upload is its user.
		if isForeignKeyError(err) {
			return store.ErrNotFound
		}

		return constraintError(err)
	})
	if err != nil {
		return store.Upload{}, fmt.Errorf("create upload of user %d: %w", upload.UserID, err)
	}

	return uploadOf(row), nil
}

func (r uploads) Get(ctx context.Context, userID int64, id string) (store.Upload, error) {
	row, err := r.q.read().GetUserUpload(ctx, sqlcgen.GetUserUploadParams{UserID: userID, ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.Upload{}, fmt.Errorf("get upload of user %d: %w", userID, err)
	}

	return uploadOf(row), nil
}

func (r uploads) Advance(ctx context.Context, userID int64, id string, from, to int64, expiresAt time.Time) (store.Upload, error) {
	if to < from {
		return store.Upload{}, fmt.Errorf("advance upload of user %d from %d to %d: %w", userID, from, to, store.ErrInvalid)
	}

	var row sqlcgen.Upload

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		row, err = q.AdvanceUserUpload(ctx, sqlcgen.AdvanceUserUploadParams{
			ToOffset:   to,
			ExpiresAt:  toMillis(expiresAt),
			UserID:     userID,
			ID:         id,
			FromOffset: from,
		})
		if errors.Is(err, sql.ErrNoRows) {
			// The upload is missing, or its offset is not from.
			_, err = q.GetUserUpload(ctx, sqlcgen.GetUserUploadParams{UserID: userID, ID: id})

			switch {
			case errors.Is(err, sql.ErrNoRows):
				return store.ErrNotFound
			case err != nil:
				return err
			default:
				return store.ErrConflict
			}
		}

		// The check of the offset refuses an offset past the length.
		return constraintError(err)
	})
	if err != nil {
		return store.Upload{}, fmt.Errorf("advance upload of user %d from %d to %d: %w", userID, from, to, err)
	}

	return uploadOf(row), nil
}

func (r uploads) Delete(ctx context.Context, userID int64, id string) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		deleted, err := q.DeleteUserUpload(ctx, sqlcgen.DeleteUserUploadParams{UserID: userID, ID: id})
		if err != nil {
			return err
		}

		if deleted == 0 {
			return store.ErrNotFound
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("delete upload of user %d: %w", userID, err)
	}

	return nil
}

func (r uploads) DeleteExpired(ctx context.Context) ([]string, error) {
	var ids []string

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		ids, err = q.DeleteExpiredUploads(ctx, toMillis(r.clk.Now()))

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("delete expired uploads: %w", err)
	}

	return ids, nil
}

func (r uploads) IDs(ctx context.Context) ([]string, error) {
	ids, err := r.q.read().ListUploadIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list uploads: %w", err)
	}

	return ids, nil
}

func uploadOf(row sqlcgen.Upload) store.Upload {
	return store.Upload{
		ID:        row.ID,
		UserID:    row.UserID,
		FileName:  row.FileName.String,
		Length:    row.Length,
		Offset:    row.Offset,
		CreatedAt: fromMillis(row.CreatedAt),
		ExpiresAt: fromMillis(row.ExpiresAt),
	}
}
