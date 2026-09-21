package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultAppleTokenURL  = "https://appleid.apple.com/auth/token"
	defaultAppleRevokeURL = "https://appleid.apple.com/auth/revoke"

	defaultAppleClientHTTPTimeout = 10 * time.Second
	defaultAppleClientSecretTTL   = 5 * time.Minute

	maxAppleResponseBytes = 1 << 20
)

var (
	ErrInvalidAppleClientConfig = errors.New(
		"invalid Apple client configuration",
	)

	ErrAppleTokenExchange = errors.New(
		"Apple token exchange failed",
	)

	ErrAppleTokenRevocation = errors.New(
		"Apple token revocation failed",
	)
)

type AppleClientConfig struct {
	ClientID   string
	TeamID     string
	KeyID      string
	PrivateKey string
}

type AppleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
}

type appleErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type AppleClient struct {
	clientID string
	teamID   string
	keyID    string

	privateKey *ecdsa.PrivateKey

	tokenURL  string
	revokeURL string

	httpClient *http.Client

	clientSecretTTL time.Duration
}

func NewAppleClient(
	config AppleClientConfig,
) (*AppleClient, error) {
	clientID := strings.TrimSpace(
		config.ClientID,
	)

	teamID := strings.TrimSpace(
		config.TeamID,
	)

	keyID := strings.TrimSpace(
		config.KeyID,
	)

	privateKeyPEM := strings.TrimSpace(
		config.PrivateKey,
	)

	if clientID == "" ||
		teamID == "" ||
		keyID == "" ||
		privateKeyPEM == "" {

		return nil,
			ErrInvalidAppleClientConfig
	}

	privateKey, err :=
		parseApplePrivateKey(
			[]byte(privateKeyPEM),
		)

	if err != nil {
		return nil,
			fmt.Errorf(
				"%w: %v",
				ErrInvalidAppleClientConfig,
				err,
			)
	}

	return &AppleClient{
		clientID:   clientID,
		teamID:     teamID,
		keyID:      keyID,
		privateKey: privateKey,

		tokenURL:  defaultAppleTokenURL,
		revokeURL: defaultAppleRevokeURL,

		httpClient: &http.Client{
			Timeout: defaultAppleClientHTTPTimeout,
		},

		clientSecretTTL: defaultAppleClientSecretTTL,
	}, nil
}

func (c *AppleClient) ExchangeAuthorizationCode(
	ctx context.Context,
	authorizationCode string,
) (AppleTokenResponse, error) {
	if c == nil {
		return AppleTokenResponse{},
			ErrInvalidAppleClientConfig
	}

	authorizationCode =
		strings.TrimSpace(
			authorizationCode,
		)

	if authorizationCode == "" {
		return AppleTokenResponse{},
			fmt.Errorf(
				"%w: authorization code is required",
				ErrAppleTokenExchange,
			)
	}

	clientSecret, err :=
		c.createClientSecret()

	if err != nil {
		return AppleTokenResponse{},
			fmt.Errorf(
				"%w: %v",
				ErrAppleTokenExchange,
				err,
			)
	}

	form := url.Values{}

	form.Set(
		"client_id",
		c.clientID,
	)

	form.Set(
		"client_secret",
		clientSecret,
	)

	form.Set(
		"code",
		authorizationCode,
	)

	form.Set(
		"grant_type",
		"authorization_code",
	)

	request, err :=
		http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			c.tokenURL,
			strings.NewReader(
				form.Encode(),
			),
		)

	if err != nil {
		return AppleTokenResponse{},
			fmt.Errorf(
				"%w: create request: %v",
				ErrAppleTokenExchange,
				err,
			)
	}

	request.Header.Set(
		"Content-Type",
		"application/x-www-form-urlencoded",
	)

	response, err :=
		c.httpClient.Do(request)

	if err != nil {
		return AppleTokenResponse{},
			fmt.Errorf(
				"%w: request failed: %v",
				ErrAppleTokenExchange,
				err,
			)
	}
	defer response.Body.Close()

	body, err :=
		readLimitedAppleResponse(
			response.Body,
		)

	if err != nil {
		return AppleTokenResponse{},
			fmt.Errorf(
				"%w: read response: %v",
				ErrAppleTokenExchange,
				err,
			)
	}

	if response.StatusCode !=
		http.StatusOK {

		return AppleTokenResponse{},
			appleAPIError(
				ErrAppleTokenExchange,
				response.StatusCode,
				body,
			)
	}

	var tokenResponse AppleTokenResponse

	if err :=
		json.Unmarshal(
			body,
			&tokenResponse,
		); err != nil {

		return AppleTokenResponse{},
			fmt.Errorf(
				"%w: decode response: %v",
				ErrAppleTokenExchange,
				err,
			)
	}

	tokenResponse.AccessToken =
		strings.TrimSpace(
			tokenResponse.AccessToken,
		)

	tokenResponse.RefreshToken =
		strings.TrimSpace(
			tokenResponse.RefreshToken,
		)

	tokenResponse.IDToken =
		strings.TrimSpace(
			tokenResponse.IDToken,
		)

	tokenResponse.TokenType =
		strings.TrimSpace(
			tokenResponse.TokenType,
		)

	if tokenResponse.RefreshToken == "" {
		return AppleTokenResponse{},
			fmt.Errorf(
				"%w: Apple response did not contain a refresh token",
				ErrAppleTokenExchange,
			)
	}

	if tokenResponse.IDToken == "" {
		return AppleTokenResponse{},
			fmt.Errorf(
				"%w: Apple response did not contain an identity token",
				ErrAppleTokenExchange,
			)
	}

	return tokenResponse, nil
}

func (c *AppleClient) RevokeRefreshToken(
	ctx context.Context,
	refreshToken string,
) error {
	if c == nil {
		return ErrInvalidAppleClientConfig
	}

	refreshToken =
		strings.TrimSpace(
			refreshToken,
		)

	if refreshToken == "" {
		return fmt.Errorf(
			"%w: refresh token is required",
			ErrAppleTokenRevocation,
		)
	}

	clientSecret, err :=
		c.createClientSecret()

	if err != nil {
		return fmt.Errorf(
			"%w: %v",
			ErrAppleTokenRevocation,
			err,
		)
	}

	form := url.Values{}

	form.Set(
		"client_id",
		c.clientID,
	)

	form.Set(
		"client_secret",
		clientSecret,
	)

	form.Set(
		"token",
		refreshToken,
	)

	form.Set(
		"token_type_hint",
		"refresh_token",
	)

	request, err :=
		http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			c.revokeURL,
			strings.NewReader(
				form.Encode(),
			),
		)

	if err != nil {
		return fmt.Errorf(
			"%w: create request: %v",
			ErrAppleTokenRevocation,
			err,
		)
	}

	request.Header.Set(
		"Content-Type",
		"application/x-www-form-urlencoded",
	)

	response, err :=
		c.httpClient.Do(request)

	if err != nil {
		return fmt.Errorf(
			"%w: request failed: %v",
			ErrAppleTokenRevocation,
			err,
		)
	}
	defer response.Body.Close()

	body, err :=
		readLimitedAppleResponse(
			response.Body,
		)

	if err != nil {
		return fmt.Errorf(
			"%w: read response: %v",
			ErrAppleTokenRevocation,
			err,
		)
	}

	if response.StatusCode !=
		http.StatusOK {

		return appleAPIError(
			ErrAppleTokenRevocation,
			response.StatusCode,
			body,
		)
	}

	return nil
}

func (c *AppleClient) createClientSecret() (
	string,
	error,
) {
	if c == nil ||
		c.privateKey == nil {

		return "",
			ErrInvalidAppleClientConfig
	}

	now := time.Now().UTC()

	claims := jwt.RegisteredClaims{
		Issuer: c.teamID,

		Subject: c.clientID,

		Audience: jwt.ClaimStrings{
			defaultAppleIssuer,
		},

		IssuedAt: jwt.NewNumericDate(
			now,
		),

		ExpiresAt: jwt.NewNumericDate(
			now.Add(
				c.clientSecretTTL,
			),
		),
	}

	token :=
		jwt.NewWithClaims(
			jwt.SigningMethodES256,
			claims,
		)

	token.Header["kid"] =
		c.keyID

	clientSecret, err :=
		token.SignedString(
			c.privateKey,
		)

	if err != nil {
		return "",
			fmt.Errorf(
				"sign Apple client secret: %w",
				err,
			)
	}

	return clientSecret, nil
}

func parseApplePrivateKey(
	privateKeyPEM []byte,
) (*ecdsa.PrivateKey, error) {
	block, _ :=
		pem.Decode(
			privateKeyPEM,
		)

	if block == nil {
		return nil,
			errors.New(
				"Apple private key is not valid PEM",
			)
	}

	parsedKey, err :=
		x509.ParsePKCS8PrivateKey(
			block.Bytes,
		)

	if err != nil {
		return nil,
			fmt.Errorf(
				"parse Apple private key: %w",
				err,
			)
	}

	privateKey, ok :=
		parsedKey.(*ecdsa.PrivateKey)

	if !ok {
		return nil,
			errors.New(
				"Apple private key is not an EC private key",
			)
	}

	if privateKey.Curve == nil ||
		privateKey.Curve.Params() == nil ||
		privateKey.Curve.Params().Name != "P-256" {

		return nil,
			errors.New(
				"Apple private key must use the P-256 curve",
			)
	}

	return privateKey, nil
}

func readLimitedAppleResponse(
	reader io.Reader,
) ([]byte, error) {
	if reader == nil {
		return nil,
			errors.New(
				"response body is required",
			)
	}

	limited :=
		io.LimitReader(
			reader,
			maxAppleResponseBytes+1,
		)

	body, err :=
		io.ReadAll(limited)

	if err != nil {
		return nil, err
	}

	if len(body) >
		maxAppleResponseBytes {

		return nil,
			errors.New(
				"Apple response exceeded maximum size",
			)
	}

	return body, nil
}

func appleAPIError(
	baseError error,
	statusCode int,
	body []byte,
) error {
	var response appleErrorResponse

	if len(body) > 0 {
		_ = json.Unmarshal(
			body,
			&response,
		)
	}

	code :=
		strings.TrimSpace(
			response.Error,
		)

	description :=
		strings.TrimSpace(
			response.ErrorDescription,
		)

	switch {
	case code != "" &&
		description != "":

		return fmt.Errorf(
			"%w: status=%d code=%s description=%s",
			baseError,
			statusCode,
			code,
			description,
		)

	case code != "":
		return fmt.Errorf(
			"%w: status=%d code=%s",
			baseError,
			statusCode,
			code,
		)

	default:
		return fmt.Errorf(
			"%w: status=%d",
			baseError,
			statusCode,
		)
	}
}
