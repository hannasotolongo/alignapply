package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if !strings.HasPrefix(hash, "pbkdf2_sha256$600000$") {
		t.Fatalf("unexpected hash format %q", hash)
	}

	if err := VerifyPassword("correct horse battery", hash); err != nil {
		t.Fatalf("VerifyPassword(correct) error = %v", err)
	}

	if err := VerifyPassword("wrong password!", hash); !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("VerifyPassword(wrong) error = %v, want ErrPasswordMismatch", err)
	}
}

func TestHashPasswordUsesUniqueSalt(t *testing.T) {
	first, _ := HashPassword("same password here")
	second, _ := HashPassword("same password here")
	if first == second {
		t.Fatal("two hashes of the same password must differ")
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("short"); err == nil {
		t.Fatal("short password must be rejected")
	}
	if err := ValidatePassword("long enough pass"); err != nil {
		t.Fatalf("valid password rejected: %v", err)
	}
}
