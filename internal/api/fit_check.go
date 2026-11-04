package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
	"github.com/hannasotolongo/casemade-backend/internal/matching"
)

const (
	fitCheckMaxResumeChars = 40_000
	fitCheckMaxJobChars    = 30_000
	fitCheckMinJobChars    = 200

	// Anonymous visitors get a few free checks per IP per day; signed-in
	// Pro users get the same hourly allowance as job searches.
	fitCheckAnonymousPerDay = 5
)

type fitCheckRequest struct {
	ResumeText     string `json:"resumeText"`
	JobDescription string `json:"jobDescription"`
	JobTitle       string `json:"jobTitle,omitempty"`
	Company        string `json:"company,omitempty"`
}

// Gap Closer: every requirement Jane couldn't confirm gets an honest next
// step instead of a bare "missing".
const (
	gapAddIfTrue  = "add_if_true" // likely you have it; your résumé just doesn't say so
	gapQuickWin   = "quick_win"   // a certification or short course can close it
	gapRealGap    = "real_gap"    // experience, degree, or license you'd need to earn
	gapStrengthen = "strengthen"  // partially shown; make the evidence clearer
)

type fitCheckGap struct {
	Requirement string `json:"requirement"`
	Category    string `json:"category"`
	Importance  string `json:"importance"`
	Kind        string `json:"kind"`
	Advice      string `json:"advice"`
}

type fitCheckResponse struct {
	MatchPercentage int    `json:"matchPercentage"`
	MatchLevel      string `json:"matchLevel"`
	Verdict         string `json:"verdict"`
	Scorable        bool   `json:"scorable"`

	SupportedCount  int `json:"supportedCount"`
	PartialCount    int `json:"partialCount"`
	MissingCount    int `json:"missingCount"`
	RequiredMissing int `json:"requiredMissing"`

	// Details are only included for users with Pro access.
	Locked      bool          `json:"locked"`
	Supported   []string      `json:"supported,omitempty"`
	Gaps        []fitCheckGap `json:"gaps,omitempty"`
	Explanation string        `json:"explanation,omitempty"`
}

// POST /api/v1/fit-check
//
// Public: anyone can paste a job and a résumé and see how they fit. The
// score is free; the requirement-by-requirement breakdown and Gap Closer
// advice need an active trial or plan.
func (s *Server) handleFitCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}

	pro := false
	limiterKey := "fit-anon:" + clientIP(r)
	limiter := s.fitCheckAnonLimiter

	if userID := s.sessionManager.OptionalUserID(r); userID != "" && s.userCareerRepo != nil {
		if user, err := s.userCareerRepo.GetUserByID(r.Context(), userID); err == nil {
			if current, err := s.entitlementFor(r.Context(), user); err == nil && current.Pro {
				pro = true
				limiterKey = "fit-user:" + user.ID
				limiter = s.fitCheckUserLimiter
			}
		}
	}

	if !limiter.Allow(limiterKey) {
		if pro {
			writeError(w, http.StatusTooManyRequests, "You've run a lot of fit checks this hour. Give Jane a few minutes and try again.")
		} else {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "You've used today's free fit checks. Start your free trial for unlimited checks and the full gap breakdown.",
				"code":  "fit_check_limit",
			})
		}
		return
	}

	var request fitCheckRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resume := strings.TrimSpace(request.ResumeText)
	job := strings.TrimSpace(request.JobDescription)
	switch {
	case resume == "":
		writeError(w, http.StatusBadRequest, "Add your résumé first.")
		return
	case len(resume) > fitCheckMaxResumeChars:
		writeError(w, http.StatusBadRequest, "That résumé is too long. Try a shorter version (under about 10 pages).")
		return
	case len(job) < fitCheckMinJobChars:
		writeError(w, http.StatusBadRequest, "Paste the full job posting, including the requirements or qualifications section.")
		return
	case len(job) > fitCheckMaxJobChars:
		writeError(w, http.StatusBadRequest, "That posting is too long. Paste just the description and requirements.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	enriched := jobs.EnrichJob(jobs.Job{
		ID:          "fit-check",
		Source:      "fit-check",
		Title:       strings.TrimSpace(request.JobTitle),
		Company:     strings.TrimSpace(request.Company),
		Description: job,
	})

	done := make(chan jobs.JobMatch, 1)
	go func() {
		done <- matching.Match(jobs.SearchRequest{ResumeText: resume}, enriched)
	}()

	var match jobs.JobMatch
	select {
	case match = <-done:
	case <-ctx.Done():
		writeError(w, http.StatusGatewayTimeout, "Jane is taking too long on this one. Try again in a minute.")
		return
	}

	writeJSON(w, http.StatusOK, buildFitCheckResponse(match, pro))
}

func buildFitCheckResponse(match jobs.JobMatch, pro bool) fitCheckResponse {
	response := fitCheckResponse{
		MatchPercentage: match.MatchPercentage,
		MatchLevel:      match.MatchLevel,
		Scorable:        match.MatchLevel != "Insufficient Evidence",
		SupportedCount:  len(match.SupportedRequirements),
		PartialCount:    len(match.PartialRequirements),
		MissingCount:    len(match.MissingRequirements),
		Locked:          !pro,
	}

	byText := make(map[string]jobs.Requirement, len(match.Requirements))
	for _, requirement := range match.Requirements {
		byText[normalizeRequirementKey(requirement.Text)] = requirement
	}

	var gaps []fitCheckGap
	for _, text := range match.MissingRequirements {
		requirement := lookupRequirement(byText, text)
		if requirement.Importance == jobs.RequirementRequired {
			response.RequiredMissing++
		}
		gaps = append(gaps, classifyGap(requirement, false))
	}
	for _, text := range match.PartialRequirements {
		gaps = append(gaps, classifyGap(lookupRequirement(byText, text), true))
	}

	response.Verdict = fitVerdict(response)

	if pro {
		response.Supported = match.SupportedRequirements
		response.Gaps = gaps
		response.Explanation = match.Explanation
	}

	return response
}

func normalizeRequirementKey(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

func lookupRequirement(byText map[string]jobs.Requirement, text string) jobs.Requirement {
	if requirement, ok := byText[normalizeRequirementKey(text)]; ok {
		return requirement
	}
	return jobs.Requirement{Text: text, Category: jobs.RequirementOther, Importance: jobs.RequirementRequired}
}

func classifyGap(requirement jobs.Requirement, partial bool) fitCheckGap {
	gap := fitCheckGap{
		Requirement: requirement.Text,
		Category:    string(requirement.Category),
		Importance:  string(requirement.Importance),
	}
	if gap.Importance == "" {
		gap.Importance = string(jobs.RequirementRequired)
	}

	if partial {
		gap.Kind = gapStrengthen
		gap.Advice = "Your résumé hints at this but doesn't show it clearly. Add a specific example (what you did, with what, and the result) so a recruiter sees it in seconds."
		return gap
	}

	switch requirement.Category {
	case jobs.RequirementCertification:
		gap.Kind = gapQuickWin
		gap.Advice = "If you hold this certification, add it to your profile. If not, this is often a short course or exam. Worth checking how fast you can earn it."
	case jobs.RequirementLicense:
		gap.Kind = gapRealGap
		gap.Advice = "If you're licensed, add the license type and state to your profile. If not, this is usually a hard requirement, so apply only if the posting says it can be earned after hire."
	case jobs.RequirementEducation:
		gap.Kind = gapRealGap
		gap.Advice = "If you have this degree or are enrolled, add it to your profile. If not, look for wording like \"or equivalent experience\" before applying."
	case jobs.RequirementExperience:
		gap.Kind = gapRealGap
		gap.Advice = "Jane couldn't find enough matching experience. If you have it, spell out the years and role on your résumé. If you don't, treat this posting as a stretch."
	case jobs.RequirementPhysical, jobs.RequirementTravel:
		gap.Kind = gapAddIfTrue
		gap.Advice = "Résumés rarely mention this. If it's true for you, you can say so in your application or interview."
	default:
		gap.Kind = gapAddIfTrue
		gap.Advice = "Jane didn't see this on your résumé. If you've used it, add it to your profile with one real example. If you haven't, leave it off."
	}
	return gap
}

func fitVerdict(response fitCheckResponse) string {
	if !response.Scorable {
		return "This posting doesn't list clear enough requirements for Jane to score. Try pasting the full description, including qualifications."
	}
	switch response.MatchLevel {
	case "Strong Applicant":
		if response.RequiredMissing == 0 {
			return "Apply. You meet what this job asks for."
		}
		return "Strong fit. Address the missing must-have before you apply."
	case "Moderate Match":
		return "Worth applying if you close a gap or two first."
	default:
		if response.MatchPercentage >= 50 {
			return "A stretch, but not out of reach. Close the must-have gaps first, or apply knowing it's a long shot."
		}
		return "Jane would skip this one. Your time is better spent on closer matches."
	}
}
