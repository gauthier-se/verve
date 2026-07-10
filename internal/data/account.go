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
	return insertAccount(ctx, m.DB, a)
}

// insertAccount inserts an account through any querier.
func insertAccount(ctx context.Context, q querier, a *Account) error {
	const query = `
		INSERT INTO accounts (email, password_hash, date_of_birth, biological_sex, blood_type)
		VALUES (?, ?, ?, ?, ?)
		RETURNING id, created_at, updated_at`

	args := []any{a.Email, a.PasswordHash, a.DateOfBirth, a.BiologicalSex, a.BloodType}
	err := q.QueryRowContext(ctx, query, args...).Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
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
	return m.getOne(ctx, query, email)
}

// Any reports whether the instance has at least one Account. It backs the
// first-run bootstrap gate (ADR 0017): web signup is open only while this is
// false, and the create endpoint re-checks it server-side before writing.
func (m AccountModel) Any(ctx context.Context) (bool, error) {
	const query = `SELECT EXISTS(SELECT 1 FROM accounts)`
	var exists bool
	if err := m.DB.QueryRowContext(ctx, query).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// GetByID returns the account with the given id, or ErrRecordNotFound. Used to
// resolve the authenticated Account from a session's account_id.
func (m AccountModel) GetByID(ctx context.Context, id int64) (*Account, error) {
	const query = `
		SELECT id, email, password_hash, date_of_birth, biological_sex, blood_type, created_at, updated_at
		FROM accounts
		WHERE id = ?`
	return m.getOne(ctx, query, id)
}

// SetPassword stores a new password hash for the account, bumping updated_at. It
// returns ErrRecordNotFound if no account has that id. The caller hashes the
// password (argon2id) before calling; the model never sees a plaintext password.
func (m AccountModel) SetPassword(ctx context.Context, id int64, passwordHash string) error {
	const query = `
		UPDATE accounts
		SET password_hash = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?`
	res, err := m.DB.ExecContext(ctx, query, passwordHash, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// getOne runs a single-row account SELECT and scans it, mapping no-rows to
// ErrRecordNotFound. Shared by GetByEmail and GetByID.
func (m AccountModel) getOne(ctx context.Context, query string, arg any) (*Account, error) {
	var a Account
	err := m.DB.QueryRowContext(ctx, query, arg).Scan(
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
