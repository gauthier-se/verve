package store

import (
	"context"
	"time"
)

// UserRole is the role of a user.
type UserRole string

// The roles of a user.
const (
	RoleAdmin  UserRole = "admin"
	RoleMember UserRole = "member"
)

// UserStatus is the state of an account.
type UserStatus string

// The statuses of an account.
const (
	UserActive   UserStatus = "active"
	UserDisabled UserStatus = "disabled"
	UserDeleting UserStatus = "deleting"
)

// NewUser describes a user to create. auth.ValidateUsername holds the rules of a username.
type NewUser struct {
	Username string
	// Email is empty for none.
	Email string
	// PasswordHash is empty for a user who logs in through an external identity.
	PasswordHash string
	Role         UserRole
}

// User is an account. Times are in UTC with millisecond precision.
type User struct {
	ID int64
	// Username and Email keep the case they were created with.
	Username string
	// Email is empty for none.
	Email string
	// PasswordHash is empty for a user who logs in through an external identity.
	PasswordHash string
	Role         UserRole
	Status       UserStatus
	CreatedAt    time.Time
	// LastLoginAt is the time of the last login, or zero when there was none.
	LastLoginAt time.Time
}

// UsersRepository stores the accounts. Usernames and emails are unique ignoring case, in every
// alphabet. A username never holds an @ and an email always does, so that a login cannot match two
// users.
type UsersRepository interface {
	// Create stores an active user with the current time as CreatedAt, and returns it. It returns
	// ErrConflict when the username or the email is taken, and ErrInvalid when the username holds an
	// @ or the email has none.
	Create(ctx context.Context, user NewUser) (User, error)
	// Get returns the user of id, or ErrNotFound.
	Get(ctx context.Context, id int64) (User, error)
	// GetByLogin returns the user whose username or email is login, ignoring case, or ErrNotFound.
	GetByLogin(ctx context.Context, login string) (User, error)
	// Count returns the number of users, whatever their status.
	Count(ctx context.Context) (int64, error)
}
