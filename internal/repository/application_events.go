package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type ApplicationEvent struct {
	ID              string
	ApplicationID   string
	EventType       string
	PreviousStatus  *string
	NewStatus       string
	Source          string
	SourceProvider  string
	SourceMessageID *string
	Confidence      *float64
	Summary         string
	OccurredAt      time.Time
	CreatedAt       time.Time
}

type RecordApplicationEventParams struct {
	UserID          string
	ApplicationID   string
	EventType       string
	NewStatus       string
	Source          string
	SourceProvider  string
	SourceMessageID *string
	Confidence      *float64
	Summary         string
	OccurredAt      *time.Time
}

func (r *UserJobsRepository) RecordApplicationEvent(
	ctx context.Context,
	params RecordApplicationEventParams,
) (Application, ApplicationEvent, error) {
	if err := validateRecordApplicationEventParams(params); err != nil {
		return Application{}, ApplicationEvent{}, err
	}

	userID := strings.TrimSpace(params.UserID)
	applicationID := strings.TrimSpace(params.ApplicationID)
	eventType := strings.TrimSpace(params.EventType)
	newStatus := strings.TrimSpace(params.NewStatus)
	source := strings.TrimSpace(params.Source)
	sourceProvider := strings.TrimSpace(params.SourceProvider)
	summary := strings.TrimSpace(params.Summary)

	sourceMessageID := normalizeOptionalString(params.SourceMessageID)

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Application{}, ApplicationEvent{}, fmt.Errorf(
			"repository: begin application event transaction: %w",
			err,
		)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var application Application

	const lockApplicationQuery = `
SELECT
	id::text,
	user_id::text,
	job_id::text,
	status,
	applied_at,
	notes,
	created_at,
	updated_at
FROM applications
WHERE id = $1
  AND user_id = $2
FOR UPDATE;
`

	err = tx.QueryRow(
		ctx,
		lockApplicationQuery,
		applicationID,
		userID,
	).Scan(
		&application.ID,
		&application.UserID,
		&application.JobID,
		&application.Status,
		&application.AppliedAt,
		&application.Notes,
		&application.CreatedAt,
		&application.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Application{}, ApplicationEvent{}, fmt.Errorf(
			"repository: application %s for user %s not found",
			applicationID,
			userID,
		)
	}
	if err != nil {
		return Application{}, ApplicationEvent{}, fmt.Errorf(
			"repository: lock application: %w",
			err,
		)
	}

	previousStatus := application.Status

	if sourceMessageID != nil {
		existing, found, err := getApplicationEventByExternalMessage(
			ctx,
			tx,
			applicationID,
			source,
			sourceProvider,
			*sourceMessageID,
		)
		if err != nil {
			return Application{}, ApplicationEvent{}, err
		}
		if found {
			if err := tx.Commit(ctx); err != nil {
				return Application{}, ApplicationEvent{}, fmt.Errorf(
					"repository: commit duplicate application event transaction: %w",
					err,
				)
			}
			return application, existing, nil
		}
	}

	occurredAt := time.Now().UTC()
	if params.OccurredAt != nil {
		occurredAt = params.OccurredAt.UTC()
	}

	const insertEventQuery = `
INSERT INTO application_events (
	application_id,
	event_type,
	previous_status,
	new_status,
	source,
	source_provider,
	source_message_id,
	confidence,
	summary,
	occurred_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING
	id::text,
	application_id::text,
	event_type,
	previous_status,
	new_status,
	source,
	source_provider,
	source_message_id,
	confidence,
	summary,
	occurred_at,
	created_at;
`

	var event ApplicationEvent
	err = tx.QueryRow(
		ctx,
		insertEventQuery,
		applicationID,
		eventType,
		previousStatus,
		newStatus,
		source,
		sourceProvider,
		sourceMessageID,
		params.Confidence,
		summary,
		occurredAt,
	).Scan(
		&event.ID,
		&event.ApplicationID,
		&event.EventType,
		&event.PreviousStatus,
		&event.NewStatus,
		&event.Source,
		&event.SourceProvider,
		&event.SourceMessageID,
		&event.Confidence,
		&event.Summary,
		&event.OccurredAt,
		&event.CreatedAt,
	)
	if err != nil {
		return Application{}, ApplicationEvent{}, fmt.Errorf(
			"repository: insert application event: %w",
			err,
		)
	}

	const updateApplicationQuery = `
UPDATE applications
SET
	status = $1,
	applied_at = CASE
		WHEN $1 = 'applied' AND applied_at IS NULL THEN $2
		ELSE applied_at
	END,
	updated_at = NOW()
WHERE id = $3
  AND user_id = $4
RETURNING
	id::text,
	user_id::text,
	job_id::text,
	status,
	applied_at,
	notes,
	created_at,
	updated_at;
`

	err = tx.QueryRow(
		ctx,
		updateApplicationQuery,
		newStatus,
		occurredAt,
		applicationID,
		userID,
	).Scan(
		&application.ID,
		&application.UserID,
		&application.JobID,
		&application.Status,
		&application.AppliedAt,
		&application.Notes,
		&application.CreatedAt,
		&application.UpdatedAt,
	)
	if err != nil {
		return Application{}, ApplicationEvent{}, fmt.Errorf(
			"repository: update application status: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return Application{}, ApplicationEvent{}, fmt.Errorf(
			"repository: commit application event transaction: %w",
			err,
		)
	}

	return application, event, nil
}

func (r *UserJobsRepository) ListApplicationEvents(
	ctx context.Context,
	userID string,
	applicationID string,
) ([]ApplicationEvent, error) {
	userID = strings.TrimSpace(userID)
	applicationID = strings.TrimSpace(applicationID)

	if userID == "" {
		return nil, errors.New(
			"repository: application event user ID is required",
		)
	}
	if applicationID == "" {
		return nil, errors.New(
			"repository: application event application ID is required",
		)
	}

	const query = `
SELECT
	e.id::text,
	e.application_id::text,
	e.event_type,
	e.previous_status,
	e.new_status,
	e.source,
	e.source_provider,
	e.source_message_id,
	e.confidence,
	e.summary,
	e.occurred_at,
	e.created_at
FROM application_events e
JOIN applications a
	ON a.id = e.application_id
WHERE e.application_id = $1
  AND a.user_id = $2
ORDER BY
	e.occurred_at ASC,
	e.created_at ASC,
	e.id ASC;
`

	rows, err := r.pool.Query(
		ctx,
		query,
		applicationID,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"repository: list application events: %w",
			err,
		)
	}
	defer rows.Close()

	events := make([]ApplicationEvent, 0)

	for rows.Next() {
		var event ApplicationEvent
		if err := rows.Scan(
			&event.ID,
			&event.ApplicationID,
			&event.EventType,
			&event.PreviousStatus,
			&event.NewStatus,
			&event.Source,
			&event.SourceProvider,
			&event.SourceMessageID,
			&event.Confidence,
			&event.Summary,
			&event.OccurredAt,
			&event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf(
				"repository: scan application event: %w",
				err,
			)
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"repository: iterate application events: %w",
			err,
		)
	}

	return events, nil
}

func getApplicationEventByExternalMessage(
	ctx context.Context,
	tx pgx.Tx,
	applicationID string,
	source string,
	sourceProvider string,
	sourceMessageID string,
) (ApplicationEvent, bool, error) {
	const query = `
SELECT
	id::text,
	application_id::text,
	event_type,
	previous_status,
	new_status,
	source,
	source_provider,
	source_message_id,
	confidence,
	summary,
	occurred_at,
	created_at
FROM application_events
WHERE application_id = $1
  AND source = $2
  AND source_provider = $3
  AND source_message_id = $4
LIMIT 1;
`

	var event ApplicationEvent
	err := tx.QueryRow(
		ctx,
		query,
		applicationID,
		source,
		sourceProvider,
		sourceMessageID,
	).Scan(
		&event.ID,
		&event.ApplicationID,
		&event.EventType,
		&event.PreviousStatus,
		&event.NewStatus,
		&event.Source,
		&event.SourceProvider,
		&event.SourceMessageID,
		&event.Confidence,
		&event.Summary,
		&event.OccurredAt,
		&event.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApplicationEvent{}, false, nil
	}
	if err != nil {
		return ApplicationEvent{}, false, fmt.Errorf(
			"repository: get application event by external message: %w",
			err,
		)
	}

	return event, true, nil
}

func validateRecordApplicationEventParams(
	params RecordApplicationEventParams,
) error {
	if strings.TrimSpace(params.UserID) == "" {
		return errors.New(
			"repository: application event user ID is required",
		)
	}
	if strings.TrimSpace(params.ApplicationID) == "" {
		return errors.New(
			"repository: application event application ID is required",
		)
	}

	switch strings.TrimSpace(params.EventType) {
	case "application_created",
		"application_submitted",
		"application_received",
		"recruiter_contact",
		"phone_screen",
		"interview_invitation",
		"interview_scheduled",
		"interview_completed",
		"offer_received",
		"hired",
		"rejected",
		"withdrawn",
		"status_changed":
	default:
		return fmt.Errorf(
			"repository: invalid application event type %q",
			params.EventType,
		)
	}

	switch strings.TrimSpace(params.NewStatus) {
	case "saved",
		"applied",
		"received",
		"recruiter_contact",
		"interviewing",
		"offer",
		"hired",
		"rejected",
		"withdrawn":
	default:
		return fmt.Errorf(
			"repository: invalid application status %q",
			params.NewStatus,
		)
	}

	switch strings.TrimSpace(params.Source) {
	case "manual", "email", "system", "ats":
	default:
		return fmt.Errorf(
			"repository: invalid application event source %q",
			params.Source,
		)
	}

	if params.Confidence != nil {
		if *params.Confidence < 0 || *params.Confidence > 1 {
			return errors.New(
				"repository: application event confidence must be between 0 and 1",
			)
		}
	}

	return nil
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}

	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}

	return &normalized
}
