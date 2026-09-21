package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Resume struct {
	ID            string
	UserID        string
	Filename      string
	ContentType   string
	StorageKey    string
	ExtractedText string
	IsPrimary     bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type CreateResumeParams struct {
	UserID        string
	Filename      string
	ContentType   string
	StorageKey    string
	ExtractedText string
	IsPrimary     bool
}

type SavedJob struct {
	ID        string
	UserID    string
	JobID     string
	CreatedAt time.Time
}

type Application struct {
	ID        string
	UserID    string
	JobID     string
	Status    string
	AppliedAt *time.Time
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type UpsertApplicationParams struct {
	UserID    string
	JobID     string
	Status    string
	AppliedAt *time.Time
	Notes     string
}

type UserJobsRepository struct {
	pool *pgxpool.Pool
}

func NewUserJobsRepository(pool *pgxpool.Pool) (*UserJobsRepository, error) {
	if pool == nil {
		return nil, errors.New("repository: PostgreSQL pool is required")
	}
	return &UserJobsRepository{pool: pool}, nil
}

// CreateResume stores resume metadata and extracted text. If IsPrimary is true,
// the operation atomically clears the previous primary resume for the user.
func (r *UserJobsRepository) CreateResume(
	ctx context.Context,
	params CreateResumeParams,
) (Resume, error) {
	if err := validateCreateResumeParams(params); err != nil {
		return Resume{}, err
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Resume{}, fmt.Errorf("repository: begin resume transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if params.IsPrimary {
		if _, err := tx.Exec(
			ctx,
			`UPDATE resumes
			 SET is_primary = FALSE, updated_at = NOW()
			 WHERE user_id = $1 AND is_primary = TRUE`,
			strings.TrimSpace(params.UserID),
		); err != nil {
			return Resume{}, fmt.Errorf(
				"repository: clear previous primary resume: %w",
				err,
			)
		}
	}

	const query = `
INSERT INTO resumes (
	user_id,
	filename,
	content_type,
	storage_key,
	extracted_text,
	is_primary
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING
	id::text,
	user_id::text,
	filename,
	content_type,
	storage_key,
	extracted_text,
	is_primary,
	created_at,
	updated_at;
`

	var resume Resume
	err = tx.QueryRow(
		ctx,
		query,
		strings.TrimSpace(params.UserID),
		strings.TrimSpace(params.Filename),
		strings.TrimSpace(params.ContentType),
		strings.TrimSpace(params.StorageKey),
		params.ExtractedText,
		params.IsPrimary,
	).Scan(
		&resume.ID,
		&resume.UserID,
		&resume.Filename,
		&resume.ContentType,
		&resume.StorageKey,
		&resume.ExtractedText,
		&resume.IsPrimary,
		&resume.CreatedAt,
		&resume.UpdatedAt,
	)
	if err != nil {
		return Resume{}, fmt.Errorf("repository: insert resume: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Resume{}, fmt.Errorf("repository: commit resume transaction: %w", err)
	}

	return resume, nil
}

func (r *UserJobsRepository) GetResumeByID(
	ctx context.Context,
	resumeID string,
) (Resume, error) {
	resumeID = strings.TrimSpace(resumeID)
	if resumeID == "" {
		return Resume{}, errors.New("repository: resume ID is required")
	}

	const query = `
SELECT
	id::text,
	user_id::text,
	filename,
	content_type,
	storage_key,
	extracted_text,
	is_primary,
	created_at,
	updated_at
FROM resumes
WHERE id = $1;
`

	var resume Resume
	err := r.pool.QueryRow(ctx, query, resumeID).Scan(
		&resume.ID,
		&resume.UserID,
		&resume.Filename,
		&resume.ContentType,
		&resume.StorageKey,
		&resume.ExtractedText,
		&resume.IsPrimary,
		&resume.CreatedAt,
		&resume.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Resume{}, fmt.Errorf("repository: resume %s not found", resumeID)
	}
	if err != nil {
		return Resume{}, fmt.Errorf("repository: get resume: %w", err)
	}

	return resume, nil
}

func (r *UserJobsRepository) GetPrimaryResume(
	ctx context.Context,
	userID string,
) (Resume, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Resume{}, errors.New("repository: user ID is required")
	}

	const query = `
SELECT
	id::text,
	user_id::text,
	filename,
	content_type,
	storage_key,
	extracted_text,
	is_primary,
	created_at,
	updated_at
FROM resumes
WHERE user_id = $1
  AND is_primary = TRUE
ORDER BY updated_at DESC, id DESC
LIMIT 1;
`

	var resume Resume
	err := r.pool.QueryRow(ctx, query, userID).Scan(
		&resume.ID,
		&resume.UserID,
		&resume.Filename,
		&resume.ContentType,
		&resume.StorageKey,
		&resume.ExtractedText,
		&resume.IsPrimary,
		&resume.CreatedAt,
		&resume.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Resume{}, fmt.Errorf(
			"repository: primary resume for user %s not found",
			userID,
		)
	}
	if err != nil {
		return Resume{}, fmt.Errorf("repository: get primary resume: %w", err)
	}

	return resume, nil
}

func (r *UserJobsRepository) SetPrimaryResume(
	ctx context.Context,
	userID string,
	resumeID string,
) error {
	userID = strings.TrimSpace(userID)
	resumeID = strings.TrimSpace(resumeID)

	if userID == "" {
		return errors.New("repository: user ID is required")
	}
	if resumeID == "" {
		return errors.New("repository: resume ID is required")
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("repository: begin primary resume transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var exists bool
	err = tx.QueryRow(
		ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM resumes
			WHERE id = $1 AND user_id = $2
		)`,
		resumeID,
		userID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("repository: verify resume ownership: %w", err)
	}
	if !exists {
		return fmt.Errorf(
			"repository: resume %s does not belong to user %s",
			resumeID,
			userID,
		)
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE resumes
		 SET is_primary = FALSE, updated_at = NOW()
		 WHERE user_id = $1 AND is_primary = TRUE`,
		userID,
	); err != nil {
		return fmt.Errorf("repository: clear primary resume: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE resumes
		 SET is_primary = TRUE, updated_at = NOW()
		 WHERE id = $1 AND user_id = $2`,
		resumeID,
		userID,
	); err != nil {
		return fmt.Errorf("repository: set primary resume: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("repository: commit primary resume transaction: %w", err)
	}

	return nil
}

func (r *UserJobsRepository) DeleteResume(
	ctx context.Context,
	userID string,
	resumeID string,
) error {
	userID = strings.TrimSpace(userID)
	resumeID = strings.TrimSpace(resumeID)

	if userID == "" {
		return errors.New("repository: user ID is required")
	}
	if resumeID == "" {
		return errors.New("repository: resume ID is required")
	}

	commandTag, err := r.pool.Exec(
		ctx,
		`DELETE FROM resumes WHERE id = $1 AND user_id = $2`,
		resumeID,
		userID,
	)
	if err != nil {
		return fmt.Errorf("repository: delete resume: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf(
			"repository: resume %s not found for user %s",
			resumeID,
			userID,
		)
	}

	return nil
}

func (r *UserJobsRepository) SaveJob(
	ctx context.Context,
	userID string,
	jobID string,
) (SavedJob, error) {
	userID = strings.TrimSpace(userID)
	jobID = strings.TrimSpace(jobID)

	if userID == "" {
		return SavedJob{}, errors.New("repository: user ID is required")
	}
	if jobID == "" {
		return SavedJob{}, errors.New("repository: job ID is required")
	}

	const query = `
INSERT INTO saved_jobs (user_id, job_id)
VALUES ($1, $2)
ON CONFLICT (user_id, job_id)
DO UPDATE SET user_id = EXCLUDED.user_id
RETURNING
	id::text,
	user_id::text,
	job_id::text,
	created_at;
`

	var saved SavedJob
	err := r.pool.QueryRow(ctx, query, userID, jobID).Scan(
		&saved.ID,
		&saved.UserID,
		&saved.JobID,
		&saved.CreatedAt,
	)
	if err != nil {
		return SavedJob{}, fmt.Errorf("repository: save job: %w", err)
	}

	return saved, nil
}

func (r *UserJobsRepository) UnsaveJob(
	ctx context.Context,
	userID string,
	jobID string,
) error {
	userID = strings.TrimSpace(userID)
	jobID = strings.TrimSpace(jobID)

	if userID == "" {
		return errors.New("repository: user ID is required")
	}
	if jobID == "" {
		return errors.New("repository: job ID is required")
	}

	if _, err := r.pool.Exec(
		ctx,
		`DELETE FROM saved_jobs WHERE user_id = $1 AND job_id = $2`,
		userID,
		jobID,
	); err != nil {
		return fmt.Errorf("repository: unsave job: %w", err)
	}

	return nil
}

func (r *UserJobsRepository) ListSavedJobIDs(
	ctx context.Context,
	userID string,
) ([]string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errors.New("repository: user ID is required")
	}

	rows, err := r.pool.Query(
		ctx,
		`SELECT job_id::text
		 FROM saved_jobs
		 WHERE user_id = $1
		 ORDER BY created_at DESC, id DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("repository: list saved jobs: %w", err)
	}
	defer rows.Close()

	jobIDs := make([]string, 0)
	for rows.Next() {
		var jobID string
		if err := rows.Scan(&jobID); err != nil {
			return nil, fmt.Errorf("repository: scan saved job: %w", err)
		}
		jobIDs = append(jobIDs, jobID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate saved jobs: %w", err)
	}

	return jobIDs, nil
}

func (r *UserJobsRepository) UpsertApplication(
	ctx context.Context,
	params UpsertApplicationParams,
) (Application, error) {
	if err := validateApplicationParams(params); err != nil {
		return Application{}, err
	}

	const query = `
INSERT INTO applications (
	user_id,
	job_id,
	status,
	applied_at,
	notes
)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, job_id)
DO UPDATE SET
	status = EXCLUDED.status,
	applied_at = EXCLUDED.applied_at,
	notes = EXCLUDED.notes,
	updated_at = NOW()
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

	var application Application
	err := r.pool.QueryRow(
		ctx,
		query,
		strings.TrimSpace(params.UserID),
		strings.TrimSpace(params.JobID),
		strings.TrimSpace(params.Status),
		params.AppliedAt,
		strings.TrimSpace(params.Notes),
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
		return Application{}, fmt.Errorf("repository: upsert application: %w", err)
	}

	return application, nil
}

func (r *UserJobsRepository) GetApplication(
	ctx context.Context,
	userID string,
	jobID string,
) (Application, error) {
	userID = strings.TrimSpace(userID)
	jobID = strings.TrimSpace(jobID)

	if userID == "" {
		return Application{}, errors.New("repository: user ID is required")
	}
	if jobID == "" {
		return Application{}, errors.New("repository: job ID is required")
	}

	const query = `
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
WHERE user_id = $1
  AND job_id = $2;
`

	var application Application
	err := r.pool.QueryRow(ctx, query, userID, jobID).Scan(
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
		return Application{}, fmt.Errorf(
			"repository: application for user %s and job %s not found",
			userID,
			jobID,
		)
	}
	if err != nil {
		return Application{}, fmt.Errorf("repository: get application: %w", err)
	}

	return application, nil
}

func (r *UserJobsRepository) ListApplications(
	ctx context.Context,
	userID string,
) ([]Application, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errors.New("repository: user ID is required")
	}

	const query = `
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
WHERE user_id = $1
ORDER BY updated_at DESC, id DESC;
`

	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("repository: list applications: %w", err)
	}
	defer rows.Close()

	applications := make([]Application, 0)

	for rows.Next() {
		var application Application
		if err := rows.Scan(
			&application.ID,
			&application.UserID,
			&application.JobID,
			&application.Status,
			&application.AppliedAt,
			&application.Notes,
			&application.CreatedAt,
			&application.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("repository: scan application: %w", err)
		}
		applications = append(applications, application)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate applications: %w", err)
	}

	return applications, nil
}

func validateCreateResumeParams(params CreateResumeParams) error {
	if strings.TrimSpace(params.UserID) == "" {
		return errors.New("repository: resume user ID is required")
	}
	if strings.TrimSpace(params.Filename) == "" {
		return errors.New("repository: resume filename is required")
	}
	if strings.TrimSpace(params.ContentType) == "" {
		return errors.New("repository: resume content type is required")
	}
	if strings.TrimSpace(params.StorageKey) == "" {
		return errors.New("repository: resume storage key is required")
	}
	return nil
}

func validateApplicationParams(params UpsertApplicationParams) error {
	if strings.TrimSpace(params.UserID) == "" {
		return errors.New("repository: application user ID is required")
	}
	if strings.TrimSpace(params.JobID) == "" {
		return errors.New("repository: application job ID is required")
	}

	switch strings.TrimSpace(params.Status) {
	case "saved", "applied", "interviewing", "offer", "rejected", "withdrawn":
	default:
		return fmt.Errorf(
			"repository: invalid application status %q",
			params.Status,
		)
	}

	return nil
}
