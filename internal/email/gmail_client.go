package email

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
)

type GmailProfile struct {
	EmailAddress string `json:"emailAddress"`
	HistoryID    string `json:"historyId"`
}

func GetGmailProfile(
	ctx context.Context,
	accessToken string,
) (*GmailProfile, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("Gmail access token is required")
	}

	token := &oauth2.Token{
		AccessToken: accessToken,
		TokenType:   "Bearer",
	}

	client := oauth2.NewClient(
		ctx,
		oauth2.StaticTokenSource(token),
	)

	req, err := client.Get(
		"https://gmail.googleapis.com/gmail/v1/users/me/profile",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get Gmail profile: %w",
			err,
		)
	}
	defer req.Body.Close()

	if req.StatusCode < 200 || req.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"Gmail profile request failed with status %s",
			req.Status,
		)
	}

	var profile GmailProfile

	if err := json.NewDecoder(req.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf(
			"decode Gmail profile: %w",
			err,
		)
	}

	if profile.EmailAddress == "" {
		return nil, fmt.Errorf(
			"Gmail profile did not contain an email address",
		)
	}

	return &profile, nil
}

func GetGmailProfileWithClient(
	ctx context.Context,
	client *http.Client,
) (*GmailProfile, error) {
	if client == nil {
		return nil, fmt.Errorf("Gmail HTTP client is required")
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://gmail.googleapis.com/gmail/v1/users/me/profile",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create Gmail profile request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get Gmail profile: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"Gmail profile request failed with status %s",
			resp.Status,
		)
	}

	var profile GmailProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("decode Gmail profile: %w", err)
	}

	if profile.EmailAddress == "" {
		return nil, fmt.Errorf("Gmail profile did not contain an email address")
	}

	return &profile, nil
}
