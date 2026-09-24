package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EmailConnection struct {
	ID            string
	UserID        string
	Provider      string
	ProviderEmail string
	AccessToken   string
	RefreshToken  string
	TokenExpiry   *time.Time
	Scopes        []string
	LastHistoryID *string
	LastSyncedAt  *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type SaveEmailConnectionParams struct {
	UserID        string
	Provider      string
	ProviderEmail string
	AccessToken   string
	RefreshToken  string
	TokenExpiry   *time.Time
	Scopes        []string
}

type EmailConnectionsRepository struct {
	pool *pgxpool.Pool
}

func NewEmailConnectionsRepository(
	pool *pgxpool.Pool,
) *EmailConnectionsRepository {
	return &EmailConnectionsRepository{
		pool: pool,
	}
}

func (r *EmailConnectionsRepository) Save(
	ctx context.Context,
	params SaveEmailConnectionParams,
) (*EmailConnection, error) {
	id := uuid.NewString()

	var connection EmailConnection

	err := r.pool.QueryRow(
		ctx,
		`
		INSERT INTO email_connections (
			id,
			user_id,
			provider,
			provider_email,
			access_token,
			refresh_token,
			token_expiry,
			scopes
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
		ON CONFLICT (user_id, provider)
		DO UPDATE SET
			provider_email = EXCLUDED.provider_email,
			access_token = EXCLUDED.access_token,
			refresh_token = CASE
				WHEN EXCLUDED.refresh_token <> ''
				THEN EXCLUDED.refresh_token
				ELSE email_connections.refresh_token
			END,
			token_expiry = EXCLUDED.token_expiry,
			scopes = EXCLUDED.scopes,
			updated_at = NOW()
		RETURNING
			id,
			user_id,
			provider,
			COALESCE(provider_email, ''),
			access_token,
			refresh_token,
			token_expiry,
			scopes,
			last_history_id,
			last_synced_at,
			created_at,
			updated_at
		`,
		id,
		params.UserID,
		params.Provider,
		params.ProviderEmail,
		params.AccessToken,
		params.RefreshToken,
		params.TokenExpiry,
		params.Scopes,
	).Scan(
		&connection.ID,
		&connection.UserID,
		&connection.Provider,
		&connection.ProviderEmail,
		&connection.AccessToken,
		&connection.RefreshToken,
		&connection.TokenExpiry,
		&connection.Scopes,
		&connection.LastHistoryID,
		&connection.LastSyncedAt,
		&connection.CreatedAt,
		&connection.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &connection, nil
}

func (r *EmailConnectionsRepository) GetByUserAndProvider(
	ctx context.Context,
	userID string,
	provider string,
) (*EmailConnection, error) {
	var connection EmailConnection

	err := r.pool.QueryRow(
		ctx,
		`
		SELECT
			id,
			user_id,
			provider,
			COALESCE(provider_email, ''),
			access_token,
			refresh_token,
			token_expiry,
			scopes,
			last_history_id,
			last_synced_at,
			created_at,
			updated_at
		FROM email_connections
		WHERE user_id = $1
		  AND provider = $2
		`,
		userID,
		provider,
	).Scan(
		&connection.ID,
		&connection.UserID,
		&connection.Provider,
		&connection.ProviderEmail,
		&connection.AccessToken,
		&connection.RefreshToken,
		&connection.TokenExpiry,
		&connection.Scopes,
		&connection.LastHistoryID,
		&connection.LastSyncedAt,
		&connection.CreatedAt,
		&connection.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &connection, nil
}

func (r *EmailConnectionsRepository) UpdateCredentials(
	ctx context.Context,
	userID string,
	provider string,
	accessToken string,
	refreshToken string,
	tokenExpiry *time.Time,
) error {
	_, err := r.pool.Exec(
		ctx,
		`
		UPDATE email_connections
		SET
			access_token = $3,
			refresh_token = CASE
				WHEN $4 <> '' THEN $4
				ELSE refresh_token
			END,
			token_expiry = $5,
			updated_at = NOW()
		WHERE user_id = $1
		  AND provider = $2
		`,
		userID,
		provider,
		accessToken,
		refreshToken,
		tokenExpiry,
	)

	return err
}

func (r *EmailConnectionsRepository) UpdateSyncMetadata(
	ctx context.Context,
	userID string,
	provider string,
	providerEmail string,
	historyID string,
) error {
	_, err := r.pool.Exec(
		ctx,
		`
		UPDATE email_connections
		SET
			provider_email = CASE
				WHEN $3 <> '' THEN $3
				ELSE provider_email
			END,
			last_history_id = CASE
				WHEN $4 <> '' THEN $4
				ELSE last_history_id
			END,
			last_synced_at = NOW(),
			updated_at = NOW()
		WHERE user_id = $1
		  AND provider = $2
		`,
		userID,
		provider,
		providerEmail,
		historyID,
	)

	return err
}

func (r *EmailConnectionsRepository) Delete(
	ctx context.Context,
	userID string,
	provider string,
) error {
	_, err := r.pool.Exec(
		ctx,
		`
		DELETE FROM email_connections
		WHERE user_id = $1
		  AND provider = $2
		`,
		userID,
		provider,
	)

	return err
}
