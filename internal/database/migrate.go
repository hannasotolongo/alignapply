package database

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"
)

// baselineProbes detect migrations that were applied by hand before the
// schema_migrations table existed. These migrations are not safe to re-run,
// so if their objects already exist they are recorded instead of executed.
// Every other migration is written to be re-runnable.
var baselineProbes = map[string]string{
	"001_initial_schema.sql":       `SELECT to_regclass('public.users') IS NOT NULL`,
	"002_apple_auth.sql":           `SELECT to_regclass('public.apple_auth_credentials') IS NOT NULL`,
	"003_application_tracking.sql": `SELECT to_regclass('public.application_events') IS NOT NULL`,
}

// migrationLockID is an arbitrary constant for pg_advisory_lock so that
// several instances starting at once do not migrate concurrently.
const migrationLockID = 7_311_202_611

// Migrate applies every *.sql file in files, in name order, that has not
// been recorded in schema_migrations.
func (db *DB) Migrate(ctx context.Context, files fs.FS) error {
	if db == nil || db.pool == nil {
		return errors.New("database: connection pool is not initialized")
	}

	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("database: acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("database: lock migrations: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockID)
	}()

	if _, err := conn.Exec(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    name TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`); err != nil {
		return fmt.Errorf("database: create schema_migrations: %w", err)
	}

	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return fmt.Errorf("database: list migrations: %w", err)
	}
	sort.Strings(names)

	for _, name := range names {
		var applied bool
		if err := conn.QueryRow(
			ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)`,
			name,
		).Scan(&applied); err != nil {
			return fmt.Errorf("database: check migration %s: %w", name, err)
		}
		if applied {
			continue
		}

		if probe, ok := baselineProbes[name]; ok {
			var exists bool
			if err := conn.QueryRow(ctx, probe).Scan(&exists); err != nil {
				return fmt.Errorf("database: probe migration %s: %w", name, err)
			}
			if exists {
				if _, err := conn.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
					return fmt.Errorf("database: record baseline %s: %w", name, err)
				}
				log.Printf("migration %s already present; recorded as applied", name)
				continue
			}
		}

		contents, err := fs.ReadFile(files, name)
		if err != nil {
			return fmt.Errorf("database: read migration %s: %w", name, err)
		}

		// No arguments → simple protocol, which allows multi-statement files
		// (each file manages its own BEGIN/COMMIT).
		if _, err := conn.Exec(ctx, string(contents)); err != nil {
			return fmt.Errorf("database: apply migration %s: %w", name, err)
		}

		if _, err := conn.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
			return fmt.Errorf("database: record migration %s: %w", name, err)
		}

		log.Printf("applied migration %s", strings.TrimSuffix(name, ".sql"))
	}

	return nil
}
