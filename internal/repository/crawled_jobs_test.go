package repository_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/database"
	"github.com/hannasotolongo/casemade-backend/internal/jobs"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
	"github.com/hannasotolongo/casemade-backend/migrations"
)

// Runs against an empty database: set ALIGNAPPLY_MIGRATE_TEST_DATABASE_URL.
func TestCrawledJobsRepository(t *testing.T) {
	url := os.Getenv("ALIGNAPPLY_MIGRATE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ALIGNAPPLY_MIGRATE_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.New(ctx, url)
	if err != nil {
		t.Skipf("PostgreSQL unavailable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx, migrations.Files); err != nil {
		t.Fatal(err)
	}
	repo, _ := repository.NewCrawledJobsRepository(db.Pool())

	target := jobs.Board{ATS: "workday", Slug: "target.wd5.myworkdayjobs.com/target/targetcareers", Company: "Target"}
	stripe := jobs.Board{ATS: "greenhouse", Slug: "stripe", Company: "Stripe"}
	if err := repo.SeedBoards(ctx, []jobs.Board{target, stripe}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SeedBoards(ctx, []jobs.Board{target}); err != nil { // seeding twice is fine
		t.Fatal(err)
	}
	due, err := repo.DueBoards(ctx, 10, 20*time.Hour)
	if err != nil || len(due) != 2 {
		t.Fatalf("due boards = %v, %v", due, err)
	}

	job := func(id, title string) jobs.Job {
		return jobs.Job{ID: id, Title: title, Company: "Target", Description: "Requirements: 2+ years of retail leadership.",
			Location: "Southern Pines, NC", CountryCode: "US", ApplyURL: "https://example.com/" + id}
	}
	first := []jobs.Job{job("w:1", "Store Manager"), job("w:2", "Food & Beverage Team Leader"), job("w:3", "Barista")}
	if err := repo.SaveBoardJobs(ctx, target, first, []string{"w:1", "w:2", "w:3"}, true); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordCrawl(ctx, target, "ok", 3, false); err != nil {
		t.Fatal(err)
	}
	known, _ := repo.KnownJobIDs(ctx, target)
	if len(known) != 3 {
		t.Fatalf("known = %v", known)
	}

	found, err := repo.SearchJobs(ctx, jobs.TitleSearchPattern("Retail Store Manager"), "", 50)
	if err != nil || len(found) != 1 || found[0].Title != "Store Manager" || found[0].Source != "workday" {
		t.Fatalf("search = %+v, %v", found, err)
	}

	// The next full read no longer lists the Barista job: it closes.
	if err := repo.SaveBoardJobs(ctx, target, nil, []string{"w:1", "w:2"}, true); err != nil {
		t.Fatal(err)
	}
	if found, _ := repo.SearchJobs(ctx, jobs.TitleSearchPattern("Barista"), "", 50); len(found) != 0 {
		t.Fatalf("closed job still found: %+v", found)
	}
	// A partial read never closes jobs it didn't see.
	if err := repo.SaveBoardJobs(ctx, target, nil, []string{"w:1"}, false); err != nil {
		t.Fatal(err)
	}
	if found, _ := repo.SearchJobs(ctx, jobs.TitleSearchPattern("Team Leader"), "", 50); len(found) != 1 {
		t.Fatalf("partial read closed a job: %+v", found)
	}

	// Read boards aren't due again until they age; failing boards switch off.
	due, _ = repo.DueBoards(ctx, 10, 20*time.Hour)
	if len(due) != 1 || due[0].ATS != "greenhouse" {
		t.Fatalf("due after crawl = %+v", due)
	}
	for i := 0; i < 6; i++ {
		_ = repo.RecordCrawl(ctx, stripe, "HTTP 500", 0, true)
	}
	if due, _ = repo.DueBoards(ctx, 10, 0); len(due) != 1 || due[0].ATS != "workday" {
		t.Fatalf("a board failing 6 times in a row should switch off: %+v", due)
	}

	if err := repo.PruneClosed(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if known, _ = repo.KnownJobIDs(ctx, target); len(known) != 2 {
		t.Fatalf("prune should delete only the closed job: %v", known)
	}
}
