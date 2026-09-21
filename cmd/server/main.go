package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/api"
	"github.com/hannasotolongo/casemade-backend/internal/auth"
	"github.com/hannasotolongo/casemade-backend/internal/database"
	"github.com/hannasotolongo/casemade-backend/internal/matching"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
)

const defaultDatabaseURL = "postgres://localhost:5432/alignapply?sslmode=disable"

func main() {
	ctx := context.Background()

	// Semantic evidence retrieval.
	semanticProvider := matching.NewOllamaSemanticProvider()

	matching.SetSemanticRetriever(
		matching.HybridSemanticRetriever{
			Provider: semanticProvider,
		},
	)

	// Evidence verification.
	evidenceVerifier := matching.NewOllamaEvidenceVerifier()
	matching.SetEvidenceVerifier(evidenceVerifier)

	log.Println(
		"Semantic evidence retrieval configured with Ollama (nomic-embed-text)",
	)
	log.Println(
		"Evidence verification configured with Ollama (qwen3:4b)",
	)

	// PostgreSQL.
	databaseURL := strings.TrimSpace(
		os.Getenv("DATABASE_URL"),
	)
	if databaseURL == "" {
		databaseURL = defaultDatabaseURL
	}

	db, err := database.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf(
			"PostgreSQL startup failed: %v",
			err,
		)
	}
	defer db.Close()

	log.Println("PostgreSQL connection established")

	// Persistence repositories.
	userCareerRepository, err :=
		repository.NewUserCareerRepository(db.Pool())
	if err != nil {
		log.Fatalf(
			"create user/career repository: %v",
			err,
		)
	}

	jobRepository, err :=
		repository.NewJobRepository(db.Pool())
	if err != nil {
		log.Fatalf(
			"create job repository: %v",
			err,
		)
	}

	matchRepository, err :=
		repository.NewMatchRepository(db.Pool())
	if err != nil {
		log.Fatalf(
			"create match repository: %v",
			err,
		)
	}

	userJobsRepository, err :=
		repository.NewUserJobsRepository(db.Pool())
	if err != nil {
		log.Fatalf(
			"create user/jobs repository: %v",
			err,
		)
	}

	appleAuthRepository, err :=
		repository.NewAppleAuthRepository(db.Pool())
	if err != nil {
		log.Fatalf(
			"create Apple auth repository: %v",
			err,
		)
	}

	log.Println(
		"AlignApply persistence repositories initialized",
	)

	// Sign in with Apple identity-token verification.
	appleClientID := strings.TrimSpace(
		os.Getenv("APPLE_CLIENT_ID"),
	)
	if appleClientID == "" {
		log.Fatal(
			"APPLE_CLIENT_ID environment variable is required",
		)
	}

	appleVerifier, err :=
		auth.NewAppleVerifier(appleClientID)
	if err != nil {
		log.Fatalf(
			"create Apple verifier: %v",
			err,
		)
	}

	// AlignApply API sessions.
	sessionSecret := strings.TrimSpace(
		os.Getenv("SESSION_SECRET"),
	)
	if sessionSecret == "" {
		log.Fatal(
			"SESSION_SECRET environment variable is required",
		)
	}

	sessionManager, err :=
		auth.NewSessionManager(sessionSecret)
	if err != nil {
		log.Fatalf(
			"create session manager: %v",
			err,
		)
	}

	// Apple server-to-server authorization.
	//
	// These credentials are available after Sign in with Apple is configured
	// for the production Apple Developer team. Local development can continue
	// without them.
	appleTeamID := strings.TrimSpace(
		os.Getenv("APPLE_TEAM_ID"),
	)

	appleKeyID := strings.TrimSpace(
		os.Getenv("APPLE_KEY_ID"),
	)

	applePrivateKey := strings.TrimSpace(
		os.Getenv("APPLE_PRIVATE_KEY"),
	)

	var appleClient *auth.AppleClient

	appleServerCredentialsConfigured :=
		appleTeamID != "" &&
			appleKeyID != "" &&
			applePrivateKey != ""

	appleServerCredentialsPartiallyConfigured :=
		appleTeamID != "" ||
			appleKeyID != "" ||
			applePrivateKey != ""

	if appleServerCredentialsPartiallyConfigured &&
		!appleServerCredentialsConfigured {

		log.Fatal(
			"APPLE_TEAM_ID, APPLE_KEY_ID, and APPLE_PRIVATE_KEY must all be configured together",
		)
	}

	if appleServerCredentialsConfigured {
		appleClient, err =
			auth.NewAppleClient(
				auth.AppleClientConfig{
					ClientID:   appleClientID,
					TeamID:     appleTeamID,
					KeyID:      appleKeyID,
					PrivateKey: applePrivateKey,
				},
			)

		if err != nil {
			log.Fatalf(
				"create Apple server client: %v",
				err,
			)
		}

		log.Println(
			"Apple server-to-server authorization initialized",
		)
	} else {
		log.Println(
			"Apple server-to-server authorization not configured; authorization-code exchange and revocation are disabled",
		)
	}

	log.Println(
		"AlignApply authentication initialized",
	)

	server := api.NewServer(
		api.Dependencies{
			Repositories: api.Repositories{
				UserCareer: userCareerRepository,
				Jobs:       jobRepository,
				Matches:    matchRepository,
				UserJobs:   userJobsRepository,
				AppleAuth:  appleAuthRepository,
			},
			AppleVerifier:  appleVerifier,
			AppleClient:    appleClient,
			SessionManager: sessionManager,
		},
	)

	httpServer := &http.Server{
		Addr:              ":8080",
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Println(
		"AlignApply API listening on http://localhost:8080",
	)

	if err := httpServer.ListenAndServe(); err != nil &&
		err != http.ErrServerClosed {

		log.Fatal(err)
	}
}
