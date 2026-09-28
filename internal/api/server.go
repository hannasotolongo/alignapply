package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/auth"
	"github.com/hannasotolongo/casemade-backend/internal/jobs"
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
}

type Repositories struct {
	UserCareer       *repository.UserCareerRepository
	Jobs             *repository.JobRepository
	Matches          *repository.MatchRepository
	UserJobs         *repository.UserJobsRepository
	AppleAuth        *repository.AppleAuthRepository
	EmailConnections *repository.EmailConnectionsRepository
}

type Dependencies struct {
	Repositories

	AppleVerifier  *auth.AppleVerifier
	AppleClient    *auth.AppleClient
	SessionManager *auth.SessionManager
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
		mux: http.NewServeMux(),
		jobProvider: jobs.NewMultiProvider(
			jobs.NewJobOpportunitiesProvider(),
			jobs.NewLeverProvider(
				jobs.DefaultATSEmployers(),
			),
		),
		pipeline: jobs.NewPipeline(),
	}

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
				http.HandlerFunc(
					s.handleGmailConnect,
				),
			),
		)

		s.mux.Handle(
			"/api/v1/email/gmail/status",
			s.sessionManager.Middleware(
				http.HandlerFunc(
					s.handleGmailConnectionStatus,
				),
			),
		)

		s.mux.Handle(
			"/api/v1/email/gmail/disconnect",
			s.sessionManager.Middleware(
				http.HandlerFunc(
					s.handleGmailDisconnect,
				),
			),
		)

		s.mux.Handle(
			"/api/v1/email/gmail/profile",
			s.sessionManager.Middleware(
				http.HandlerFunc(
					s.handleGmailProfile,
				),
			),
		)

		s.mux.Handle(
			"/api/v1/email/gmail/messages",
			s.sessionManager.Middleware(
				http.HandlerFunc(
					s.handleGmailMessages,
				),
			),
		)

		s.mux.Handle(
			"/api/v1/email/gmail/classification-preview",
			s.sessionManager.Middleware(
				http.HandlerFunc(
					s.handleGmailClassificationPreview,
				),
			),
		)
		s.mux.Handle(
			"/api/v1/email/gmail/sync",
			s.sessionManager.Middleware(
				http.HandlerFunc(
					s.handleGmailSync,
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

	s.mux.HandleFunc(
		"/api/v1/job-matches",
		s.handleJobMatches,
	)

	s.mux.HandleFunc(
		"/api/v1/resume-recommendation",
		s.handleResumeRecommendation,
	)
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

	var request jobs.SearchRequest

	decoder := json.NewDecoder(r.Body)
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

	for index, job := range processedJobs {

		jobStarted := time.Now()

		log.Printf(
			"[job-matches] match START job=%d/%d company=%q title=%q requirements=%d",
			index+1,
			len(processedJobs),
			job.Company,
			job.Title,
			len(job.Requirements),
		)

		match :=
			matching.Match(
				request,
				job,
			)

		log.Printf(
			"[job-matches] match COMPLETE job=%d/%d duration=%s level=%q percentage=%d",
			index+1,
			len(processedJobs),
			time.Since(jobStarted),
			match.MatchLevel,
			match.MatchPercentage,
		)

		matches = append(
			matches,
			match,
		)
	}

	log.Printf(
		"[job-matches] matching COMPLETE duration=%s matches=%d",
		time.Since(matchingStarted),
		len(matches),
	)

	sortStarted := time.Now()

	sortJobMatches(matches)

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
						"Needs Review"

			rightScorable :=
				right.MatchLevel !=
					"Insufficient Evidence" &&
					right.MatchLevel !=
						"Needs Review"

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
