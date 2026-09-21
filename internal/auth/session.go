package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultSessionIssuer   = "alignapply"
	defaultSessionAudience = "alignapply-ios"
	defaultSessionTTL      = 24 * time.Hour
)

var (
	ErrInvalidSessionToken = errors.New("invalid session token")
	ErrExpiredSessionToken = errors.New("expired session token")
)

type SessionManager struct {
	secret   []byte
	issuer   string
	audience string
	ttl      time.Duration
}

type SessionClaims struct {
	UserID string `json:"user_id"`

	jwt.RegisteredClaims
}

func NewSessionManager(
	secret string,
) (*SessionManager, error) {
	secret = strings.TrimSpace(secret)

	if secret == "" {
		return nil, errors.New(
			"auth: session secret is required",
		)
	}

	if len(secret) < 32 {
		return nil, errors.New(
			"auth: session secret must be at least 32 characters",
		)
	}

	return &SessionManager{
		secret:   []byte(secret),
		issuer:   defaultSessionIssuer,
		audience: defaultSessionAudience,
		ttl:      defaultSessionTTL,
	}, nil
}

func (m *SessionManager) Create(
	userID string,
) (string, error) {
	if m == nil {
		return "", errors.New(
			"auth: session manager is not initialized",
		)
	}

	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", errors.New(
			"auth: user ID is required",
		)
	}

	now := time.Now().UTC()

	claims := SessionClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:  m.issuer,
			Subject: userID,
			Audience: jwt.ClaimStrings{
				m.audience,
			},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(
				now.Add(m.ttl),
			),
		},
	}

	token := jwt.NewWithClaims(
		jwt.SigningMethodHS256,
		claims,
	)

	signedToken, err := token.SignedString(
		m.secret,
	)
	if err != nil {
		return "", fmt.Errorf(
			"auth: sign session token: %w",
			err,
		)
	}

	return signedToken, nil
}

func (m *SessionManager) Verify(
	tokenString string,
) (SessionClaims, error) {
	if m == nil {
		return SessionClaims{}, errors.New(
			"auth: session manager is not initialized",
		)
	}

	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return SessionClaims{}, ErrInvalidSessionToken
	}

	claims := &SessionClaims{}

	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{
			jwt.SigningMethodHS256.Alg(),
		}),
		jwt.WithIssuer(m.issuer),
		jwt.WithAudience(m.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)

	token, err := parser.ParseWithClaims(
		tokenString,
		claims,
		func(token *jwt.Token) (any, error) {
			return m.secret, nil
		},
	)

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return SessionClaims{}, ErrExpiredSessionToken
		}

		return SessionClaims{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidSessionToken,
			err,
		)
	}

	if token == nil || !token.Valid {
		return SessionClaims{}, ErrInvalidSessionToken
	}

	userID := strings.TrimSpace(claims.UserID)
	subject := strings.TrimSpace(claims.Subject)

	if userID == "" || subject == "" {
		return SessionClaims{}, fmt.Errorf(
			"%w: missing user identity",
			ErrInvalidSessionToken,
		)
	}

	if userID != subject {
		return SessionClaims{}, fmt.Errorf(
			"%w: inconsistent user identity",
			ErrInvalidSessionToken,
		)
	}

	return *claims, nil
}

func BearerToken(
	authorizationHeader string,
) (string, error) {
	authorizationHeader = strings.TrimSpace(
		authorizationHeader,
	)

	if authorizationHeader == "" {
		return "", ErrInvalidSessionToken
	}

	parts := strings.Fields(
		authorizationHeader,
	)

	if len(parts) != 2 ||
		!strings.EqualFold(parts[0], "Bearer") ||
		strings.TrimSpace(parts[1]) == "" {

		return "", ErrInvalidSessionToken
	}

	return strings.TrimSpace(parts[1]), nil
}
