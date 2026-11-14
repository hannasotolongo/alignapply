package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

// The Free plan (Nov 2026). Anyone with an account can use Jane without
// paying, with limits that keep the free plan cheap to run:
//
//   - Job search with match scores: the top FreeMatchesShown jobs per search,
//     up to FreeSearchesPerDay searches a day. Scoring runs on Jane's own
//     server, so this costs almost nothing.
//   - What the résumé is missing: the top FreeMissingShown items per job.
//     The fixes for partial matches ("say this more clearly") are Pro.
//   - Apply on the employer's own site: always free.
//   - One cover letter and one interview practice question, ever. These use
//     the AI writer, which costs money per call.
//
// Pro and Interview Prep are unchanged.
const (
	FreeMatchesShown   = 5
	FreeMissingShown   = 3
	FreeSearchesPerDay = 10

	FreeCoverLetters          = 0
	FreeInterviewQuestionSets = 1
	FreeInterviewFeedback     = 1

	featureFreeCoverLetter        = "free_cover_letter"
	featureFreeInterviewQuestions = "free_interview_questions"
	featureFreeInterviewFeedback  = "free_interview_feedback"
)

// Lifetime allowances are counted from the start of usage records.
var freeAllowanceStart = time.Unix(0, 0)

type freeRequestKey struct{}

// isFreeRequest reports whether this request is running on a free allowance
// (see proOrFree), so a handler can trim its answer to the free amount.
func isFreeRequest(r *http.Request) bool {
	free, _ := r.Context().Value(freeRequestKey{}).(bool)
	return free
}

func writeFreeLimitReached(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusPaymentRequired, map[string]string{
		"error": message,
		"code":  "free_limit_reached",
	})
}

// statusRecorder remembers the status a handler wrote, so a free allowance
// is only used up when the request actually worked.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(body)
}

// proOrFree lets paying users through as before (Pro, or Interview Prep when
// interview is true) and gives everyone else `limit` free uses of feature.
// A use only counts when the handler answers 200.
func (s *Server) proOrFree(feature string, limit int, interview bool, limitMessage string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.requireSessionUser(w, r)
		if !ok {
			return
		}

		current, err := s.entitlementFor(r.Context(), user)
		if err != nil {
			log.Printf("load entitlement for user %s: %v", user.ID, err)
			writeError(w, http.StatusInternalServerError, "failed to load subscription")
			return
		}

		if current.Pro || (interview && current.Interview) {
			next.ServeHTTP(w, r)
			return
		}

		if s.billingRepo == nil {
			writeSubscriptionRequired(w)
			return
		}

		used, err := s.billingRepo.CountUsageSince(r.Context(), user.ID, feature, freeAllowanceStart)
		if err != nil {
			log.Printf("count %s usage for user %s: %v", feature, user.ID, err)
			writeError(w, http.StatusInternalServerError, "failed to check usage")
			return
		}
		if used >= limit {
			writeFreeLimitReached(w, limitMessage)
			return
		}

		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r.WithContext(context.WithValue(r.Context(), freeRequestKey{}, true)))

		if recorder.status == http.StatusOK {
			// A fresh context: the request's may already be done.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.billingRepo.RecordUsage(ctx, user.ID, feature); err != nil {
				log.Printf("record %s usage for user %s: %v", feature, user.ID, err)
			}
		}
	})
}

// trimMatchesForFree keeps the best FreeMatchesShown jobs and, for each, the
// top FreeMissingShown missing requirements. It returns how many more
// scored jobs Pro would show.
func trimMatchesForFree(matches []jobs.JobMatch) ([]jobs.JobMatch, int) {
	locked := 0
	if len(matches) > FreeMatchesShown {
		locked = len(matches) - FreeMatchesShown
		matches = matches[:FreeMatchesShown]
	}
	trimmed := make([]jobs.JobMatch, len(matches))
	for index, match := range matches {
		if len(match.MissingRequirements) > FreeMissingShown {
			match.MissingRequirements = append([]string(nil), match.MissingRequirements[:FreeMissingShown]...)
		}
		match.PartialRequirements = []string{}
		trimmed[index] = match
	}
	return trimmed, locked
}

// freeAllowanceResponse tells the app what a free account has left.
type freeAllowanceResponse struct {
	MatchesShown          int `json:"matchesShown"`
	MissingShown          int `json:"missingShown"`
	SearchesPerDay        int `json:"searchesPerDay"`
	SearchesToday         int `json:"searchesToday"`
	CoverLettersLeft      int `json:"coverLettersLeft"`
	InterviewSetsLeft     int `json:"interviewSetsLeft"`
	InterviewFeedbackLeft int `json:"interviewFeedbackLeft"`
}

func (s *Server) freeAllowanceFor(ctx context.Context, userID string) *freeAllowanceResponse {
	response := &freeAllowanceResponse{
		MatchesShown:          FreeMatchesShown,
		MissingShown:          FreeMissingShown,
		SearchesPerDay:        FreeSearchesPerDay,
		CoverLettersLeft:      FreeCoverLetters,
		InterviewSetsLeft:     FreeInterviewQuestionSets,
		InterviewFeedbackLeft: FreeInterviewFeedback,
	}
	if s.billingRepo == nil {
		return response
	}
	left := func(feature string, limit int) int {
		used, err := s.billingRepo.CountUsageSince(ctx, userID, feature, freeAllowanceStart)
		if err != nil {
			log.Printf("count %s usage for user %s: %v", feature, userID, err)
			return limit
		}
		return max(limit-used, 0)
	}
	response.CoverLettersLeft = left(featureFreeCoverLetter, FreeCoverLetters)
	response.InterviewSetsLeft = left(featureFreeInterviewQuestions, FreeInterviewQuestionSets)
	response.InterviewFeedbackLeft = left(featureFreeInterviewFeedback, FreeInterviewFeedback)
	if used, err := s.billingRepo.CountUsageSince(ctx, userID, featureJobMatch, time.Now().Add(-24*time.Hour)); err == nil {
		response.SearchesToday = used
	}
	return response
}
