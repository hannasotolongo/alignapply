package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultMaxConns        = int32(10)
	defaultMinConns        = int32(1)
	defaultMaxConnLifetime = time.Hour
	defaultMaxConnIdleTime = 30 * time.Minute
	defaultHealthTimeout   = 5 * time.Second
)

// DB owns the PostgreSQL connection pool used by AlignApply.
type DB struct {
	pool *pgxpool.Pool
}

// New creates a PostgreSQL connection pool and verifies the database is reachable.
func New(ctx context.Context, databaseURL string) (*DB, error) {
	if ctx == nil {
		return nil, errors.New("database: context is required")
	}

	if databaseURL == "" {
		return nil, errors.New("database: DATABASE_URL is required")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("database: parse DATABASE_URL: %w", err)
	}

	config.MaxConns = defaultMaxConns
	config.MinConns = defaultMinConns
	config.MaxConnLifetime = defaultMaxConnLifetime
	config.MaxConnIdleTime = defaultMaxConnIdleTime

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("database: create connection pool: %w", err)
	}

	db := &DB{pool: pool}

	healthCtx, cancel := context.WithTimeout(ctx, defaultHealthTimeout)
	defer cancel()

	if err := db.Ping(healthCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: connect: %w", err)
	}

	return db, nil
}

// Pool exposes the connection pool to repository implementations.
func (db *DB) Pool() *pgxpool.Pool {
	if db == nil {
		return nil
	}
	return db.pool
}

// Ping verifies that PostgreSQL is reachable.
func (db *DB) Ping(ctx context.Context) error {
	if db == nil || db.pool == nil {
		return errors.New("database: connection pool is not initialized")
	}

	if ctx == nil {
		return errors.New("database: context is required")
	}

	if err := db.pool.Ping(ctx); err != nil {
		return fmt.Errorf("database: ping: %w", err)
	}

	return nil
}

// Close releases all PostgreSQL connections.
func (db *DB) Close() {
	if db == nil || db.pool == nil {
		return
	}

	db.pool.Close()
}
