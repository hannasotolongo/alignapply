package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// Password hashes use PBKDF2-HMAC-SHA256 from the standard library so the
// backend gains no new dependencies. 600,000 iterations follows current
// OWASP guidance for PBKDF2-SHA256.
const (
	passwordHashScheme     = "pbkdf2_sha256"
	passwordHashIterations = 600_000
	passwordSaltBytes      = 16
	passwordKeyBytes       = 32

	MinPasswordLength = 10
	MaxPasswordLength = 256
)

var ErrPasswordMismatch = errors.New("auth: password does not match")

// ValidatePassword enforces the sign-up password policy.
func ValidatePassword(password string) error {
	length := utf8.RuneCountInString(password)

	if length < MinPasswordLength {
		return fmt.Errorf(
			"password must be at least %d characters",
			MinPasswordLength,
		)
	}

	if length > MaxPasswordLength {
		return fmt.Errorf(
			"password must not exceed %d characters",
			MaxPasswordLength,
		)
	}

	return nil
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}

	key, err := pbkdf2.Key(
		sha256.New,
		password,
		salt,
		passwordHashIterations,
		passwordKeyBytes,
	)
	if err != nil {
		return "", fmt.Errorf("auth: derive key: %w", err)
	}

	return strings.Join(
		[]string{
			passwordHashScheme,
			strconv.Itoa(passwordHashIterations),
			base64.RawStdEncoding.EncodeToString(salt),
			base64.RawStdEncoding.EncodeToString(key),
		},
		"$",
	), nil
}

// VerifyPassword compares a password against a stored hash in constant time.
func VerifyPassword(password string, encoded string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != passwordHashScheme {
		return errors.New("auth: unsupported password hash")
	}

	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 {
		return errors.New("auth: invalid password hash iterations")
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return errors.New("auth: invalid password hash salt")
	}

	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(expected) == 0 {
		return errors.New("auth: invalid password hash key")
	}

	actual, err := pbkdf2.Key(
		sha256.New,
		password,
		salt,
		iterations,
		len(expected),
	)
	if err != nil {
		return fmt.Errorf("auth: derive key: %w", err)
	}

	if subtle.ConstantTimeCompare(actual, expected) != 1 {
		return ErrPasswordMismatch
	}

	return nil
}

// DummyPasswordHash is verified against when a login email does not exist so
// that response timing does not reveal which emails have accounts.
var DummyPasswordHash = sync.OnceValue(func() string {
	hash, err := HashPassword("alignapply-timing-equalization")
	if err != nil {
		panic(err)
	}
	return hash
})
