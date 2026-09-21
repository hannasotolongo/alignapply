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

type MatchResult struct {
	ID              string
	UserID          string
	JobID           string
	CareerProfileID *string
	ResumeID        *string
	MatchPercentage int
	MatchLevel      string
	Explanation     string
	MatcherVersion  string
	CreatedAt       time.Time
	Requirements    []RequirementMatchResult
}

type RequirementMatchResult struct {
	ID               string
	MatchResultID    string
	JobRequirementID string
	Status           string
	Score            float64
	CreatedAt        time.Time
	Evidence         []RequirementMatchEvidence
}

type RequirementMatchEvidence struct {
	ID                       string
	RequirementMatchResultID string
	CareerProfileEvidenceID  *string
	ResumeID                 *string
	EvidenceText             string
	EvidenceCategory         string
	EvidenceSource           string
	RetrievalRank            int
	CreatedAt                time.Time
}

type CreateMatchResultParams struct {
	UserID          string
	JobID           string
	CareerProfileID *string
	ResumeID        *string
	MatchPercentage int
	MatchLevel      string
	Explanation     string
	MatcherVersion  string
	Requirements    []RequirementMatchInput
}

type RequirementMatchInput struct {
	JobRequirementID string
	Status           string
	Score            float64
	Evidence         []RequirementMatchEvidenceInput
}

type RequirementMatchEvidenceInput struct {
	CareerProfileEvidenceID *string
	ResumeID                *string
	EvidenceText            string
	EvidenceCategory        string
	EvidenceSource          string
	RetrievalRank           int
}

type MatchRepository struct {
	pool *pgxpool.Pool
}

func NewMatchRepository(pool *pgxpool.Pool) (*MatchRepository, error) {
	if pool == nil {
		return nil, errors.New("repository: PostgreSQL pool is required")
	}
	return &MatchRepository{pool: pool}, nil
}

// CreateMatchResult stores one immutable matching run together with its
// requirement-level decisions and the exact evidence used for those decisions.
// The entire write is transactional so provenance can never be partially saved.
func (r *MatchRepository) CreateMatchResult(
	ctx context.Context,
	params CreateMatchResultParams,
) (MatchResult, error) {
	if err := validateMatchResultParams(params); err != nil {
		return MatchResult{}, err
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return MatchResult{}, fmt.Errorf("repository: begin match transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	const matchQuery = `
INSERT INTO match_results (
	user_id,
	job_id,
	career_profile_id,
	resume_id,
	match_percentage,
	match_level,
	explanation,
	matcher_version
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING
	id::text,
	user_id::text,
	job_id::text,
	career_profile_id::text,
	resume_id::text,
	match_percentage,
	match_level,
	explanation,
	matcher_version,
	created_at;
`

	var result MatchResult
	err = tx.QueryRow(
		ctx,
		matchQuery,
		strings.TrimSpace(params.UserID),
		strings.TrimSpace(params.JobID),
		nullableStringPointer(params.CareerProfileID),
		nullableStringPointer(params.ResumeID),
		params.MatchPercentage,
		strings.TrimSpace(params.MatchLevel),
		strings.TrimSpace(params.Explanation),
		strings.TrimSpace(params.MatcherVersion),
	).Scan(
		&result.ID,
		&result.UserID,
		&result.JobID,
		&result.CareerProfileID,
		&result.ResumeID,
		&result.MatchPercentage,
		&result.MatchLevel,
		&result.Explanation,
		&result.MatcherVersion,
		&result.CreatedAt,
	)
	if err != nil {
		return MatchResult{}, fmt.Errorf("repository: insert match result: %w", err)
	}

	result.Requirements = make([]RequirementMatchResult, 0, len(params.Requirements))

	for _, requirementInput := range params.Requirements {
		requirement, err := insertRequirementMatch(
			ctx,
			tx,
			result.ID,
			requirementInput,
		)
		if err != nil {
			return MatchResult{}, err
		}

		result.Requirements = append(result.Requirements, requirement)
	}

	if err := tx.Commit(ctx); err != nil {
		return MatchResult{}, fmt.Errorf("repository: commit match transaction: %w", err)
	}

	return result, nil
}

func insertRequirementMatch(
	ctx context.Context,
	tx pgx.Tx,
	matchResultID string,
	input RequirementMatchInput,
) (RequirementMatchResult, error) {
	const query = `
INSERT INTO requirement_match_results (
	match_result_id,
	job_requirement_id,
	status,
	score
)
VALUES ($1, $2, $3, $4)
RETURNING
	id::text,
	match_result_id::text,
	job_requirement_id::text,
	status,
	score::float8,
	created_at;
`

	var result RequirementMatchResult
	err := tx.QueryRow(
		ctx,
		query,
		matchResultID,
		strings.TrimSpace(input.JobRequirementID),
		strings.TrimSpace(input.Status),
		input.Score,
	).Scan(
		&result.ID,
		&result.MatchResultID,
		&result.JobRequirementID,
		&result.Status,
		&result.Score,
		&result.CreatedAt,
	)
	if err != nil {
		return RequirementMatchResult{}, fmt.Errorf(
			"repository: insert requirement match result: %w",
			err,
		)
	}

	result.Evidence = make(
		[]RequirementMatchEvidence,
		0,
		len(input.Evidence),
	)

	for _, evidenceInput := range input.Evidence {
		evidence, err := insertRequirementEvidence(
			ctx,
			tx,
			result.ID,
			evidenceInput,
		)
		if err != nil {
			return RequirementMatchResult{}, err
		}

		result.Evidence = append(result.Evidence, evidence)
	}

	return result, nil
}

func insertRequirementEvidence(
	ctx context.Context,
	tx pgx.Tx,
	requirementMatchResultID string,
	input RequirementMatchEvidenceInput,
) (RequirementMatchEvidence, error) {
	const query = `
INSERT INTO requirement_match_evidence (
	requirement_match_result_id,
	career_profile_evidence_id,
	resume_id,
	evidence_text,
	evidence_category,
	evidence_source,
	retrieval_rank
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING
	id::text,
	requirement_match_result_id::text,
	career_profile_evidence_id::text,
	resume_id::text,
	evidence_text,
	evidence_category,
	evidence_source,
	retrieval_rank,
	created_at;
`

	var result RequirementMatchEvidence
	err := tx.QueryRow(
		ctx,
		query,
		requirementMatchResultID,
		nullableStringPointer(input.CareerProfileEvidenceID),
		nullableStringPointer(input.ResumeID),
		strings.TrimSpace(input.EvidenceText),
		strings.TrimSpace(input.EvidenceCategory),
		strings.TrimSpace(input.EvidenceSource),
		input.RetrievalRank,
	).Scan(
		&result.ID,
		&result.RequirementMatchResultID,
		&result.CareerProfileEvidenceID,
		&result.ResumeID,
		&result.EvidenceText,
		&result.EvidenceCategory,
		&result.EvidenceSource,
		&result.RetrievalRank,
		&result.CreatedAt,
	)
	if err != nil {
		return RequirementMatchEvidence{}, fmt.Errorf(
			"repository: insert requirement match evidence: %w",
			err,
		)
	}

	return result, nil
}

func (r *MatchRepository) GetMatchResultByID(
	ctx context.Context,
	matchResultID string,
) (MatchResult, error) {
	matchResultID = strings.TrimSpace(matchResultID)
	if matchResultID == "" {
		return MatchResult{}, errors.New("repository: match result ID is required")
	}

	const query = `
SELECT
	id::text,
	user_id::text,
	job_id::text,
	career_profile_id::text,
	resume_id::text,
	match_percentage,
	match_level,
	explanation,
	matcher_version,
	created_at
FROM match_results
WHERE id = $1;
`

	result, err := scanMatchResult(
		r.pool.QueryRow(ctx, query, matchResultID),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return MatchResult{}, fmt.Errorf(
			"repository: match result %s not found",
			matchResultID,
		)
	}
	if err != nil {
		return MatchResult{}, fmt.Errorf("repository: get match result: %w", err)
	}

	requirements, err := r.getRequirementMatches(ctx, result.ID)
	if err != nil {
		return MatchResult{}, err
	}

	result.Requirements = requirements
	return result, nil
}

func (r *MatchRepository) GetLatestMatchForUserJob(
	ctx context.Context,
	userID string,
	jobID string,
) (MatchResult, error) {
	userID = strings.TrimSpace(userID)
	jobID = strings.TrimSpace(jobID)

	if userID == "" {
		return MatchResult{}, errors.New("repository: user ID is required")
	}
	if jobID == "" {
		return MatchResult{}, errors.New("repository: job ID is required")
	}

	const query = `
SELECT
	id::text,
	user_id::text,
	job_id::text,
	career_profile_id::text,
	resume_id::text,
	match_percentage,
	match_level,
	explanation,
	matcher_version,
	created_at
FROM match_results
WHERE user_id = $1
  AND job_id = $2
ORDER BY created_at DESC, id DESC
LIMIT 1;
`

	result, err := scanMatchResult(
		r.pool.QueryRow(ctx, query, userID, jobID),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return MatchResult{}, fmt.Errorf(
			"repository: no match result for user %s and job %s",
			userID,
			jobID,
		)
	}
	if err != nil {
		return MatchResult{}, fmt.Errorf(
			"repository: get latest user job match: %w",
			err,
		)
	}

	requirements, err := r.getRequirementMatches(ctx, result.ID)
	if err != nil {
		return MatchResult{}, err
	}

	result.Requirements = requirements
	return result, nil
}

func (r *MatchRepository) getRequirementMatches(
	ctx context.Context,
	matchResultID string,
) ([]RequirementMatchResult, error) {
	const query = `
SELECT
	id::text,
	match_result_id::text,
	job_requirement_id::text,
	status,
	score::float8,
	created_at
FROM requirement_match_results
WHERE match_result_id = $1
ORDER BY created_at, id;
`

	rows, err := r.pool.Query(ctx, query, matchResultID)
	if err != nil {
		return nil, fmt.Errorf(
			"repository: query requirement match results: %w",
			err,
		)
	}
	defer rows.Close()

	results := make([]RequirementMatchResult, 0)

	for rows.Next() {
		var result RequirementMatchResult
		if err := rows.Scan(
			&result.ID,
			&result.MatchResultID,
			&result.JobRequirementID,
			&result.Status,
			&result.Score,
			&result.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf(
				"repository: scan requirement match result: %w",
				err,
			)
		}

		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"repository: iterate requirement match results: %w",
			err,
		)
	}

	for i := range results {
		evidence, err := r.getRequirementEvidence(ctx, results[i].ID)
		if err != nil {
			return nil, err
		}
		results[i].Evidence = evidence
	}

	return results, nil
}

func (r *MatchRepository) getRequirementEvidence(
	ctx context.Context,
	requirementMatchResultID string,
) ([]RequirementMatchEvidence, error) {
	const query = `
SELECT
	id::text,
	requirement_match_result_id::text,
	career_profile_evidence_id::text,
	resume_id::text,
	evidence_text,
	evidence_category,
	evidence_source,
	retrieval_rank,
	created_at
FROM requirement_match_evidence
WHERE requirement_match_result_id = $1
ORDER BY retrieval_rank, created_at, id;
`

	rows, err := r.pool.Query(ctx, query, requirementMatchResultID)
	if err != nil {
		return nil, fmt.Errorf(
			"repository: query requirement match evidence: %w",
			err,
		)
	}
	defer rows.Close()

	evidence := make([]RequirementMatchEvidence, 0)

	for rows.Next() {
		var item RequirementMatchEvidence
		if err := rows.Scan(
			&item.ID,
			&item.RequirementMatchResultID,
			&item.CareerProfileEvidenceID,
			&item.ResumeID,
			&item.EvidenceText,
			&item.EvidenceCategory,
			&item.EvidenceSource,
			&item.RetrievalRank,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf(
				"repository: scan requirement match evidence: %w",
				err,
			)
		}

		evidence = append(evidence, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"repository: iterate requirement match evidence: %w",
			err,
		)
	}

	return evidence, nil
}

func scanMatchResult(row rowScanner) (MatchResult, error) {
	var result MatchResult

	err := row.Scan(
		&result.ID,
		&result.UserID,
		&result.JobID,
		&result.CareerProfileID,
		&result.ResumeID,
		&result.MatchPercentage,
		&result.MatchLevel,
		&result.Explanation,
		&result.MatcherVersion,
		&result.CreatedAt,
	)

	return result, err
}

func validateMatchResultParams(params CreateMatchResultParams) error {
	if strings.TrimSpace(params.UserID) == "" {
		return errors.New("repository: match user ID is required")
	}
	if strings.TrimSpace(params.JobID) == "" {
		return errors.New("repository: match job ID is required")
	}
	if params.MatchPercentage < 0 || params.MatchPercentage > 100 {
		return errors.New(
			"repository: match percentage must be between 0 and 100",
		)
	}

	validLevels := map[string]struct{}{
		"Best Fit":              {},
		"Good Fit":              {},
		"Reach":                 {},
		"Insufficient Evidence": {},
	}
	if _, ok := validLevels[strings.TrimSpace(params.MatchLevel)]; !ok {
		return fmt.Errorf(
			"repository: invalid match level %q",
			params.MatchLevel,
		)
	}

	if strings.TrimSpace(params.MatcherVersion) == "" {
		return errors.New("repository: matcher version is required")
	}

	seenRequirements := make(map[string]struct{}, len(params.Requirements))

	for i, requirement := range params.Requirements {
		requirementID := strings.TrimSpace(requirement.JobRequirementID)
		if requirementID == "" {
			return fmt.Errorf(
				"repository: requirement match %d job requirement ID is required",
				i,
			)
		}
		if _, exists := seenRequirements[requirementID]; exists {
			return fmt.Errorf(
				"repository: duplicate job requirement ID %q",
				requirementID,
			)
		}
		seenRequirements[requirementID] = struct{}{}

		switch strings.TrimSpace(requirement.Status) {
		case "supported", "partial", "missing":
		default:
			return fmt.Errorf(
				"repository: requirement match %d has invalid status %q",
				i,
				requirement.Status,
			)
		}

		if requirement.Score < 0 || requirement.Score > 1 {
			return fmt.Errorf(
				"repository: requirement match %d score must be between 0 and 1",
				i,
			)
		}

		seenRanks := make(map[int]struct{}, len(requirement.Evidence))
		for j, evidence := range requirement.Evidence {
			if strings.TrimSpace(evidence.EvidenceText) == "" {
				return fmt.Errorf(
					"repository: requirement match %d evidence %d text is required",
					i,
					j,
				)
			}
			if strings.TrimSpace(evidence.EvidenceCategory) == "" {
				return fmt.Errorf(
					"repository: requirement match %d evidence %d category is required",
					i,
					j,
				)
			}
			if strings.TrimSpace(evidence.EvidenceSource) == "" {
				return fmt.Errorf(
					"repository: requirement match %d evidence %d source is required",
					i,
					j,
				)
			}
			if evidence.RetrievalRank < 0 {
				return fmt.Errorf(
					"repository: requirement match %d evidence %d retrieval rank cannot be negative",
					i,
					j,
				)
			}
			if _, exists := seenRanks[evidence.RetrievalRank]; exists {
				return fmt.Errorf(
					"repository: requirement match %d has duplicate evidence retrieval rank %d",
					i,
					evidence.RetrievalRank,
				)
			}
			seenRanks[evidence.RetrievalRank] = struct{}{}
		}
	}

	return nil
}

func nullableStringPointer(value *string) any {
	if value == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}

	return trimmed
}
