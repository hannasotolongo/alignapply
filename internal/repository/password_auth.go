package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrEmailAlreadyRegistered = errors.New(
		"repository: email already registered",
	)

	ErrPasswordUserNotFound = errors.New(
		"repository: password user not found",
	)
)

// CreatePasswordUser registers a web user. Emails are compared
// case-insensitively; the stored email keeps the user's casing.
func (r *UserCareerRepository) CreatePasswordUser(
	ctx context.Context,
	email string,
	displayName string,
	passwordHash string,
) (User, error) {
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)

	if email == "" {
		return User{}, errors.New("repository: email is required")
	}

	if strings.TrimSpace(passwordHash) == "" {
		return User{}, errors.New("repository: password hash is required")
	}

	const query = `
INSERT INTO users (email, display_name, password_hash)
VALUES ($1, NULLIF($2, ''), $3)
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
		email,
		displayName,
		passwordHash,
	).Scan(
		&user.ID,
		&user.AppleSubject,
		&user.Email,
		&user.DisplayName,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.DeletedAt,
	)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return User{}, ErrEmailAlreadyRegistered
	}

	if err != nil {
		return User{}, fmt.Errorf("repository: create password user: %w", err)
	}

	return user, nil
}

// GetPasswordUserByEmail returns the user and stored password hash.
func (r *UserCareerRepository) GetPasswordUserByEmail(
	ctx context.Context,
	email string,
) (User, string, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return User{}, "", ErrPasswordUserNotFound
	}

	const query = `
SELECT
	id::text,
	COALESCE(apple_subject, ''),
	COALESCE(email, ''),
	COALESCE(display_name, ''),
	created_at,
	updated_at,
	deleted_at,
	password_hash
FROM users
WHERE LOWER(email) = LOWER($1)
  AND password_hash IS NOT NULL
  AND deleted_at IS NULL;
`

	var (
		user         User
		passwordHash string
	)

	err := r.pool.QueryRow(ctx, query, email).Scan(
		&user.ID,
		&user.AppleSubject,
		&user.Email,
		&user.DisplayName,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.DeletedAt,
		&passwordHash,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", ErrPasswordUserNotFound
	}

	if err != nil {
		return User{}, "", fmt.Errorf("repository: get password user: %w", err)
	}

	return user, passwordHash, nil
}
