package api

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/hannasotolongo/casemade-backend/internal/repository"
)

// trackJobRequest saves a job-match search result to the user's tracker.
// Search results are not persisted by /job-matches, so the job is stored
// first and then a "saved" application is created for it.
type trackJobRequest struct {
	Job struct {
		ID          string `json:"id"`
		Source      string `json:"source"`
		Title       string `json:"title"`
		Company     string `json:"company"`
		Location    string `json:"location"`
		Description string `json:"description"`
		ApplyURL    string `json:"applyURL"`
	} `json:"job"`
	Notes string `json:"notes"`
}

func (s *Server) handleTrackJob(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}

	if s.jobRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "persistence unavailable")
		return
	}

	userID, ok := s.requireApplicationUser(w, r)
	if !ok {
		return
	}

	var request trackJobRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	job := request.Job
	job.ID = strings.TrimSpace(job.ID)
	job.Source = strings.TrimSpace(job.Source)
	job.Title = strings.TrimSpace(job.Title)
	job.Company = strings.TrimSpace(job.Company)
	job.Location = strings.TrimSpace(job.Location)
	job.ApplyURL = strings.TrimSpace(job.ApplyURL)
	notes := strings.TrimSpace(request.Notes)

	if job.Title == "" || job.Company == "" {
		writeError(w, http.StatusBadRequest, "job title and company are required")
		return
	}

	if len(job.Title) > 300 || len(job.Company) > 300 ||
		len(job.Location) > 300 || len(job.Description) > 50_000 ||
		len(job.ID) > 300 || len(job.Source) > 100 || len(notes) > 5000 {
		writeError(w, http.StatusBadRequest, "job fields are too long")
		return
	}

	if job.ApplyURL != "" {
		parsed, err := url.Parse(job.ApplyURL)
		if err != nil ||
			(parsed.Scheme != "https" && parsed.Scheme != "http") ||
			len(job.ApplyURL) > 2000 {
			writeError(w, http.StatusBadRequest, "applyURL must be an http(s) URL")
			return
		}
	}

	// Client-supplied jobs live under a "web:" source namespace so they can
	// never overwrite jobs ingested from trusted providers.
	source := "web"
	if job.Source != "" {
		source = "web:" + job.Source
	}

	stored, err := s.jobRepo.UpsertJob(r.Context(), repository.UpsertJobParams{
		Source:      source,
		SourceJobID: job.ID,
		Title:       job.Title,
		Company:     job.Company,
		Location:    job.Location,
		Description: job.Description,
		ApplyURL:    job.ApplyURL,
		IsActive:    true,
	})
	if err != nil {
		log.Printf("track job for user %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "failed to save job")
		return
	}

	existing, err := s.userJobsRepo.GetApplication(r.Context(), userID, stored.ID)
	if err == nil {
		writeJSON(w, http.StatusOK, newApplicationResponse(existing))
		return
	}
	if !errors.Is(err, repository.ErrApplicationNotFound) {
		log.Printf("load application for user %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "failed to save application")
		return
	}

	application, err := s.userJobsRepo.UpsertApplication(
		r.Context(),
		repository.UpsertApplicationParams{
			UserID: userID,
			JobID:  stored.ID,
			Status: "saved",
			Notes:  notes,
		},
	)
	if err != nil {
		log.Printf("create application for user %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "failed to save application")
		return
	}

	writeJSON(w, http.StatusCreated, newApplicationResponse(application))
}
