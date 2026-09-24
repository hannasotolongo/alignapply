package email

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type oauthStatePayload struct {
	UserID string `json:"uid"`
	Nonce  string `json:"nonce"`
	Expiry int64  `json:"exp"`
}

func NewOAuthState(userID string) (string, error) {
	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		return "", fmt.Errorf("SESSION_SECRET is required")
	}

	nonceBytes := make([]byte, 24)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", fmt.Errorf("generate OAuth nonce: %w", err)
	}

	payload := oauthStatePayload{
		UserID: userID,
		Nonce:  base64.RawURLEncoding.EncodeToString(nonceBytes),
		Expiry: time.Now().Add(10 * time.Minute).Unix(),
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode OAuth state: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(raw)

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return encoded + "." + signature, nil
}

func ParseOAuthState(state string) (string, error) {
	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		return "", fmt.Errorf("SESSION_SECRET is required")
	}

	parts := strings.Split(state, ".")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid OAuth state")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))

	expected := mac.Sum(nil)

	actual, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(expected, actual) {
		return "", fmt.Errorf("invalid OAuth state signature")
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("invalid OAuth state payload")
	}

	var payload oauthStatePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("invalid OAuth state payload")
	}

	if payload.UserID == "" || payload.Nonce == "" {
		return "", fmt.Errorf("invalid OAuth state payload")
	}

	if time.Now().Unix() > payload.Expiry {
		return "", fmt.Errorf("OAuth state expired")
	}

	return payload.UserID, nil
}
