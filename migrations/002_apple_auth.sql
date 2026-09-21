BEGIN;

CREATE TABLE apple_auth_credentials (
    user_id UUID PRIMARY KEY
        REFERENCES users(id)
        ON DELETE CASCADE,

    refresh_token TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT apple_auth_credentials_refresh_token_not_empty
        CHECK (length(trim(refresh_token)) > 0)
);

COMMIT;