package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/auth"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestApplicationsAPIIntegration(t *testing.T) {
	databaseURL := os.Getenv("ALIGNAPPLY_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = defaultAPITestDatabaseURL
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

	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("PostgreSQL unavailable: %v", err)
	}

	userCareerRepo, err :=
		repository.NewUserCareerRepository(pool)
	if err != nil {
		t.Fatalf(
			"NewUserCareerRepository() error = %v",
			err,
		)
	}

	jobRepo, err :=
		repository.NewJobRepository(pool)
	if err != nil {
		t.Fatalf(
			"NewJobRepository() error = %v",
			err,
		)
	}

	userJobsRepo, err :=
		repository.NewUserJobsRepository(pool)
	if err != nil {
		t.Fatalf(
			"NewUserJobsRepository() error = %v",
			err,
		)
	}

	suffix := time.Now().UnixNano()

	user, err :=
		userCareerRepo.UpsertAppleUser(
			ctx,
			fmt.Sprintf(
				"applications-api-integration-%d",
				suffix,
			),
			fmt.Sprintf(
				"applications-api-%d@example.com",
				suffix,
			),
			"Applications API Integration User",
		)
	if err != nil {
		t.Fatalf(
			"UpsertAppleUser() error = %v",
			err,
		)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel :=
			context.WithTimeout(
				context.Background(),
				5*time.Second,
			)
		defer cleanupCancel()

		if err :=
			userCareerRepo.DeleteUser(
				cleanupCtx,
				user.ID,
			); err != nil {

			t.Errorf(
				"delete test user: %v",
				err,
			)
		}
	})

	otherUser, err :=
		userCareerRepo.UpsertAppleUser(
			ctx,
			fmt.Sprintf(
				"applications-api-other-%d",
				suffix,
			),
			fmt.Sprintf(
				"applications-api-other-%d@example.com",
				suffix,
			),
			"Applications API Other User",
		)
	if err != nil {
		t.Fatalf(
			"create other user: %v",
			err,
		)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel :=
			context.WithTimeout(
				context.Background(),
				5*time.Second,
			)
		defer cleanupCancel()

		if err :=
			userCareerRepo.DeleteUser(
				cleanupCtx,
				otherUser.ID,
			); err != nil {

			t.Errorf(
				"delete other test user: %v",
				err,
			)
		}
	})

	job, err :=
		jobRepo.UpsertJob(
			ctx,
			repository.UpsertJobParams{
				Source: "integration_test",
				SourceJobID: fmt.Sprintf(
					"applications-api-job-%d",
					suffix,
				),
				Title:   "Backend Engineer",
				Company: "AlignApply Test Company",
				Description: "Integration-test job for the " +
					"application tracker API.",
				IsActive: true,
			},
		)
	if err != nil {
		t.Fatalf(
			"create test job: %v",
			err,
		)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel :=
			context.WithTimeout(
				context.Background(),
				5*time.Second,
			)
		defer cleanupCancel()

		if _, err :=
			pool.Exec(
				cleanupCtx,
				`DELETE FROM jobs WHERE id = $1`,
				job.ID,
			); err != nil {

			t.Errorf(
				"delete test job: %v",
				err,
			)
		}
	})

	sessionManager, err :=
		auth.NewSessionManager(
			"alignapply-applications-api-integration-test-secret-123456789",
		)
	if err != nil {
		t.Fatalf(
			"NewSessionManager() error = %v",
			err,
		)
	}

	accessToken, err :=
		sessionManager.Create(user.ID)
	if err != nil {
		t.Fatalf(
			"sessionManager.Create() error = %v",
			err,
		)
	}

	otherAccessToken, err :=
		sessionManager.Create(otherUser.ID)
	if err != nil {
		t.Fatalf(
			"other sessionManager.Create() error = %v",
			err,
		)
	}

	server := NewServer(
		Dependencies{
			Repositories: Repositories{
				UserCareer: userCareerRepo,
				Jobs:       jobRepo,
				UserJobs:   userJobsRepo,
			},
			SessionManager: sessionManager,
		},
	)

	t.Run(
		"rejects unauthenticated application list",
		func(t *testing.T) {
			request :=
				httptest.NewRequest(
					http.MethodGet,
					"/api/v1/applications",
					nil,
				)

			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code !=
				http.StatusUnauthorized {

				t.Fatalf(
					"status = %d, want %d; body = %s",
					recorder.Code,
					http.StatusUnauthorized,
					recorder.Body.String(),
				)
			}
		},
	)

	t.Run(
		"empty application list is array",
		func(t *testing.T) {
			request :=
				httptest.NewRequest(
					http.MethodGet,
					"/api/v1/applications",
					nil,
				)

			request.Header.Set(
				"Authorization",
				"Bearer "+accessToken,
			)

			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code != http.StatusOK {
				t.Fatalf(
					"status = %d, want %d; body = %s",
					recorder.Code,
					http.StatusOK,
					recorder.Body.String(),
				)
			}

			var response []applicationResponse

			if err :=
				json.NewDecoder(
					recorder.Body,
				).Decode(
					&response,
				); err != nil {

				t.Fatalf(
					"decode response: %v",
					err,
				)
			}

			if response == nil {
				t.Fatal(
					"applications must be [] instead of null",
				)
			}

			if len(response) != 0 {
				t.Fatalf(
					"application count = %d, want 0",
					len(response),
				)
			}
		},
	)

	var application applicationResponse

	t.Run(
		"PUT creates tracked application",
		func(t *testing.T) {
			body, err := json.Marshal(
				applicationRequest{
					JobID:  job.ID,
					Status: "saved",
					Notes:  "Interested in this role.",
				},
			)
			if err != nil {
				t.Fatalf(
					"marshal request: %v",
					err,
				)
			}

			request :=
				httptest.NewRequest(
					http.MethodPut,
					"/api/v1/applications",
					bytes.NewReader(body),
				)

			request.Header.Set(
				"Authorization",
				"Bearer "+accessToken,
			)
			request.Header.Set(
				"Content-Type",
				"application/json",
			)

			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code != http.StatusOK {
				t.Fatalf(
					"status = %d, want %d; body = %s",
					recorder.Code,
					http.StatusOK,
					recorder.Body.String(),
				)
			}

			if err :=
				json.NewDecoder(
					recorder.Body,
				).Decode(
					&application,
				); err != nil {

				t.Fatalf(
					"decode application: %v",
					err,
				)
			}

			if application.ID == "" {
				t.Fatal(
					"application ID must not be empty",
				)
			}

			if application.JobID != job.ID {
				t.Fatalf(
					"jobId = %q, want %q",
					application.JobID,
					job.ID,
				)
			}

			if application.Status != "saved" {
				t.Fatalf(
					"status = %q, want saved",
					application.Status,
				)
			}

			if application.Notes !=
				"Interested in this role." {

				t.Fatalf(
					"notes = %q",
					application.Notes,
				)
			}
		},
	)

	t.Run(
		"GET lists tracked application",
		func(t *testing.T) {
			request :=
				httptest.NewRequest(
					http.MethodGet,
					"/api/v1/applications",
					nil,
				)

			request.Header.Set(
				"Authorization",
				"Bearer "+accessToken,
			)

			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code != http.StatusOK {
				t.Fatalf(
					"status = %d, want %d; body = %s",
					recorder.Code,
					http.StatusOK,
					recorder.Body.String(),
				)
			}

			var response []applicationResponse

			if err :=
				json.NewDecoder(
					recorder.Body,
				).Decode(
					&response,
				); err != nil {

				t.Fatalf(
					"decode applications: %v",
					err,
				)
			}

			if len(response) != 1 {
				t.Fatalf(
					"application count = %d, want 1",
					len(response),
				)
			}

			if response[0].ID != application.ID {
				t.Fatalf(
					"application ID = %q, want %q",
					response[0].ID,
					application.ID,
				)
			}
		},
	)

	t.Run(
		"POST manual event updates application",
		func(t *testing.T) {
			body, err := json.Marshal(
				applicationEventRequest{
					EventType: "application_submitted",
					NewStatus: "applied",
					Summary:   "Application submitted.",
				},
			)
			if err != nil {
				t.Fatalf(
					"marshal event request: %v",
					err,
				)
			}

			request :=
				httptest.NewRequest(
					http.MethodPost,
					"/api/v1/applications/"+
						application.ID+
						"/events",
					bytes.NewReader(body),
				)

			request.Header.Set(
				"Authorization",
				"Bearer "+accessToken,
			)
			request.Header.Set(
				"Content-Type",
				"application/json",
			)

			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code != http.StatusOK {
				t.Fatalf(
					"status = %d, want %d; body = %s",
					recorder.Code,
					http.StatusOK,
					recorder.Body.String(),
				)
			}

			var response applicationEventResultResponse

			if err :=
				json.NewDecoder(
					recorder.Body,
				).Decode(
					&response,
				); err != nil {

				t.Fatalf(
					"decode event response: %v",
					err,
				)
			}

			if response.Application.Status !=
				"applied" {

				t.Fatalf(
					"application status = %q, want applied",
					response.Application.Status,
				)
			}

			if response.Application.AppliedAt == nil {
				t.Fatal(
					"appliedAt must be set after application_submitted",
				)
			}

			if response.Event.EventType !=
				"application_submitted" {

				t.Fatalf(
					"eventType = %q",
					response.Event.EventType,
				)
			}

			if response.Event.NewStatus !=
				"applied" {

				t.Fatalf(
					"newStatus = %q, want applied",
					response.Event.NewStatus,
				)
			}

			if response.Event.Source != "manual" {
				t.Fatalf(
					"source = %q, want manual",
					response.Event.Source,
				)
			}

			if response.Event.PreviousStatus == nil ||
				*response.Event.PreviousStatus !=
					"saved" {

				t.Fatalf(
					"previousStatus = %v, want saved",
					response.Event.PreviousStatus,
				)
			}
		},
	)

	t.Run(
		"PUT cannot bypass event history for status change",
		func(t *testing.T) {
			body, err := json.Marshal(
				applicationRequest{
					JobID:  job.ID,
					Status: "rejected",
					Notes:  "Attempted direct status change.",
				},
			)
			if err != nil {
				t.Fatalf(
					"marshal direct status request: %v",
					err,
				)
			}

			request :=
				httptest.NewRequest(
					http.MethodPut,
					"/api/v1/applications",
					bytes.NewReader(body),
				)

			request.Header.Set(
				"Authorization",
				"Bearer "+accessToken,
			)
			request.Header.Set(
				"Content-Type",
				"application/json",
			)

			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code != http.StatusConflict {
				t.Fatalf(
					"status = %d, want %d; body = %s",
					recorder.Code,
					http.StatusConflict,
					recorder.Body.String(),
				)
			}

			persisted, err :=
				userJobsRepo.GetApplication(
					ctx,
					user.ID,
					job.ID,
				)
			if err != nil {
				t.Fatalf(
					"load application after rejected PUT: %v",
					err,
				)
			}

			if persisted.Status != "applied" {
				t.Fatalf(
					"status = %q after rejected PUT, want applied",
					persisted.Status,
				)
			}
		},
	)

	t.Run(
		"GET returns application timeline",
		func(t *testing.T) {
			request :=
				httptest.NewRequest(
					http.MethodGet,
					"/api/v1/applications/"+
						application.ID+
						"/events",
					nil,
				)

			request.Header.Set(
				"Authorization",
				"Bearer "+accessToken,
			)

			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code != http.StatusOK {
				t.Fatalf(
					"status = %d, want %d; body = %s",
					recorder.Code,
					http.StatusOK,
					recorder.Body.String(),
				)
			}

			var response []applicationEventResponse

			if err :=
				json.NewDecoder(
					recorder.Body,
				).Decode(
					&response,
				); err != nil {

				t.Fatalf(
					"decode timeline: %v",
					err,
				)
			}

			if len(response) != 1 {
				t.Fatalf(
					"event count = %d, want 1",
					len(response),
				)
			}

			if response[0].ApplicationID !=
				application.ID {

				t.Fatalf(
					"applicationId = %q, want %q",
					response[0].ApplicationID,
					application.ID,
				)
			}

			if response[0].NewStatus != "applied" {
				t.Fatalf(
					"newStatus = %q, want applied",
					response[0].NewStatus,
				)
			}
		},
	)

	t.Run(
		"other user cannot modify application",
		func(t *testing.T) {
			body, err := json.Marshal(
				applicationEventRequest{
					EventType: "rejected",
					NewStatus: "rejected",
					Summary:   "Unauthorized change.",
				},
			)
			if err != nil {
				t.Fatalf(
					"marshal ownership request: %v",
					err,
				)
			}

			request :=
				httptest.NewRequest(
					http.MethodPost,
					"/api/v1/applications/"+
						application.ID+
						"/events",
					bytes.NewReader(body),
				)

			request.Header.Set(
				"Authorization",
				"Bearer "+otherAccessToken,
			)
			request.Header.Set(
				"Content-Type",
				"application/json",
			)

			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code == http.StatusOK {
				t.Fatalf(
					"other user modified application; body = %s",
					recorder.Body.String(),
				)
			}

			persisted, err :=
				userJobsRepo.GetApplication(
					ctx,
					user.ID,
					job.ID,
				)
			if err != nil {
				t.Fatalf(
					"load application after ownership attempt: %v",
					err,
				)
			}

			if persisted.Status != "applied" {
				t.Fatalf(
					"status changed to %q after unauthorized request",
					persisted.Status,
				)
			}
		},
	)

	t.Run(
		"rejects client supplied machine provenance",
		func(t *testing.T) {
			body := []byte(`{
				"eventType": "interview_invitation",
				"newStatus": "interviewing",
				"summary": "Interview invitation.",
				"source": "email",
				"sourceProvider": "gmail"
			}`)

			request :=
				httptest.NewRequest(
					http.MethodPost,
					"/api/v1/applications/"+
						application.ID+
						"/events",
					bytes.NewReader(body),
				)

			request.Header.Set(
				"Authorization",
				"Bearer "+accessToken,
			)
			request.Header.Set(
				"Content-Type",
				"application/json",
			)

			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code !=
				http.StatusBadRequest {

				t.Fatalf(
					"status = %d, want %d; body = %s",
					recorder.Code,
					http.StatusBadRequest,
					recorder.Body.String(),
				)
			}
		},
	)

	t.Run(
		"persisted status matches event",
		func(t *testing.T) {
			persisted, err :=
				userJobsRepo.GetApplication(
					ctx,
					user.ID,
					job.ID,
				)
			if err != nil {
				t.Fatalf(
					"GetApplication() error = %v",
					err,
				)
			}

			if persisted.Status != "applied" {
				t.Fatalf(
					"status = %q, want applied",
					persisted.Status,
				)
			}

			if persisted.AppliedAt == nil {
				t.Fatal(
					"persisted appliedAt must not be nil",
				)
			}
		},
	)
}
