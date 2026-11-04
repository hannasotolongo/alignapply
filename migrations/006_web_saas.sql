BEGIN;

-- Web (email + password) sign-in. Apple users keep apple_subject and have no
-- password_hash; web users have a password_hash and a unique email.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS password_hash TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS users_password_email_unique_idx
    ON users (LOWER(email))
    WHERE password_hash IS NOT NULL AND deleted_at IS NULL;

-- One Stripe customer/subscription per user. Rows are written only by the
-- server (checkout + verified Stripe webhooks), never from client input.
CREATE TABLE IF NOT EXISTS subscriptions (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    stripe_customer_id TEXT NOT NULL UNIQUE,
    stripe_subscription_id TEXT UNIQUE,
    status TEXT NOT NULL DEFAULT 'none',
    price_id TEXT,
    current_period_end TIMESTAMPTZ,
    cancel_at_period_end BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Metered feature usage for free-tier quotas and per-user rate limits.
CREATE TABLE IF NOT EXISTS usage_events (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    feature TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS usage_events_user_feature_time_idx
    ON usage_events (user_id, feature, created_at DESC);

-- Stripe retries webhooks; this makes processing idempotent.
CREATE TABLE IF NOT EXISTS stripe_webhook_events (
    event_id TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMIT;
