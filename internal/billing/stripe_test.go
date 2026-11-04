package billing

import (
	"errors"
	"testing"
	"time"
)

func TestVerifyWebhookAcceptsValidSignature(t *testing.T) {
	payload := []byte(`{"id":"evt_1","type":"customer.subscription.updated","data":{"object":{"id":"sub_1"}}}`)
	now := time.Now()

	event, err := VerifyWebhook(payload, SignWebhookPayload(payload, "whsec_test", now), "whsec_test", DefaultWebhookTolerance, now)
	if err != nil {
		t.Fatalf("VerifyWebhook() error = %v", err)
	}
	if event.ID != "evt_1" || event.Type != "customer.subscription.updated" {
		t.Fatalf("event = %+v", event)
	}
}

func TestVerifyWebhookRejectsWrongSecret(t *testing.T) {
	payload := []byte(`{"id":"evt_1"}`)
	now := time.Now()

	_, err := VerifyWebhook(payload, SignWebhookPayload(payload, "whsec_other", now), "whsec_test", DefaultWebhookTolerance, now)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("error = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyWebhookRejectsTamperedPayload(t *testing.T) {
	now := time.Now()
	header := SignWebhookPayload([]byte(`{"id":"evt_1"}`), "whsec_test", now)

	_, err := VerifyWebhook([]byte(`{"id":"evt_2"}`), header, "whsec_test", DefaultWebhookTolerance, now)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("error = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyWebhookRejectsStaleTimestamp(t *testing.T) {
	payload := []byte(`{"id":"evt_1"}`)
	signedAt := time.Now().Add(-10 * time.Minute)

	_, err := VerifyWebhook(payload, SignWebhookPayload(payload, "whsec_test", signedAt), "whsec_test", DefaultWebhookTolerance, time.Now())
	if !errors.Is(err, ErrStaleWebhook) {
		t.Fatalf("error = %v, want ErrStaleWebhook", err)
	}
}

func TestSubscriptionPeriodEndFallsBackToItems(t *testing.T) {
	var subscription Subscription
	subscription.Items.Data = append(subscription.Items.Data, struct {
		CurrentPeriodEnd int64 `json:"current_period_end"`
		Price            struct {
			ID string `json:"id"`
		} `json:"price"`
	}{CurrentPeriodEnd: 1_800_000_000})

	end := subscription.PeriodEnd()
	if end == nil || end.Unix() != 1_800_000_000 {
		t.Fatalf("PeriodEnd() = %v", end)
	}
}
