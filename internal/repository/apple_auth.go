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

var (
	ErrAppleAuthCredentialNotFound = errors.New(
		"Apple auth credential not found",
	)

	ErrInvalidAppleAuthCredential = errors.New(
		"invalid Apple auth credential",
	)
)

type AppleAuthCredential struct {
	UserID       string
	RefreshToken string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type AppleAuthRepository struct {
	pool *pgxpool.Pool
}

func NewAppleAuthRepository(
	pool *pgxpool.Pool,
) (*AppleAuthRepository, error) {
	if pool == nil {
		return nil, errors.New(
			"Apple auth repository requires a database pool",
		)
	}

	return &AppleAuthRepository{
		pool: pool,
	}, nil
}

func (r *AppleAuthRepository) UpsertRefreshToken(
	ctx context.Context,
	userID string,
	refreshToken string,
) (AppleAuthCredential, error) {
	if r == nil || r.pool == nil {
		return AppleAuthCredential{}, errors.New(
			"Apple auth repository is unavailable",
		)
	}

	userID = strings.TrimSpace(userID)
	refreshToken = strings.TrimSpace(refreshToken)

	if userID == "" || refreshToken == "" {
		return AppleAuthCredential{},
			ErrInvalidAppleAuthCredential
	}

	const query = `
		INSERT INTO apple_auth_credentials (
			user_id,
			refresh_token
		)
		VALUES ($1, $2)
		ON CONFLICT (user_id)
		DO UPDATE SET
			refresh_token = EXCLUDED.refresh_token,
			updated_at = NOW()
		RETURNING
			user_id,
			refresh_token,
			created_at,
			updated_at
	`

	var credential AppleAuthCredential

	err := r.pool.QueryRow(
		ctx,
		query,
		userID,
		refreshToken,
	).Scan(
		&credential.UserID,
		&credential.RefreshToken,
		&credential.CreatedAt,
		&credential.UpdatedAt,
	)

	if err != nil {
		return AppleAuthCredential{},
			fmt.Errorf(
				"upsert Apple refresh token: %w",
				err,
			)
	}

	return credential, nil
}

func (r *AppleAuthRepository) GetByUserID(
	ctx context.Context,
	userID string,
) (AppleAuthCredential, error) {
	if r == nil || r.pool == nil {
		return AppleAuthCredential{}, errors.New(
			"Apple auth repository is unavailable",
		)
	}

	userID = strings.TrimSpace(userID)
	if userID == "" {
		return AppleAuthCredential{},
			ErrInvalidAppleAuthCredential
	}

	const query = `
		SELECT
			user_id,
			refresh_token,
			created_at,
			updated_at
		FROM apple_auth_credentials
		WHERE user_id = $1
	`

	var credential AppleAuthCredential

	err := r.pool.QueryRow(
		ctx,
		query,
		userID,
	).Scan(
		&credential.UserID,
		&credential.RefreshToken,
		&credential.CreatedAt,
		&credential.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AppleAuthCredential{},
				ErrAppleAuthCredentialNotFound
		}

		return AppleAuthCredential{},
			fmt.Errorf(
				"get Apple auth credential: %w",
				err,
			)
	}

	return credential, nil
}

func (r *AppleAuthRepository) DeleteByUserID(
	ctx context.Context,
	userID string,
) error {
	if r == nil || r.pool == nil {
		return errors.New(
			"Apple auth repository is unavailable",
		)
	}

	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ErrInvalidAppleAuthCredential
	}

	const query = `
		DELETE FROM apple_auth_credentials
		WHERE user_id = $1
	`

	_, err := r.pool.Exec(
		ctx,
		query,
		userID,
	)

	if err != nil {
		return fmt.Errorf(
			"delete Apple auth credential: %w",
			err,
		)
	}

	return nil
}
