package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestApplicationEventsIntegration(t *testing.T) {
	databaseURL := os.Getenv("ALIGNAPPLY_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://localhost:5432/alignapply?sslmode=disable"
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create PostgreSQL pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
	})

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("PostgreSQL unavailable: %v", err)
	}

	userRepo, err := NewUserCareerRepository(pool)
	if err != nil {
		t.Fatalf("create user repository: %v", err)
	}

	jobRepo, err := NewJobRepository(pool)
	if err != nil {
		t.Fatalf("create job repository: %v", err)
	}

	userJobsRepo, err := NewUserJobsRepository(pool)
	if err != nil {
		t.Fatalf("create user jobs repository: %v", err)
	}

	suffix := time.Now().UnixNano()

	user, err := userRepo.UpsertAppleUser(
		ctx,
		fmt.Sprintf("application-events-test-%d", suffix),
		fmt.Sprintf("application-events-%d@example.com", suffix),
		"Application Events Test",
	)
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}

	t.Cleanup(func() {
		if err := userRepo.DeleteUser(context.Background(), user.ID); err != nil {
			t.Errorf("delete test user: %v", err)
		}
	})

	job, err := jobRepo.UpsertJob(
		ctx,
		UpsertJobParams{
			Source:      "integration_test",
			SourceJobID: fmt.Sprintf("application-events-job-%d", suffix),
			Title:       "Backend Engineer",
			Company:     "AlignApply Test Company",
			Description: "Integration-test job for application tracking.",
			IsActive:    true,
		},
	)
	if err != nil {
		t.Fatalf("create test job: %v", err)
	}

	t.Cleanup(func() {
		_, err := pool.Exec(
			context.Background(),
			`DELETE FROM jobs WHERE id = $1`,
			job.ID,
		)
		if err != nil {
			t.Errorf("delete test job: %v", err)
		}
	})

	application, err := userJobsRepo.UpsertApplication(
		ctx,
		UpsertApplicationParams{
			UserID: user.ID,
			JobID:  job.ID,
			Status: "saved",
			Notes:  "integration test",
		},
	)
	if err != nil {
		t.Fatalf("create test application: %v", err)
	}

	t.Run("manual applied transition", func(t *testing.T) {
		occurredAt := time.Now().UTC().Truncate(time.Microsecond)

		updated, event, err := userJobsRepo.RecordApplicationEvent(
			ctx,
			RecordApplicationEventParams{
				UserID:        user.ID,
				ApplicationID: application.ID,
				EventType:     "application_submitted",
				NewStatus:     "applied",
				Source:        "manual",
				Summary:       "Application submitted.",
				OccurredAt:    &occurredAt,
			},
		)
		if err != nil {
			t.Fatalf("record applied event: %v", err)
		}

		if updated.Status != "applied" {
			t.Fatalf(
				"expected application status applied, got %q",
				updated.Status,
			)
		}

		if updated.AppliedAt == nil {
			t.Fatal("expected applied_at to be populated")
		}

		if event.ApplicationID != application.ID {
			t.Fatalf(
				"expected event application %q, got %q",
				application.ID,
				event.ApplicationID,
			)
		}

		if event.PreviousStatus == nil || *event.PreviousStatus != "saved" {
			t.Fatalf(
				"expected previous status saved, got %#v",
				event.PreviousStatus,
			)
		}

		if event.NewStatus != "applied" {
			t.Fatalf(
				"expected new status applied, got %q",
				event.NewStatus,
			)
		}

		application = updated
	})

	t.Run("email interview transition", func(t *testing.T) {
		messageID := fmt.Sprintf("gmail-message-%d", suffix)
		confidence := 0.99

		updated, event, err := userJobsRepo.RecordApplicationEvent(
			ctx,
			RecordApplicationEventParams{
				UserID:          user.ID,
				ApplicationID:   application.ID,
				EventType:       "interview_invitation",
				NewStatus:       "interviewing",
				Source:          "email",
				SourceProvider:  "gmail",
				SourceMessageID: &messageID,
				Confidence:      &confidence,
				Summary:         "Interview invitation detected.",
			},
		)
		if err != nil {
			t.Fatalf("record interview event: %v", err)
		}

		if updated.Status != "interviewing" {
			t.Fatalf(
				"expected interviewing status, got %q",
				updated.Status,
			)
		}

		if event.Source != "email" {
			t.Fatalf("expected email source, got %q", event.Source)
		}

		if event.SourceProvider != "gmail" {
			t.Fatalf(
				"expected gmail provider, got %q",
				event.SourceProvider,
			)
		}

		if event.SourceMessageID == nil || *event.SourceMessageID != messageID {
			t.Fatalf(
				"expected message ID %q, got %#v",
				messageID,
				event.SourceMessageID,
			)
		}

		if event.Confidence == nil || *event.Confidence != confidence {
			t.Fatalf(
				"expected confidence %v, got %#v",
				confidence,
				event.Confidence,
			)
		}

		application = updated
	})

	t.Run("duplicate external message is idempotent", func(t *testing.T) {
		messageID := fmt.Sprintf("duplicate-message-%d", suffix)
		confidence := 0.97

		firstApplication, firstEvent, err :=
			userJobsRepo.RecordApplicationEvent(
				ctx,
				RecordApplicationEventParams{
					UserID:          user.ID,
					ApplicationID:   application.ID,
					EventType:       "recruiter_contact",
					NewStatus:       "recruiter_contact",
					Source:          "email",
					SourceProvider:  "gmail",
					SourceMessageID: &messageID,
					Confidence:      &confidence,
					Summary:         "Recruiter contacted candidate.",
				},
			)
		if err != nil {
			t.Fatalf("record first external event: %v", err)
		}

		secondApplication, secondEvent, err :=
			userJobsRepo.RecordApplicationEvent(
				ctx,
				RecordApplicationEventParams{
					UserID:          user.ID,
					ApplicationID:   application.ID,
					EventType:       "recruiter_contact",
					NewStatus:       "recruiter_contact",
					Source:          "email",
					SourceProvider:  "gmail",
					SourceMessageID: &messageID,
					Confidence:      &confidence,
					Summary:         "Recruiter contacted candidate.",
				},
			)
		if err != nil {
			t.Fatalf("record duplicate external event: %v", err)
		}

		if firstEvent.ID != secondEvent.ID {
			t.Fatalf(
				"expected duplicate to return event %q, got %q",
				firstEvent.ID,
				secondEvent.ID,
			)
		}

		if firstApplication.Status != secondApplication.Status {
			t.Fatalf(
				"duplicate changed status from %q to %q",
				firstApplication.Status,
				secondApplication.Status,
			)
		}

		var count int
		err = pool.QueryRow(
			ctx,
			`SELECT COUNT(*)
			 FROM application_events
			 WHERE application_id = $1
			   AND source = 'email'
			   AND source_provider = 'gmail'
			   AND source_message_id = $2`,
			application.ID,
			messageID,
		).Scan(&count)
		if err != nil {
			t.Fatalf("count duplicate events: %v", err)
		}

		if count != 1 {
			t.Fatalf(
				"expected exactly 1 external event, got %d",
				count,
			)
		}

		application = secondApplication
	})

	t.Run("timeline persists in chronological order", func(t *testing.T) {
		events, err := userJobsRepo.ListApplicationEvents(
			ctx,
			user.ID,
			application.ID,
		)
		if err != nil {
			t.Fatalf("list application events: %v", err)
		}

		if len(events) != 3 {
			t.Fatalf("expected 3 events, got %d", len(events))
		}

		for i := 1; i < len(events); i++ {
			if events[i].OccurredAt.Before(events[i-1].OccurredAt) {
				t.Fatalf(
					"events are not chronological: event %d occurs before event %d",
					i,
					i-1,
				)
			}
		}
	})

	t.Run("ownership isolation", func(t *testing.T) {
		otherUser, err := userRepo.UpsertAppleUser(
			ctx,
			fmt.Sprintf("application-events-other-%d", suffix),
			fmt.Sprintf("application-events-other-%d@example.com", suffix),
			"Other Test User",
		)
		if err != nil {
			t.Fatalf("create other user: %v", err)
		}

		t.Cleanup(func() {
			if err := userRepo.DeleteUser(
				context.Background(),
				otherUser.ID,
			); err != nil {
				t.Errorf("delete other user: %v", err)
			}
		})

		_, _, err = userJobsRepo.RecordApplicationEvent(
			ctx,
			RecordApplicationEventParams{
				UserID:        otherUser.ID,
				ApplicationID: application.ID,
				EventType:     "status_changed",
				NewStatus:     "offer",
				Source:        "manual",
				Summary:       "Unauthorized update attempt.",
			},
		)
		if err == nil {
			t.Fatal("expected another user to be unable to update application")
		}

		events, err := userJobsRepo.ListApplicationEvents(
			ctx,
			otherUser.ID,
			application.ID,
		)
		if err != nil {
			t.Fatalf("list events as other user: %v", err)
		}

		if len(events) != 0 {
			t.Fatalf(
				"expected other user to see 0 events, got %d",
				len(events),
			)
		}
	})

	t.Run("persisted current status matches timeline", func(t *testing.T) {
		persisted, err := userJobsRepo.GetApplication(
			ctx,
			user.ID,
			job.ID,
		)
		if err != nil {
			t.Fatalf("get persisted application: %v", err)
		}

		if persisted.Status != application.Status {
			t.Fatalf(
				"persisted status %q does not match expected %q",
				persisted.Status,
				application.Status,
			)
		}

		if persisted.AppliedAt == nil {
			t.Fatal("expected persisted applied_at")
		}
	})
}
