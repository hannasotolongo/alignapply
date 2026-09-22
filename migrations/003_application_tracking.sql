BEGIN;

-- Expand the current application status model so AlignApply can represent
-- externally detected recruiting progress as well as manual user updates.
ALTER TABLE applications
    DROP CONSTRAINT IF EXISTS applications_status_check;

ALTER TABLE applications
    ADD CONSTRAINT applications_status_check
    CHECK (
        status IN (
            'saved',
            'applied',
            'received',
            'recruiter_contact',
            'interviewing',
            'offer',
            'hired',
            'rejected',
            'withdrawn'
        )
    );

-- Immutable history of meaningful events for an application.
-- applications.status remains the application's current state while this
-- table preserves how and when that state changed.
CREATE TABLE application_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    application_id UUID NOT NULL
        REFERENCES applications(id)
        ON DELETE CASCADE,

    event_type TEXT NOT NULL CHECK (
        event_type IN (
            'application_created',
            'application_submitted',
            'application_received',
            'recruiter_contact',
            'phone_screen',
            'interview_invitation',
            'interview_scheduled',
            'interview_completed',
            'offer_received',
            'hired',
            'rejected',
            'withdrawn',
            'status_changed'
        )
    ),

    previous_status TEXT CHECK (
        previous_status IS NULL OR
        previous_status IN (
            'saved',
            'applied',
            'received',
            'recruiter_contact',
            'interviewing',
            'offer',
            'hired',
            'rejected',
            'withdrawn'
        )
    ),

    new_status TEXT NOT NULL CHECK (
        new_status IN (
            'saved',
            'applied',
            'received',
            'recruiter_contact',
            'interviewing',
            'offer',
            'hired',
            'rejected',
            'withdrawn'
        )
    ),

    source TEXT NOT NULL CHECK (
        source IN (
            'manual',
            'email',
            'system',
            'ats'
        )
    ),

    -- Provider identifier such as gmail/outlook or, later, an ATS provider.
    source_provider TEXT NOT NULL DEFAULT '',

    -- External message/event identifier used for idempotency so the same
    -- email or provider event cannot update an application twice.
    source_message_id TEXT,

    -- Confidence is populated for machine-detected events.
    -- Manual/system events may leave it NULL.
    confidence DOUBLE PRECISION CHECK (
        confidence IS NULL OR
        (confidence >= 0.0 AND confidence <= 1.0)
    ),

    -- Human-readable explanation suitable for the application timeline.
    summary TEXT NOT NULL DEFAULT '',

    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX application_events_application_time_idx
    ON application_events (
        application_id,
        occurred_at DESC,
        created_at DESC
    );

-- Prevent duplicate processing of the same external message/event while
-- still allowing NULL for manual events.
CREATE UNIQUE INDEX application_events_external_message_idx
    ON application_events (
        source,
        source_provider,
        source_message_id
    )
    WHERE source_message_id IS NOT NULL;

COMMIT;
