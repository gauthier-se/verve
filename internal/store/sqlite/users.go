package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

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

func (r users) List(ctx context.Context, page store.PageRequest) ([]store.User, error) {
	if page.Limit < 1 {
		return nil, fmt.Errorf("list users: limit %d below 1: %w", page.Limit, store.ErrInvalid)
	}

	// The position before every user, so that the query always has one.
	params := sqlcgen.ListUsersParams{AfterAt: math.MaxInt64, AfterID: math.MaxInt64, MaxUsers: int64(page.Limit)}

	if page.After != (store.Cursor{}) {
		params.AfterAt = toMillis(page.After.At)
		params.AfterID = page.After.ID
	}

	rows, err := r.q.read().ListUsers(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	listed := make([]store.User, 0, len(rows))
	for _, row := range rows {
		listed = append(listed, userOf(row))
	}

	return listed, nil
}

func (r users) SetRole(ctx context.Context, id int64, role store.UserRole) (store.User, error) {
	user, err := r.update(ctx, func(q *sqlcgen.Queries) (sqlcgen.User, error) {
		return q.SetUserRole(ctx, sqlcgen.SetUserRoleParams{Role: string(role), ID: id})
	})
	if err != nil {
		return store.User{}, fmt.Errorf("set role of user %d: %w", id, err)
	}

	return user, nil
}

func (r users) SetStatus(ctx context.Context, id int64, status store.UserStatus) (store.User, error) {
	user, err := r.update(ctx, func(q *sqlcgen.Queries) (sqlcgen.User, error) {
		return q.SetUserStatus(ctx, sqlcgen.SetUserStatusParams{Status: string(status), ID: id})
	})
	if err != nil {
		return store.User{}, fmt.Errorf("set status of user %d: %w", id, err)
	}

	return user, nil
}

// update runs on the writer the update of a user that fn makes, and returns the updated user, or
// store.ErrNotFound when fn updates no user.
func (r users) update(ctx context.Context, fn func(q *sqlcgen.Queries) (sqlcgen.User, error)) (store.User, error) {
	var row sqlcgen.User

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		row, err = fn(q)
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNotFound
		}

		return err
	})
	if err != nil {
		return store.User{}, err
	}

	return userOf(row), nil
}

func (r users) CountActiveAdmins(ctx context.Context) (int64, error) {
	count, err := r.q.read().CountActiveAdmins(ctx)
	if err != nil {
		return 0, fmt.Errorf("count active admins: %w", err)
	}

	return count, nil
}

// Delete relies on the foreign keys of the tables of the user, which cascade.
func (r users) Delete(ctx context.Context, id int64) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		deleted, err := q.DeleteUser(ctx, id)
		if err != nil {
			return err
		}

		if deleted == 0 {
			return store.ErrNotFound
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("delete user %d: %w", id, err)
	}

	return nil
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

// isForeignKeyError reports whether err is the failure of a foreign key constraint.
func isForeignKeyError(err error) bool {
	var sqliteErr *sqlite.Error

	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY
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
