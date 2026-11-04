BEGIN;

-- One-time passes (e.g. the 90-day Job Search Sprint) and one trial per user.
ALTER TABLE subscriptions
    ADD COLUMN IF NOT EXISTS pass_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS trial_used BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE subscriptions
   SET trial_used = TRUE
 WHERE stripe_subscription_id IS NOT NULL AND trial_used = FALSE;

-- Email ownership. School licenses only apply to verified addresses.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ;

-- Single-use tokens for email verification and password reset. Only a
-- SHA-256 hash of each token is stored.
CREATE TABLE IF NOT EXISTS auth_tokens (
    token_hash TEXT PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose TEXT NOT NULL CHECK (purpose IN ('verify_email', 'reset_password')),
    email TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS auth_tokens_user_purpose_idx
    ON auth_tokens (user_id, purpose, created_at DESC);

-- School / program licenses. Every verified user whose email domain is
-- exactly email_domain gets Pro while the license is active. Subdomains are
-- not covered.
CREATE TABLE IF NOT EXISTS organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    email_domain TEXT NOT NULL UNIQUE,
    owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    stripe_customer_id TEXT UNIQUE,
    stripe_subscription_id TEXT UNIQUE,
    status TEXT NOT NULL DEFAULT 'none',
    current_period_end TIMESTAMPTZ,
    cancel_at_period_end BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (email_domain = LOWER(email_domain))
);

COMMIT;
