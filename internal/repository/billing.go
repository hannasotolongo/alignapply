package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrSubscriptionNotFound = errors.New(
	"repository: subscription not found",
)

type Subscription struct {
	UserID               string
	StripeCustomerID     string
	StripeSubscriptionID string
	Status               string
	PriceID              string
	CurrentPeriodEnd     *time.Time
	CancelAtPeriodEnd    bool
	PassExpiresAt        *time.Time
	TrialUsed            bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// SubscriptionActive reports whether a recurring subscription grants Pro.
// past_due keeps access during Stripe's automatic payment retries.
func (s Subscription) SubscriptionActive() bool {
	return SubscriptionStatusActive(s.Status)
}

// PassActive reports whether a one-time pass (e.g. the Sprint) is current.
func (s Subscription) PassActive(now time.Time) bool {
	return s.PassExpiresAt != nil && s.PassExpiresAt.After(now)
}

// IsPro reports whether the user's own purchases currently grant Pro.
func (s Subscription) IsPro() bool {
	return s.SubscriptionActive() || s.PassActive(time.Now())
}

func SubscriptionStatusActive(status string) bool {
	switch status {
	case "active", "trialing", "past_due":
		return true
	default:
		return false
	}
}

type StripeSubscriptionUpdate struct {
	StripeCustomerID     string
	StripeSubscriptionID string
	Status               string
	PriceID              string
	CurrentPeriodEnd     *time.Time
	CancelAtPeriodEnd    bool
}

type BillingRepository struct {
	pool *pgxpool.Pool
}

func NewBillingRepository(pool *pgxpool.Pool) (*BillingRepository, error) {
	if pool == nil {
		return nil, errors.New("repository: PostgreSQL pool is required")
	}

	return &BillingRepository{pool: pool}, nil
}

const subscriptionColumns = `
	user_id::text,
	stripe_customer_id,
	COALESCE(stripe_subscription_id, ''),
	status,
	COALESCE(price_id, ''),
	current_period_end,
	cancel_at_period_end,
	pass_expires_at,
	trial_used,
	created_at,
	updated_at
`

func scanSubscription(row pgx.Row) (Subscription, error) {
	var subscription Subscription
	err := row.Scan(
		&subscription.UserID,
		&subscription.StripeCustomerID,
		&subscription.StripeSubscriptionID,
		&subscription.Status,
		&subscription.PriceID,
		&subscription.CurrentPeriodEnd,
		&subscription.CancelAtPeriodEnd,
		&subscription.PassExpiresAt,
		&subscription.TrialUsed,
		&subscription.CreatedAt,
		&subscription.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, ErrSubscriptionNotFound
	}
	return subscription, err
}

func (r *BillingRepository) GetSubscription(
	ctx context.Context,
	userID string,
) (Subscription, error) {
	subscription, err := scanSubscription(
		r.pool.QueryRow(
			ctx,
			`SELECT `+subscriptionColumns+` FROM subscriptions WHERE user_id = $1`,
			strings.TrimSpace(userID),
		),
	)
	if err != nil && !errors.Is(err, ErrSubscriptionNotFound) {
		return Subscription{}, fmt.Errorf("repository: get subscription: %w", err)
	}
	return subscription, err
}

// AttachStripeCustomer records the Stripe customer created for a user.
// An existing customer ID is never replaced.
func (r *BillingRepository) AttachStripeCustomer(
	ctx context.Context,
	userID string,
	stripeCustomerID string,
) (Subscription, error) {
	const query = `
INSERT INTO subscriptions (user_id, stripe_customer_id)
VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET updated_at = subscriptions.updated_at
RETURNING ` + subscriptionColumns

	subscription, err := scanSubscription(
		r.pool.QueryRow(
			ctx,
			query,
			strings.TrimSpace(userID),
			strings.TrimSpace(stripeCustomerID),
		),
	)
	if err != nil {
		return Subscription{}, fmt.Errorf("repository: attach Stripe customer: %w", err)
	}
	return subscription, nil
}

// ApplyStripeSubscription stores the latest subscription state from Stripe,
// keyed by the Stripe customer that was created for the user.
func (r *BillingRepository) ApplyStripeSubscription(
	ctx context.Context,
	update StripeSubscriptionUpdate,
) (Subscription, error) {
	const query = `
UPDATE subscriptions SET
	stripe_subscription_id = NULLIF($2, ''),
	status = $3,
	price_id = NULLIF($4, ''),
	current_period_end = $5,
	cancel_at_period_end = $6,
	trial_used = trial_used OR $2 <> '',
	updated_at = NOW()
WHERE stripe_customer_id = $1
RETURNING ` + subscriptionColumns

	subscription, err := scanSubscription(
		r.pool.QueryRow(
			ctx,
			query,
			strings.TrimSpace(update.StripeCustomerID),
			strings.TrimSpace(update.StripeSubscriptionID),
			strings.TrimSpace(update.Status),
			strings.TrimSpace(update.PriceID),
			update.CurrentPeriodEnd,
			update.CancelAtPeriodEnd,
		),
	)
	if err != nil && !errors.Is(err, ErrSubscriptionNotFound) {
		return Subscription{}, fmt.Errorf("repository: apply Stripe subscription: %w", err)
	}
	return subscription, err
}

// MarkWebhookEventProcessed returns false when the event was already handled.
func (r *BillingRepository) MarkWebhookEventProcessed(
	ctx context.Context,
	eventID string,
	eventType string,
) (bool, error) {
	result, err := r.pool.Exec(
		ctx,
		`INSERT INTO stripe_webhook_events (event_id, event_type)
		 VALUES ($1, $2)
		 ON CONFLICT (event_id) DO NOTHING`,
		eventID,
		eventType,
	)
	if err != nil {
		return false, fmt.Errorf("repository: record webhook event: %w", err)
	}
	return result.RowsAffected() == 1, nil
}

// ForgetWebhookEvent lets Stripe retry an event whose processing failed.
func (r *BillingRepository) ForgetWebhookEvent(
	ctx context.Context,
	eventID string,
) error {
	_, err := r.pool.Exec(
		ctx,
		`DELETE FROM stripe_webhook_events WHERE event_id = $1`,
		eventID,
	)
	return err
}

func (r *BillingRepository) RecordUsage(
	ctx context.Context,
	userID string,
	feature string,
) error {
	_, err := r.pool.Exec(
		ctx,
		`INSERT INTO usage_events (user_id, feature) VALUES ($1, $2)`,
		strings.TrimSpace(userID),
		feature,
	)
	if err != nil {
		return fmt.Errorf("repository: record usage: %w", err)
	}
	return nil
}

func (r *BillingRepository) CountUsageSince(
	ctx context.Context,
	userID string,
	feature string,
	since time.Time,
) (int, error) {
	var count int
	err := r.pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM usage_events
		 WHERE user_id = $1 AND feature = $2 AND created_at >= $3`,
		strings.TrimSpace(userID),
		feature,
		since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("repository: count usage: %w", err)
	}
	return count, nil
}

// GrantPass extends (or starts) a one-time pass for the customer's user.
// Extending from the later of now or the current expiry means buying a
// second pass adds time instead of overlapping.
func (r *BillingRepository) GrantPass(
	ctx context.Context,
	stripeCustomerID string,
	userID string,
	days int,
) (Subscription, error) {
	const query = `
UPDATE subscriptions SET
	pass_expires_at = GREATEST(COALESCE(pass_expires_at, NOW()), NOW()) + make_interval(days => $3),
	updated_at = NOW()
WHERE stripe_customer_id = $1 AND user_id::text = $2
RETURNING ` + subscriptionColumns

	subscription, err := scanSubscription(
		r.pool.QueryRow(ctx, query, strings.TrimSpace(stripeCustomerID), strings.TrimSpace(userID), days),
	)
	if err != nil && !errors.Is(err, ErrSubscriptionNotFound) {
		return Subscription{}, fmt.Errorf("repository: grant pass: %w", err)
	}
	return subscription, err
}
