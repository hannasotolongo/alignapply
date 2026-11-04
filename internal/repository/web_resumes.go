package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

var ErrResumeNotFound = errors.New("repository: resume not found")

// ListResumes returns a user's résumés, newest first, without their text.
func (r *UserJobsRepository) ListResumes(
	ctx context.Context,
	userID string,
) ([]Resume, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT id::text, user_id::text, filename, COALESCE(content_type, ''),
		        COALESCE(storage_key, ''), '', is_primary, created_at, updated_at
		   FROM resumes
		  WHERE user_id = $1
		  ORDER BY is_primary DESC, created_at DESC`,
		strings.TrimSpace(userID),
	)
	if err != nil {
		return nil, fmt.Errorf("repository: list resumes: %w", err)
	}
	defer rows.Close()

	resumes := make([]Resume, 0)
	for rows.Next() {
		var resume Resume
		if err := rows.Scan(
			&resume.ID,
			&resume.UserID,
			&resume.Filename,
			&resume.ContentType,
			&resume.StorageKey,
			&resume.ExtractedText,
			&resume.IsPrimary,
			&resume.CreatedAt,
			&resume.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("repository: scan resume: %w", err)
		}
		resumes = append(resumes, resume)
	}

	return resumes, rows.Err()
}

// GetResumeForUser returns one résumé, including text, only if the user owns it.
func (r *UserJobsRepository) GetResumeForUser(
	ctx context.Context,
	userID string,
	resumeID string,
) (Resume, error) {
	var resume Resume
	err := r.pool.QueryRow(
		ctx,
		`SELECT id::text, user_id::text, filename, COALESCE(content_type, ''),
		        COALESCE(storage_key, ''), COALESCE(extracted_text, ''),
		        is_primary, created_at, updated_at
		   FROM resumes
		  WHERE id::text = $1 AND user_id = $2`,
		strings.TrimSpace(resumeID),
		strings.TrimSpace(userID),
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
	if errors.Is(err, pgx.ErrNoRows) {
		return Resume{}, ErrResumeNotFound
	}
	if err != nil {
		return Resume{}, fmt.Errorf("repository: get resume: %w", err)
	}
	return resume, nil
}

func (r *UserJobsRepository) CountResumes(
	ctx context.Context,
	userID string,
) (int, error) {
	var count int
	err := r.pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM resumes WHERE user_id = $1`,
		strings.TrimSpace(userID),
	).Scan(&count)
	return count, err
}

// ---------------------------------------------------------------------------
// Preferences
// ---------------------------------------------------------------------------

func (r *UserJobsRepository) GetPreferences(
	ctx context.Context,
	userID string,
) (json.RawMessage, error) {
	var preferences json.RawMessage
	err := r.pool.QueryRow(
		ctx,
		`SELECT preferences FROM user_preferences WHERE user_id = $1`,
		strings.TrimSpace(userID),
	).Scan(&preferences)
	if errors.Is(err, pgx.ErrNoRows) {
		return json.RawMessage(`{}`), nil
	}
	if err != nil {
		return nil, fmt.Errorf("repository: get preferences: %w", err)
	}
	return preferences, nil
}

func (r *UserJobsRepository) SavePreferences(
	ctx context.Context,
	userID string,
	preferences json.RawMessage,
) error {
	_, err := r.pool.Exec(
		ctx,
		`INSERT INTO user_preferences (user_id, preferences)
		 VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE
		   SET preferences = EXCLUDED.preferences, updated_at = NOW()`,
		strings.TrimSpace(userID),
		preferences,
	)
	if err != nil {
		return fmt.Errorf("repository: save preferences: %w", err)
	}
	return nil
}
