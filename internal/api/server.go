package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
	"github.com/hannasotolongo/casemade-backend/internal/matching"
)

type Server struct {
	mux         *http.ServeMux
	jobProvider *jobs.MultiProvider
	pipeline    *jobs.Pipeline
}

func NewServer() *Server {
	server := &Server{
		mux: http.NewServeMux(),
		jobProvider: jobs.NewMultiProvider(
			jobs.NewJobOpportunitiesProvider(),
			jobs.NewLeverProvider(
				jobs.DefaultATSEmployers(),
			),
		),
		pipeline: jobs.NewPipeline(),
	}

	server.routes()

	return server
}

func (s *Server) routes() {
	s.mux.HandleFunc("/healthz", s.handleHealth)
	s.mux.HandleFunc("/api/v1/job-matches", s.handleJobMatches)
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) handleHealth(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		map[string]string{
			"status": "healthy",
		},
	)
}

func (s *Server) handleJobMatches(
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

	var request jobs.SearchRequest

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

	normalizeSearchRequest(&request)

	// During the migration to structured candidate evidence, either source is
	// valid:
	//
	//   1. CareerProfile — the preferred user-confirmed evidence source.
	//   2. ResumeText — legacy support for existing clients.
	//
	// This keeps the current iOS client working while allowing the structured
	// Career Profile client to migrate independently.
	if !request.CareerProfile.HasEvidence() &&
		request.ResumeText == "" {

		http.Error(
			w,
			"careerProfile or resumeText is required",
			http.StatusBadRequest,
		)
		return
	}

	if request.TargetRole == "" {
		http.Error(
			w,
			"targetRole is required",
			http.StatusBadRequest,
		)
		return
	}

	if request.Location == "" {
		http.Error(
			w,
			"location is required",
			http.StatusBadRequest,
		)
		return
	}

	ctx, cancel := context.WithTimeout(
		r.Context(),
		20*time.Second,
	)
	defer cancel()

	searchResult := s.jobProvider.Search(
		ctx,
		request,
	)

	for _, providerError := range searchResult.Errors {
		log.Printf(
			"job provider %s failed: %v",
			providerError.Provider,
			providerError.Err,
		)
	}

	processedJobs := s.pipeline.Process(
		request,
		searchResult.Jobs,
	)

	matches := make(
		[]jobs.JobMatch,
		0,
		len(processedJobs),
	)

	for _, job := range processedJobs {
		match := matching.Match(
			request,
			job,
		)

		matches = append(
			matches,
			match,
		)
	}

	sortJobMatches(matches)

	writeJSON(
		w,
		http.StatusOK,
		matches,
	)
}

func normalizeSearchRequest(
	request *jobs.SearchRequest,
) {
	request.ResumeText =
		strings.TrimSpace(request.ResumeText)

	// CareerProfile.Normalize only removes surrounding whitespace,
	// empty entries, and duplicate structured values. It does not invent,
	// rewrite, or infer candidate evidence.
	request.CareerProfile.Normalize()

	request.TargetRole =
		strings.TrimSpace(request.TargetRole)

	request.Location =
		strings.TrimSpace(request.Location)

	request.MinimumSalary =
		strings.TrimSpace(request.MinimumSalary)

	request.CountryCode =
		strings.ToUpper(
			strings.TrimSpace(
				request.CountryCode,
			),
		)

	if len(request.EmploymentType) > 0 {
		normalized := make(
			[]string,
			0,
			len(request.EmploymentType),
		)

		seen := make(map[string]struct{})

		for _, value := range request.EmploymentType {
			value = strings.TrimSpace(value)

			if value == "" {
				continue
			}

			key := strings.ToLower(value)

			if _, exists := seen[key]; exists {
				continue
			}

			seen[key] = struct{}{}

			normalized = append(
				normalized,
				value,
			)
		}

		request.EmploymentType = normalized
	}
}

func sortJobMatches(
	matches []jobs.JobMatch,
) {
	sort.SliceStable(
		matches,
		func(i, j int) bool {
			left := matches[i]
			right := matches[j]

			leftScorable :=
				left.MatchLevel != "Insufficient Evidence" &&
					left.MatchLevel != "Needs Review"

			rightScorable :=
				right.MatchLevel != "Insufficient Evidence" &&
					right.MatchLevel != "Needs Review"

			if leftScorable != rightScorable {
				return leftScorable
			}

			if left.MatchPercentage !=
				right.MatchPercentage {
				return left.MatchPercentage >
					right.MatchPercentage
			}

			leftLevel := matchLevelRank(
				left.MatchLevel,
			)

			rightLevel := matchLevelRank(
				right.MatchLevel,
			)

			if leftLevel != rightLevel {
				return leftLevel > rightLevel
			}

			if left.PostedAt != nil &&
				right.PostedAt != nil &&
				!left.PostedAt.Equal(
					*right.PostedAt,
				) {
				return left.PostedAt.After(
					*right.PostedAt,
				)
			}

			if left.PostedAt != nil &&
				right.PostedAt == nil {
				return true
			}

			if left.PostedAt == nil &&
				right.PostedAt != nil {
				return false
			}

			leftCompany :=
				strings.ToLower(
					strings.TrimSpace(
						left.Company,
					),
				)

			rightCompany :=
				strings.ToLower(
					strings.TrimSpace(
						right.Company,
					),
				)

			if leftCompany != rightCompany {
				return leftCompany <
					rightCompany
			}

			return strings.ToLower(
				strings.TrimSpace(
					left.Title,
				),
			) <
				strings.ToLower(
					strings.TrimSpace(
						right.Title,
					),
				)
		},
	)
}

func matchLevelRank(
	level string,
) int {
	switch level {
	case "Best Fit", "Strong Match":
		return 3

	case "Good Fit", "Moderate Match":
		return 2

	case "Reach", "Stretch":
		return 1

	default:
		return 0
	}
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	value any,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf(
			"failed to encode response: %v",
			err,
		)
	}
}
