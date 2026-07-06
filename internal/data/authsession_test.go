package data

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAuthSessionInsertAndLookup(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	accID := seedAccount(t, models)

	s := AuthSession{TokenHash: "hash-1", AccountID: accID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := models.AuthSessions.Insert(ctx, s); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := models.AuthSessions.AccountIDByToken(ctx, "hash-1")
	if err != nil {
		t.Fatalf("AccountIDByToken: %v", err)
	}
	if got != accID {
		t.Errorf("account id = %d, want %d", got, accID)
	}
}

func TestAuthSessionUnknownToken(t *testing.T) {
	_, models := openTestDB(t)
	_, err := models.AuthSessions.AccountIDByToken(context.Background(), "nope")
	if !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("AccountIDByToken error = %v, want ErrRecordNotFound", err)
	}
}

func TestAuthSessionExpiredIsNotReturned(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	accID := seedAccount(t, models)

	expired := AuthSession{TokenHash: "old", AccountID: accID, ExpiresAt: time.Now().Add(-time.Minute)}
	if err := models.AuthSessions.Insert(ctx, expired); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if _, err := models.AuthSessions.AccountIDByToken(ctx, "old"); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("expired session lookup = %v, want ErrRecordNotFound", err)
	}
}

func TestAuthSessionDelete(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	accID := seedAccount(t, models)

	s := AuthSession{TokenHash: "live", AccountID: accID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := models.AuthSessions.Insert(ctx, s); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := models.AuthSessions.Delete(ctx, "live"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := models.AuthSessions.AccountIDByToken(ctx, "live"); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("after Delete lookup = %v, want ErrRecordNotFound", err)
	}
	// Deleting again is not an error (idempotent logout).
	if err := models.AuthSessions.Delete(ctx, "live"); err != nil {
		t.Errorf("second Delete: %v", err)
	}
}

func TestAuthSessionDeleteExpired(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	accID := seedAccount(t, models)

	sessions := []AuthSession{
		{TokenHash: "a", AccountID: accID, ExpiresAt: time.Now().Add(-time.Hour)},
		{TokenHash: "b", AccountID: accID, ExpiresAt: time.Now().Add(-time.Minute)},
		{TokenHash: "c", AccountID: accID, ExpiresAt: time.Now().Add(time.Hour)},
	}
	for _, s := range sessions {
		if err := models.AuthSessions.Insert(ctx, s); err != nil {
			t.Fatalf("Insert %s: %v", s.TokenHash, err)
		}
	}

	n, err := models.AuthSessions.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n != 2 {
		t.Errorf("DeleteExpired removed %d, want 2", n)
	}
	if _, err := models.AuthSessions.AccountIDByToken(ctx, "c"); err != nil {
		t.Errorf("live session gone after DeleteExpired: %v", err)
	}
}

func TestAuthSessionCascadesOnAccountDelete(t *testing.T) {
	db, models := openTestDB(t)
	ctx := context.Background()
	accID := seedAccount(t, models)

	s := AuthSession{TokenHash: "cascade", AccountID: accID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := models.AuthSessions.Insert(ctx, s); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, accID); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_sessions WHERE account_id = ?`, accID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("sessions after account delete = %d, want 0 (cascade)", count)
	}
}

func TestAccountSetPasswordAndGetByID(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	accID := seedAccount(t, models)

	if err := models.Accounts.SetPassword(ctx, accID, "argon2-hash"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	got, err := models.Accounts.GetByID(ctx, accID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.PasswordHash == nil || *got.PasswordHash != "argon2-hash" {
		t.Errorf("PasswordHash = %v, want argon2-hash", got.PasswordHash)
	}

	if err := models.Accounts.SetPassword(ctx, 9999, "x"); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("SetPassword unknown id = %v, want ErrRecordNotFound", err)
	}
	if _, err := models.Accounts.GetByID(ctx, 9999); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("GetByID unknown id = %v, want ErrRecordNotFound", err)
	}
}
