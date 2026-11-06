package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/hannasotolongo/casemade-backend/internal/tailor"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/auth"
	"github.com/hannasotolongo/casemade-backend/internal/billing"
	"github.com/hannasotolongo/casemade-backend/internal/jobs"
	"github.com/hannasotolongo/casemade-backend/internal/mailer"
	"github.com/hannasotolongo/casemade-backend/internal/matching"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
)

type Server struct {
	mux *http.ServeMux

	jobProvider *jobs.MultiProvider
	pipeline    *jobs.Pipeline

	userCareerRepo       *repository.UserCareerRepository
	jobRepo              *repository.JobRepository
	matchRepo            *repository.MatchRepository
	userJobsRepo         *repository.UserJobsRepository
	appleAuthRepo        *repository.AppleAuthRepository
	emailConnectionsRepo *repository.EmailConnectionsRepository

	appleVerifier  *auth.AppleVerifier
	appleClient    *auth.AppleClient
	sessionManager *auth.SessionManager

	billingRepo   *repository.BillingRepository
	accountsRepo  *repository.AccountsRepository
	crawledJobs   *repository.CrawledJobsRepository
	mailer        *mailer.Client
	stripe        *billing.Client
	billingConfig BillingConfig
	web           WebConfig
	authLimiter   *attemptLimiter

	applicationKit *tailor.Client
	kitLimiter     *attemptLimiter

	fitCheckUserLimiter *attemptLimiter
}

// WebConfig configures the browser-facing SaaS: where it is hosted (used for
// Stripe redirect URLs), cookie security, and the static frontend to serve.
type WebConfig struct {
	BaseURL      string
	CookieSecure bool
	Static       fs.FS
	SupportEmail string
}

type Repositories struct {
	UserCareer       *repository.UserCareerRepository
	Jobs             *repository.JobRepository
	Matches          *repository.MatchRepository
	UserJobs         *repository.UserJobsRepository
	AppleAuth        *repository.AppleAuthRepository
	EmailConnections *repository.EmailConnectionsRepository
	Billing          *repository.BillingRepository
	Accounts         *repository.AccountsRepository
	// CrawledJobs holds the jobs Jane's own crawler has saved (optional).
	CrawledJobs *repository.CrawledJobsRepository
}

type Dependencies struct {
	Repositories

	// ApplicationKit builds truth-locked tailored résumés (optional).
	ApplicationKit *tailor.Client

	AppleVerifier  *auth.AppleVerifier
	AppleClient    *auth.AppleClient
	SessionManager *auth.SessionManager

	Stripe        *billing.Client
	Mailer        *mailer.Client
	BillingConfig BillingConfig
	Web           WebConfig
}

type appleSignInRequest struct {
	IdentityToken     string `json:"identityToken"`
	AuthorizationCode string `json:"authorizationCode,omitempty"`
	DisplayName       string `json:"displayName,omitempty"`
}

type authUserResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

type appleSignInResponse struct {
	AccessToken string           `json:"accessToken"`
	TokenType   string           `json:"tokenType"`
	User        authUserResponse `json:"user"`
}

func NewServer(
	dependencies ...Dependencies,
) *Server {
	server := &Server{
		mux:      http.NewServeMux(),
		pipeline: jobs.NewPipeline(),

		authLimiter: newAttemptLimiter(20, 15*time.Minute),

		kitLimiter:          newAttemptLimiter(20, time.Hour),
		fitCheckUserLimiter: newAttemptLimiter(ProJobMatchesPerHour, time.Hour),
	}

	server.jobProvider = newJobProvider(server)

	if len(dependencies) > 0 {
		deps := dependencies[0]

		server.userCareerRepo =
			deps.Repositories.UserCareer

		server.jobRepo =
			deps.Repositories.Jobs

		server.matchRepo =
			deps.Repositories.Matches

		server.userJobsRepo =
			deps.Repositories.UserJobs

		server.appleAuthRepo =
			deps.Repositories.AppleAuth

		server.emailConnectionsRepo =
			deps.Repositories.EmailConnections

		server.appleVerifier =
			deps.AppleVerifier

		server.appleClient =
			deps.AppleClient

		server.sessionManager =
			deps.SessionManager

		server.billingRepo =
			deps.Repositories.Billing

		server.accountsRepo = deps.Repositories.Accounts
		server.crawledJobs = deps.Repositories.CrawledJobs
		server.mailer = deps.Mailer
		server.stripe = deps.Stripe
		server.billingConfig = deps.BillingConfig
		server.applicationKit = deps.ApplicationKit
		server.web = deps.Web
		server.web.BaseURL = strings.TrimRight(
			strings.TrimSpace(server.web.BaseURL),
			"/",
		)
	}

	server.routes()

	return server
}

func (s *Server) routes() {
	s.mux.HandleFunc(
		"/healthz",
		s.handleHealth,
	)

	s.mux.HandleFunc(
		"/api/v1/auth/apple",
		s.handleAppleSignIn,
	)

	if s.sessionManager != nil {
		s.mux.Handle(
			"/api/v1/me",
			s.sessionManager.Middleware(
				http.HandlerFunc(
					s.handleMe,
				),
			),
		)

		s.mux.Handle(
			"/api/v1/account",
			s.sessionManager.Middleware(
				http.HandlerFunc(
					s.handleAccount,
				),
			),
		)

		s.mux.Handle(
			"/api/v1/career-profile",
			s.sessionManager.Middleware(
				http.HandlerFunc(
					s.handleCareerProfile,
				),
			),
		)

		s.mux.Handle(
			"/api/v1/applications",
			s.sessionManager.Middleware(
				http.HandlerFunc(
					s.handleApplications,
				),
			),
		)

		s.mux.Handle(
			"/api/v1/applications/",
			s.sessionManager.Middleware(
				http.HandlerFunc(
					s.handleApplicationResource,
				),
			),
		)

		s.mux.Handle(
			"/api/v1/email/gmail/connect",
			s.sessionManager.Middleware(
				s.requirePro(
					http.HandlerFunc(
						s.handleGmailConnect,
					),
				),
			),
		)

		s.mux.Handle(
			"/api/v1/email/gmail/status",
			s.sessionManager.Middleware(
				s.requirePro(
					http.HandlerFunc(
						s.handleGmailConnectionStatus,
					),
				),
			),
		)

		s.mux.Handle(
			"/api/v1/email/gmail/disconnect",
			s.sessionManager.Middleware(
				s.requirePro(
					http.HandlerFunc(
						s.handleGmailDisconnect,
					),
				),
			),
		)

		s.mux.Handle(
			"/api/v1/email/gmail/profile",
			s.sessionManager.Middleware(
				s.requirePro(
					http.HandlerFunc(
						s.handleGmailProfile,
					),
				),
			),
		)

		s.mux.Handle(
			"/api/v1/email/gmail/messages",
			s.sessionManager.Middleware(
				s.requirePro(
					http.HandlerFunc(
						s.handleGmailMessages,
					),
				),
			),
		)

		s.mux.Handle(
			"/api/v1/email/gmail/classification-preview",
			s.sessionManager.Middleware(
				s.requirePro(
					http.HandlerFunc(
						s.handleGmailClassificationPreview,
					),
				),
			),
		)
		s.mux.Handle(
			"/api/v1/email/gmail/sync",
			s.sessionManager.Middleware(
				s.requirePro(
					http.HandlerFunc(
						s.handleGmailSync,
					),
				),
			),
		)

		s.mux.HandleFunc(
			"/api/v1/email/gmail/callback",
			s.handleGmailCallback,
		)
	} else {
		s.mux.HandleFunc(
			"/api/v1/me",
			s.handleAuthenticationUnavailable,
		)

		s.mux.HandleFunc(
			"/api/v1/account",
			s.handleAuthenticationUnavailable,
		)

		s.mux.HandleFunc(
			"/api/v1/career-profile",
			s.handleAuthenticationUnavailable,
		)

		s.mux.HandleFunc(
			"/api/v1/applications",
			s.handleAuthenticationUnavailable,
		)

		s.mux.HandleFunc(
			"/api/v1/applications/",
			s.handleAuthenticationUnavailable,
		)
	}

	// -------------------------------------------------------------
	// Paid / metered features. These run the expensive provider and
	// LLM pipeline, so they always require a signed-in user.
	// -------------------------------------------------------------

	if s.sessionManager != nil {
		s.mux.Handle(
			"/api/v1/job-matches",
			s.sessionManager.Middleware(
				http.HandlerFunc(s.handleJobMatches),
			),
		)

		s.mux.Handle(
			"/api/v1/resume-recommendation",
			s.sessionManager.Middleware(
				s.requirePro(
					http.HandlerFunc(s.handleResumeRecommendation),
				),
			),
		)

		// Email verification and password reset.
		s.mux.Handle(
			"/api/v1/auth/verify-email/send",
			s.sessionManager.Middleware(
				http.HandlerFunc(s.handleSendVerificationEmail),
			),
		)
		s.mux.HandleFunc("/api/v1/auth/password-reset/request", s.handlePasswordResetRequest)
		s.mux.HandleFunc("/api/v1/auth/password-reset/confirm", s.handlePasswordResetConfirm)
		s.mux.HandleFunc("/verify-email", s.handleVerifyEmailLink)

		// Web (email + password) authentication.
		s.mux.HandleFunc("/api/v1/fit-check", s.handleFitCheck)
		s.mux.Handle("/api/v1/application-kit", s.sessionManager.Middleware(s.requirePro(http.HandlerFunc(s.handleApplicationKit))))
		s.mux.Handle("/api/v1/interview-feedback", s.sessionManager.Middleware(s.requireInterview(http.HandlerFunc(s.handleInterviewFeedback))))
		s.mux.Handle("/api/v1/interview-questions", s.sessionManager.Middleware(s.requireInterview(http.HandlerFunc(s.handleInterviewQuestions))))
		s.mux.HandleFunc("/api/v1/auth/signup", s.handlePasswordSignUp)
		s.mux.HandleFunc("/api/v1/auth/login", s.handlePasswordLogin)
		s.mux.HandleFunc("/api/v1/auth/logout", s.handleLogout)

		s.mux.Handle(
			"/api/v1/resumes",
			s.sessionManager.Middleware(
				http.HandlerFunc(s.handleResumes),
			),
		)

		s.mux.Handle(
			"/api/v1/resumes/",
			s.sessionManager.Middleware(
				http.HandlerFunc(s.handleResumeResource),
			),
		)

		s.mux.Handle(
			"/api/v1/preferences",
			s.sessionManager.Middleware(
				http.HandlerFunc(s.handlePreferences),
			),
		)

		s.mux.Handle(
			"/api/v1/applications/track",
			s.sessionManager.Middleware(
				http.HandlerFunc(s.handleTrackJob),
			),
		)

		// Billing.
		s.mux.Handle(
			"/api/v1/billing/status",
			s.sessionManager.Middleware(
				http.HandlerFunc(s.handleBillingStatus),
			),
		)

		s.mux.Handle(
			"/api/v1/billing/checkout",
			s.sessionManager.Middleware(
				http.HandlerFunc(s.handleBillingCheckout),
			),
		)

		s.mux.Handle(
			"/api/v1/billing/portal",
			s.sessionManager.Middleware(
				http.HandlerFunc(s.handleBillingPortal),
			),
		)
	} else {
		s.mux.HandleFunc(
			"/api/v1/job-matches",
			s.handleAuthenticationUnavailable,
		)

		s.mux.HandleFunc(
			"/api/v1/resume-recommendation",
			s.handleAuthenticationUnavailable,
		)
	}

	s.mux.HandleFunc("/api/v1/billing/plans", s.handleBillingPlans)

	// Stripe calls this directly; it is authenticated by signature.
	s.mux.HandleFunc("/api/v1/billing/webhook", s.handleStripeWebhook)

	// Unknown API paths return JSON 404s rather than the web app.
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})

	if s.web.Static != nil {
		for path := range legalPages {
			s.mux.HandleFunc(path, s.handleLegalPage)
		}
		s.mux.Handle("/", newStaticHandler(s.web.Static))
	}
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) handleHealth(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		map[string]string{
			"status": "healthy",
		},
	)
}

func (s *Server) handleAppleSignIn(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	if s.appleVerifier == nil ||
		s.sessionManager == nil ||
		s.userCareerRepo == nil {

		writeJSON(
			w,
			http.StatusServiceUnavailable,
			map[string]string{
				"error": "authentication unavailable",
			},
		)
		return
	}

	var request appleSignInRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&request); err != nil {
		writeJSON(
			w,
			http.StatusBadRequest,
			map[string]string{
				"error": "invalid request body",
			},
		)
		return
	}

	request.IdentityToken =
		strings.TrimSpace(
			request.IdentityToken,
		)

	request.AuthorizationCode =
		strings.TrimSpace(
			request.AuthorizationCode,
		)

	request.DisplayName =
		strings.TrimSpace(
			request.DisplayName,
		)

	if request.IdentityToken == "" {
		writeJSON(
			w,
			http.StatusBadRequest,
			map[string]string{
				"error": "identityToken is required",
			},
		)
		return
	}

	verifyCtx, cancel :=
		context.WithTimeout(
			r.Context(),
			10*time.Second,
		)
	defer cancel()

	identity, err :=
		s.appleVerifier.Verify(
			verifyCtx,
			request.IdentityToken,
		)

	if err != nil {
		log.Printf(
			"Apple identity verification failed: %v",
			err,
		)

		writeJSON(
			w,
			http.StatusUnauthorized,
			map[string]string{
				"error": "invalid Apple identity token",
			},
		)
		return
	}

	var refreshToken string

	if s.appleClient != nil {
		if s.appleAuthRepo == nil {
			log.Printf(
				"Apple authentication repository unavailable",
			)

			writeJSON(
				w,
				http.StatusServiceUnavailable,
				map[string]string{
					"error": "authentication unavailable",
				},
			)
			return
		}

		if request.AuthorizationCode == "" {
			writeJSON(
				w,
				http.StatusBadRequest,
				map[string]string{
					"error": "authorizationCode is required",
				},
			)
			return
		}

		exchangeCtx, exchangeCancel :=
			context.WithTimeout(
				r.Context(),
				10*time.Second,
			)
		defer exchangeCancel()

		tokenResponse, exchangeErr :=
			s.appleClient.ExchangeAuthorizationCode(
				exchangeCtx,
				request.AuthorizationCode,
			)

		if exchangeErr != nil {
			log.Printf(
				"Apple authorization code exchange failed: %v",
				exchangeErr,
			)

			writeJSON(
				w,
				http.StatusUnauthorized,
				map[string]string{
					"error": "Apple authorization failed",
				},
			)
			return
		}

		exchangedVerifyCtx, exchangedVerifyCancel :=
			context.WithTimeout(
				r.Context(),
				10*time.Second,
			)
		defer exchangedVerifyCancel()

		exchangedIdentity, verifyErr :=
			s.appleVerifier.Verify(
				exchangedVerifyCtx,
				tokenResponse.IDToken,
			)

		if verifyErr != nil {
			log.Printf(
				"Apple exchanged identity verification failed: %v",
				verifyErr,
			)

			writeJSON(
				w,
				http.StatusUnauthorized,
				map[string]string{
					"error": "Apple authorization failed",
				},
			)
			return
		}

		if exchangedIdentity.Subject !=
			identity.Subject {

			log.Printf(
				"Apple identity subject mismatch",
			)

			writeJSON(
				w,
				http.StatusUnauthorized,
				map[string]string{
					"error": "Apple authorization failed",
				},
			)
			return
		}

		refreshToken =
			strings.TrimSpace(
				tokenResponse.RefreshToken,
			)

		if refreshToken == "" {
			log.Printf(
				"Apple authorization response missing refresh token",
			)

			writeJSON(
				w,
				http.StatusUnauthorized,
				map[string]string{
					"error": "Apple authorization failed",
				},
			)
			return
		}
	}

	email := ""

	if identity.EmailVerified {
		email = strings.TrimSpace(
			identity.Email,
		)
	}

	user, err :=
		s.userCareerRepo.UpsertAppleUser(
			r.Context(),
			identity.Subject,
			email,
			request.DisplayName,
		)

	if err != nil {
		log.Printf(
			"Apple user upsert failed: %v",
			err,
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to create user session",
			},
		)
		return
	}

	if refreshToken != "" {
		_, err =
			s.appleAuthRepo.UpsertRefreshToken(
				r.Context(),
				user.ID,
				refreshToken,
			)

		if err != nil {
			log.Printf(
				"Apple refresh token persistence failed: %v",
				err,
			)

			writeJSON(
				w,
				http.StatusInternalServerError,
				map[string]string{
					"error": "failed to create user session",
				},
			)
			return
		}
	}

	accessToken, err :=
		s.sessionManager.Create(
			user.ID,
		)

	if err != nil {
		log.Printf(
			"session creation failed: %v",
			err,
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to create user session",
			},
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		appleSignInResponse{
			AccessToken: accessToken,
			TokenType:   "Bearer",
			User: authUserResponse{
				ID:          user.ID,
				Email:       user.Email,
				DisplayName: user.DisplayName,
			},
		},
	)
}

func (s *Server) handleMe(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	if s.userCareerRepo == nil {
		writeJSON(
			w,
			http.StatusServiceUnavailable,
			map[string]string{
				"error": "persistence unavailable",
			},
		)
		return
	}

	userID, err :=
		auth.RequireUserID(r)

	if err != nil {
		writeJSON(
			w,
			http.StatusUnauthorized,
			map[string]string{
				"error": "unauthorized",
			},
		)
		return
	}

	user, err :=
		s.userCareerRepo.GetUserByID(
			r.Context(),
			userID,
		)

	if err != nil {
		writeJSON(
			w,
			http.StatusUnauthorized,
			map[string]string{
				"error": "unauthorized",
			},
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		authUserResponse{
			ID:          user.ID,
			Email:       user.Email,
			DisplayName: user.DisplayName,
		},
	)
}

func (s *Server) handleAccount(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodDelete {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	if s.userCareerRepo == nil ||
		s.appleAuthRepo == nil {

		writeJSON(
			w,
			http.StatusServiceUnavailable,
			map[string]string{
				"error": "account deletion unavailable",
			},
		)
		return
	}

	userID, err :=
		auth.RequireUserID(r)

	if err != nil {
		writeJSON(
			w,
			http.StatusUnauthorized,
			map[string]string{
				"error": "unauthorized",
			},
		)
		return
	}

	// Confirm that this session still belongs to an active user.
	if _, err :=
		s.userCareerRepo.GetUserByID(
			r.Context(),
			userID,
		); err != nil {

		writeJSON(
			w,
			http.StatusUnauthorized,
			map[string]string{
				"error": "unauthorized",
			},
		)
		return
	}

	credential, credentialErr :=
		s.appleAuthRepo.GetByUserID(
			r.Context(),
			userID,
		)

	if credentialErr == nil {
		// If this account has an Apple refresh token, revoke the
		// Sign in with Apple authorization before deleting the
		// local account and its dependent data.
		if s.appleClient == nil {
			log.Printf(
				"account deletion blocked: Apple revocation client unavailable for user %s",
				userID,
			)

			writeJSON(
				w,
				http.StatusServiceUnavailable,
				map[string]string{
					"error": "account deletion temporarily unavailable",
				},
			)
			return
		}

		revokeCtx, revokeCancel :=
			context.WithTimeout(
				r.Context(),
				10*time.Second,
			)
		defer revokeCancel()

		if err :=
			s.appleClient.RevokeRefreshToken(
				revokeCtx,
				credential.RefreshToken,
			); err != nil {

			log.Printf(
				"Apple authorization revocation failed for user %s: %v",
				userID,
				err,
			)

			writeJSON(
				w,
				http.StatusBadGateway,
				map[string]string{
					"error": "failed to revoke Apple authorization",
				},
			)
			return
		}
	} else if !errors.Is(
		credentialErr,
		repository.ErrAppleAuthCredentialNotFound,
	) {
		log.Printf(
			"Apple credential lookup failed for user %s: %v",
			userID,
			credentialErr,
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to delete account",
			},
		)
		return
	}

	if err := s.cancelBillingForDeletedAccount(
		r.Context(),
		userID,
	); err != nil {
		log.Printf(
			"account deletion blocked: cancel subscription for user %s: %v",
			userID,
			err,
		)

		writeError(
			w,
			http.StatusBadGateway,
			"failed to cancel subscription; account not deleted",
		)
		return
	}

	if err :=
		s.userCareerRepo.DeleteUser(
			r.Context(),
			userID,
		); err != nil {

		log.Printf(
			"account deletion failed for user %s: %v",
			userID,
			err,
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to delete account",
			},
		)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAuthenticationUnavailable(
	w http.ResponseWriter,
	r *http.Request,
) {
	writeJSON(
		w,
		http.StatusServiceUnavailable,
		map[string]string{
			"error": "authentication unavailable",
		},
	)
}

func (s *Server) handleJobMatches(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestStarted := time.Now()

	log.Printf(
		"[job-matches] REQUEST START",
	)

	defer func() {
		log.Printf(
			"[job-matches] REQUEST COMPLETE total=%s",
			time.Since(requestStarted),
		)
	}()

	if r.Method != http.MethodPost {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}

	var request jobs.SearchRequest

	decoder := json.NewDecoder(
		http.MaxBytesReader(w, r.Body, maxApplicationRequestBodyBytes),
	)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&request); err != nil {
		http.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	normalizeSearchRequest(&request)

	if !request.CareerProfile.HasEvidence() &&
		request.ResumeText == "" {

		http.Error(
			w,
			"careerProfile or resumeText is required",
			http.StatusBadRequest,
		)
		return
	}

	if request.TargetRole == "" {
		http.Error(
			w,
			"targetRole is required",
			http.StatusBadRequest,
		)
		return
	}

	if request.Location == "" {
		http.Error(
			w,
			"location is required",
			http.StatusBadRequest,
		)
		return
	}

	log.Printf(
		"[job-matches] validated request role=%q location=%q",
		request.TargetRole,
		request.Location,
	)

	// Count the search only once it is valid, so malformed requests do not
	// spend a free user's weekly allowance.
	if !s.consumeJobMatchQuota(w, r, user) {
		return
	}

	ctx, cancel :=
		context.WithTimeout(
			r.Context(),
			20*time.Second,
		)
	defer cancel()

	searchStarted := time.Now()

	log.Printf(
		"[job-matches] provider search START",
	)

	searchResult :=
		s.jobProvider.Search(
			ctx,
			request,
		)

	log.Printf(
		"[job-matches] provider search COMPLETE duration=%s jobs=%d errors=%d",
		time.Since(searchStarted),
		len(searchResult.Jobs),
		len(searchResult.Errors),
	)

	for _, providerError := range searchResult.Errors {

		log.Printf(
			"job provider %s failed: %v",
			providerError.Provider,
			providerError.Err,
		)
	}

	pipelineStarted := time.Now()

	log.Printf(
		"[job-matches] pipeline START jobs=%d",
		len(searchResult.Jobs),
	)

	processedJobs :=
		s.pipeline.Process(
			request,
			searchResult.Jobs,
		)

	log.Printf(
		"[job-matches] pipeline COMPLETE duration=%s jobs=%d",
		time.Since(pipelineStarted),
		len(processedJobs),
	)

	matches := make(
		[]jobs.JobMatch,
		0,
		len(processedJobs),
	)

	matchingStarted := time.Now()

	log.Printf(
		"[job-matches] matching START jobs=%d",
		len(processedJobs),
	)

	// Score several jobs at once. Search uses the fast scorer (no OpenAI
	// calls per requirement) plus a cache, so 50 jobs take seconds, not minutes.
	matches = matches[:len(processedJobs)]
	const jobWorkers = 8
	work := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < jobWorkers && worker < len(processedJobs); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range work {
				matches[index] = previewOrScore(request, processedJobs[index])
				// Distance depends on this search's place, not the cached score.
				matches[index].DistanceMiles = processedJobs[index].DistanceMiles
			}
		}()
	}
	for index := range processedJobs {
		work <- index
	}
	close(work)
	wg.Wait()

	log.Printf(
		"[job-matches] matching COMPLETE duration=%s matches=%d",
		time.Since(matchingStarted),
		len(matches),
	)

	// Postings with no readable requirements can't be scored; showing them
	// as 0% results is noise, so they're left out of the response.
	scored := matches[:0]
	for _, match := range matches {
		if match.MatchLevel != "Insufficient Evidence" {
			scored = append(scored, match)
		}
	}
	matches = scored

	sortStarted := time.Now()

	sortJobMatches(matches)

	// Jane's crawler can find hundreds of jobs for a common role; the best
	// fits come first, so the list stops at a readable length.
	if len(matches) > maxJobMatchesReturned {
		matches = matches[:maxJobMatchesReturned]
	}

	log.Printf(
		"[job-matches] sorting COMPLETE duration=%s",
		time.Since(sortStarted),
	)

	responseStarted := time.Now()

	log.Printf(
		"[job-matches] response START matches=%d",
		len(matches),
	)

	writeJSON(
		w,
		http.StatusOK,
		matches,
	)

	log.Printf(
		"[job-matches] response COMPLETE duration=%s",
		time.Since(responseStarted),
	)
}

func normalizeSearchRequest(
	request *jobs.SearchRequest,
) {
	request.ResumeText =
		strings.TrimSpace(
			request.ResumeText,
		)

	request.CareerProfile.Normalize()

	request.TargetRole =
		strings.TrimSpace(
			request.TargetRole,
		)

	request.Location =
		strings.TrimSpace(
			request.Location,
		)

	request.MinimumSalary =
		strings.TrimSpace(
			request.MinimumSalary,
		)

	request.CountryCode =
		strings.ToUpper(
			strings.TrimSpace(
				request.CountryCode,
			),
		)

	if len(request.EmploymentType) > 0 {
		normalized := make(
			[]string,
			0,
			len(request.EmploymentType),
		)

		seen :=
			make(
				map[string]struct{},
			)

		for _, value := range request.EmploymentType {

			value =
				strings.TrimSpace(
					value,
				)

			if value == "" {
				continue
			}

			key :=
				strings.ToLower(
					value,
				)

			if _, exists :=
				seen[key]; exists {

				continue
			}

			seen[key] =
				struct{}{}

			normalized =
				append(
					normalized,
					value,
				)
		}

		request.EmploymentType =
			normalized
	}
}

func sortJobMatches(
	matches []jobs.JobMatch,
) {
	sort.SliceStable(
		matches,
		func(i, j int) bool {
			left := matches[i]
			right := matches[j]

			leftScorable :=
				left.MatchLevel !=
					"Insufficient Evidence" &&
					left.MatchLevel !=
						"Needs Review" &&
					left.MatchLevel != previewMatchLevel

			rightScorable :=
				right.MatchLevel !=
					"Insufficient Evidence" &&
					right.MatchLevel !=
						"Needs Review" &&
					right.MatchLevel != previewMatchLevel

			if leftScorable !=
				rightScorable {

				return leftScorable
			}

			if left.MatchPercentage !=
				right.MatchPercentage {

				return left.MatchPercentage >
					right.MatchPercentage
			}

			leftLevel :=
				matchLevelRank(
					left.MatchLevel,
				)

			rightLevel :=
				matchLevelRank(
					right.MatchLevel,
				)

			if leftLevel != rightLevel {
				return leftLevel >
					rightLevel
			}

			if left.PostedAt != nil &&
				right.PostedAt != nil &&
				!left.PostedAt.Equal(
					*right.PostedAt,
				) {

				return left.PostedAt.After(
					*right.PostedAt,
				)
			}

			if left.PostedAt != nil &&
				right.PostedAt == nil {

				return true
			}

			if left.PostedAt == nil &&
				right.PostedAt != nil {

				return false
			}

			leftCompany :=
				strings.ToLower(
					strings.TrimSpace(
						left.Company,
					),
				)

			rightCompany :=
				strings.ToLower(
					strings.TrimSpace(
						right.Company,
					),
				)

			if leftCompany != rightCompany {
				return leftCompany <
					rightCompany
			}

			return strings.ToLower(
				strings.TrimSpace(
					left.Title,
				),
			) <
				strings.ToLower(
					strings.TrimSpace(
						right.Title,
					),
				)
		},
	)
}

func matchLevelRank(
	level string,
) int {
	switch level {
	case "Best Fit", "Strong Match":
		return 3

	case "Good Fit", "Moderate Match":
		return 2

	case "Reach", "Stretch":
		return 1

	default:
		return 0
	}
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	value any,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	if err :=
		json.NewEncoder(w).Encode(value); err != nil {

		log.Printf(
			"failed to encode response: %v",
			err,
		)
	}
}

// previewMatchLevel marks jobs from sources that only share a short preview
// of the description (Jooble). Jane lists them without a score
// rather than guessing a fit from a few sentences.
const previewMatchLevel = "Preview"

// maxJobMatchesReturned caps one search's results.
const maxJobMatchesReturned = 60

func previewOrScore(request jobs.SearchRequest, job jobs.Job) jobs.JobMatch {
	match := matching.MatchFast(request, job)
	if !jobs.IsPreviewSource(job.Source) {
		return match
	}
	match.MatchLevel = previewMatchLevel
	match.MatchPercentage = 0
	match.Explanation = "Jane can only see a preview of this posting. Open it to read the full description, then paste it into Fit Check and Jane will score your fit."
	match.SupportedRequirements = nil
	match.PartialRequirements = nil
	match.MissingRequirements = nil
	return match
}

// newJobProvider assembles Jane's job sources, all free: company job boards
// and other free sources, Jooble only when those come up short, and Lever
// company boards alongside. (The paid Job Opportunities feed is no longer
// used.)
func newJobProvider(server *Server) *jobs.MultiProvider {
	return jobs.NewMultiProvider(
		jobs.NewFallbackProvider(
			jobs.NoFeed{},
			15,
			// Jobs Jane's own crawler has saved from employers' job boards.
			jobs.NewStoredJobsProvider(func() jobs.BoardStore {
				if server.crawledJobs == nil {
					return nil
				}
				return server.crawledJobs
			}),
			// Every Workday employer's own job search, asked at search time.
			jobs.NewCachedProvider(jobs.NewWorkdayLiveProvider(jobs.CrawlSeedBoards()), 3*time.Hour),
			jobs.NewGreenhouseProvider(jobs.GreenhouseBoards()),
		// Every source above links straight to the employer's own job
		// posting. Job boards that send people to their own site first
		// (Remotive, Jooble) are no longer used. Federal jobs, whose
		// official application site is USAJobs, only fill in when the
		// employer sources don't have enough matching jobs for the search.
		).WithLastResort(usaJobsBackup()),
		jobs.NewLeverProvider(
			jobs.DefaultATSEmployers(),
		),
	)
}

// joobleSource is Jooble when JOOBLE_API_KEY is set. Results are cached for
// 24 hours, and calls are capped at JOOBLE_REQUEST_LIMIT (default 450) per
// JOOBLE_LIMIT_PERIOD ("month" by default, or "day"), counted in the
// database so restarts don't reset the count.
func joobleSource(server *Server) jobs.Provider {
	provider := jobs.NewJoobleProvider()
	if provider == nil {
		log.Printf("Jooble job source not configured (set JOOBLE_API_KEY)")
		return nil
	}
	limit := 450
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("JOOBLE_REQUEST_LIMIT"))); err == nil && value > 0 {
		limit = value
	}
	window, period := 30*24*time.Hour, "30 days"
	if strings.EqualFold(strings.TrimSpace(os.Getenv("JOOBLE_LIMIT_PERIOD")), "day") {
		window, period = 24*time.Hour, "day"
	}
	log.Printf("Jooble job source configured (up to %d requests per %s)", limit, period)
	budget := &externalCallBudget{server: server, source: jobs.SourceJooble, limit: limit, window: window, memory: jobs.NewMemoryBudget(limit, window)}
	return jobs.NewCachedProvider(provider.WithBudget(budget), 24*time.Hour)
}

// externalCallBudget counts calls in the database when there is one, and in
// memory otherwise.
type externalCallBudget struct {
	server *Server
	source string
	limit  int
	window time.Duration
	memory *jobs.MemoryBudget
}

func (b *externalCallBudget) Reserve(ctx context.Context) (bool, error) {
	if b.server.billingRepo == nil {
		return b.memory.Reserve(ctx)
	}
	allowed, err := b.server.billingRepo.ReserveAPICall(ctx, b.source, b.limit, time.Now().Add(-b.window))
	if err == nil && !allowed {
		log.Printf("[job-backups] %s request allowance used up (%d per window); skipping it until calls age out", b.source, b.limit)
	}
	return allowed, err
}

// usaJobsBackup is the USAJobs source when its free key is configured.
func usaJobsBackup() jobs.Provider {
	provider := jobs.NewUSAJobsProvider()
	if provider == nil {
		log.Printf("USAJobs backup source not configured (set USAJOBS_API_KEY and USAJOBS_EMAIL)")
		return nil
	}
	log.Printf("USAJobs backup source configured")
	return jobs.NewCachedProvider(provider, 6*time.Hour)
}
