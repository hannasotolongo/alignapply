package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testAppleTeamID  = "TESTTEAM123"
	testAppleAuthKey = "TESTKEY123"
)

func TestAppleClientExchangeAuthorizationCode(t *testing.T) {
	privateKeyPEM := generateAppleClientTestPrivateKey(t)

	var receivedForm url.Values

	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Fatalf(
						"method = %s, want POST",
						r.Method,
					)
				}

				if contentType := r.Header.Get("Content-Type"); contentType !=
					"application/x-www-form-urlencoded" {
					t.Fatalf(
						"Content-Type = %q",
						contentType,
					)
				}

				if err := r.ParseForm(); err != nil {
					t.Fatalf(
						"ParseForm() error = %v",
						err,
					)
				}

				receivedForm = r.PostForm

				w.Header().Set(
					"Content-Type",
					"application/json",
				)

				_ = json.NewEncoder(w).Encode(
					AppleTokenResponse{
						AccessToken:  "apple-access-token",
						TokenType:    "Bearer",
						ExpiresIn:    3600,
						RefreshToken: "apple-refresh-token",
						IDToken:      "apple-id-token",
					},
				)
			},
		),
	)
	defer server.Close()

	client := newAppleClientForTest(
		t,
		privateKeyPEM,
	)

	client.tokenURL = server.URL

	response, err :=
		client.ExchangeAuthorizationCode(
			context.Background(),
			"authorization-code-123",
		)

	if err != nil {
		t.Fatalf(
			"ExchangeAuthorizationCode() error = %v",
			err,
		)
	}

	if response.RefreshToken != "apple-refresh-token" {
		t.Fatalf(
			"RefreshToken = %q, want %q",
			response.RefreshToken,
			"apple-refresh-token",
		)
	}

	if response.IDToken != "apple-id-token" {
		t.Fatalf(
			"IDToken = %q, want %q",
			response.IDToken,
			"apple-id-token",
		)
	}

	if receivedForm.Get("client_id") != testAppleClientID {
		t.Fatalf(
			"client_id = %q, want %q",
			receivedForm.Get("client_id"),
			testAppleClientID,
		)
	}

	if receivedForm.Get("code") != "authorization-code-123" {
		t.Fatalf(
			"code = %q, want %q",
			receivedForm.Get("code"),
			"authorization-code-123",
		)
	}

	if receivedForm.Get("grant_type") != "authorization_code" {
		t.Fatalf(
			"grant_type = %q",
			receivedForm.Get("grant_type"),
		)
	}

	clientSecret := receivedForm.Get("client_secret")
	if clientSecret == "" {
		t.Fatal("client_secret was not sent")
	}

	verifyAppleClientSecret(
		t,
		clientSecret,
		client.privateKey.PublicKey,
	)
}

func TestAppleClientRevokeRefreshToken(t *testing.T) {
	privateKeyPEM := generateAppleClientTestPrivateKey(t)

	var receivedForm url.Values

	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Fatalf(
						"ParseForm() error = %v",
						err,
					)
				}

				receivedForm = r.PostForm

				w.WriteHeader(http.StatusOK)
			},
		),
	)
	defer server.Close()

	client := newAppleClientForTest(
		t,
		privateKeyPEM,
	)

	client.revokeURL = server.URL

	err := client.RevokeRefreshToken(
		context.Background(),
		"refresh-token-123",
	)

	if err != nil {
		t.Fatalf(
			"RevokeRefreshToken() error = %v",
			err,
		)
	}

	if receivedForm.Get("client_id") != testAppleClientID {
		t.Fatalf(
			"client_id = %q",
			receivedForm.Get("client_id"),
		)
	}

	if receivedForm.Get("token") != "refresh-token-123" {
		t.Fatalf(
			"token = %q",
			receivedForm.Get("token"),
		)
	}

	if receivedForm.Get("token_type_hint") != "refresh_token" {
		t.Fatalf(
			"token_type_hint = %q",
			receivedForm.Get("token_type_hint"),
		)
	}

	if receivedForm.Get("client_secret") == "" {
		t.Fatal("client_secret was not sent")
	}
}

func TestAppleClientExchangeRejectsAppleError(t *testing.T) {
	privateKeyPEM := generateAppleClientTestPrivateKey(t)

	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(
					"Content-Type",
					"application/json",
				)

				w.WriteHeader(http.StatusBadRequest)

				_, _ = io.WriteString(
					w,
					`{"error":"invalid_grant","error_description":"invalid authorization code"}`,
				)
			},
		),
	)
	defer server.Close()

	client := newAppleClientForTest(
		t,
		privateKeyPEM,
	)

	client.tokenURL = server.URL

	_, err := client.ExchangeAuthorizationCode(
		context.Background(),
		"invalid-code",
	)

	if err == nil {
		t.Fatal(
			"ExchangeAuthorizationCode() accepted Apple error response",
		)
	}

	if !strings.Contains(
		err.Error(),
		"invalid_grant",
	) {
		t.Fatalf(
			"error = %q, expected invalid_grant",
			err.Error(),
		)
	}
}

func TestAppleClientExchangeRequiresRefreshToken(t *testing.T) {
	privateKeyPEM := generateAppleClientTestPrivateKey(t)

	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(
					"Content-Type",
					"application/json",
				)

				_ = json.NewEncoder(w).Encode(
					AppleTokenResponse{
						AccessToken: "access-token",
						TokenType:   "Bearer",
						ExpiresIn:   3600,
						IDToken:     "id-token",
					},
				)
			},
		),
	)
	defer server.Close()

	client := newAppleClientForTest(
		t,
		privateKeyPEM,
	)

	client.tokenURL = server.URL

	if _, err :=
		client.ExchangeAuthorizationCode(
			context.Background(),
			"authorization-code",
		); err == nil {

		t.Fatal(
			"ExchangeAuthorizationCode() accepted response without refresh token",
		)
	}
}

func TestAppleClientRevokeRejectsAppleError(t *testing.T) {
	privateKeyPEM := generateAppleClientTestPrivateKey(t)

	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(
					"Content-Type",
					"application/json",
				)

				w.WriteHeader(http.StatusBadRequest)

				_, _ = io.WriteString(
					w,
					`{"error":"invalid_token"}`,
				)
			},
		),
	)
	defer server.Close()

	client := newAppleClientForTest(
		t,
		privateKeyPEM,
	)

	client.revokeURL = server.URL

	err := client.RevokeRefreshToken(
		context.Background(),
		"bad-refresh-token",
	)

	if err == nil {
		t.Fatal(
			"RevokeRefreshToken() accepted Apple error response",
		)
	}

	if !strings.Contains(
		err.Error(),
		"invalid_token",
	) {
		t.Fatalf(
			"error = %q, expected invalid_token",
			err.Error(),
		)
	}
}

func TestNewAppleClientRejectsInvalidPrivateKey(t *testing.T) {
	_, err := NewAppleClient(
		AppleClientConfig{
			ClientID:   testAppleClientID,
			TeamID:     testAppleTeamID,
			KeyID:      testAppleAuthKey,
			PrivateKey: "not-a-private-key",
		},
	)

	if err == nil {
		t.Fatal(
			"NewAppleClient() accepted invalid private key",
		)
	}
}

func newAppleClientForTest(
	t *testing.T,
	privateKeyPEM string,
) *AppleClient {
	t.Helper()

	client, err := NewAppleClient(
		AppleClientConfig{
			ClientID:   testAppleClientID,
			TeamID:     testAppleTeamID,
			KeyID:      testAppleAuthKey,
			PrivateKey: privateKeyPEM,
		},
	)

	if err != nil {
		t.Fatalf(
			"NewAppleClient() error = %v",
			err,
		)
	}

	return client
}

func generateAppleClientTestPrivateKey(
	t *testing.T,
) string {
	t.Helper()

	privateKey, err :=
		ecdsa.GenerateKey(
			elliptic.P256(),
			rand.Reader,
		)

	if err != nil {
		t.Fatalf(
			"ecdsa.GenerateKey() error = %v",
			err,
		)
	}

	encoded, err :=
		x509.MarshalPKCS8PrivateKey(
			privateKey,
		)

	if err != nil {
		t.Fatalf(
			"x509.MarshalPKCS8PrivateKey() error = %v",
			err,
		)
	}

	block := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: encoded,
	}

	return string(
		pem.EncodeToMemory(block),
	)
}

func verifyAppleClientSecret(
	t *testing.T,
	clientSecret string,
	publicKey ecdsa.PublicKey,
) {
	t.Helper()

	claims := &jwt.RegisteredClaims{}

	token, err :=
		jwt.ParseWithClaims(
			clientSecret,
			claims,
			func(token *jwt.Token) (any, error) {
				if token.Method.Alg() !=
					jwt.SigningMethodES256.Alg() {

					t.Fatalf(
						"signing method = %q, want ES256",
						token.Method.Alg(),
					)
				}

				return &publicKey, nil
			},
			jwt.WithValidMethods(
				[]string{
					jwt.SigningMethodES256.Alg(),
				},
			),
			jwt.WithIssuer(
				testAppleTeamID,
			),
			jwt.WithAudience(
				defaultAppleIssuer,
			),
		)

	if err != nil {
		t.Fatalf(
			"client secret verification error = %v",
			err,
		)
	}

	if !token.Valid {
		t.Fatal("client secret is invalid")
	}

	if claims.Subject != testAppleClientID {
		t.Fatalf(
			"client secret subject = %q, want %q",
			claims.Subject,
			testAppleClientID,
		)
	}

	if token.Header["kid"] != testAppleAuthKey {
		t.Fatalf(
			"client secret kid = %v, want %q",
			token.Header["kid"],
			testAppleAuthKey,
		)
	}
}
