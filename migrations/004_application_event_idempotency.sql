BEGIN;

-- The original external-message index was global across every application.
-- Provider message IDs should instead be deduplicated within an application.
DROP INDEX IF EXISTS application_events_external_message_idx;

CREATE UNIQUE INDEX application_events_external_message_idx
    ON application_events (
        application_id,
        source,
        source_provider,
        source_message_id
    )
    WHERE source_message_id IS NOT NULL;

COMMIT;
