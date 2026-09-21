package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultAppleIssuer  = "https://appleid.apple.com"
	defaultAppleKeysURL = "https://appleid.apple.com/auth/keys"

	defaultAppleHTTPTimeout = 10 * time.Second
	defaultAppleKeyCacheTTL = 6 * time.Hour
)

var (
	ErrInvalidAppleToken = errors.New("invalid Apple identity token")
	ErrAppleKeyNotFound  = errors.New("Apple signing key not found")
)

type AppleIdentity struct {
	Subject        string
	Email          string
	EmailVerified  bool
	IsPrivateEmail bool
}

type AppleVerifier struct {
	clientID string
	issuer   string
	keysURL  string

	httpClient *http.Client
	cacheTTL   time.Duration

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	keysUntil time.Time
}

type appleClaims struct {
	Email          string `json:"email,omitempty"`
	EmailVerified  any    `json:"email_verified,omitempty"`
	IsPrivateEmail any    `json:"is_private_email,omitempty"`

	jwt.RegisteredClaims
}

type appleJWKS struct {
	Keys []appleJWK `json:"keys"`
}

type appleJWK struct {
	KTY string `json:"kty"`
	KID string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func NewAppleVerifier(clientID string) (*AppleVerifier, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return nil, errors.New("auth: Apple client ID is required")
	}

	return &AppleVerifier{
		clientID: clientID,
		issuer:   defaultAppleIssuer,
		keysURL:  defaultAppleKeysURL,
		httpClient: &http.Client{
			Timeout: defaultAppleHTTPTimeout,
		},
		cacheTTL: defaultAppleKeyCacheTTL,
		keys:     make(map[string]*rsa.PublicKey),
	}, nil
}

func (v *AppleVerifier) Verify(
	ctx context.Context,
	identityToken string,
) (AppleIdentity, error) {
	if v == nil {
		return AppleIdentity{}, errors.New(
			"auth: Apple verifier is not initialized",
		)
	}

	if ctx == nil {
		return AppleIdentity{}, errors.New(
			"auth: context is required",
		)
	}

	identityToken = strings.TrimSpace(identityToken)
	if identityToken == "" {
		return AppleIdentity{}, fmt.Errorf(
			"%w: token is empty",
			ErrInvalidAppleToken,
		)
	}

	claims := &appleClaims{}

	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{
			jwt.SigningMethodRS256.Alg(),
		}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.clientID),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)

	token, err := parser.ParseWithClaims(
		identityToken,
		claims,
		func(token *jwt.Token) (any, error) {
			kidValue, ok := token.Header["kid"]
			if !ok {
				return nil, fmt.Errorf(
					"%w: missing kid",
					ErrInvalidAppleToken,
				)
			}

			kid, ok := kidValue.(string)
			if !ok || strings.TrimSpace(kid) == "" {
				return nil, fmt.Errorf(
					"%w: invalid kid",
					ErrInvalidAppleToken,
				)
			}

			return v.signingKey(
				ctx,
				strings.TrimSpace(kid),
			)
		},
	)
	if err != nil {
		return AppleIdentity{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidAppleToken,
			err,
		)
	}

	if token == nil || !token.Valid {
		return AppleIdentity{}, ErrInvalidAppleToken
	}

	subject := strings.TrimSpace(claims.Subject)
	if subject == "" {
		return AppleIdentity{}, fmt.Errorf(
			"%w: missing subject",
			ErrInvalidAppleToken,
		)
	}

	return AppleIdentity{
		Subject:        subject,
		Email:          strings.TrimSpace(claims.Email),
		EmailVerified:  claimBool(claims.EmailVerified),
		IsPrivateEmail: claimBool(claims.IsPrivateEmail),
	}, nil
}

func (v *AppleVerifier) signingKey(
	ctx context.Context,
	kid string,
) (*rsa.PublicKey, error) {
	if key := v.cachedKey(kid); key != nil {
		return key, nil
	}

	if err := v.refreshKeys(ctx); err != nil {
		return nil, err
	}

	if key := v.cachedKey(kid); key != nil {
		return key, nil
	}

	return nil, fmt.Errorf(
		"%w: %s",
		ErrAppleKeyNotFound,
		kid,
	)
}

func (v *AppleVerifier) cachedKey(
	kid string,
) *rsa.PublicKey {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if time.Now().After(v.keysUntil) {
		return nil
	}

	return v.keys[kid]
}

func (v *AppleVerifier) refreshKeys(
	ctx context.Context,
) error {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		v.keysURL,
		nil,
	)
	if err != nil {
		return fmt.Errorf(
			"auth: create Apple key request: %w",
			err,
		)
	}

	response, err := v.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf(
			"auth: fetch Apple signing keys: %w",
			err,
		)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"auth: Apple signing keys returned HTTP %d",
			response.StatusCode,
		)
	}

	var jwks appleJWKS

	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&jwks); err != nil {
		return fmt.Errorf(
			"auth: decode Apple signing keys: %w",
			err,
		)
	}

	if len(jwks.Keys) == 0 {
		return errors.New(
			"auth: Apple returned no signing keys",
		)
	}

	keys := make(map[string]*rsa.PublicKey)

	for _, jwk := range jwks.Keys {
		if jwk.KTY != "RSA" {
			continue
		}

		if jwk.KID == "" ||
			jwk.N == "" ||
			jwk.E == "" {
			continue
		}

		if jwk.Alg != "" && jwk.Alg != "RS256" {
			continue
		}

		key, err := rsaPublicKey(jwk.N, jwk.E)
		if err != nil {
			continue
		}

		keys[jwk.KID] = key
	}

	if len(keys) == 0 {
		return errors.New(
			"auth: Apple returned no usable RSA signing keys",
		)
	}

	v.mu.Lock()
	v.keys = keys
	v.keysUntil = time.Now().Add(v.cacheTTL)
	v.mu.Unlock()

	return nil
}

func rsaPublicKey(
	modulusEncoded string,
	exponentEncoded string,
) (*rsa.PublicKey, error) {
	modulusBytes, err := base64.RawURLEncoding.DecodeString(
		modulusEncoded,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"decode RSA modulus: %w",
			err,
		)
	}

	exponentBytes, err := base64.RawURLEncoding.DecodeString(
		exponentEncoded,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"decode RSA exponent: %w",
			err,
		)
	}

	if len(modulusBytes) == 0 ||
		len(exponentBytes) == 0 {
		return nil, errors.New(
			"RSA key contains empty modulus or exponent",
		)
	}

	modulus := new(big.Int).SetBytes(modulusBytes)

	exponent := 0
	for _, value := range exponentBytes {
		exponent = exponent<<8 + int(value)
	}

	if exponent <= 0 {
		return nil, errors.New(
			"RSA key contains invalid exponent",
		)
	}

	return &rsa.PublicKey{
		N: modulus,
		E: exponent,
	}, nil
}

func claimBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed

	case string:
		return strings.EqualFold(
			strings.TrimSpace(typed),
			"true",
		)

	default:
		return false
	}
}
