package email

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

type gmailMessageListResponse struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
}

type gmailMessageResponse struct {
	ID           string `json:"id"`
	InternalDate string `json:"internalDate"`

	Payload struct {
		Headers []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`

		Body struct {
			Data string `json:"data"`
		} `json:"body"`

		Parts []gmailMessagePart `json:"parts"`
	} `json:"payload"`
}

type gmailMessagePart struct {
	MimeType string `json:"mimeType"`

	Body struct {
		Data string `json:"data"`
	} `json:"body"`

	Parts []gmailMessagePart `json:"parts"`
}

// FetchRecentGmailMessages retrieves recent Gmail messages and converts
// them into AlignApply's provider-independent Message model.
func FetchRecentGmailMessages(
	ctx context.Context,
	accessToken string,
	maxResults int,
) ([]Message, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("Gmail access token is required")
	}

	if maxResults <= 0 {
		maxResults = 25
	}

	client := gmailHTTPClient(ctx, accessToken)

	values := url.Values{}
	values.Set("maxResults", strconv.Itoa(maxResults))

	// Keep the first sync focused on messages likely to matter to
	// application tracking rather than scanning the entire mailbox.
	values.Set(
		"q",
		"newer_than:30d",
	)

	listURL :=
		"https://gmail.googleapis.com/gmail/v1/users/me/messages?" +
			values.Encode()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		listURL,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create Gmail list request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list Gmail messages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, gmailResponseError(
			"list Gmail messages",
			resp,
		)
	}

	var list gmailMessageListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf(
			"decode Gmail message list: %w",
			err,
		)
	}

	messages := make([]Message, 0, len(list.Messages))

	for _, item := range list.Messages {
		message, err := fetchGmailMessage(
			ctx,
			client,
			item.ID,
		)
		if err != nil {
			return nil, err
		}

		messages = append(messages, *message)
	}

	return messages, nil
}

func fetchGmailMessage(
	ctx context.Context,
	client *http.Client,
	messageID string,
) (*Message, error) {
	messageURL :=
		"https://gmail.googleapis.com/gmail/v1/users/me/messages/" +
			url.PathEscape(messageID) +
			"?format=full"

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		messageURL,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create Gmail message request: %w",
			err,
		)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf(
			"get Gmail message %s: %w",
			messageID,
			err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, gmailResponseError(
			"get Gmail message "+messageID,
			resp,
		)
	}

	var gmailMessage gmailMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&gmailMessage); err != nil {
		return nil, fmt.Errorf(
			"decode Gmail message %s: %w",
			messageID,
			err,
		)
	}

	var from string
	var subject string

	for _, header := range gmailMessage.Payload.Headers {
		switch strings.ToLower(header.Name) {
		case "from":
			from = header.Value
		case "subject":
			subject = header.Value
		}
	}

	receivedAt := time.Time{}

	if gmailMessage.InternalDate != "" {
		milliseconds, err :=
			strconv.ParseInt(
				gmailMessage.InternalDate,
				10,
				64,
			)
		if err == nil {
			receivedAt = time.UnixMilli(milliseconds)
		}
	}

	body := extractGmailBody(
		gmailMessage.Payload.Body.Data,
		gmailMessage.Payload.Parts,
	)

	return &Message{
		ID:         gmailMessage.ID,
		Provider:   "gmail",
		From:       from,
		Subject:    subject,
		Body:       body,
		ReceivedAt: receivedAt,
	}, nil
}

func extractGmailBody(
	rootData string,
	parts []gmailMessagePart,
) string {
	if rootData != "" {
		if decoded := decodeGmailBody(rootData); decoded != "" {
			return decoded
		}
	}

	// Prefer text/plain when Gmail supplies a multipart message.
	for _, part := range parts {
		if part.MimeType == "text/plain" &&
			part.Body.Data != "" {

			if decoded :=
				decodeGmailBody(part.Body.Data); decoded != "" {
				return decoded
			}
		}
	}

	// Recursively inspect nested multipart sections.
	for _, part := range parts {
		if len(part.Parts) > 0 {
			if body := extractGmailBody(
				part.Body.Data,
				part.Parts,
			); body != "" {
				return body
			}
		}
	}

	// HTML is an acceptable fallback for classification if no
	// plain-text representation exists.
	for _, part := range parts {
		if part.MimeType == "text/html" &&
			part.Body.Data != "" {

			if decoded :=
				decodeGmailBody(part.Body.Data); decoded != "" {
				return decoded
			}
		}
	}

	return ""
}

func decodeGmailBody(data string) string {
	if data == "" {
		return ""
	}

	decoded, err :=
		base64.RawURLEncoding.DecodeString(data)
	if err != nil {
		decoded, err =
			base64.URLEncoding.DecodeString(data)
		if err != nil {
			return ""
		}
	}

	return string(decoded)
}

func gmailHTTPClient(
	ctx context.Context,
	accessToken string,
) *http.Client {
	token := &oauth2.Token{
		AccessToken: accessToken,
		TokenType:   "Bearer",
	}

	return oauth2.NewClient(
		ctx,
		oauth2.StaticTokenSource(token),
	)
}

func gmailResponseError(
	operation string,
	resp *http.Response,
) error {
	body, _ := io.ReadAll(
		io.LimitReader(resp.Body, 4096),
	)

	if len(body) == 0 {
		return fmt.Errorf(
			"%s failed with status %s",
			operation,
			resp.Status,
		)
	}

	return fmt.Errorf(
		"%s failed with status %s: %s",
		operation,
		resp.Status,
		strings.TrimSpace(string(body)),
	)
}

func FetchRecentGmailMessagesWithClient(
	ctx context.Context,
	client *http.Client,
	maxResults int,
) ([]Message, error) {
	if client == nil {
		return nil, fmt.Errorf("Gmail HTTP client is required")
	}

	if maxResults <= 0 {
		maxResults = 25
	}

	values := url.Values{}
	values.Set("maxResults", strconv.Itoa(maxResults))
	values.Set("q", "newer_than:30d")

	listURL :=
		"https://gmail.googleapis.com/gmail/v1/users/me/messages?" +
			values.Encode()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		listURL,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create Gmail list request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list Gmail messages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, gmailResponseError("list Gmail messages", resp)
	}

	var list gmailMessageListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("decode Gmail message list: %w", err)
	}

	messages := make([]Message, 0, len(list.Messages))

	for _, item := range list.Messages {
		message, err := fetchGmailMessage(
			ctx,
			client,
			item.ID,
		)
		if err != nil {
			return nil, err
		}

		messages = append(messages, *message)
	}

	return messages, nil
}
