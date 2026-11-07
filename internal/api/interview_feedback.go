package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/tailor"
)

type interviewFeedbackRequest struct {
	Question       string `json:"question"`
	Answer         string `json:"answer"`
	ResumeText     string `json:"resumeText"`
	JobDescription string `json:"jobDescription"`
	JobTitle       string `json:"jobTitle,omitempty"`
	// Stage is the interview stage being practiced, for stage-aware coaching.
	Stage string `json:"stage,omitempty"`
}

// POST /api/v1/interview-feedback (Pro)
func (s *Server) handleInterviewFeedback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	if s.applicationKit == nil {
		writeError(w, http.StatusServiceUnavailable, "Interview feedback isn't available right now. Please try again later.")
		return
	}
	if !s.kitLimiter.Allow("feedback:" + user.ID) {
		writeError(w, http.StatusTooManyRequests, "You've practiced a lot this hour. Take a short break and try again.")
		return
	}

	var request interviewFeedbackRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	request.Question = strings.TrimSpace(request.Question)
	request.Answer = strings.TrimSpace(request.Answer)
	switch {
	case request.Question == "" || len(request.Question) > 1000:
		writeError(w, http.StatusBadRequest, "Pick an interview question to practice.")
		return
	case len(request.Answer) < 40:
		writeError(w, http.StatusBadRequest, "Write a little more of your answer (a few sentences) so Jane can coach you.")
		return
	case len(request.Answer) > 6000:
		writeError(w, http.StatusBadRequest, "That answer is long for an interview. Try trimming it to a minute or two of speaking.")
		return
	case len(request.ResumeText) > fitCheckMaxResumeChars || len(request.JobDescription) > fitCheckMaxJobChars:
		writeError(w, http.StatusBadRequest, "That résumé or posting is too long.")
		return
	}

	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	feedback, err := s.applicationKit.Feedback(ctx, tailor.FeedbackRequest{
		Question:       request.Question,
		Answer:         request.Answer,
		ResumeText:     strings.TrimSpace(request.ResumeText),
		JobDescription: strings.TrimSpace(request.JobDescription),
		JobTitle:       strings.TrimSpace(request.JobTitle),
		Stage:          strings.TrimSpace(request.Stage),
	})
	if err != nil {
		if errors.Is(err, tailor.ErrUnavailable) {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		log.Printf("[interview-feedback] FAILED user=%s after=%s: %v", user.ID, time.Since(started), err)
		writeError(w, http.StatusBadGateway, "Jane couldn't review that answer just now. Try again in a minute.")
		return
	}
	log.Printf("[interview-feedback] DONE user=%s after=%s", user.ID, time.Since(started))
	writeJSON(w, http.StatusOK, feedback)
}

// maxExcludedQuestions caps how many earlier questions "More questions"
// sends back, which keeps the request small.
const maxExcludedQuestions = 60

type interviewQuestionsRequest struct {
	ResumeText     string `json:"resumeText"`
	JobDescription string `json:"jobDescription,omitempty"`
	JobTitle       string `json:"jobTitle,omitempty"`
	// Company is the employer the interview is with, so questions can
	// reflect it.
	Company string `json:"company,omitempty"`
	// Stage is the interview type to practice for (phone, behavioral,
	// role, final, ask, or mixed by default).
	Stage string `json:"stage,omitempty"`
	// Exclude lists questions already practiced, for "More questions".
	Exclude []string `json:"exclude,omitempty"`
	// JobID loads the description of a job saved in My applications.
	JobID string `json:"jobId,omitempty"`
}

// POST /api/v1/interview-questions (Pro or Interview Prep)
func (s *Server) handleInterviewQuestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	if s.applicationKit == nil {
		writeError(w, http.StatusServiceUnavailable, "Interview practice isn't available right now. Please try again later.")
		return
	}
	if !s.kitLimiter.Allow("questions:" + user.ID) {
		writeError(w, http.StatusTooManyRequests, "You've generated a lot of practice sets this hour. Take a short break and try again.")
		return
	}

	var request interviewQuestionsRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	request.ResumeText = strings.TrimSpace(request.ResumeText)
	request.JobDescription = strings.TrimSpace(request.JobDescription)
	request.JobTitle = strings.TrimSpace(request.JobTitle)

	if jobID := strings.TrimSpace(request.JobID); jobID != "" && s.userJobsRepo != nil && s.jobRepo != nil {
		if _, err := s.userJobsRepo.GetApplication(r.Context(), user.ID, jobID); err != nil {
			writeError(w, http.StatusNotFound, "That job isn't in your applications.")
			return
		}
		job, err := s.jobRepo.GetJobByID(r.Context(), jobID)
		if err != nil {
			writeError(w, http.StatusNotFound, "Couldn't load that job.")
			return
		}
		request.JobDescription = strings.TrimSpace(job.Description)
		if request.JobTitle == "" {
			request.JobTitle = strings.TrimSpace(job.Title + " at " + job.Company)
		}
	}

	if company := strings.TrimSpace(request.Company); company != "" && !strings.Contains(strings.ToLower(request.JobTitle), strings.ToLower(company)) {
		if request.JobTitle == "" {
			request.JobTitle = "Interview at " + company
		} else {
			request.JobTitle += " at " + company
		}
	}

	switch {
	case request.ResumeText == "":
		writeError(w, http.StatusBadRequest, "Add a résumé first so Jane can tailor your practice.")
		return
	case len(request.JobDescription) < 80:
		writeError(w, http.StatusBadRequest, "Paste the job posting (or pick a saved job with a description) so Jane knows what they'll ask.")
		return
	case len(request.ResumeText) > fitCheckMaxResumeChars || len(request.JobDescription) > fitCheckMaxJobChars:
		writeError(w, http.StatusBadRequest, "That résumé or posting is too long.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	exclude := make([]string, 0, len(request.Exclude))
	for _, question := range request.Exclude {
		if question = strings.TrimSpace(question); question != "" && len(exclude) < maxExcludedQuestions {
			if len(question) > 300 {
				question = question[:300]
			}
			exclude = append(exclude, question)
		}
	}
	questions, err := s.applicationKit.Questions(ctx, request.ResumeText, request.JobDescription, request.JobTitle,
		tailor.QuestionOptions{Stage: strings.TrimSpace(request.Stage), Exclude: exclude})
	if err != nil {
		log.Printf("[interview-questions] FAILED user=%s: %v", user.ID, err)
		writeError(w, http.StatusBadGateway, "Jane couldn't write practice questions just now. Try again in a minute.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"stage":          strings.TrimSpace(request.Stage),
		"questions":      questions,
		"jobTitle":       request.JobTitle,
		"jobDescription": request.JobDescription,
	})
}
