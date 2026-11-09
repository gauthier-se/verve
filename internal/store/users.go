package store

import (
	"context"
	"strings"
	"time"
)

// FoldCase returns the form that the usernames and emails differing only by case share, in every
// alphabet: logins are unique and looked up by it (ADR 0013). Going through the upper case first
// also merges letters with two lower case forms, such as the final sigma. A change of it needs a
// migration of the stored forms.
func FoldCase(s string) string {
	return strings.ToLower(strings.ToUpper(s))
}

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
	// ErrConflict when the username or the email is taken, and ErrInvalid when the username is empty
	// or holds an @, or when the email has none.
	Create(ctx context.Context, user NewUser) (User, error)
	// Get returns the user of id, or ErrNotFound.
	Get(ctx context.Context, id int64) (User, error)
	// GetByLogin returns the user whose username or email is login, ignoring case, or ErrNotFound.
	GetByLogin(ctx context.Context, login string) (User, error)
	// SetPasswordHash replaces the password hash of the user of id, or returns ErrNotFound. An empty
	// hash leaves the user without a password.
	SetPasswordHash(ctx context.Context, id int64, hash string) error
	// RecordLogin sets the LastLoginAt of the user of id to the current time and returns the user,
	// or returns ErrNotFound.
	RecordLogin(ctx context.Context, id int64) (User, error)
	// Count returns the number of users, whatever their status.
	Count(ctx context.Context) (int64, error)
	// List returns the users newest first by CreatedAt then ID, whatever their status: at most
	// page.Limit of them, after page.After. It returns ErrInvalid when page.Limit is below 1.
	List(ctx context.Context, page PageRequest) ([]User, error)
	// SetRole sets the role of the user of id and returns the user, or returns ErrNotFound.
	SetRole(ctx context.Context, id int64, role UserRole) (User, error)
	// SetStatus sets the status of the user of id and returns the user, or returns ErrNotFound.
	SetStatus(ctx context.Context, id int64, status UserStatus) (User, error)
	// CountActiveAdmins returns the number of active users whose role is admin.
	CountActiveAdmins(ctx context.Context) (int64, error)
}

// Cursor returns the position of the user in the lists of UsersRepository.List.
func (u User) Cursor() Cursor {
	return Cursor{At: u.CreatedAt, ID: u.ID}
}
