package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
	"github.com/hannasotolongo/casemade-backend/internal/matching"
)

type resumeRecommendationInput struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ResumeText string `json:"resumeText"`
}

type resumeRecommendationRequest struct {
	Job     jobs.Job                    `json:"job"`
	Resumes []resumeRecommendationInput `json:"resumes"`
}

type resumeRecommendationResult struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	MatchPercentage       int      `json:"matchPercentage"`
	MatchLevel            string   `json:"matchLevel"`
	SupportedRequirements []string `json:"supportedRequirements"`
	PartialRequirements   []string `json:"partialRequirements"`
	MissingRequirements   []string `json:"missingRequirements"`
	Explanation           string   `json:"explanation"`
}

type resumeRecommendationResponse struct {
	RecommendedResumeID string                       `json:"recommendedResumeID"`
	Results             []resumeRecommendationResult `json:"results"`
}

func (s *Server) handleResumeRecommendation(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	var request resumeRecommendationRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&request); err != nil {
		http.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	if len(request.Job.Requirements) == 0 {
		http.Error(
			w,
			"job requirements are required",
			http.StatusBadRequest,
		)
		return
	}

	if len(request.Resumes) == 0 {
		http.Error(
			w,
			"at least one resume is required",
			http.StatusBadRequest,
		)
		return
	}

	results := make(
		[]resumeRecommendationResult,
		0,
		len(request.Resumes),
	)

	for _, resume := range request.Resumes {
		resume.ID = strings.TrimSpace(resume.ID)
		resume.Name = strings.TrimSpace(resume.Name)
		resume.ResumeText = strings.TrimSpace(
			resume.ResumeText,
		)

		if resume.ID == "" ||
			resume.ResumeText == "" {
			continue
		}

		match := matching.Match(
			jobs.SearchRequest{
				ResumeText: resume.ResumeText,
			},
			request.Job,
		)

		results = append(
			results,
			resumeRecommendationResult{
				ID:              resume.ID,
				Name:            resume.Name,
				MatchPercentage: match.MatchPercentage,
				MatchLevel:      match.MatchLevel,

				SupportedRequirements: match.SupportedRequirements,

				PartialRequirements: match.PartialRequirements,

				MissingRequirements: match.MissingRequirements,

				Explanation: match.Explanation,
			},
		)
	}

	if len(results) == 0 {
		http.Error(
			w,
			"no usable resumes were provided",
			http.StatusBadRequest,
		)
		return
	}

	/*
		Rank using the matcher's evidence output.

		1. Fewer missing requirements wins.
		2. More supported requirements wins.
		3. Fewer partial requirements wins.
		4. Match percentage breaks remaining ties.

		This intentionally does not use résumé length,
		filename, upload order, or raw keyword count.
	*/
	sort.SliceStable(
		results,
		func(i, j int) bool {
			left := results[i]
			right := results[j]

			if len(left.MissingRequirements) !=
				len(right.MissingRequirements) {
				return len(left.MissingRequirements) <
					len(right.MissingRequirements)
			}

			if len(left.SupportedRequirements) !=
				len(right.SupportedRequirements) {
				return len(left.SupportedRequirements) >
					len(right.SupportedRequirements)
			}

			if len(left.PartialRequirements) !=
				len(right.PartialRequirements) {
				return len(left.PartialRequirements) <
					len(right.PartialRequirements)
			}

			if left.MatchPercentage !=
				right.MatchPercentage {
				return left.MatchPercentage >
					right.MatchPercentage
			}

			return left.Name < right.Name
		},
	)

	writeJSON(
		w,
		http.StatusOK,
		resumeRecommendationResponse{
			RecommendedResumeID: results[0].ID,
			Results:             results,
		},
	)
}
