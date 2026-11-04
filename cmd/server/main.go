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
	"github.com/hannasotolongo/casemade-backend/internal/billing"
	"github.com/hannasotolongo/casemade-backend/internal/database"
	"github.com/hannasotolongo/casemade-backend/internal/mailer"
	"github.com/hannasotolongo/casemade-backend/internal/matching"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
	"github.com/hannasotolongo/casemade-backend/internal/web"
	"github.com/hannasotolongo/casemade-backend/migrations"
)

const (
	defaultDatabaseURL = "postgres://localhost:5432/alignapply?sslmode=disable"
	defaultPort        = "8080"
)

func main() {
	ctx := context.Background()

	// ---------------------------------------------------------
	// Matching / inference
	// ---------------------------------------------------------

	openAIAPIKey := strings.TrimSpace(
		os.Getenv("OPENAI_API_KEY"),
	)

	if openAIAPIKey != "" {
		semanticProvider :=
			matching.NewOpenAISemanticProvider(openAIAPIKey)

		matching.SetSemanticRetriever(
			matching.HybridSemanticRetriever{
				Provider: semanticProvider,
			},
		)

		evidenceVerifier :=
			matching.NewOpenAIEvidenceVerifier(openAIAPIKey)

		matching.SetEvidenceVerifier(evidenceVerifier)

		log.Println(
			"Semantic evidence retrieval configured with OpenAI (text-embedding-3-small)",
		)

		log.Println(
			"Evidence verification configured with OpenAI",
		)
	} else {
		semanticProvider :=
			matching.NewOllamaSemanticProvider()

		matching.SetSemanticRetriever(
			matching.HybridSemanticRetriever{
				Provider: semanticProvider,
			},
		)

		evidenceVerifier :=
			matching.NewOllamaEvidenceVerifier()

		matching.SetEvidenceVerifier(evidenceVerifier)

		log.Println(
			"Semantic evidence retrieval configured with Ollama (nomic-embed-text)",
		)

		log.Println(
			"Evidence verification configured with Ollama (qwen3:4b)",
		)
	}
	// ---------------------------------------------------------
	// PostgreSQL
	// ---------------------------------------------------------

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

	if err := db.Migrate(ctx, migrations.Files); err != nil {
		log.Fatalf("database migration failed: %v", err)
	}

	log.Println("Database migrations up to date")

	// ---------------------------------------------------------
	// Persistence repositories
	// ---------------------------------------------------------

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

	emailConnectionsRepository :=
		repository.NewEmailConnectionsRepository(db.Pool())

	log.Println(
		"AlignApply persistence repositories initialized",
	)

	// ---------------------------------------------------------
	// Sign in with Apple
	// ---------------------------------------------------------

	// Optional: the web SaaS signs users in with email + password, so Sign in
	// with Apple is only enabled when APPLE_CLIENT_ID is configured.
	appleClientID := strings.TrimSpace(
		os.Getenv("APPLE_CLIENT_ID"),
	)

	var appleVerifier *auth.AppleVerifier

	if appleClientID != "" {
		appleVerifier, err =
			auth.NewAppleVerifier(appleClientID)
		if err != nil {
			log.Fatalf(
				"create Apple verifier: %v",
				err,
			)
		}
	} else {
		log.Println(
			"APPLE_CLIENT_ID not set; Sign in with Apple disabled",
		)
	}

	// ---------------------------------------------------------
	// AlignApply API sessions
	// ---------------------------------------------------------

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

	// ---------------------------------------------------------
	// Apple server-to-server authorization
	// ---------------------------------------------------------

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

	if appleServerCredentialsConfigured && appleClientID == "" {
		log.Fatal(
			"APPLE_CLIENT_ID is required when Apple server credentials are configured",
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

	// ---------------------------------------------------------
	// Web app + Stripe billing
	// ---------------------------------------------------------

	appBaseURL := strings.TrimRight(
		strings.TrimSpace(os.Getenv("APP_BASE_URL")),
		"/",
	)
	if appBaseURL == "" {
		appBaseURL = "http://localhost:" + envOrDefault("PORT", defaultPort)
	}

	api.SetTrustProxyHeaders(os.Getenv("TRUST_PROXY_HEADERS") == "true")

	billingRepository, err :=
		repository.NewBillingRepository(db.Pool())
	if err != nil {
		log.Fatalf("create billing repository: %v", err)
	}

	billingConfig := api.BillingConfig{
		WebhookSecret: strings.TrimSpace(os.Getenv("STRIPE_WEBHOOK_SECRET")),
		Plans: []api.Plan{
			{
				Key:          "monthly",
				Name:         "Pro Monthly",
				Kind:         api.PlanKindSubscription,
				Interval:     "month",
				TrialDays:    3,
				DisplayPrice: envOrDefault("PRO_MONTHLY_DISPLAY_PRICE", "$14.99"),
				PriceID:      strings.TrimSpace(os.Getenv("STRIPE_PRICE_PRO_MONTHLY")),
			},
			{
				Key:          "sprint",
				Name:         "Job Search Sprint",
				Kind:         api.PlanKindPass,
				Interval:     "90 days",
				PassDays:     90,
				DisplayPrice: envOrDefault("SPRINT_DISPLAY_PRICE", "$39"),
				PriceID:      strings.TrimSpace(os.Getenv("STRIPE_PRICE_SPRINT")),
			},
			{
				Key:          "school",
				Name:         "School License",
				Kind:         api.PlanKindOrganization,
				Interval:     "year",
				DisplayPrice: envOrDefault("SCHOOL_DISPLAY_PRICE", "$1,000"),
				PriceID:      strings.TrimSpace(os.Getenv("STRIPE_PRICE_SCHOOL")),
			},
		},
	}

	mailClient, err := mailer.NewResend(
		os.Getenv("RESEND_API_KEY"),
		os.Getenv("EMAIL_FROM"),
	)
	if err != nil {
		log.Fatalf("configure email: %v", err)
	}
	if mailClient == nil {
		log.Println("RESEND_API_KEY not set; email verification, password reset, and school licenses disabled")
	} else {
		log.Println("Transactional email configured (Resend)")
	}

	accountsRepository := repository.NewAccountsRepository(userCareerRepository)

	var stripeClient *billing.Client

	if secret := strings.TrimSpace(os.Getenv("STRIPE_SECRET_KEY")); secret != "" {
		stripeClient, err = billing.NewClient(secret)
		if err != nil {
			log.Fatalf("create Stripe client: %v", err)
		}

		if billingConfig.WebhookSecret == "" {
			log.Fatal("STRIPE_WEBHOOK_SECRET is required when STRIPE_SECRET_KEY is set")
		}

		configuredPrices := 0
		for _, plan := range billingConfig.Plans {
			if plan.PriceID != "" {
				configuredPrices++
			}
		}
		if configuredPrices == 0 {
			log.Fatal("set at least one of STRIPE_PRICE_PRO_MONTHLY, STRIPE_PRICE_SPRINT, STRIPE_PRICE_SCHOOL")
		}

		log.Println("Stripe billing configured")
	} else {
		log.Println("STRIPE_SECRET_KEY not set; billing disabled (open access until Stripe is configured)")
	}

	// ---------------------------------------------------------
	// API
	// ---------------------------------------------------------

	server := api.NewServer(
		api.Dependencies{
			Repositories: api.Repositories{
				UserCareer:       userCareerRepository,
				Jobs:             jobRepository,
				Matches:          matchRepository,
				UserJobs:         userJobsRepository,
				AppleAuth:        appleAuthRepository,
				EmailConnections: emailConnectionsRepository,
				Billing:          billingRepository,
				Accounts:         accountsRepository,
			},
			AppleVerifier:  appleVerifier,
			AppleClient:    appleClient,
			SessionManager: sessionManager,
			Stripe:         stripeClient,
			Mailer:         mailClient,
			BillingConfig:  billingConfig,
			Web: api.WebConfig{
				BaseURL:      appBaseURL,
				CookieSecure: strings.HasPrefix(appBaseURL, "https://"),
				Static:       web.Static(),
				SupportEmail: strings.TrimSpace(os.Getenv("SUPPORT_EMAIL")),
			},
		},
	)

	// Production hosts provide PORT.
	// Local development falls back to 8080.
	port := strings.TrimSpace(os.Getenv("PORT"))

	if port == "" {
		port = defaultPort
	}

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf(
		"AlignApply API listening on port %s",
		port,
	)

	if err := httpServer.ListenAndServe(); err != nil &&
		err != http.ErrServerClosed {

		log.Fatal(err)
	}
}

func envOrDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
