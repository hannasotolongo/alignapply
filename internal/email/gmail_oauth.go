package email

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const GmailReadonlyScope = "https://www.googleapis.com/auth/gmail.readonly"

func GmailOAuthConfig() (*oauth2.Config, error) {
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	redirectURL := os.Getenv("GOOGLE_REDIRECT_URL")

	if clientID == "" {
		return nil, fmt.Errorf("GOOGLE_CLIENT_ID is required")
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("GOOGLE_CLIENT_SECRET is required")
	}
	if redirectURL == "" {
		return nil, fmt.Errorf("GOOGLE_REDIRECT_URL is required")
	}

	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{GmailReadonlyScope},
		Endpoint:     google.Endpoint,
	}, nil
}

func GmailAuthorizationURL(state string) (string, error) {
	config, err := GmailOAuthConfig()
	if err != nil {
		return "", err
	}

	return config.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	), nil
}

type GmailToken struct {
	AccessToken  string
	RefreshToken string
	Expiry       *time.Time
}

func GmailHTTPClient(
	ctx context.Context,
	accessToken string,
	refreshToken string,
	expiry *time.Time,
) (*http.Client, func() GmailToken, error) {
	config, err := GmailOAuthConfig()
	if err != nil {
		return nil, nil, err
	}

	token := &oauth2.Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
	}

	if expiry != nil {
		token.Expiry = *expiry
	}

	source := config.TokenSource(ctx, token)
	client := oauth2.NewClient(ctx, source)

	current := func() GmailToken {
		refreshed, err := source.Token()
		if err != nil {
			return GmailToken{}
		}

		var tokenExpiry *time.Time
		if !refreshed.Expiry.IsZero() {
			value := refreshed.Expiry
			tokenExpiry = &value
		}

		return GmailToken{
			AccessToken:  refreshed.AccessToken,
			RefreshToken: refreshed.RefreshToken,
			Expiry:       tokenExpiry,
		}
	}

	return client, current, nil
}
