package api

import (
	"context"
	"log"
	"net/http"
	"time"
)

// Plan limits for the interview prep relaunch (Nov 2026). Each AI call costs
// money, so every plan has a ceiling:
//
//   - Free: 3 graded answers, 3 question sets and 1 cover letter, ever.
//   - Prep: 40 graded answers, 3 tailored résumés and 3 cover letters a
//     month; question sets are unlimited (within the abuse guard).
//   - Pro and Lifetime: fair use of 300 graded answers and 300 documents a
//     month.
const (
	FreeGradedAnswers         = 3
	PrepGradedAnswersPerMonth = 40
	ProGradedAnswersPerMonth  = 300

	FreeQuestionSets         = 3
	PaidQuestionSetsPerMonth = 500

	PrepCoverLettersPerMonth = 3
	PrepResumesPerMonth      = 3
	ProDocumentsPerMonth     = 300

	featureGradedAnswer = "graded_answer"
	featureQuestionSet  = "question_set"
	featureCoverLetter  = "cover_letter"
	featureTailored     = "tailored_resume"
)

// meter is one metered feature: its free allowance (counted over all time,
// under the older free_* feature names) and its monthly paid allowances.
type meter struct {
	freeFeature string
	freeLimit   int
	feature     string
	prepLimit   int
	proLimit    int
	freeMessage string
	prepMessage string
	proMessage  string
}

var (
	gradedAnswerMeter = meter{
		freeFeature: featureFreeInterviewFeedback, freeLimit: FreeGradedAnswers,
		feature: featureGradedAnswer, prepLimit: PrepGradedAnswersPerMonth, proLimit: ProGradedAnswersPerMonth,
		freeMessage: "You've used your 3 free graded answers. Choose Prep or Pro to keep practicing with Jane.",
		prepMessage: "You've used this month's 40 graded answers. Go unlimited with Pro, or keep going next month.",
		proMessage:  "You've reached this month's fair-use limit of 300 graded answers. It resets next month.",
	}
	questionSetMeter = meter{
		freeFeature: featureFreeInterviewQuestions, freeLimit: FreeQuestionSets,
		feature: featureQuestionSet, prepLimit: PaidQuestionSetsPerMonth, proLimit: PaidQuestionSetsPerMonth,
		freeMessage: "You've used your free question sets. Choose Prep or Pro for unlimited questions.",
		prepMessage: "You've generated a lot of question sets this month. Try again next month.",
		proMessage:  "You've generated a lot of question sets this month. Try again next month.",
	}
	coverLetterMeter = meter{
		freeFeature: "", freeLimit: 0,
		feature: featureCoverLetter, prepLimit: PrepCoverLettersPerMonth, proLimit: ProDocumentsPerMonth,
		freeMessage: "Cover letters are part of Prep and Pro.",
		prepMessage: "You've used this month's 3 cover letters. Go unlimited with Pro, or write more next month.",
		proMessage:  "You've reached this month's fair-use limit for cover letters. It resets next month.",
	}
	tailoredResumeMeter = meter{
		freeFeature: "", freeLimit: 0,
		feature: featureTailored, prepLimit: PrepResumesPerMonth, proLimit: ProDocumentsPerMonth,
		freeMessage: "Tailored résumés are part of Prep and Pro.",
		prepMessage: "You've used this month's 3 tailored résumés. Go unlimited with Pro, or tailor more next month.",
		proMessage:  "You've reached this month's fair-use limit for tailored résumés. It resets next month.",
	}
)

func monthStart(now time.Time) time.Time {
	year, month, _ := now.UTC().Date()
	return time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
}

// meterState is which allowance applies to a user right now.
type meterState struct {
	feature string
	since   time.Time
	limit   int
	free    bool
	message string
}

func (m meter) stateFor(current entitlement) meterState {
	switch {
	case current.Pro:
		return meterState{feature: m.feature, since: monthStart(time.Now()), limit: m.proLimit, message: m.proMessage}
	case current.Interview:
		return meterState{feature: m.feature, since: monthStart(time.Now()), limit: m.prepLimit, message: m.prepMessage}
	default:
		return meterState{feature: m.freeFeature, since: freeAllowanceStart, limit: m.freeLimit, free: true, message: m.freeMessage}
	}
}

// metered allows a request while the user's plan has allowance left, and
// counts it once it worked.
func (s *Server) metered(m meter, next http.Handler) http.Handler {
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
		state := m.stateFor(current)
		if state.limit <= 0 || state.feature == "" {
			writeJSON(w, http.StatusPaymentRequired, map[string]string{"error": state.message, "code": "pro_required"})
			return
		}
		if s.billingRepo == nil {
			if state.free {
				writeSubscriptionRequired(w)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		used, err := s.billingRepo.CountUsageSince(r.Context(), user.ID, state.feature, state.since)
		if err != nil {
			log.Printf("count %s usage for user %s: %v", state.feature, user.ID, err)
			writeError(w, http.StatusInternalServerError, "failed to check usage")
			return
		}
		if used >= state.limit {
			code := "plan_limit_reached"
			if state.free {
				code = "free_limit_reached"
			}
			writeJSON(w, http.StatusPaymentRequired, map[string]string{"error": state.message, "code": code})
			return
		}
		ctx := r.Context()
		if state.free {
			ctx = context.WithValue(ctx, freeRequestKey{}, true)
		}
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r.WithContext(ctx))
		if recorder.status == http.StatusOK {
			recordCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.billingRepo.RecordUsage(recordCtx, user.ID, state.feature); err != nil {
				log.Printf("record %s usage for user %s: %v", state.feature, user.ID, err)
			}
		}
	})
}

// planUsage is what a user has used of each allowance, for the app.
type planUsage struct {
	Period         string `json:"period"`
	AnswersUsed    int    `json:"answersUsed"`
	AnswersLimit   int    `json:"answersLimit"`
	ResumesUsed    int    `json:"resumesUsed"`
	ResumesLimit   int    `json:"resumesLimit"`
	LettersUsed    int    `json:"lettersUsed"`
	LettersLimit   int    `json:"lettersLimit"`
	QuestionsUsed  int    `json:"questionSetsUsed"`
	QuestionsLimit int    `json:"questionSetsLimit"`
}

func (s *Server) planUsageFor(ctx context.Context, userID string, current entitlement) *planUsage {
	usage := &planUsage{Period: "month"}
	if !current.Pro && !current.Interview {
		usage.Period = "total"
	}
	count := func(m meter) (int, int) {
		state := m.stateFor(current)
		if state.feature == "" || s.billingRepo == nil {
			return 0, state.limit
		}
		used, err := s.billingRepo.CountUsageSince(ctx, userID, state.feature, state.since)
		if err != nil {
			log.Printf("count %s usage for user %s: %v", state.feature, userID, err)
		}
		return used, state.limit
	}
	usage.AnswersUsed, usage.AnswersLimit = count(gradedAnswerMeter)
	usage.ResumesUsed, usage.ResumesLimit = count(tailoredResumeMeter)
	usage.LettersUsed, usage.LettersLimit = count(coverLetterMeter)
	usage.QuestionsUsed, usage.QuestionsLimit = count(questionSetMeter)
	return usage
}
