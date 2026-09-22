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

const defaultAPITestDatabaseURL = "postgres://localhost:5432/alignapply?sslmode=disable"

func TestCareerProfileAPIIntegration(t *testing.T) {
	databaseURL := os.Getenv(
		"ALIGNAPPLY_TEST_DATABASE_URL",
	)

	if databaseURL == "" {
		databaseURL = defaultAPITestDatabaseURL
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()

	pool, err := pgxpool.New(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Skipf(
			"PostgreSQL unavailable: %v",
			err,
		)
	}

	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Skipf(
			"PostgreSQL unavailable: %v",
			err,
		)
	}

	userCareerRepo, err :=
		repository.NewUserCareerRepository(
			pool,
		)

	if err != nil {
		t.Fatalf(
			"NewUserCareerRepository() error = %v",
			err,
		)
	}

	appleSubject := fmt.Sprintf(
		"alignapply-api-integration-%d",
		time.Now().UnixNano(),
	)

	user, err :=
		userCareerRepo.UpsertAppleUser(
			ctx,
			appleSubject,
			"api-integration@example.com",
			"API Integration User",
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

		if _, cleanupErr :=
			pool.Exec(
				cleanupCtx,
				`DELETE FROM users WHERE id = $1`,
				user.ID,
			); cleanupErr != nil {

			t.Logf(
				"cleanup user: %v",
				cleanupErr,
			)
		}
	})

	sessionManager, err :=
		auth.NewSessionManager(
			"alignapply-api-integration-test-secret-123456789",
		)

	if err != nil {
		t.Fatalf(
			"NewSessionManager() error = %v",
			err,
		)
	}

	accessToken, err :=
		sessionManager.Create(
			user.ID,
		)

	if err != nil {
		t.Fatalf(
			"sessionManager.Create() error = %v",
			err,
		)
	}

	server := NewServer(
		Dependencies{
			Repositories: Repositories{
				UserCareer: userCareerRepo,
			},
			SessionManager: sessionManager,
		},
	)

	t.Run(
		"rejects unauthenticated GET",
		func(t *testing.T) {
			request :=
				httptest.NewRequest(
					http.MethodGet,
					"/api/v1/career-profile",
					nil,
				)

			recorder :=
				httptest.NewRecorder()

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
		"GET before profile creation returns empty profile",
		func(t *testing.T) {
			request :=
				httptest.NewRequest(
					http.MethodGet,
					"/api/v1/career-profile",
					nil,
				)

			request.Header.Set(
				"Authorization",
				"Bearer "+accessToken,
			)

			recorder :=
				httptest.NewRecorder()

			server.Handler().ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code !=
				http.StatusOK {

				t.Fatalf(
					"status = %d, want %d; body = %s",
					recorder.Code,
					http.StatusOK,
					recorder.Body.String(),
				)
			}

			var response careerProfileResponse

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

			if response.ID != "" {
				t.Fatalf(
					"empty profile ID = %q, want empty",
					response.ID,
				)
			}

			if response.Evidence == nil {
				t.Fatal(
					"empty profile evidence must be [] instead of null",
				)
			}

			if len(response.Evidence) != 0 {
				t.Fatalf(
					"empty profile evidence count = %d, want 0",
					len(response.Evidence),
				)
			}
		},
	)

	t.Run(
		"PUT creates profile",
		func(t *testing.T) {
			body := []byte(`{
				"headline": "Backend and ML Systems Engineer",
				"summary": "Builds reliable backend, distributed, and machine learning systems.",
				"targetRole": "Software Engineer",
				"location": "Miami, FL",
				"evidence": [
					{
						"category": "experience",
						"entryIndex": 0,
						"text": "Designed distributed systems, REST APIs, and concurrent services.",
						"source": "career_profile.experience"
					},
					{
						"category": "skill",
						"entryIndex": 0,
						"text": "Go",
						"source": "career_profile.skills"
					},
					{
						"category": "project",
						"entryIndex": 0,
						"text": "Built a predictive GPU workload control plane for LLM inference.",
						"source": "career_profile.projects"
					}
				]
			}`)

			request :=
				httptest.NewRequest(
					http.MethodPut,
					"/api/v1/career-profile",
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

			recorder :=
				httptest.NewRecorder()

			server.Handler().ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code !=
				http.StatusOK {

				t.Fatalf(
					"status = %d, want %d; body = %s",
					recorder.Code,
					http.StatusOK,
					recorder.Body.String(),
				)
			}

			var response careerProfileResponse

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

			if response.ID == "" {
				t.Fatal(
					"PUT returned empty profile ID",
				)
			}

			if response.Headline !=
				"Backend and ML Systems Engineer" {

				t.Fatalf(
					"headline = %q",
					response.Headline,
				)
			}

			if response.TargetRole !=
				"Software Engineer" {

				t.Fatalf(
					"targetRole = %q",
					response.TargetRole,
				)
			}

			if len(response.Evidence) != 3 {
				t.Fatalf(
					"evidence count = %d, want 3",
					len(response.Evidence),
				)
			}

			for index, item := range response.Evidence {

				if item.ID == "" {
					t.Fatalf(
						"evidence[%d] returned empty ID",
						index,
					)
				}

				if item.Source == "" {
					t.Fatalf(
						"evidence[%d] returned empty source",
						index,
					)
				}
			}
		},
	)

	t.Run(
		"GET returns persisted profile",
		func(t *testing.T) {
			request :=
				httptest.NewRequest(
					http.MethodGet,
					"/api/v1/career-profile",
					nil,
				)

			request.Header.Set(
				"Authorization",
				"Bearer "+accessToken,
			)

			recorder :=
				httptest.NewRecorder()

			server.Handler().ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code !=
				http.StatusOK {

				t.Fatalf(
					"status = %d, want %d; body = %s",
					recorder.Code,
					http.StatusOK,
					recorder.Body.String(),
				)
			}

			var response careerProfileResponse

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

			if response.ID == "" {
				t.Fatal(
					"GET returned empty profile ID",
				)
			}

			if response.Headline !=
				"Backend and ML Systems Engineer" {

				t.Fatalf(
					"headline = %q",
					response.Headline,
				)
			}

			if response.Summary !=
				"Builds reliable backend, distributed, and machine learning systems." {

				t.Fatalf(
					"summary = %q",
					response.Summary,
				)
			}

			if response.TargetRole !=
				"Software Engineer" {

				t.Fatalf(
					"targetRole = %q",
					response.TargetRole,
				)
			}

			if response.Location !=
				"Miami, FL" {

				t.Fatalf(
					"location = %q",
					response.Location,
				)
			}

			if len(response.Evidence) != 3 {
				t.Fatalf(
					"evidence count = %d, want 3",
					len(response.Evidence),
				)
			}
		},
	)

	t.Run(
		"PUT replaces existing evidence",
		func(t *testing.T) {
			body := []byte(`{
				"headline": "Software Engineer",
				"summary": "Backend systems engineer.",
				"targetRole": "Backend Software Engineer",
				"location": "Miami, FL",
				"evidence": [
					{
						"category": "experience",
						"entryIndex": 0,
						"text": "Built reliable Go services and APIs.",
						"source": "career_profile.experience"
					}
				]
			}`)

			request :=
				httptest.NewRequest(
					http.MethodPut,
					"/api/v1/career-profile",
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

			recorder :=
				httptest.NewRecorder()

			server.Handler().ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code !=
				http.StatusOK {

				t.Fatalf(
					"status = %d, want %d; body = %s",
					recorder.Code,
					http.StatusOK,
					recorder.Body.String(),
				)
			}

			var response careerProfileResponse

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

			if response.TargetRole !=
				"Backend Software Engineer" {

				t.Fatalf(
					"targetRole = %q",
					response.TargetRole,
				)
			}

			if len(response.Evidence) != 1 {
				t.Fatalf(
					"replacement evidence count = %d, want 1",
					len(response.Evidence),
				)
			}

			if response.Evidence[0].Text !=
				"Built reliable Go services and APIs." {

				t.Fatalf(
					"replacement evidence text = %q",
					response.Evidence[0].Text,
				)
			}
		},
	)

	t.Run(
		"GET returns replacement profile",
		func(t *testing.T) {
			request :=
				httptest.NewRequest(
					http.MethodGet,
					"/api/v1/career-profile",
					nil,
				)

			request.Header.Set(
				"Authorization",
				"Bearer "+accessToken,
			)

			recorder :=
				httptest.NewRecorder()

			server.Handler().ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code !=
				http.StatusOK {

				t.Fatalf(
					"status = %d, want %d; body = %s",
					recorder.Code,
					http.StatusOK,
					recorder.Body.String(),
				)
			}

			var response careerProfileResponse

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

			if response.Headline !=
				"Software Engineer" {

				t.Fatalf(
					"headline = %q",
					response.Headline,
				)
			}

			if response.TargetRole !=
				"Backend Software Engineer" {

				t.Fatalf(
					"targetRole = %q",
					response.TargetRole,
				)
			}

			if len(response.Evidence) != 1 {
				t.Fatalf(
					"evidence count = %d, want 1",
					len(response.Evidence),
				)
			}

			if response.Evidence[0].Category !=
				"experience" {

				t.Fatalf(
					"evidence category = %q",
					response.Evidence[0].Category,
				)
			}

			if response.Evidence[0].Source !=
				"career_profile.experience" {

				t.Fatalf(
					"evidence source = %q",
					response.Evidence[0].Source,
				)
			}
		},
	)

	t.Run(
		"PUT rejects invalid evidence",
		func(t *testing.T) {
			body := []byte(`{
				"headline": "Software Engineer",
				"evidence": [
					{
						"category": "invalid-category",
						"entryIndex": 0,
						"text": "Invalid evidence.",
						"source": "career_profile.invalid"
					}
				]
			}`)

			request :=
				httptest.NewRequest(
					http.MethodPut,
					"/api/v1/career-profile",
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

			recorder :=
				httptest.NewRecorder()

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
}
