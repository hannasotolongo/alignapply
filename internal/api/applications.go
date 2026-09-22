package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/auth"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
)

const maxApplicationRequestBodyBytes int64 = 1 << 20

type applicationRequest struct {
	JobID     string     `json:"jobId"`
	Status    string     `json:"status"`
	AppliedAt *time.Time `json:"appliedAt,omitempty"`
	Notes     string     `json:"notes"`
}

type applicationResponse struct {
	ID        string     `json:"id"`
	JobID     string     `json:"jobId"`
	Status    string     `json:"status"`
	AppliedAt *time.Time `json:"appliedAt,omitempty"`
	Notes     string     `json:"notes"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

type applicationEventRequest struct {
	EventType  string     `json:"eventType"`
	NewStatus  string     `json:"newStatus"`
	Summary    string     `json:"summary"`
	OccurredAt *time.Time `json:"occurredAt,omitempty"`
}

type applicationEventResponse struct {
	ID              string    `json:"id"`
	ApplicationID   string    `json:"applicationId"`
	EventType       string    `json:"eventType"`
	PreviousStatus  *string   `json:"previousStatus,omitempty"`
	NewStatus       string    `json:"newStatus"`
	Source          string    `json:"source"`
	SourceProvider  string    `json:"sourceProvider,omitempty"`
	SourceMessageID *string   `json:"sourceMessageId,omitempty"`
	Confidence      *float64  `json:"confidence,omitempty"`
	Summary         string    `json:"summary"`
	OccurredAt      time.Time `json:"occurredAt"`
	CreatedAt       time.Time `json:"createdAt"`
}

type applicationEventResultResponse struct {
	Application applicationResponse      `json:"application"`
	Event       applicationEventResponse `json:"event"`
}

func (s *Server) handleApplications(
	w http.ResponseWriter,
	r *http.Request,
) {
	switch r.Method {
	case http.MethodGet:
		s.handleListApplications(w, r)

	case http.MethodPut:
		s.handlePutApplication(w, r)

	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
	}
}

func (s *Server) handleApplicationResource(
	w http.ResponseWriter,
	r *http.Request,
) {
	const prefix = "/api/v1/applications/"

	path := strings.TrimPrefix(r.URL.Path, prefix)
	path = strings.Trim(path, "/")

	parts := strings.Split(path, "/")
	if len(parts) != 2 ||
		strings.TrimSpace(parts[0]) == "" ||
		parts[1] != "events" {

		http.NotFound(w, r)
		return
	}

	applicationID := strings.TrimSpace(parts[0])

	switch r.Method {
	case http.MethodGet:
		s.handleListApplicationEvents(
			w,
			r,
			applicationID,
		)

	case http.MethodPost:
		s.handlePostApplicationEvent(
			w,
			r,
			applicationID,
		)

	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
	}
}

func (s *Server) handleListApplications(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := s.requireApplicationUser(w, r)
	if !ok {
		return
	}

	applications, err := s.userJobsRepo.ListApplications(
		r.Context(),
		userID,
	)
	if err != nil {
		log.Printf(
			"list applications for user %s: %v",
			userID,
			err,
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to load applications",
			},
		)
		return
	}

	response := make(
		[]applicationResponse,
		0,
		len(applications),
	)

	for _, application := range applications {
		response = append(
			response,
			newApplicationResponse(application),
		)
	}

	writeJSON(
		w,
		http.StatusOK,
		response,
	)
}

func (s *Server) handlePutApplication(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := s.requireApplicationUser(w, r)
	if !ok {
		return
	}

	var request applicationRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeJSON(
			w,
			http.StatusBadRequest,
			map[string]string{
				"error": "invalid request body",
			},
		)
		return
	}

	normalizeApplicationRequest(&request)

	if err := validateApplicationRequest(request); err != nil {
		writeJSON(
			w,
			http.StatusBadRequest,
			map[string]string{
				"error": err.Error(),
			},
		)
		return
	}

	existing, err := s.userJobsRepo.GetApplication(
		r.Context(),
		userID,
		request.JobID,
	)

	switch {
	case err == nil:
		if existing.Status != request.Status {
			writeJSON(
				w,
				http.StatusConflict,
				map[string]string{
					"error": "application status changes must be recorded as events",
				},
			)
			return
		}

	case errors.Is(
		err,
		repository.ErrApplicationNotFound,
	):
		// New applications may be created through PUT.
		// Later status transitions must be recorded as events.

	default:
		log.Printf(
			"get application for user %s job %s before upsert: %v",
			userID,
			request.JobID,
			err,
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to load application",
			},
		)
		return
	}

	application, err := s.userJobsRepo.UpsertApplication(
		r.Context(),
		repository.UpsertApplicationParams{
			UserID:    userID,
			JobID:     request.JobID,
			Status:    request.Status,
			AppliedAt: request.AppliedAt,
			Notes:     request.Notes,
		},
	)
	if err != nil {
		log.Printf(
			"upsert application for user %s job %s: %v",
			userID,
			request.JobID,
			err,
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to save application",
			},
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		newApplicationResponse(application),
	)
}

func (s *Server) handleListApplicationEvents(
	w http.ResponseWriter,
	r *http.Request,
	applicationID string,
) {
	userID, ok := s.requireApplicationUser(w, r)
	if !ok {
		return
	}

	events, err := s.userJobsRepo.ListApplicationEvents(
		r.Context(),
		userID,
		applicationID,
	)
	if err != nil {
		log.Printf(
			"list application events for user %s application %s: %v",
			userID,
			applicationID,
			err,
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to load application events",
			},
		)
		return
	}

	response := make(
		[]applicationEventResponse,
		0,
		len(events),
	)

	for _, event := range events {
		response = append(
			response,
			newApplicationEventResponse(event),
		)
	}

	writeJSON(
		w,
		http.StatusOK,
		response,
	)
}

func (s *Server) handlePostApplicationEvent(
	w http.ResponseWriter,
	r *http.Request,
	applicationID string,
) {
	userID, ok := s.requireApplicationUser(w, r)
	if !ok {
		return
	}

	var request applicationEventRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeJSON(
			w,
			http.StatusBadRequest,
			map[string]string{
				"error": "invalid request body",
			},
		)
		return
	}

	normalizeApplicationEventRequest(&request)

	if err := validateApplicationEventRequest(request); err != nil {
		writeJSON(
			w,
			http.StatusBadRequest,
			map[string]string{
				"error": err.Error(),
			},
		)
		return
	}

	application, event, err :=
		s.userJobsRepo.RecordApplicationEvent(
			r.Context(),
			repository.RecordApplicationEventParams{
				UserID:        userID,
				ApplicationID: applicationID,
				EventType:     request.EventType,
				NewStatus:     request.NewStatus,
				Source:        "manual",
				Summary:       request.Summary,
				OccurredAt:    request.OccurredAt,
			},
		)
	if err != nil {
		log.Printf(
			"record application event for user %s application %s: %v",
			userID,
			applicationID,
			err,
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to update application",
			},
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		applicationEventResultResponse{
			Application: newApplicationResponse(
				application,
			),
			Event: newApplicationEventResponse(
				event,
			),
		},
	)
}

func (s *Server) requireApplicationUser(
	w http.ResponseWriter,
	r *http.Request,
) (string, bool) {
	if s.userJobsRepo == nil ||
		s.userCareerRepo == nil {

		writeJSON(
			w,
			http.StatusServiceUnavailable,
			map[string]string{
				"error": "persistence unavailable",
			},
		)
		return "", false
	}

	userID, err := auth.RequireUserID(r)
	if err != nil {
		writeJSON(
			w,
			http.StatusUnauthorized,
			map[string]string{
				"error": "unauthorized",
			},
		)
		return "", false
	}

	if _, err := s.userCareerRepo.GetUserByID(
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
		return "", false
	}

	return userID, true
}

func decodeApplicationJSON(
	r *http.Request,
	destination any,
) error {
	r.Body = http.MaxBytesReader(
		nil,
		r.Body,
		maxApplicationRequestBodyBytes,
	)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		return err
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New(
			"request body must contain exactly one JSON object",
		)
	}

	return nil
}

func normalizeApplicationRequest(
	request *applicationRequest,
) {
	request.JobID = strings.TrimSpace(
		request.JobID,
	)
	request.Status = strings.TrimSpace(
		request.Status,
	)
	request.Notes = strings.TrimSpace(
		request.Notes,
	)
}

func normalizeApplicationEventRequest(
	request *applicationEventRequest,
) {
	request.EventType = strings.TrimSpace(
		request.EventType,
	)
	request.NewStatus = strings.TrimSpace(
		request.NewStatus,
	)
	request.Summary = strings.TrimSpace(
		request.Summary,
	)
}

func validateApplicationRequest(
	request applicationRequest,
) error {
	if request.JobID == "" {
		return errors.New("jobId is required")
	}

	if !validApplicationStatus(request.Status) {
		return errors.New("invalid application status")
	}

	if len(request.Notes) > 5000 {
		return errors.New(
			"notes must not exceed 5000 characters",
		)
	}

	return nil
}

func validateApplicationEventRequest(
	request applicationEventRequest,
) error {
	if !validManualApplicationEventType(
		request.EventType,
	) {
		return errors.New(
			"invalid application event type",
		)
	}

	if !validApplicationStatus(
		request.NewStatus,
	) {
		return errors.New(
			"invalid application status",
		)
	}

	if len(request.Summary) > 1000 {
		return errors.New(
			"summary must not exceed 1000 characters",
		)
	}

	return nil
}

func validApplicationStatus(status string) bool {
	switch status {
	case "saved",
		"applied",
		"received",
		"recruiter_contact",
		"interviewing",
		"offer",
		"hired",
		"rejected",
		"withdrawn":
		return true

	default:
		return false
	}
}

func validManualApplicationEventType(
	eventType string,
) bool {
	switch eventType {
	case "application_created",
		"application_submitted",
		"application_received",
		"recruiter_contact",
		"phone_screen",
		"interview_invitation",
		"interview_scheduled",
		"interview_completed",
		"offer_received",
		"hired",
		"rejected",
		"withdrawn",
		"status_changed":
		return true

	default:
		return false
	}
}

func newApplicationResponse(
	application repository.Application,
) applicationResponse {
	return applicationResponse{
		ID:        application.ID,
		JobID:     application.JobID,
		Status:    application.Status,
		AppliedAt: application.AppliedAt,
		Notes:     application.Notes,
		CreatedAt: application.CreatedAt,
		UpdatedAt: application.UpdatedAt,
	}
}

func newApplicationEventResponse(
	event repository.ApplicationEvent,
) applicationEventResponse {
	return applicationEventResponse{
		ID:              event.ID,
		ApplicationID:   event.ApplicationID,
		EventType:       event.EventType,
		PreviousStatus:  event.PreviousStatus,
		NewStatus:       event.NewStatus,
		Source:          event.Source,
		SourceProvider:  event.SourceProvider,
		SourceMessageID: event.SourceMessageID,
		Confidence:      event.Confidence,
		Summary:         event.Summary,
		OccurredAt:      event.OccurredAt,
		CreatedAt:       event.CreatedAt,
	}
}
