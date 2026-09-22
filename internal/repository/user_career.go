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

var ErrCareerProfileNotFound = errors.New(
	"repository: career profile not found",
)

type User struct {
	ID           string
	AppleSubject string
	Email        string
	DisplayName  string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
}

type CareerProfile struct {
	ID         string
	UserID     string
	Headline   string
	Summary    string
	TargetRole string
	Location   string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Evidence   []CareerProfileEvidence
}

type CareerProfileEvidence struct {
	ID              string
	CareerProfileID string
	Category        string
	EntryIndex      int
	Text            string
	Source          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type UpsertCareerProfileParams struct {
	UserID     string
	Headline   string
	Summary    string
	TargetRole string
	Location   string
	Evidence   []CareerProfileEvidenceInput
}

type CareerProfileEvidenceInput struct {
	Category   string
	EntryIndex int
	Text       string
	Source     string
}

type UserCareerRepository struct {
	pool *pgxpool.Pool
}

func NewUserCareerRepository(pool *pgxpool.Pool) (*UserCareerRepository, error) {
	if pool == nil {
		return nil, errors.New("repository: PostgreSQL pool is required")
	}

	return &UserCareerRepository{pool: pool}, nil
}

// UpsertAppleUser creates a user for an Apple subject or updates the user's
// latest email/display name. Empty optional values do not erase existing data.
func (r *UserCareerRepository) UpsertAppleUser(
	ctx context.Context,
	appleSubject string,
	email string,
	displayName string,
) (User, error) {
	appleSubject = strings.TrimSpace(appleSubject)
	if appleSubject == "" {
		return User{}, errors.New("repository: Apple subject is required")
	}

	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)

	const query = `
INSERT INTO users (apple_subject, email, display_name)
VALUES ($1, NULLIF($2, ''), NULLIF($3, ''))
ON CONFLICT (apple_subject) DO UPDATE SET
	email = COALESCE(NULLIF(EXCLUDED.email, ''), users.email),
	display_name = COALESCE(NULLIF(EXCLUDED.display_name, ''), users.display_name),
	updated_at = NOW(),
	deleted_at = NULL
RETURNING
	id::text,
	COALESCE(apple_subject, ''),
	COALESCE(email, ''),
	COALESCE(display_name, ''),
	created_at,
	updated_at,
	deleted_at;
`

	var user User
	err := r.pool.QueryRow(
		ctx,
		query,
		appleSubject,
		email,
		displayName,
	).Scan(
		&user.ID,
		&user.AppleSubject,
		&user.Email,
		&user.DisplayName,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.DeletedAt,
	)
	if err != nil {
		return User{}, fmt.Errorf("repository: upsert Apple user: %w", err)
	}

	return user, nil
}

func (r *UserCareerRepository) GetUserByID(
	ctx context.Context,
	userID string,
) (User, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return User{}, errors.New("repository: user ID is required")
	}

	const query = `
SELECT
	id::text,
	COALESCE(apple_subject, ''),
	COALESCE(email, ''),
	COALESCE(display_name, ''),
	created_at,
	updated_at,
	deleted_at
FROM users
WHERE id = $1
  AND deleted_at IS NULL;
`

	var user User
	err := r.pool.QueryRow(ctx, query, userID).Scan(
		&user.ID,
		&user.AppleSubject,
		&user.Email,
		&user.DisplayName,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.DeletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, fmt.Errorf("repository: user %s not found", userID)
	}
	if err != nil {
		return User{}, fmt.Errorf("repository: get user: %w", err)
	}

	return user, nil
}

// UpsertCareerProfile replaces the profile's structured evidence atomically.
// A failure leaves the previously saved profile/evidence unchanged.
func (r *UserCareerRepository) UpsertCareerProfile(
	ctx context.Context,
	params UpsertCareerProfileParams,
) (CareerProfile, error) {
	params.UserID = strings.TrimSpace(params.UserID)
	if params.UserID == "" {
		return CareerProfile{}, errors.New("repository: user ID is required")
	}

	if err := validateEvidenceInputs(params.Evidence); err != nil {
		return CareerProfile{}, err
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CareerProfile{}, fmt.Errorf("repository: begin career profile transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	const profileQuery = `
INSERT INTO career_profiles (
	user_id,
	headline,
	summary,
	target_role,
	location
)
VALUES (
	$1,
	NULLIF($2, ''),
	NULLIF($3, ''),
	NULLIF($4, ''),
	NULLIF($5, '')
)
ON CONFLICT (user_id) DO UPDATE SET
	headline = EXCLUDED.headline,
	summary = EXCLUDED.summary,
	target_role = EXCLUDED.target_role,
	location = EXCLUDED.location,
	updated_at = NOW()
RETURNING
	id::text,
	user_id::text,
	COALESCE(headline, ''),
	COALESCE(summary, ''),
	COALESCE(target_role, ''),
	COALESCE(location, ''),
	created_at,
	updated_at;
`

	var profile CareerProfile
	err = tx.QueryRow(
		ctx,
		profileQuery,
		params.UserID,
		strings.TrimSpace(params.Headline),
		strings.TrimSpace(params.Summary),
		strings.TrimSpace(params.TargetRole),
		strings.TrimSpace(params.Location),
	).Scan(
		&profile.ID,
		&profile.UserID,
		&profile.Headline,
		&profile.Summary,
		&profile.TargetRole,
		&profile.Location,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)
	if err != nil {
		return CareerProfile{}, fmt.Errorf("repository: upsert career profile: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`DELETE FROM career_profile_evidence WHERE career_profile_id = $1`,
		profile.ID,
	); err != nil {
		return CareerProfile{}, fmt.Errorf("repository: replace career profile evidence: %w", err)
	}

	const evidenceQuery = `
INSERT INTO career_profile_evidence (
	career_profile_id,
	category,
	entry_index,
	text,
	source
)
VALUES ($1, $2, $3, $4, $5)
RETURNING
	id::text,
	career_profile_id::text,
	category,
	entry_index,
	text,
	source,
	created_at,
	updated_at;
`

	profile.Evidence = make([]CareerProfileEvidence, 0, len(params.Evidence))

	for _, input := range params.Evidence {
		var item CareerProfileEvidence
		err := tx.QueryRow(
			ctx,
			evidenceQuery,
			profile.ID,
			strings.TrimSpace(input.Category),
			input.EntryIndex,
			strings.TrimSpace(input.Text),
			strings.TrimSpace(input.Source),
		).Scan(
			&item.ID,
			&item.CareerProfileID,
			&item.Category,
			&item.EntryIndex,
			&item.Text,
			&item.Source,
			&item.CreatedAt,
			&item.UpdatedAt,
		)
		if err != nil {
			return CareerProfile{}, fmt.Errorf("repository: insert career profile evidence: %w", err)
		}

		profile.Evidence = append(profile.Evidence, item)
	}

	if err := tx.Commit(ctx); err != nil {
		return CareerProfile{}, fmt.Errorf("repository: commit career profile transaction: %w", err)
	}

	return profile, nil
}

func (r *UserCareerRepository) GetCareerProfileByUserID(
	ctx context.Context,
	userID string,
) (CareerProfile, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return CareerProfile{}, errors.New("repository: user ID is required")
	}

	const profileQuery = `
SELECT
	id::text,
	user_id::text,
	COALESCE(headline, ''),
	COALESCE(summary, ''),
	COALESCE(target_role, ''),
	COALESCE(location, ''),
	created_at,
	updated_at
FROM career_profiles
WHERE user_id = $1;
`

	var profile CareerProfile
	err := r.pool.QueryRow(ctx, profileQuery, userID).Scan(
		&profile.ID,
		&profile.UserID,
		&profile.Headline,
		&profile.Summary,
		&profile.TargetRole,
		&profile.Location,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CareerProfile{}, fmt.Errorf(
			"%w: user %s",
			ErrCareerProfileNotFound,
			userID,
		)
	}
	if err != nil {
		return CareerProfile{}, fmt.Errorf("repository: get career profile: %w", err)
	}

	const evidenceQuery = `
SELECT
	id::text,
	career_profile_id::text,
	category,
	entry_index,
	text,
	source,
	created_at,
	updated_at
FROM career_profile_evidence
WHERE career_profile_id = $1
ORDER BY category, entry_index, created_at, id;
`

	rows, err := r.pool.Query(ctx, evidenceQuery, profile.ID)
	if err != nil {
		return CareerProfile{}, fmt.Errorf("repository: query career profile evidence: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item CareerProfileEvidence
		if err := rows.Scan(
			&item.ID,
			&item.CareerProfileID,
			&item.Category,
			&item.EntryIndex,
			&item.Text,
			&item.Source,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return CareerProfile{}, fmt.Errorf("repository: scan career profile evidence: %w", err)
		}

		profile.Evidence = append(profile.Evidence, item)
	}

	if err := rows.Err(); err != nil {
		return CareerProfile{}, fmt.Errorf("repository: iterate career profile evidence: %w", err)
	}

	return profile, nil
}
func (r *UserCareerRepository) DeleteUser(
	ctx context.Context,
	userID string,
) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return errors.New("repository: user ID is required")
	}

	const query = `
DELETE FROM users
WHERE id = $1
  AND deleted_at IS NULL;
`

	result, err := r.pool.Exec(
		ctx,
		query,
		userID,
	)
	if err != nil {
		return fmt.Errorf(
			"repository: delete user: %w",
			err,
		)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf(
			"repository: user %s not found",
			userID,
		)
	}

	return nil
}
func validateEvidenceInputs(evidence []CareerProfileEvidenceInput) error {
	validCategories := map[string]struct{}{
		"skill":         {},
		"experience":    {},
		"education":     {},
		"certification": {},
		"license":       {},
		"project":       {},
		"summary":       {},
	}

	seen := make(map[string]struct{}, len(evidence))

	for i, item := range evidence {
		category := strings.TrimSpace(item.Category)
		text := strings.TrimSpace(item.Text)
		source := strings.TrimSpace(item.Source)

		if _, ok := validCategories[category]; !ok {
			return fmt.Errorf(
				"repository: evidence %d has invalid category %q",
				i,
				category,
			)
		}

		if item.EntryIndex < 0 {
			return fmt.Errorf(
				"repository: evidence %d has negative entry index",
				i,
			)
		}

		if text == "" {
			return fmt.Errorf(
				"repository: evidence %d text is required",
				i,
			)
		}

		if source == "" {
			return fmt.Errorf(
				"repository: evidence %d source is required",
				i,
			)
		}

		key := fmt.Sprintf("%s\x00%d\x00%s", category, item.EntryIndex, text)
		if _, exists := seen[key]; exists {
			return fmt.Errorf(
				"repository: duplicate evidence at index %d",
				i,
			)
		}
		seen[key] = struct{}{}
	}

	return nil
}
