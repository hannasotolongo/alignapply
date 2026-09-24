package email

import (
	"os"
	"testing"
)

func TestOAuthStateRoundTrip(t *testing.T) {
	t.Setenv("SESSION_SECRET", "test-session-secret")

	state, err := NewOAuthState("user-123")
	if err != nil {
		t.Fatal(err)
	}

	userID, err := ParseOAuthState(state)
	if err != nil {
		t.Fatal(err)
	}

	if userID != "user-123" {
		t.Fatalf("expected user-123, got %q", userID)
	}
}

func TestOAuthStateRejectsTampering(t *testing.T) {
	t.Setenv("SESSION_SECRET", "test-session-secret")

	state, err := NewOAuthState("user-123")
	if err != nil {
		t.Fatal(err)
	}

	state += "x"

	if _, err := ParseOAuthState(state); err == nil {
		t.Fatal("expected tampered state to fail")
	}
}

func TestOAuthStateRequiresSecret(t *testing.T) {
	old := os.Getenv("SESSION_SECRET")
	_ = os.Unsetenv("SESSION_SECRET")
	t.Cleanup(func() {
		_ = os.Setenv("SESSION_SECRET", old)
	})

	if _, err := NewOAuthState("user-123"); err == nil {
		t.Fatal("expected missing secret to fail")
	}
}
