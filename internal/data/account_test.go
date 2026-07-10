package data

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

// testLogger returns a slog logger that discards output, for use in tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func ptr[T any](v T) *T { return &v }

func TestAccountInsertAndGetByEmail(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()

	acc := &Account{
		Email:         "alice@example.com",
		DateOfBirth:   ptr("1990-01-01"),
		BiologicalSex: ptr("female"),
	}
	if err := models.Accounts.Insert(ctx, acc); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if acc.ID == 0 {
		t.Error("Insert did not populate ID")
	}
	if acc.CreatedAt == "" {
		t.Error("Insert did not populate CreatedAt")
	}

	got, err := models.Accounts.GetByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if got.ID != acc.ID {
		t.Errorf("ID = %d, want %d", got.ID, acc.ID)
	}
	if got.Email != "alice@example.com" {
		t.Errorf("Email = %q, want alice@example.com", got.Email)
	}
	if got.PasswordHash != nil {
		t.Errorf("PasswordHash = %v, want nil", got.PasswordHash)
	}
	if got.DateOfBirth == nil || *got.DateOfBirth != "1990-01-01" {
		t.Errorf("DateOfBirth = %v, want 1990-01-01", got.DateOfBirth)
	}
}

func TestAccountInsertDuplicateEmail(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()

	first := &Account{Email: "dup@example.com"}
	if err := models.Accounts.Insert(ctx, first); err != nil {
		t.Fatalf("first Insert: %v", err)
	}

	second := &Account{Email: "dup@example.com"}
	err := models.Accounts.Insert(ctx, second)
	if !errors.Is(err, ErrDuplicateEmail) {
		t.Errorf("second Insert error = %v, want ErrDuplicateEmail", err)
	}
}

func TestAccountAny(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()

	any, err := models.Accounts.Any(ctx)
	if err != nil {
		t.Fatalf("Any on empty instance: %v", err)
	}
	if any {
		t.Error("Any = true on a fresh instance, want false")
	}

	if err := models.Accounts.Insert(ctx, &Account{Email: "first@example.com"}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	any, err = models.Accounts.Any(ctx)
	if err != nil {
		t.Fatalf("Any after insert: %v", err)
	}
	if !any {
		t.Error("Any = false after an account exists, want true")
	}
}

func TestAccountGetByEmailNotFound(t *testing.T) {
	_, models := openTestDB(t)

	_, err := models.Accounts.GetByEmail(context.Background(), "missing@example.com")
	if !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("GetByEmail error = %v, want ErrRecordNotFound", err)
	}
}
