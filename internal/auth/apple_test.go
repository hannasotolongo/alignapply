package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testAppleClientID = "com.hannasotolongo.AlignApply"
	testAppleIssuer   = "https://appleid.apple.com"
	testAppleKeyID    = "test-apple-key"
)

func TestAppleVerifierAcceptsValidIdentityToken(t *testing.T) {
	privateKey := generateTestRSAKey(t)
	server := newTestAppleJWKS(t, privateKey, testAppleKeyID)
	defer server.Close()

	verifier := newTestAppleVerifier(
		t,
		server.URL,
	)

	token := signTestAppleToken(
		t,
		privateKey,
		testAppleKeyID,
		testAppleIssuer,
		testAppleClientID,
		"apple-user-123",
		time.Now().Add(time.Hour),
	)

	identity, err := verifier.Verify(
		context.Background(),
		token,
	)
	if err != nil {
		t.Fatalf(
			"Verify() error = %v",
			err,
		)
	}

	if identity.Subject != "apple-user-123" {
		t.Fatalf(
			"Subject = %q, want %q",
			identity.Subject,
			"apple-user-123",
		)
	}

	if identity.Email != "hanna@example.com" {
		t.Fatalf(
			"Email = %q, want %q",
			identity.Email,
			"hanna@example.com",
		)
	}

	if !identity.EmailVerified {
		t.Fatal(
			"EmailVerified = false, want true",
		)
	}

	if identity.IsPrivateEmail {
		t.Fatal(
			"IsPrivateEmail = true, want false",
		)
	}
}

func TestAppleVerifierRejectsWrongAudience(t *testing.T) {
	privateKey := generateTestRSAKey(t)
	server := newTestAppleJWKS(t, privateKey, testAppleKeyID)
	defer server.Close()

	verifier := newTestAppleVerifier(
		t,
		server.URL,
	)

	token := signTestAppleToken(
		t,
		privateKey,
		testAppleKeyID,
		testAppleIssuer,
		"com.someoneelse.app",
		"apple-user-123",
		time.Now().Add(time.Hour),
	)

	if _, err := verifier.Verify(
		context.Background(),
		token,
	); err == nil {
		t.Fatal(
			"Verify() accepted token with wrong audience",
		)
	}
}

func TestAppleVerifierRejectsWrongIssuer(t *testing.T) {
	privateKey := generateTestRSAKey(t)
	server := newTestAppleJWKS(t, privateKey, testAppleKeyID)
	defer server.Close()

	verifier := newTestAppleVerifier(
		t,
		server.URL,
	)

	token := signTestAppleToken(
		t,
		privateKey,
		testAppleKeyID,
		"https://example.com",
		testAppleClientID,
		"apple-user-123",
		time.Now().Add(time.Hour),
	)

	if _, err := verifier.Verify(
		context.Background(),
		token,
	); err == nil {
		t.Fatal(
			"Verify() accepted token with wrong issuer",
		)
	}
}

func TestAppleVerifierRejectsExpiredToken(t *testing.T) {
	privateKey := generateTestRSAKey(t)
	server := newTestAppleJWKS(t, privateKey, testAppleKeyID)
	defer server.Close()

	verifier := newTestAppleVerifier(
		t,
		server.URL,
	)

	token := signTestAppleToken(
		t,
		privateKey,
		testAppleKeyID,
		testAppleIssuer,
		testAppleClientID,
		"apple-user-123",
		time.Now().Add(-time.Hour),
	)

	if _, err := verifier.Verify(
		context.Background(),
		token,
	); err == nil {
		t.Fatal(
			"Verify() accepted expired token",
		)
	}
}

func TestAppleVerifierRejectsUnknownSigningKey(t *testing.T) {
	privateKey := generateTestRSAKey(t)

	server := newTestAppleJWKS(
		t,
		privateKey,
		"different-key",
	)
	defer server.Close()

	verifier := newTestAppleVerifier(
		t,
		server.URL,
	)

	token := signTestAppleToken(
		t,
		privateKey,
		testAppleKeyID,
		testAppleIssuer,
		testAppleClientID,
		"apple-user-123",
		time.Now().Add(time.Hour),
	)

	if _, err := verifier.Verify(
		context.Background(),
		token,
	); err == nil {
		t.Fatal(
			"Verify() accepted token with unknown signing key",
		)
	}
}

func TestAppleVerifierRejectsMissingSubject(t *testing.T) {
	privateKey := generateTestRSAKey(t)
	server := newTestAppleJWKS(t, privateKey, testAppleKeyID)
	defer server.Close()

	verifier := newTestAppleVerifier(
		t,
		server.URL,
	)

	now := time.Now().UTC()

	claims := appleClaims{
		Email:          "hanna@example.com",
		EmailVerified:  true,
		IsPrivateEmail: false,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    testAppleIssuer,
			Audience:  jwt.ClaimStrings{testAppleClientID},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}

	token := jwt.NewWithClaims(
		jwt.SigningMethodRS256,
		claims,
	)

	token.Header["kid"] = testAppleKeyID

	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf(
			"SignedString() error = %v",
			err,
		)
	}

	if _, err := verifier.Verify(
		context.Background(),
		signedToken,
	); err == nil {
		t.Fatal(
			"Verify() accepted token without subject",
		)
	}
}

func newTestAppleVerifier(
	t *testing.T,
	keysURL string,
) *AppleVerifier {
	t.Helper()

	verifier, err :=
		NewAppleVerifier(testAppleClientID)
	if err != nil {
		t.Fatalf(
			"NewAppleVerifier() error = %v",
			err,
		)
	}

	verifier.keysURL = keysURL
	verifier.issuer = testAppleIssuer
	verifier.cacheTTL = time.Minute

	return verifier
}

func newTestAppleJWKS(
	t *testing.T,
	privateKey *rsa.PrivateKey,
	keyID string,
) *httptest.Server {
	t.Helper()

	publicKey := privateKey.PublicKey

	modulus :=
		base64.RawURLEncoding.EncodeToString(
			publicKey.N.Bytes(),
		)

	exponent :=
		base64.RawURLEncoding.EncodeToString(
			big.NewInt(
				int64(publicKey.E),
			).Bytes(),
		)

	response := appleJWKS{
		Keys: []appleJWK{
			{
				KTY: "RSA",
				KID: keyID,
				Use: "sig",
				Alg: "RS256",
				N:   modulus,
				E:   exponent,
			},
		},
	}

	return httptest.NewServer(
		http.HandlerFunc(
			func(
				w http.ResponseWriter,
				r *http.Request,
			) {
				if r.Method != http.MethodGet {
					http.Error(
						w,
						"method not allowed",
						http.StatusMethodNotAllowed,
					)
					return
				}

				w.Header().Set(
					"Content-Type",
					"application/json",
				)

				if err :=
					json.NewEncoder(w).Encode(response); err != nil {

					t.Errorf(
						"encode JWKS response: %v",
						err,
					)
				}
			},
		),
	)
}

func generateTestRSAKey(
	t *testing.T,
) *rsa.PrivateKey {
	t.Helper()

	privateKey, err :=
		rsa.GenerateKey(
			rand.Reader,
			2048,
		)

	if err != nil {
		t.Fatalf(
			"rsa.GenerateKey() error = %v",
			err,
		)
	}

	return privateKey
}

func signTestAppleToken(
	t *testing.T,
	privateKey *rsa.PrivateKey,
	keyID string,
	issuer string,
	audience string,
	subject string,
	expiresAt time.Time,
) string {
	t.Helper()

	now := time.Now().UTC()

	claims := appleClaims{
		Email:          "hanna@example.com",
		EmailVerified:  true,
		IsPrivateEmail: false,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(
		jwt.SigningMethodRS256,
		claims,
	)

	token.Header["kid"] = keyID

	signedToken, err :=
		token.SignedString(privateKey)

	if err != nil {
		t.Fatalf(
			"SignedString() error = %v",
			err,
		)
	}

	return signedToken
}
