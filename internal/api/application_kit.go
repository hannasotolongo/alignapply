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

type applicationKitRequest struct {
	ResumeText     string   `json:"resumeText"`
	JobDescription string   `json:"jobDescription"`
	JobTitle       string   `json:"jobTitle,omitempty"`
	Company        string   `json:"company,omitempty"`
	Gaps           []string `json:"gaps,omitempty"`
}

// POST /api/v1/application-kit (Pro)
//
// Builds a truth-locked tailored résumé, keywords and interview prep for one
// job. Every tailored line is traced to the user's own résumé.
func (s *Server) handleApplicationKit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	if s.applicationKit == nil {
		log.Printf("[application-kit] unavailable: OpenAI key not configured")
		writeError(w, http.StatusServiceUnavailable, "Application kits aren't available right now. Please try again later.")
		return
	}
	if !s.kitLimiter.Allow("kit:" + user.ID) {
		log.Printf("[application-kit] user %s rate limited", user.ID)
		writeError(w, http.StatusTooManyRequests, "You've built a lot of kits this hour. Give Jane a few minutes.")
		return
	}

	var request applicationKitRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		log.Printf("[application-kit] user %s bad request body: %v", user.ID, err)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	request.ResumeText = strings.TrimSpace(request.ResumeText)
	request.JobDescription = strings.TrimSpace(request.JobDescription)
	log.Printf("[application-kit] START user=%s resume_chars=%d job_chars=%d", user.ID, len(request.ResumeText), len(request.JobDescription))
	switch {
	case request.ResumeText == "":
		writeError(w, http.StatusBadRequest, "Add your résumé first.")
		return
	case len(request.ResumeText) > fitCheckMaxResumeChars:
		writeError(w, http.StatusBadRequest, "That résumé is too long.")
		return
	case len(request.JobDescription) < 80:
		log.Printf("[application-kit] user %s job description too short (%d chars)", user.ID, len(request.JobDescription))
		writeError(w, http.StatusBadRequest, "This posting doesn't include enough detail for Jane to tailor your résumé.")
		return
	case len(request.JobDescription) > fitCheckMaxJobChars:
		writeError(w, http.StatusBadRequest, "That posting is too long.")
		return
	}
	if len(request.Gaps) > 15 {
		request.Gaps = request.Gaps[:15]
	}

	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 160*time.Second)
	defer cancel()

	kit, err := s.applicationKit.Build(ctx, tailor.Request{
		ResumeText:     request.ResumeText,
		JobDescription: request.JobDescription,
		JobTitle:       strings.TrimSpace(request.JobTitle),
		Company:        strings.TrimSpace(request.Company),
		Gaps:           request.Gaps,
	})
	if err != nil {
		if errors.Is(err, tailor.ErrUnavailable) {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		log.Printf("[application-kit] FAILED user=%s after=%s: %v", user.ID, time.Since(started), err)
		writeError(w, http.StatusBadGateway, "Jane couldn't build this kit just now. Try again in a minute.")
		return
	}
	log.Printf("[application-kit] DONE user=%s after=%s bullets=%d dropped=%d", user.ID, time.Since(started), len(kit.Bullets), kit.DroppedLines)
	writeJSON(w, http.StatusOK, kit)
}
