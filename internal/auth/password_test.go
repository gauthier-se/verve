package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("hash = %q, want an argon2id PHC string", hash)
	}

	ok, err := VerifyPassword("correct horse battery staple", hash)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Error("VerifyPassword = false for the correct password, want true")
	}

	ok, err = VerifyPassword("wrong password", hash)
	if err != nil {
		t.Fatalf("VerifyPassword (wrong): %v", err)
	}
	if ok {
		t.Error("VerifyPassword = true for a wrong password, want false")
	}
}

// TestHashIsSalted checks that hashing the same password twice yields different
// encoded hashes (a fresh random salt each time), so identical passwords are not
// detectable by comparing stored hashes.
func TestHashIsSalted(t *testing.T) {
	a, err := HashPassword("hunter2")
	if err != nil {
		t.Fatalf("HashPassword a: %v", err)
	}
	b, err := HashPassword("hunter2")
	if err != nil {
		t.Fatalf("HashPassword b: %v", err)
	}
	if a == b {
		t.Error("two hashes of the same password are identical, want distinct salts")
	}
	// Both must still verify.
	for _, h := range []string{a, b} {
		ok, err := VerifyPassword("hunter2", h)
		if err != nil || !ok {
			t.Errorf("VerifyPassword(%q) = %v, %v; want true, nil", h, ok, err)
		}
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	tests := map[string]string{
		"empty":            "",
		"not phc":          "plaintext",
		"wrong algorithm":  "$argon2i$v=19$m=65536,t=3,p=2$c29tZXNhbHQ$c29tZWhhc2g",
		"too few sections": "$argon2id$v=19$m=65536,t=3,p=2",
		"bad base64 salt":  "$argon2id$v=19$m=65536,t=3,p=2$!!!$c29tZWhhc2g",
	}
	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			ok, err := VerifyPassword("whatever", encoded)
			if err == nil {
				t.Errorf("VerifyPassword(%q) err = nil, want an error", encoded)
			}
			if ok {
				t.Errorf("VerifyPassword(%q) = true, want false", encoded)
			}
		})
	}
}
