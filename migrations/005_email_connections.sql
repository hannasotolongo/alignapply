CREATE TABLE IF NOT EXISTS email_connections (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    provider TEXT NOT NULL,
    provider_email TEXT,

    access_token TEXT NOT NULL,
    refresh_token TEXT NOT NULL,
    token_expiry TIMESTAMPTZ,

    scopes TEXT[] NOT NULL DEFAULT '{}',

    last_history_id TEXT,
    last_synced_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT email_connections_provider_check
        CHECK (provider IN ('gmail', 'outlook')),

    CONSTRAINT email_connections_user_provider_unique
        UNIQUE (user_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_email_connections_user_id
    ON email_connections(user_id);

CREATE INDEX IF NOT EXISTS idx_email_connections_provider
    ON email_connections(provider);