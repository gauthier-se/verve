package data

import (
	"context"
	"database/sql"
	"errors"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ErrDuplicateEmail is returned by AccountModel.Insert when the email is taken.
var ErrDuplicateEmail = errors.New("data: duplicate email")

// Account is a person who logs into Verve and owns their own data. It also
// carries the static profile attributes from Apple's `Me` (date of birth,
// biological sex, blood type), all nullable. PasswordHash is nullable until
// local auth lands in slice 05. Timestamps are kept as RFC 3339 strings for
// storage-portability; they are not surfaced to users in v1.
type Account struct {
	ID            int64
	Email         string
	PasswordHash  *string
	DateOfBirth   *string
	BiologicalSex *string
	BloodType     *string
	CreatedAt     string
	UpdatedAt     string
}

// AccountModel is the DAO for accounts.
type AccountModel struct {
	DB *sql.DB
}

// Insert creates the account and populates its generated ID and timestamps.
// A taken email yields ErrDuplicateEmail.
func (m AccountModel) Insert(ctx context.Context, a *Account) error {
	const query = `
		INSERT INTO accounts (email, password_hash, date_of_birth, biological_sex, blood_type)
		VALUES (?, ?, ?, ?, ?)
		RETURNING id, created_at, updated_at`

	args := []any{a.Email, a.PasswordHash, a.DateOfBirth, a.BiologicalSex, a.BloodType}
	err := m.DB.QueryRowContext(ctx, query, args...).Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		// email is the only UNIQUE column, so any unique-constraint violation is
		// a duplicate email. Matching the driver's error code is more robust than
		// substring-matching its message text.
		var sqErr *sqlite.Error
		if errors.As(err, &sqErr) && sqErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return ErrDuplicateEmail
		}
		return err
	}
	return nil
}

// GetByEmail returns the account with the given email, or ErrRecordNotFound.
func (m AccountModel) GetByEmail(ctx context.Context, email string) (*Account, error) {
	const query = `
		SELECT id, email, password_hash, date_of_birth, biological_sex, blood_type, created_at, updated_at
		FROM accounts
		WHERE email = ?`

	var a Account
	err := m.DB.QueryRowContext(ctx, query, email).Scan(
		&a.ID,
		&a.Email,
		&a.PasswordHash,
		&a.DateOfBirth,
		&a.BiologicalSex,
		&a.BloodType,
		&a.CreatedAt,
		&a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &a, nil
}
