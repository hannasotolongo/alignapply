// Package billing is a minimal Stripe client covering exactly what AlignApply
// needs: customers, Checkout, the Billing Portal, subscription reads and
// cancellation, and webhook signature verification. It talks to the Stripe
// REST API directly so the backend takes on no new dependencies.
package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultAPIBase = "https://api.stripe.com"

type Client struct {
	secretKey  string
	apiBase    string
	httpClient *http.Client
}

func NewClient(secretKey string) (*Client, error) {
	secretKey = strings.TrimSpace(secretKey)
	if secretKey == "" {
		return nil, errors.New("billing: Stripe secret key is required")
	}

	return &Client{
		secretKey:  secretKey,
		apiBase:    defaultAPIBase,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// WithAPIBase points the client at a fake Stripe server in tests.
func (c *Client) WithAPIBase(base string) *Client {
	c.apiBase = strings.TrimRight(base, "/")
	return c
}

type APIError struct {
	StatusCode int
	Type       string `json:"type"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf(
		"stripe: %d %s: %s",
		e.StatusCode,
		e.Type,
		e.Message,
	)
}

func (c *Client) do(
	ctx context.Context,
	method string,
	path string,
	form url.Values,
	idempotencyKey string,
	destination any,
) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}

	request, err := http.NewRequestWithContext(
		ctx,
		method,
		c.apiBase+path,
		body,
	)
	if err != nil {
		return fmt.Errorf("billing: build request: %w", err)
	}

	request.SetBasicAuth(c.secretKey, "")
	if form != nil {
		request.Header.Set(
			"Content-Type",
			"application/x-www-form-urlencoded",
		)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("billing: %s %s: %w", method, path, err)
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("billing: read response: %w", err)
	}

	if response.StatusCode >= 300 {
		var envelope struct {
			Error APIError `json:"error"`
		}
		_ = json.Unmarshal(payload, &envelope)
		envelope.Error.StatusCode = response.StatusCode
		return &envelope.Error
	}

	if destination == nil {
		return nil
	}

	if err := json.Unmarshal(payload, destination); err != nil {
		return fmt.Errorf("billing: decode response: %w", err)
	}

	return nil
}

func (c *Client) CreateCustomer(
	ctx context.Context,
	email string,
	userID string,
) (string, error) {
	form := url.Values{}
	if email != "" {
		form.Set("email", email)
	}
	form.Set("metadata[user_id]", userID)

	var customer struct {
		ID string `json:"id"`
	}

	// The user ID makes retries safe: a repeated request returns the same
	// customer instead of creating a duplicate.
	if err := c.do(
		ctx,
		http.MethodPost,
		"/v1/customers",
		form,
		"alignapply-customer-"+userID,
		&customer,
	); err != nil {
		return "", err
	}

	return customer.ID, nil
}

type CheckoutParams struct {
	CustomerID string
	PriceID    string
	UserID     string
	SuccessURL string
	CancelURL  string
}

func (c *Client) CreateCheckoutSession(
	ctx context.Context,
	params CheckoutParams,
) (string, error) {
	form := url.Values{}
	form.Set("mode", "subscription")
	form.Set("customer", params.CustomerID)
	form.Set("client_reference_id", params.UserID)
	form.Set("line_items[0][price]", params.PriceID)
	form.Set("line_items[0][quantity]", "1")
	form.Set("success_url", params.SuccessURL)
	form.Set("cancel_url", params.CancelURL)
	form.Set("allow_promotion_codes", "true")
	form.Set("subscription_data[metadata][user_id]", params.UserID)

	var session struct {
		URL string `json:"url"`
	}

	if err := c.do(
		ctx,
		http.MethodPost,
		"/v1/checkout/sessions",
		form,
		"",
		&session,
	); err != nil {
		return "", err
	}

	return session.URL, nil
}

func (c *Client) CreatePortalSession(
	ctx context.Context,
	customerID string,
	returnURL string,
) (string, error) {
	form := url.Values{}
	form.Set("customer", customerID)
	form.Set("return_url", returnURL)

	var session struct {
		URL string `json:"url"`
	}

	if err := c.do(
		ctx,
		http.MethodPost,
		"/v1/billing_portal/sessions",
		form,
		"",
		&session,
	); err != nil {
		return "", err
	}

	return session.URL, nil
}

// Subscription is the subset of a Stripe subscription that AlignApply stores.
type Subscription struct {
	ID                string `json:"id"`
	Customer          string `json:"customer"`
	Status            string `json:"status"`
	CancelAtPeriodEnd bool   `json:"cancel_at_period_end"`

	// Older Stripe API versions report the period on the subscription;
	// newer ones (2025-03-31+) report it per subscription item.
	CurrentPeriodEnd int64 `json:"current_period_end"`

	Items struct {
		Data []struct {
			CurrentPeriodEnd int64 `json:"current_period_end"`
			Price            struct {
				ID string `json:"id"`
			} `json:"price"`
		} `json:"data"`
	} `json:"items"`
}

func (s Subscription) PriceID() string {
	if len(s.Items.Data) == 0 {
		return ""
	}
	return s.Items.Data[0].Price.ID
}

func (s Subscription) PeriodEnd() *time.Time {
	end := s.CurrentPeriodEnd
	if end == 0 && len(s.Items.Data) > 0 {
		end = s.Items.Data[0].CurrentPeriodEnd
	}
	if end == 0 {
		return nil
	}
	value := time.Unix(end, 0).UTC()
	return &value
}

func (c *Client) GetSubscription(
	ctx context.Context,
	subscriptionID string,
) (Subscription, error) {
	var subscription Subscription
	err := c.do(
		ctx,
		http.MethodGet,
		"/v1/subscriptions/"+url.PathEscape(subscriptionID),
		nil,
		"",
		&subscription,
	)
	return subscription, err
}

// CancelSubscription cancels immediately. Used when a user deletes their
// account so they are never billed for an account that no longer exists.
func (c *Client) CancelSubscription(
	ctx context.Context,
	subscriptionID string,
) error {
	err := c.do(
		ctx,
		http.MethodDelete,
		"/v1/subscriptions/"+url.PathEscape(subscriptionID),
		nil,
		"",
		nil,
	)

	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return nil
	}

	return err
}

// ---------------------------------------------------------------------------
// Webhooks
// ---------------------------------------------------------------------------

type Event struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

var (
	ErrInvalidSignature = errors.New("billing: invalid webhook signature")
	ErrStaleWebhook     = errors.New("billing: webhook timestamp outside tolerance")
)

const DefaultWebhookTolerance = 5 * time.Minute

// VerifyWebhook checks the Stripe-Signature header (HMAC-SHA256 over
// "timestamp.payload") and returns the parsed event.
func VerifyWebhook(
	payload []byte,
	signatureHeader string,
	secret string,
	tolerance time.Duration,
	now time.Time,
) (Event, error) {
	var (
		timestamp  int64
		signatures [][]byte
	)

	for _, part := range strings.Split(signatureHeader, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}

		switch key {
		case "t":
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return Event{}, ErrInvalidSignature
			}
			timestamp = parsed

		case "v1":
			decoded, err := hex.DecodeString(value)
			if err == nil {
				signatures = append(signatures, decoded)
			}
		}
	}

	if timestamp == 0 || len(signatures) == 0 {
		return Event{}, ErrInvalidSignature
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("."))
	mac.Write(payload)
	expected := mac.Sum(nil)

	valid := false
	for _, signature := range signatures {
		if hmac.Equal(signature, expected) {
			valid = true
			break
		}
	}

	if !valid {
		return Event{}, ErrInvalidSignature
	}

	age := now.Sub(time.Unix(timestamp, 0))
	if age > tolerance || age < -tolerance {
		return Event{}, ErrStaleWebhook
	}

	var event Event
	if err := json.Unmarshal(payload, &event); err != nil {
		return Event{}, fmt.Errorf("billing: decode webhook event: %w", err)
	}

	return event, nil
}

// SignWebhookPayload builds a Stripe-Signature header. Used by tests.
func SignWebhookPayload(
	payload []byte,
	secret string,
	timestamp time.Time,
) string {
	ts := strconv.FormatInt(timestamp.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(payload)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}
