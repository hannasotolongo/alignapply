package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hannasotolongo/casemade-backend/internal/candidate"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
)

const (
	maxResumesPerUser    = 10
	maxResumeTextChars   = 100_000
	minResumeTextChars   = 40
	maxResumeFilename    = 200
	maxPreferencesBytes  = 4 << 10
	resumeInlineStorage  = "inline:text"
	resumeContentTypePDF = "application/pdf"
)

type resumeResponse struct {
	ID         string    `json:"id"`
	Filename   string    `json:"filename"`
	IsPrimary  bool      `json:"isPrimary"`
	CreatedAt  time.Time `json:"createdAt"`
	ResumeText string    `json:"resumeText,omitempty"`
}

type createResumeRequest struct {
	Filename    string `json:"filename"`
	ResumeText  string `json:"resumeText"`
	MakePrimary bool   `json:"makePrimary"`
}

type profileDraftEvidence struct {
	Category string `json:"category"`
	Text     string `json:"text"`
}

type profileDraftResponse struct {
	Summary  string                 `json:"summary"`
	Evidence []profileDraftEvidence `json:"evidence"`
}

func newResumeResponse(resume repository.Resume, includeText bool) resumeResponse {
	response := resumeResponse{
		ID:        resume.ID,
		Filename:  resume.Filename,
		IsPrimary: resume.IsPrimary,
		CreatedAt: resume.CreatedAt,
	}
	if includeText {
		response.ResumeText = resume.ExtractedText
	}
	return response
}

// /api/v1/resumes
func (s *Server) handleResumes(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.requireApplicationUser(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		resumes, err := s.userJobsRepo.ListResumes(r.Context(), userID)
		if err != nil {
			log.Printf("list resumes for user %s: %v", userID, err)
			writeError(w, http.StatusInternalServerError, "failed to load résumés")
			return
		}
		response := make([]resumeResponse, 0, len(resumes))
		for _, resume := range resumes {
			response = append(response, newResumeResponse(resume, false))
		}
		writeJSON(w, http.StatusOK, response)

	case http.MethodPost:
		var request createResumeRequest
		if err := decodeApplicationJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		request.Filename = strings.TrimSpace(request.Filename)
		request.ResumeText = strings.TrimSpace(strings.ToValidUTF8(request.ResumeText, ""))
		request.ResumeText = strings.ReplaceAll(request.ResumeText, "\x00", "")

		if request.Filename == "" {
			request.Filename = "Résumé"
		}
		if utf8.RuneCountInString(request.Filename) > maxResumeFilename {
			writeError(w, http.StatusBadRequest, "filename is too long")
			return
		}

		length := utf8.RuneCountInString(request.ResumeText)
		if length < minResumeTextChars {
			writeError(w, http.StatusBadRequest, "we couldn't read enough text from this résumé; if it's a scanned image, export it as a text PDF")
			return
		}
		if length > maxResumeTextChars {
			writeError(w, http.StatusBadRequest, "résumé text is too long")
			return
		}

		count, err := s.userJobsRepo.CountResumes(r.Context(), userID)
		if err != nil {
			log.Printf("count resumes for user %s: %v", userID, err)
			writeError(w, http.StatusInternalServerError, "failed to save résumé")
			return
		}
		if count >= maxResumesPerUser {
			writeError(w, http.StatusConflict, "you can keep up to 10 résumés; delete one to add another")
			return
		}

		resume, err := s.userJobsRepo.CreateResume(r.Context(), repository.CreateResumeParams{
			UserID:        userID,
			Filename:      request.Filename,
			ContentType:   resumeContentTypePDF,
			StorageKey:    resumeInlineStorage,
			ExtractedText: request.ResumeText,
			IsPrimary:     request.MakePrimary || count == 0,
		})
		if err != nil {
			log.Printf("create resume for user %s: %v", userID, err)
			writeError(w, http.StatusInternalServerError, "failed to save résumé")
			return
		}
		writeJSON(w, http.StatusCreated, newResumeResponse(resume, false))

	default:
		methodNotAllowed(w, "GET, POST")
	}
}

// /api/v1/resumes/{id}, /api/v1/resumes/{id}/primary, /api/v1/resumes/{id}/profile-draft
func (s *Server) handleResumeResource(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.requireApplicationUser(w, r)
	if !ok {
		return
	}

	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/resumes/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || len(parts) > 2 || strings.TrimSpace(parts[0]) == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	resumeID := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}

	resume, err := s.userJobsRepo.GetResumeForUser(r.Context(), userID, resumeID)
	if errors.Is(err, repository.ErrResumeNotFound) {
		writeError(w, http.StatusNotFound, "résumé not found")
		return
	}
	if err != nil {
		log.Printf("get resume %s for user %s: %v", resumeID, userID, err)
		writeError(w, http.StatusInternalServerError, "failed to load résumé")
		return
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, newResumeResponse(resume, true))

	case action == "" && r.Method == http.MethodDelete:
		if err := s.userJobsRepo.DeleteResume(r.Context(), userID, resume.ID); err != nil {
			log.Printf("delete resume %s for user %s: %v", resume.ID, userID, err)
			writeError(w, http.StatusInternalServerError, "failed to delete résumé")
			return
		}
		w.WriteHeader(http.StatusNoContent)

	case action == "primary" && r.Method == http.MethodPost:
		if err := s.userJobsRepo.SetPrimaryResume(r.Context(), userID, resume.ID); err != nil {
			log.Printf("set primary resume %s for user %s: %v", resume.ID, userID, err)
			writeError(w, http.StatusInternalServerError, "failed to update résumé")
			return
		}
		w.WriteHeader(http.StatusNoContent)

	case action == "profile-draft" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, buildProfileDraft(resume.ExtractedText))

	case action == "" || action == "primary" || action == "profile-draft":
		methodNotAllowed(w, "GET, POST, DELETE")

	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// buildProfileDraft turns résumé text into career-profile evidence that the
// user reviews and edits before saving.
func buildProfileDraft(resumeText string) profileDraftResponse {
	summary, items := candidate.DraftCareerProfile(resumeText)

	draft := profileDraftResponse{Evidence: make([]profileDraftEvidence, 0, len(items))}
	for _, item := range items {
		if !validProfileCategory(string(item.Category)) ||
			utf8.RuneCountInString(item.Text) > maxEvidenceTextLength {
			continue
		}
		draft.Evidence = append(draft.Evidence, profileDraftEvidence{
			Category: string(item.Category),
			Text:     item.Text,
		})
	}

	draft.Summary = summary
	if utf8.RuneCountInString(draft.Summary) > maxSummaryLength {
		draft.Summary = string([]rune(draft.Summary)[:maxSummaryLength])
	}
	return draft
}

func validProfileCategory(category string) bool {
	switch category {
	case "experience", "skill", "education", "certification", "license", "project":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Preferences: job-search settings and onboarding progress
// ---------------------------------------------------------------------------

type preferencesPayload struct {
	MinimumSalary      string `json:"minimumSalary"`
	Remote             bool   `json:"remote"`
	Hybrid             bool   `json:"hybrid"`
	Onsite             bool   `json:"onsite"`
	OnboardingComplete bool   `json:"onboardingComplete"`
}

func (s *Server) handlePreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.requireApplicationUser(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		raw, err := s.userJobsRepo.GetPreferences(r.Context(), userID)
		if err != nil {
			log.Printf("get preferences for user %s: %v", userID, err)
			writeError(w, http.StatusInternalServerError, "failed to load preferences")
			return
		}
		var preferences preferencesPayload
		_ = json.Unmarshal(raw, &preferences)
		writeJSON(w, http.StatusOK, preferences)

	case http.MethodPut:
		r.Body = http.MaxBytesReader(w, r.Body, maxPreferencesBytes)
		var preferences preferencesPayload
		if err := decodeApplicationJSON(r, &preferences); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		preferences.MinimumSalary = strings.TrimSpace(preferences.MinimumSalary)
		if len(preferences.MinimumSalary) > 40 {
			writeError(w, http.StatusBadRequest, "minimumSalary is too long")
			return
		}

		encoded, _ := json.Marshal(preferences)
		if err := s.userJobsRepo.SavePreferences(r.Context(), userID, encoded); err != nil {
			log.Printf("save preferences for user %s: %v", userID, err)
			writeError(w, http.StatusInternalServerError, "failed to save preferences")
			return
		}
		writeJSON(w, http.StatusOK, preferences)

	default:
		methodNotAllowed(w, "GET, PUT")
	}
}
