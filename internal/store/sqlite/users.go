package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// users implements store.UsersRepository.
type users struct {
	q   queries
	clk clock.Clock
}

func (r users) Create(ctx context.Context, user store.NewUser) (store.User, error) {
	var row sqlcgen.User

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		row, err = q.CreateUser(ctx, sqlcgen.CreateUserParams{
			Username:     user.Username,
			UsernameFold: store.FoldCase(user.Username),
			Email:        sql.NullString{String: user.Email, Valid: user.Email != ""},
			EmailFold:    sql.NullString{String: store.FoldCase(user.Email), Valid: user.Email != ""},
			PasswordHash: sql.NullString{String: user.PasswordHash, Valid: user.PasswordHash != ""},
			Role:         string(user.Role),
			Status:       string(store.UserActive),
			CreatedAt:    toMillis(r.clk.Now()),
		})

		return constraintError(err)
	})
	if err != nil {
		return store.User{}, fmt.Errorf("create user: %w", err)
	}

	return userOf(row), nil
}

func (r users) Get(ctx context.Context, id int64) (store.User, error) {
	row, err := r.q.read().GetUser(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.User{}, fmt.Errorf("get user %d: %w", id, err)
	}

	return userOf(row), nil
}

// GetByLogin keeps login out of its errors: people do type their password in the wrong field.
func (r users) GetByLogin(ctx context.Context, login string) (store.User, error) {
	row, err := r.q.read().GetUserByLogin(ctx, store.FoldCase(login))
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.User{}, fmt.Errorf("get user by login: %w", err)
	}

	return userOf(row), nil
}

func (r users) SetPasswordHash(ctx context.Context, id int64, hash string) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		updated, err := q.SetUserPasswordHash(ctx, sqlcgen.SetUserPasswordHashParams{
			PasswordHash: sql.NullString{String: hash, Valid: hash != ""},
			ID:           id,
		})
		if err != nil {
			return err
		}

		if updated == 0 {
			return store.ErrNotFound
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("set password hash of user %d: %w", id, err)
	}

	return nil
}

func (r users) RecordLogin(ctx context.Context, id int64) (store.User, error) {
	var row sqlcgen.User

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		row, err = q.SetUserLastLoginAt(ctx, sqlcgen.SetUserLastLoginAtParams{
			LastLoginAt: nullMillis(r.clk.Now()),
			ID:          id,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNotFound
		}

		return err
	})
	if err != nil {
		return store.User{}, fmt.Errorf("record login of user %d: %w", id, err)
	}

	return userOf(row), nil
}

func (r users) Count(ctx context.Context) (int64, error) {
	count, err := r.q.read().CountUsers(ctx)
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}

	return count, nil
}

// constraintError returns the store error of a failed constraint, or err for any other error.
func constraintError(err error) error {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return err
	}

	switch sqliteErr.Code() {
	case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
		return store.ErrConflict
	case sqlite3.SQLITE_CONSTRAINT_CHECK:
		return store.ErrInvalid
	default:
		return err
	}
}

func userOf(row sqlcgen.User) store.User {
	return store.User{
		ID:           row.ID,
		Username:     row.Username,
		Email:        row.Email.String,
		PasswordHash: row.PasswordHash.String,
		Role:         store.UserRole(row.Role),
		Status:       store.UserStatus(row.Status),
		CreatedAt:    fromMillis(row.CreatedAt),
		LastLoginAt:  fromNullMillis(row.LastLoginAt),
	}
}
