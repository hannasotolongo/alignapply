package database

import (
	"context"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/hannasotolongo/casemade-backend/migrations"
)

// Runs every migration against an empty database twice: the second run must
// be a no-op. Set ALIGNAPPLY_MIGRATE_TEST_DATABASE_URL to an empty database.
func TestMigrateFreshDatabaseIsIdempotent(t *testing.T) {
	url := os.Getenv("ALIGNAPPLY_MIGRATE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ALIGNAPPLY_MIGRATE_TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := New(ctx, url)
	if err != nil {
		t.Skipf("PostgreSQL unavailable: %v", err)
	}
	defer db.Close()

	for run := 1; run <= 2; run++ {
		if err := db.Migrate(ctx, migrations.Files); err != nil {
			t.Fatalf("Migrate() run %d error = %v", run, err)
		}
	}

	var count int
	if err := db.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	files, _ := fs.Glob(migrations.Files, "*.sql")
	if count != len(files) {
		t.Fatalf("schema_migrations rows = %d, want %d", count, len(files))
	}

	var hasSubscriptions bool
	_ = db.Pool().QueryRow(ctx, `SELECT to_regclass('public.subscriptions') IS NOT NULL`).Scan(&hasSubscriptions)
	if !hasSubscriptions {
		t.Fatal("subscriptions table missing after migrate")
	}
}
