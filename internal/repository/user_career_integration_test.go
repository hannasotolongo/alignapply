package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultTestDatabaseURL = "postgres://localhost:5432/alignapply?sslmode=disable"

func TestUserCareerRepositoryIntegration(t *testing.T) {
	databaseURL := os.Getenv("ALIGNAPPLY_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = defaultTestDatabaseURL
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Skipf("PostgreSQL unavailable: %v", err)
	}

	// Register this cleanup first. Go runs test cleanups in reverse order,
	// so database records registered for cleanup later are deleted before
	// the connection pool is closed.
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("PostgreSQL unavailable: %v", err)
	}

	repo, err := NewUserCareerRepository(pool)
	if err != nil {
		t.Fatalf("NewUserCareerRepository() error = %v", err)
	}

	appleSubject := fmt.Sprintf(
		"alignapply-integration-%d",
		time.Now().UnixNano(),
	)

	user, err := repo.UpsertAppleUser(
		ctx,
		appleSubject,
		"integration@example.com",
		"Integration User",
	)
	if err != nil {
		t.Fatalf("UpsertAppleUser() error = %v", err)
	}

	// This cleanup runs before pool.Close because it was registered later.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cleanupCancel()

		if _, cleanupErr := pool.Exec(
			cleanupCtx,
			`DELETE FROM users WHERE id = $1`,
			user.ID,
		); cleanupErr != nil {
			t.Logf("cleanup user: %v", cleanupErr)
		}
	})

	if user.ID == "" {
		t.Fatal("UpsertAppleUser() returned empty user ID")
	}

	if user.AppleSubject != appleSubject {
		t.Fatalf(
			"UpsertAppleUser() AppleSubject = %q, want %q",
			user.AppleSubject,
			appleSubject,
		)
	}

	if user.Email != "integration@example.com" {
		t.Fatalf(
			"UpsertAppleUser() Email = %q, want %q",
			user.Email,
			"integration@example.com",
		)
	}

	if user.DisplayName != "Integration User" {
		t.Fatalf(
			"UpsertAppleUser() DisplayName = %q, want %q",
			user.DisplayName,
			"Integration User",
		)
	}

	updatedUser, err := repo.UpsertAppleUser(
		ctx,
		appleSubject,
		"",
		"Updated Integration User",
	)
	if err != nil {
		t.Fatalf("second UpsertAppleUser() error = %v", err)
	}

	if updatedUser.ID != user.ID {
		t.Fatalf(
			"second UpsertAppleUser() ID = %q, want existing ID %q",
			updatedUser.ID,
			user.ID,
		)
	}

	if updatedUser.Email != "integration@example.com" {
		t.Fatalf(
			"second UpsertAppleUser() erased email: got %q",
			updatedUser.Email,
		)
	}

	if updatedUser.DisplayName != "Updated Integration User" {
		t.Fatalf(
			"second UpsertAppleUser() DisplayName = %q",
			updatedUser.DisplayName,
		)
	}

	storedUser, err := repo.GetUserByID(
		ctx,
		user.ID,
	)
	if err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}

	if storedUser.ID != user.ID {
		t.Fatalf(
			"GetUserByID() ID = %q, want %q",
			storedUser.ID,
			user.ID,
		)
	}

	profile, err := repo.UpsertCareerProfile(
		ctx,
		UpsertCareerProfileParams{
			UserID:     user.ID,
			Headline:   "Backend and ML Systems Engineer",
			Summary:    "Builds reliable backend, distributed, and machine learning systems.",
			TargetRole: "Software Engineer",
			Location:   "Miami, FL",
			Evidence: []CareerProfileEvidenceInput{
				{
					Category:   "experience",
					EntryIndex: 0,
					Text:       "Designed distributed systems, REST APIs, and concurrent services.",
					Source:     "career_profile.experience",
				},
				{
					Category:   "skill",
					EntryIndex: 0,
					Text:       "Go",
					Source:     "career_profile.skills",
				},
				{
					Category:   "project",
					EntryIndex: 0,
					Text:       "Built a predictive GPU workload control plane for LLM inference.",
					Source:     "career_profile.projects",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("UpsertCareerProfile() error = %v", err)
	}

	if profile.ID == "" {
		t.Fatal("UpsertCareerProfile() returned empty profile ID")
	}

	if profile.UserID != user.ID {
		t.Fatalf(
			"UpsertCareerProfile() UserID = %q, want %q",
			profile.UserID,
			user.ID,
		)
	}

	if len(profile.Evidence) != 3 {
		t.Fatalf(
			"UpsertCareerProfile() evidence count = %d, want 3",
			len(profile.Evidence),
		)
	}

	loaded, err := repo.GetCareerProfileByUserID(
		ctx,
		user.ID,
	)
	if err != nil {
		t.Fatalf("GetCareerProfileByUserID() error = %v", err)
	}

	if loaded.ID != profile.ID {
		t.Fatalf(
			"GetCareerProfileByUserID() ID = %q, want %q",
			loaded.ID,
			profile.ID,
		)
	}

	if loaded.TargetRole != "Software Engineer" {
		t.Fatalf(
			"GetCareerProfileByUserID() TargetRole = %q",
			loaded.TargetRole,
		)
	}

	if len(loaded.Evidence) != 3 {
		t.Fatalf(
			"GetCareerProfileByUserID() evidence count = %d, want 3",
			len(loaded.Evidence),
		)
	}

	replaced, err := repo.UpsertCareerProfile(
		ctx,
		UpsertCareerProfileParams{
			UserID:     user.ID,
			Headline:   "Software Engineer",
			Summary:    "Backend systems engineer.",
			TargetRole: "Backend Software Engineer",
			Location:   "Miami, FL",
			Evidence: []CareerProfileEvidenceInput{
				{
					Category:   "experience",
					EntryIndex: 0,
					Text:       "Built reliable Go services and APIs.",
					Source:     "career_profile.experience",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"replacement UpsertCareerProfile() error = %v",
			err,
		)
	}

	if replaced.ID != profile.ID {
		t.Fatalf(
			"replacement UpsertCareerProfile() ID = %q, want %q",
			replaced.ID,
			profile.ID,
		)
	}

	if len(replaced.Evidence) != 1 {
		t.Fatalf(
			"replacement UpsertCareerProfile() evidence count = %d, want 1",
			len(replaced.Evidence),
		)
	}

	reloaded, err := repo.GetCareerProfileByUserID(
		ctx,
		user.ID,
	)
	if err != nil {
		t.Fatalf(
			"reload GetCareerProfileByUserID() error = %v",
			err,
		)
	}

	if len(reloaded.Evidence) != 1 {
		t.Fatalf(
			"reloaded evidence count = %d, want 1",
			len(reloaded.Evidence),
		)
	}

	if reloaded.Evidence[0].Text !=
		"Built reliable Go services and APIs." {

		t.Fatalf(
			"reloaded evidence text = %q",
			reloaded.Evidence[0].Text,
		)
	}

	var evidenceCount int

	err = pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM career_profile_evidence
		 WHERE career_profile_id = $1`,
		profile.ID,
	).Scan(&evidenceCount)
	if err != nil {
		t.Fatalf(
			"count stored evidence: %v",
			err,
		)
	}

	if evidenceCount != 1 {
		t.Fatalf(
			"stored evidence count = %d, want 1",
			evidenceCount,
		)
	}
}
