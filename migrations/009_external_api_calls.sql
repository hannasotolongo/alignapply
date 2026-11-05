-- Calls made to rate-limited outside job sources (e.g. Jooble), so Jane can
-- stay under their request allowance across server restarts.
CREATE TABLE IF NOT EXISTS external_api_calls (
    id BIGSERIAL PRIMARY KEY,
    source TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS external_api_calls_source_time_idx
    ON external_api_calls (source, created_at DESC);
