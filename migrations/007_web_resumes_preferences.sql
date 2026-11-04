BEGIN;

-- Per-user job-search preferences and onboarding state for the web app.
CREATE TABLE IF NOT EXISTS user_preferences (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    preferences JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Web résumés are parsed in the browser; only extracted text is stored.
CREATE INDEX IF NOT EXISTS resumes_user_created_idx
    ON resumes (user_id, created_at DESC);

COMMIT;
