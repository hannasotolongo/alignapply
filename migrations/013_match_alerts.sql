-- Daily match emails: a visitor asks Jane to email new matches for a job
-- and place. Emails start only after they confirm by clicking the link in
-- the confirmation email. The résumé text is kept so new jobs can be
-- scored; unsubscribing deletes it.
CREATE TABLE IF NOT EXISTS match_alerts (
    id BIGSERIAL PRIMARY KEY,
    email TEXT NOT NULL,
    target_role TEXT NOT NULL,
    location TEXT NOT NULL,
    resume_text TEXT NOT NULL DEFAULT '',
    token TEXT NOT NULL UNIQUE,          -- in confirm and unsubscribe links
    active BOOLEAN NOT NULL DEFAULT TRUE,
    confirmed_at TIMESTAMPTZ,
    confirm_sent_at TIMESTAMPTZ,
    sent_job_ids TEXT[] NOT NULL DEFAULT '{}',
    last_sent_at TIMESTAMPTZ,
    emails_sent INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (email, target_role, location)
);

CREATE INDEX IF NOT EXISTS match_alerts_due_idx
    ON match_alerts (last_sent_at NULLS FIRST)
    WHERE active AND confirmed_at IS NOT NULL;
