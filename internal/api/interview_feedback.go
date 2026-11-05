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
