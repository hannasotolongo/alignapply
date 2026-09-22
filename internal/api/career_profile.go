package api

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/auth"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
)

type careerProfileEvidenceRequest struct {
	Category   string `json:"category"`
	EntryIndex int    `json:"entryIndex"`
	Text       string `json:"text"`
	Source     string `json:"source"`
}

type careerProfileRequest struct {
	Headline   string                         `json:"headline"`
	Summary    string                         `json:"summary"`
	TargetRole string                         `json:"targetRole"`
	Location   string                         `json:"location"`
	Evidence   []careerProfileEvidenceRequest `json:"evidence"`
}

type careerProfileEvidenceResponse struct {
	ID         string    `json:"id"`
	Category   string    `json:"category"`
	EntryIndex int       `json:"entryIndex"`
	Text       string    `json:"text"`
	Source     string    `json:"source"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type careerProfileResponse struct {
	ID         string                          `json:"id"`
	Headline   string                          `json:"headline"`
	Summary    string                          `json:"summary"`
	TargetRole string                          `json:"targetRole"`
	Location   string                          `json:"location"`
	Evidence   []careerProfileEvidenceResponse `json:"evidence"`
	CreatedAt  time.Time                       `json:"createdAt"`
	UpdatedAt  time.Time                       `json:"updatedAt"`
}

func (s *Server) handleCareerProfile(
	w http.ResponseWriter,
	r *http.Request,
) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetCareerProfile(w, r)

	case http.MethodPut:
		s.handlePutCareerProfile(w, r)

	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
	}
}

func (s *Server) handleGetCareerProfile(
	w http.ResponseWriter,
	r *http.Request,
) {
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

	userID, err := auth.RequireUserID(r)
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

	profile, err :=
		s.userCareerRepo.GetCareerProfileByUserID(
			r.Context(),
			userID,
		)

	if errors.Is(
		err,
		repository.ErrCareerProfileNotFound,
	) {
		// A user who has not created a profile yet receives an empty
		// profile instead of an error. This gives the iOS onboarding
		// flow a predictable response for first-time users.
		writeJSON(
			w,
			http.StatusOK,
			careerProfileResponse{
				Evidence: make(
					[]careerProfileEvidenceResponse,
					0,
				),
			},
		)
		return
	}

	if err != nil {
		log.Printf(
			"get career profile for user %s: %v",
			userID,
			err,
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to load career profile",
			},
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		newCareerProfileResponse(profile),
	)
}

func (s *Server) handlePutCareerProfile(
	w http.ResponseWriter,
	r *http.Request,
) {
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

	userID, err := auth.RequireUserID(r)
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

	var request careerProfileRequest

	if err :=
		decodeCareerProfileRequest(
			r,
			&request,
		); err != nil {

		writeJSON(
			w,
			http.StatusBadRequest,
			map[string]string{
				"error": "invalid request body",
			},
		)
		return
	}

	normalizeCareerProfileRequest(
		&request,
	)

	if err :=
		validateCareerProfileRequest(
			request,
		); err != nil {

		writeJSON(
			w,
			http.StatusBadRequest,
			map[string]string{
				"error": err.Error(),
			},
		)
		return
	}

	evidence :=
		make(
			[]repository.CareerProfileEvidenceInput,
			0,
			len(request.Evidence),
		)

	for _, item := range request.Evidence {

		evidence =
			append(
				evidence,
				repository.CareerProfileEvidenceInput{
					Category:   item.Category,
					EntryIndex: item.EntryIndex,
					Text:       item.Text,
					Source:     item.Source,
				},
			)
	}

	profile, err :=
		s.userCareerRepo.UpsertCareerProfile(
			r.Context(),
			repository.UpsertCareerProfileParams{
				UserID:     userID,
				Headline:   request.Headline,
				Summary:    request.Summary,
				TargetRole: request.TargetRole,
				Location:   request.Location,
				Evidence:   evidence,
			},
		)

	if err != nil {
		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to save career profile",
			},
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		newCareerProfileResponse(profile),
	)
}

func newCareerProfileResponse(
	profile repository.CareerProfile,
) careerProfileResponse {
	evidence :=
		make(
			[]careerProfileEvidenceResponse,
			0,
			len(profile.Evidence),
		)

	for _, item := range profile.Evidence {

		evidence =
			append(
				evidence,
				careerProfileEvidenceResponse{
					ID:         item.ID,
					Category:   item.Category,
					EntryIndex: item.EntryIndex,
					Text:       item.Text,
					Source:     item.Source,
					CreatedAt:  item.CreatedAt,
					UpdatedAt:  item.UpdatedAt,
				},
			)
	}

	return careerProfileResponse{
		ID:         profile.ID,
		Headline:   profile.Headline,
		Summary:    profile.Summary,
		TargetRole: profile.TargetRole,
		Location:   profile.Location,
		Evidence:   evidence,
		CreatedAt:  profile.CreatedAt,
		UpdatedAt:  profile.UpdatedAt,
	}
}

func normalizeCareerProfileRequest(
	request *careerProfileRequest,
) {
	request.Headline =
		strings.TrimSpace(
			request.Headline,
		)

	request.Summary =
		strings.TrimSpace(
			request.Summary,
		)

	request.TargetRole =
		strings.TrimSpace(
			request.TargetRole,
		)

	request.Location =
		strings.TrimSpace(
			request.Location,
		)

	for index := range request.Evidence {

		request.Evidence[index].Category =
			strings.ToLower(
				strings.TrimSpace(
					request.Evidence[index].Category,
				),
			)

		request.Evidence[index].Text =
			strings.TrimSpace(
				request.Evidence[index].Text,
			)

		request.Evidence[index].Source =
			strings.TrimSpace(
				request.Evidence[index].Source,
			)
	}
}
