package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/store"
)

// ErrLastAdmin is returned for a change that would leave no active admin. Its message is the stable
// code that the API answers with.
var ErrLastAdmin = errors.New("last_admin")

// ErrUserDeleting is returned for a change of an account being deleted, which would cancel the
// deletion. Its message is the stable code that the API answers with.
var ErrUserDeleting = errors.New("user_deleting")

// StatusChange is an admin, or the system, enabling or disabling an account.
type StatusChange struct {
	// ActorID is the admin who acts, or 0 for the system, such as the command line.
	ActorID int64
	UserID  int64
	// Status is store.UserActive or store.UserDisabled.
	Status store.UserStatus
}

// RoleChange is an admin, or the system, changing the role of an account.
type RoleChange struct {
	// ActorID is the admin who acts, or 0 for the system.
	ActorID int64
	UserID  int64
	Role    store.UserRole
}

// PasswordReset is an admin, or the system, replacing the password of an account.
type PasswordReset struct {
	// ActorID is the admin who acts, or 0 for the system.
	ActorID  int64
	UserID   int64
	Password string
}

// Admin holds the rules of the administration of accounts, for the API and the command line (FR-5
// and FR-8 of accounts and administration): a change of status, role or password revokes the
// sessions of the user, and the last active admin can be neither demoted nor disabled.
type Admin struct {
	accounts Accounts
	sessions Sessions
}

// NewAdmin returns the admin service, which checks and hashes passwords with accounts and revokes
// sessions with sessions.
func NewAdmin(accounts Accounts, sessions Sessions) Admin {
	return Admin{accounts: accounts, sessions: sessions}
}

// SetStatus enables or disables the user of change through st, and returns the user. In one
// transaction, it stores the status, revokes every session of a disabled user and records the
// audit event, with the client of ctx. A user who already has the status is returned unchanged,
// without an event.
//
// It returns ErrLastAdmin for the last active admin, checked in the transaction so that of
// concurrent changes, one keeps an admin, ErrUserDeleting for an account being deleted,
// store.ErrNotFound for an unknown user, and store.ErrInvalid for another status.
func (a Admin) SetStatus(ctx context.Context, st store.Store, change StatusChange) (store.User, error) {
	if change.Status != store.UserActive && change.Status != store.UserDisabled {
		return store.User{}, fmt.Errorf("set status %q: %w", change.Status, store.ErrInvalid)
	}

	var changed store.User

	err := st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		user, err := changeableUser(ctx, tx.Users(), change.UserID)
		if err != nil {
			return err
		}

		if user.Status == change.Status {
			changed = user

			return nil
		}

		event := audit.UserEnabled(change.ActorID, user.ID)

		if change.Status == store.UserDisabled {
			if err := keepAnActiveAdmin(ctx, tx.Users(), user); err != nil {
				return err
			}

			if _, err := a.sessions.RevokeAll(ctx, tx, user.ID); err != nil {
				return err
			}

			event = audit.UserDisabled(change.ActorID, user.ID)
		}

		changed, err = tx.Users().SetStatus(ctx, user.ID, change.Status)
		if err != nil {
			return err
		}

		return audit.Record(ctx, tx.AuditEvents(), event)
	})

	return changed, err
}

// SetRole changes the role of the user of change through st, and returns the user. In one
// transaction, it stores the role, revokes every session of the user, so that they log in again
// with a new one (FR-9 of local accounts and sessions), and records the audit event, with the
// client of ctx. A user who already has the role is returned unchanged, without an event.
//
// It returns ErrLastAdmin for the demotion of the last active admin, checked in the transaction,
// ErrUserDeleting for an account being deleted, store.ErrNotFound for an unknown user, and
// store.ErrInvalid for an unknown role.
func (a Admin) SetRole(ctx context.Context, st store.Store, change RoleChange) (store.User, error) {
	if change.Role != store.RoleAdmin && change.Role != store.RoleMember {
		return store.User{}, fmt.Errorf("set role %q: %w", change.Role, store.ErrInvalid)
	}

	var changed store.User

	err := st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		user, err := changeableUser(ctx, tx.Users(), change.UserID)
		if err != nil {
			return err
		}

		if user.Role == change.Role {
			changed = user

			return nil
		}

		if err := keepAnActiveAdmin(ctx, tx.Users(), user); err != nil {
			return err
		}

		if _, err := a.sessions.RevokeAll(ctx, tx, user.ID); err != nil {
			return err
		}

		changed, err = tx.Users().SetRole(ctx, user.ID, change.Role)
		if err != nil {
			return err
		}

		return audit.Record(ctx, tx.AuditEvents(), audit.UserRoleChanged(change.ActorID, user.ID, user.Role, change.Role))
	})

	return changed, err
}

// ResetPassword replaces the password of the user of reset through st. It checks the password
// against the policy and hashes it outside any transaction, then, in one, stores the hash, revokes
// every session of the user and records the audit event. It returns the error of the password
// policy, and store.ErrNotFound for an unknown user.
func (a Admin) ResetPassword(ctx context.Context, st store.Store, reset PasswordReset) error {
	hash, err := a.accounts.checkAndHashPassword(reset.Password)
	if err != nil {
		return err
	}

	return st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		if err := tx.Users().SetPasswordHash(ctx, reset.UserID, hash); err != nil {
			return err
		}

		if _, err := a.sessions.RevokeAll(ctx, tx, reset.UserID); err != nil {
			return err
		}

		return audit.Record(ctx, tx.AuditEvents(), audit.UserPasswordReset(reset.ActorID, reset.UserID))
	})
}

// keepAnActiveAdmin returns ErrLastAdmin when user is the only active admin of users, which a
// demotion or a disabling would leave without one.
func keepAnActiveAdmin(ctx context.Context, users store.UsersRepository, user store.User) error {
	if user.Role != store.RoleAdmin || user.Status != store.UserActive {
		return nil
	}

	count, err := users.CountActiveAdmins(ctx)
	if err != nil {
		return err
	}

	if count <= 1 {
		return ErrLastAdmin
	}

	return nil
}

// changeableUser returns the user of id read in users, ErrUserDeleting for an account being
// deleted, whose status and role no longer change, or store.ErrNotFound.
func changeableUser(ctx context.Context, users store.UsersRepository, id int64) (store.User, error) {
	user, err := users.Get(ctx, id)
	if err != nil {
		return store.User{}, err
	}

	if user.Status == store.UserDeleting {
		return store.User{}, ErrUserDeleting
	}

	return user, nil
}
