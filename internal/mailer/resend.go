// Package mailer sends transactional email (verification and password
// reset) through Resend's HTTP API. It has no dependencies beyond net/http.
package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	apiKey     string
	from       string
	apiBase    string
	httpClient *http.Client
}

type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// NewResend returns nil when email is not configured, so callers can treat a
// nil *Client as "email disabled".
func NewResend(apiKey, from string) (*Client, error) {
	apiKey = strings.TrimSpace(apiKey)
	from = strings.TrimSpace(from)
	if apiKey == "" && from == "" {
		return nil, nil
	}
	if apiKey == "" || from == "" {
		return nil, errors.New("mailer: RESEND_API_KEY and EMAIL_FROM must be set together")
	}
	return &Client{
		apiKey:     apiKey,
		from:       from,
		apiBase:    "https://api.resend.com",
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// WithAPIBase points the client at a fake server in tests.
func (c *Client) WithAPIBase(base string) *Client {
	c.apiBase = strings.TrimRight(base, "/")
	return c
}

func (c *Client) Send(ctx context.Context, message Message) error {
	if c == nil {
		return errors.New("mailer: email is not configured")
	}

	payload, err := json.Marshal(map[string]any{
		"from":    c.from,
		"to":      []string{message.To},
		"subject": message.Subject,
		"text":    message.Text,
		"html":    message.HTML,
	})
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+"/emails", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("mailer: send: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return fmt.Errorf("mailer: send: HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
