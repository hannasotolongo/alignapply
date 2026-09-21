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

type Job struct {
	ID              string
	Source          string
	SourceJobID     string
	Title           string
	Company         string
	Description     string
	Location        string
	CountryCode     string
	WorkArrangement string
	EmploymentType  string
	SalaryMin       *float64
	SalaryMax       *float64
	SalaryCurrency  string
	SalaryPeriod    string
	PostedAt        *time.Time
	ExpiresAt       *time.Time
	IsActive        bool
	ApplyURL        string
	SourceURL       string
	Metadata        []byte
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Requirements    []JobRequirement
}

type JobRequirement struct {
	ID         string
	JobID      string
	Position   int
	Text       string
	Category   string
	Importance string
	CreatedAt  time.Time
}

type UpsertJobParams struct {
	Source          string
	SourceJobID     string
	Title           string
	Company         string
	Description     string
	Location        string
	CountryCode     string
	WorkArrangement string
	EmploymentType  string
	SalaryMin       *float64
	SalaryMax       *float64
	SalaryCurrency  string
	SalaryPeriod    string
	PostedAt        *time.Time
	ExpiresAt       *time.Time
	IsActive        bool
	ApplyURL        string
	SourceURL       string
	Metadata        []byte
	Requirements    []JobRequirementInput
}

type JobRequirementInput struct {
	Position   int
	Text       string
	Category   string
	Importance string
}

type JobRepository struct {
	pool *pgxpool.Pool
}

func NewJobRepository(pool *pgxpool.Pool) (*JobRepository, error) {
	if pool == nil {
		return nil, errors.New("repository: PostgreSQL pool is required")
	}

	return &JobRepository{pool: pool}, nil
}

// UpsertJob persists a normalized job and atomically replaces its extracted
// requirements. Jobs with a source_job_id are stable across repeated provider
// fetches. Jobs without one are inserted as new records.
func (r *JobRepository) UpsertJob(
	ctx context.Context,
	params UpsertJobParams,
) (Job, error) {
	if err := validateJobParams(params); err != nil {
		return Job{}, err
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Job{}, fmt.Errorf("repository: begin job transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	metadata := params.Metadata
	if len(metadata) == 0 {
		metadata = []byte(`{}`)
	}

	var job Job

	if strings.TrimSpace(params.SourceJobID) != "" {
		job, err = upsertJobWithSourceID(ctx, tx, params, metadata)
	} else {
		job, err = insertJobWithoutSourceID(ctx, tx, params, metadata)
	}
	if err != nil {
		return Job{}, err
	}

	if _, err := tx.Exec(
		ctx,
		`DELETE FROM job_requirements WHERE job_id = $1`,
		job.ID,
	); err != nil {
		return Job{}, fmt.Errorf("repository: replace job requirements: %w", err)
	}

	const requirementQuery = `
INSERT INTO job_requirements (
	job_id,
	position,
	text,
	category,
	importance
)
VALUES ($1, $2, $3, $4, $5)
RETURNING
	id::text,
	job_id::text,
	position,
	text,
	category,
	importance,
	created_at;
`

	job.Requirements = make([]JobRequirement, 0, len(params.Requirements))

	for _, input := range params.Requirements {
		var requirement JobRequirement

		err := tx.QueryRow(
			ctx,
			requirementQuery,
			job.ID,
			input.Position,
			strings.TrimSpace(input.Text),
			strings.TrimSpace(input.Category),
			strings.TrimSpace(input.Importance),
		).Scan(
			&requirement.ID,
			&requirement.JobID,
			&requirement.Position,
			&requirement.Text,
			&requirement.Category,
			&requirement.Importance,
			&requirement.CreatedAt,
		)
		if err != nil {
			return Job{}, fmt.Errorf("repository: insert job requirement: %w", err)
		}

		job.Requirements = append(job.Requirements, requirement)
	}

	if err := tx.Commit(ctx); err != nil {
		return Job{}, fmt.Errorf("repository: commit job transaction: %w", err)
	}

	return job, nil
}

func upsertJobWithSourceID(
	ctx context.Context,
	tx pgx.Tx,
	params UpsertJobParams,
	metadata []byte,
) (Job, error) {
	const query = `
INSERT INTO jobs (
	source,
	source_job_id,
	title,
	company,
	description,
	location,
	country_code,
	work_arrangement,
	employment_type,
	salary_min,
	salary_max,
	salary_currency,
	salary_period,
	posted_at,
	expires_at,
	is_active,
	apply_url,
	source_url,
	metadata
)
VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	NULLIF($7, ''),
	NULLIF($8, ''),
	NULLIF($9, ''),
	$10,
	$11,
	NULLIF($12, ''),
	NULLIF($13, ''),
	$14,
	$15,
	$16,
	$17,
	NULLIF($18, ''),
	$19::jsonb
)
ON CONFLICT (source, source_job_id)
WHERE source_job_id IS NOT NULL AND BTRIM(source_job_id) <> ''
DO UPDATE SET
	title = EXCLUDED.title,
	company = EXCLUDED.company,
	description = EXCLUDED.description,
	location = EXCLUDED.location,
	country_code = EXCLUDED.country_code,
	work_arrangement = EXCLUDED.work_arrangement,
	employment_type = EXCLUDED.employment_type,
	salary_min = EXCLUDED.salary_min,
	salary_max = EXCLUDED.salary_max,
	salary_currency = EXCLUDED.salary_currency,
	salary_period = EXCLUDED.salary_period,
	posted_at = EXCLUDED.posted_at,
	expires_at = EXCLUDED.expires_at,
	is_active = EXCLUDED.is_active,
	apply_url = EXCLUDED.apply_url,
	source_url = EXCLUDED.source_url,
	metadata = EXCLUDED.metadata,
	updated_at = NOW()
RETURNING
	id::text,
	source,
	COALESCE(source_job_id, ''),
	title,
	company,
	description,
	location,
	COALESCE(country_code, ''),
	COALESCE(work_arrangement, ''),
	COALESCE(employment_type, ''),
	salary_min,
	salary_max,
	COALESCE(salary_currency, ''),
	COALESCE(salary_period, ''),
	posted_at,
	expires_at,
	is_active,
	apply_url,
	COALESCE(source_url, ''),
	metadata,
	created_at,
	updated_at;
`

	job, err := scanJob(
		tx.QueryRow(
			ctx,
			query,
			strings.TrimSpace(params.Source),
			strings.TrimSpace(params.SourceJobID),
			strings.TrimSpace(params.Title),
			strings.TrimSpace(params.Company),
			strings.TrimSpace(params.Description),
			strings.TrimSpace(params.Location),
			strings.TrimSpace(params.CountryCode),
			strings.TrimSpace(params.WorkArrangement),
			strings.TrimSpace(params.EmploymentType),
			params.SalaryMin,
			params.SalaryMax,
			strings.TrimSpace(params.SalaryCurrency),
			strings.TrimSpace(params.SalaryPeriod),
			params.PostedAt,
			params.ExpiresAt,
			params.IsActive,
			strings.TrimSpace(params.ApplyURL),
			strings.TrimSpace(params.SourceURL),
			metadata,
		),
	)
	if err != nil {
		return Job{}, fmt.Errorf("repository: upsert job: %w", err)
	}

	return job, nil
}

func insertJobWithoutSourceID(
	ctx context.Context,
	tx pgx.Tx,
	params UpsertJobParams,
	metadata []byte,
) (Job, error) {
	const query = `
INSERT INTO jobs (
	source,
	source_job_id,
	title,
	company,
	description,
	location,
	country_code,
	work_arrangement,
	employment_type,
	salary_min,
	salary_max,
	salary_currency,
	salary_period,
	posted_at,
	expires_at,
	is_active,
	apply_url,
	source_url,
	metadata
)
VALUES (
	$1,
	NULL,
	$2,
	$3,
	$4,
	$5,
	NULLIF($6, ''),
	NULLIF($7, ''),
	NULLIF($8, ''),
	$9,
	$10,
	NULLIF($11, ''),
	NULLIF($12, ''),
	$13,
	$14,
	$15,
	$16,
	NULLIF($17, ''),
	$18::jsonb
)
RETURNING
	id::text,
	source,
	COALESCE(source_job_id, ''),
	title,
	company,
	description,
	location,
	COALESCE(country_code, ''),
	COALESCE(work_arrangement, ''),
	COALESCE(employment_type, ''),
	salary_min,
	salary_max,
	COALESCE(salary_currency, ''),
	COALESCE(salary_period, ''),
	posted_at,
	expires_at,
	is_active,
	apply_url,
	COALESCE(source_url, ''),
	metadata,
	created_at,
	updated_at;
`

	job, err := scanJob(
		tx.QueryRow(
			ctx,
			query,
			strings.TrimSpace(params.Source),
			strings.TrimSpace(params.Title),
			strings.TrimSpace(params.Company),
			strings.TrimSpace(params.Description),
			strings.TrimSpace(params.Location),
			strings.TrimSpace(params.CountryCode),
			strings.TrimSpace(params.WorkArrangement),
			strings.TrimSpace(params.EmploymentType),
			params.SalaryMin,
			params.SalaryMax,
			strings.TrimSpace(params.SalaryCurrency),
			strings.TrimSpace(params.SalaryPeriod),
			params.PostedAt,
			params.ExpiresAt,
			params.IsActive,
			strings.TrimSpace(params.ApplyURL),
			strings.TrimSpace(params.SourceURL),
			metadata,
		),
	)
	if err != nil {
		return Job{}, fmt.Errorf("repository: insert job: %w", err)
	}

	return job, nil
}

func (r *JobRepository) GetJobByID(
	ctx context.Context,
	jobID string,
) (Job, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return Job{}, errors.New("repository: job ID is required")
	}

	const query = `
SELECT
	id::text,
	source,
	COALESCE(source_job_id, ''),
	title,
	company,
	description,
	location,
	COALESCE(country_code, ''),
	COALESCE(work_arrangement, ''),
	COALESCE(employment_type, ''),
	salary_min,
	salary_max,
	COALESCE(salary_currency, ''),
	COALESCE(salary_period, ''),
	posted_at,
	expires_at,
	is_active,
	apply_url,
	COALESCE(source_url, ''),
	metadata,
	created_at,
	updated_at
FROM jobs
WHERE id = $1;
`

	job, err := scanJob(r.pool.QueryRow(ctx, query, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, fmt.Errorf("repository: job %s not found", jobID)
	}
	if err != nil {
		return Job{}, fmt.Errorf("repository: get job: %w", err)
	}

	requirements, err := r.getRequirements(ctx, job.ID)
	if err != nil {
		return Job{}, err
	}

	job.Requirements = requirements
	return job, nil
}

func (r *JobRepository) GetJobBySourceID(
	ctx context.Context,
	source string,
	sourceJobID string,
) (Job, error) {
	source = strings.TrimSpace(source)
	sourceJobID = strings.TrimSpace(sourceJobID)

	if source == "" {
		return Job{}, errors.New("repository: job source is required")
	}
	if sourceJobID == "" {
		return Job{}, errors.New("repository: source job ID is required")
	}

	const query = `
SELECT
	id::text,
	source,
	COALESCE(source_job_id, ''),
	title,
	company,
	description,
	location,
	COALESCE(country_code, ''),
	COALESCE(work_arrangement, ''),
	COALESCE(employment_type, ''),
	salary_min,
	salary_max,
	COALESCE(salary_currency, ''),
	COALESCE(salary_period, ''),
	posted_at,
	expires_at,
	is_active,
	apply_url,
	COALESCE(source_url, ''),
	metadata,
	created_at,
	updated_at
FROM jobs
WHERE source = $1
  AND source_job_id = $2;
`

	job, err := scanJob(r.pool.QueryRow(ctx, query, source, sourceJobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, fmt.Errorf(
			"repository: job %s/%s not found",
			source,
			sourceJobID,
		)
	}
	if err != nil {
		return Job{}, fmt.Errorf("repository: get job by source ID: %w", err)
	}

	requirements, err := r.getRequirements(ctx, job.ID)
	if err != nil {
		return Job{}, err
	}

	job.Requirements = requirements
	return job, nil
}

func (r *JobRepository) getRequirements(
	ctx context.Context,
	jobID string,
) ([]JobRequirement, error) {
	const query = `
SELECT
	id::text,
	job_id::text,
	position,
	text,
	category,
	importance,
	created_at
FROM job_requirements
WHERE job_id = $1
ORDER BY position, id;
`

	rows, err := r.pool.Query(ctx, query, jobID)
	if err != nil {
		return nil, fmt.Errorf("repository: query job requirements: %w", err)
	}
	defer rows.Close()

	requirements := make([]JobRequirement, 0)

	for rows.Next() {
		var requirement JobRequirement

		if err := rows.Scan(
			&requirement.ID,
			&requirement.JobID,
			&requirement.Position,
			&requirement.Text,
			&requirement.Category,
			&requirement.Importance,
			&requirement.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("repository: scan job requirement: %w", err)
		}

		requirements = append(requirements, requirement)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate job requirements: %w", err)
	}

	return requirements, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (Job, error) {
	var job Job

	err := row.Scan(
		&job.ID,
		&job.Source,
		&job.SourceJobID,
		&job.Title,
		&job.Company,
		&job.Description,
		&job.Location,
		&job.CountryCode,
		&job.WorkArrangement,
		&job.EmploymentType,
		&job.SalaryMin,
		&job.SalaryMax,
		&job.SalaryCurrency,
		&job.SalaryPeriod,
		&job.PostedAt,
		&job.ExpiresAt,
		&job.IsActive,
		&job.ApplyURL,
		&job.SourceURL,
		&job.Metadata,
		&job.CreatedAt,
		&job.UpdatedAt,
	)

	return job, err
}

func validateJobParams(params UpsertJobParams) error {
	if strings.TrimSpace(params.Source) == "" {
		return errors.New("repository: job source is required")
	}
	if strings.TrimSpace(params.Title) == "" {
		return errors.New("repository: job title is required")
	}
	if strings.TrimSpace(params.Company) == "" {
		return errors.New("repository: job company is required")
	}
	if params.SalaryMin != nil && *params.SalaryMin < 0 {
		return errors.New("repository: salary minimum cannot be negative")
	}
	if params.SalaryMax != nil && *params.SalaryMax < 0 {
		return errors.New("repository: salary maximum cannot be negative")
	}
	if params.SalaryMin != nil &&
		params.SalaryMax != nil &&
		*params.SalaryMax < *params.SalaryMin {
		return errors.New("repository: salary maximum cannot be less than minimum")
	}

	validCategories := map[string]struct{}{
		"skill":         {},
		"experience":    {},
		"education":     {},
		"license":       {},
		"certification": {},
		"physical":      {},
		"travel":        {},
		"other":         {},
	}

	validImportance := map[string]struct{}{
		"required":  {},
		"preferred": {},
	}

	positions := make(map[int]struct{}, len(params.Requirements))

	for i, requirement := range params.Requirements {
		category := strings.TrimSpace(requirement.Category)
		importance := strings.TrimSpace(requirement.Importance)

		if requirement.Position < 0 {
			return fmt.Errorf(
				"repository: requirement %d has negative position",
				i,
			)
		}
		if strings.TrimSpace(requirement.Text) == "" {
			return fmt.Errorf(
				"repository: requirement %d text is required",
				i,
			)
		}
		if _, ok := validCategories[category]; !ok {
			return fmt.Errorf(
				"repository: requirement %d has invalid category %q",
				i,
				category,
			)
		}
		if _, ok := validImportance[importance]; !ok {
			return fmt.Errorf(
				"repository: requirement %d has invalid importance %q",
				i,
				importance,
			)
		}
		if _, exists := positions[requirement.Position]; exists {
			return fmt.Errorf(
				"repository: duplicate requirement position %d",
				requirement.Position,
			)
		}

		positions[requirement.Position] = struct{}{}
	}

	return nil
}
