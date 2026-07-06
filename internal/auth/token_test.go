package auth

import "testing"

func TestNewSessionTokenIsUniqueAndOpaque(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		tok, err := NewSessionToken()
		if err != nil {
			t.Fatalf("NewSessionToken: %v", err)
		}
		if tok == "" {
			t.Fatal("NewSessionToken returned an empty token")
		}
		if seen[tok] {
			t.Fatalf("duplicate token %q", tok)
		}
		seen[tok] = true
	}
}

func TestHashTokenIsStableAndDiffersFromToken(t *testing.T) {
	tok, err := NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	h1 := HashToken(tok)
	h2 := HashToken(tok)
	if h1 != h2 {
		t.Errorf("HashToken not stable: %q != %q", h1, h2)
	}
	if h1 == tok {
		t.Error("HashToken returned the token unchanged")
	}
	if HashToken("other") == h1 {
		t.Error("different tokens hashed to the same value")
	}
}
