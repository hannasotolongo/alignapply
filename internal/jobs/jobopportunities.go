package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const jobOpportunitiesBaseURL = "https://api.jobopportunitiesapi.org"

type JobOpportunitiesProvider struct {
	client *http.Client
}

func NewJobOpportunitiesProvider() *JobOpportunitiesProvider {
	return &JobOpportunitiesProvider{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (p *JobOpportunitiesProvider) Name() string {
	return "jobopportunities"
}

type jobOpportunitiesResponse struct {
	Data []jobOpportunitiesJob `json:"data"`
}

type jobOpportunitiesJob struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Company     string `json:"company"`
	Description string `json:"description"`

	Location string `json:"location"`
	City     string `json:"city"`
	Region   string `json:"region"`
	Country  string `json:"country"`

	Remote         string `json:"remote"`
	EmploymentType string `json:"employment_type"`

	SalaryMin      *float64 `json:"salary_min"`
	SalaryMax      *float64 `json:"salary_max"`
	SalaryCurrency string   `json:"salary_currency"`
	SalaryPeriod   string   `json:"salary_period"`

	PostedAt       string `json:"posted_at"`
	FirstSeenAt    string `json:"first_seen_at"`
	LastVerifiedAt string `json:"last_verified_at"`

	ApplyURL string `json:"apply_url"`
	Source   string `json:"source"`
	Status   string `json:"status"`
}

func (p *JobOpportunitiesProvider) Search(
	ctx context.Context,
	request SearchRequest,
) ([]Job, error) {
	role := strings.TrimSpace(request.TargetRole)
	location := strings.TrimSpace(request.Location)
	country := strings.ToUpper(strings.TrimSpace(request.CountryCode))

	// Discovery is intentionally occupation-agnostic.
	//
	// Pass 1 searches the user's exact role with their geography.
	// If that produces too small a candidate pool, Pass 2 searches the
	// exact role at country scope. AlignApply's provider-independent pipeline
	// remains authoritative for title, location, arrangement, employment type,
	// salary, freshness, and deduplication.
	searches := []struct {
		name            string
		includeLocation bool
	}{
		{
			name:            "role_and_location",
			includeLocation: true,
		},
		{
			name:            "role_country_fallback",
			includeLocation: false,
		},
	}

	const desiredCandidatePool = 10

	all := make([]Job, 0, 50)
	seen := make(map[string]struct{})
	var searchErrors []string

	for index, search := range searches {
		// Only use the fallback when the first pass did not produce
		// a useful discovery pool.
		if index > 0 && len(all) >= desiredCandidatePool {
			break
		}

		endpoint, err := url.Parse(
			jobOpportunitiesBaseURL + "/v1/jobs",
		)
		if err != nil {
			return nil, err
		}

		query := endpoint.Query()
		query.Set("limit", "50")
		query.Set("include_description", "true")

		// Whatever occupation the user entered is passed through dynamically.
		if role != "" {
			query.Set("title", role)
		}

		// Preserve the existing provider geography query on the first pass.
		// The second pass broadens discovery only; the downstream pipeline
		// still enforces the user's actual location preference.
		if search.includeLocation && location != "" {
			query.Set("q", location)
		}

		if country != "" {
			query.Set("country", country)
		}

		endpoint.RawQuery = query.Encode()

		log.Printf(
			"[jobopportunities] search=%s role=%q location=%q country=%q",
			search.name,
			role,
			func() string {
				if search.includeLocation {
					return location
				}
				return ""
			}(),
			country,
		)

		httpRequest, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			endpoint.String(),
			nil,
		)
		if err != nil {
			return nil, err
		}

		apiKey := strings.TrimSpace(os.Getenv("JOA_API_KEY"))
		if apiKey == "" {
			return nil, fmt.Errorf("JOA_API_KEY is not configured")
		}

		httpRequest.Header.Set("Authorization", "Bearer "+apiKey)
		httpRequest.Header.Set("Accept", "application/json")

		response, err := p.client.Do(httpRequest)
		if err != nil {
			searchErrors = append(
				searchErrors,
				fmt.Sprintf("%s: %v", search.name, err),
			)
			continue
		}

		if response.StatusCode < 200 || response.StatusCode >= 300 {
			status := response.StatusCode
			response.Body.Close()

			searchErrors = append(
				searchErrors,
				fmt.Sprintf("%s: HTTP %d", search.name, status),
			)
			continue
		}

		var payload jobOpportunitiesResponse

		decodeErr := json.NewDecoder(response.Body).Decode(&payload)
		response.Body.Close()

		if decodeErr != nil {
			searchErrors = append(
				searchErrors,
				fmt.Sprintf("%s: %v", search.name, decodeErr),
			)
			continue
		}

		added := 0

		for _, externalJob := range payload.Data {
			job := normalizeJobOpportunitiesJob(externalJob)

			if job.ID == "" ||
				job.Title == "" ||
				job.Company == "" ||
				job.ApplyURL == "" {
				continue
			}

			key := strings.TrimSpace(job.Source) + "|" + strings.TrimSpace(job.ID)
			if _, exists := seen[key]; exists {
				continue
			}

			seen[key] = struct{}{}
			all = append(all, job)
			added++
		}

		log.Printf(
			"[jobopportunities] search=%s upstream=%d normalized_added=%d candidate_pool=%d",
			search.name,
			len(payload.Data),
			added,
			len(all),
		)
	}

	if len(all) == 0 && len(searchErrors) > 0 {
		return nil, fmt.Errorf(
			"job discovery failed: %s",
			strings.Join(searchErrors, "; "),
		)
	}

	if len(searchErrors) > 0 {
		log.Printf(
			"[jobopportunities] partial discovery errors: %s",
			strings.Join(searchErrors, "; "),
		)
	}

	return all, nil
}

func normalizeJobOpportunitiesJob(
	input jobOpportunitiesJob,
) Job {
	location := normalizedProviderLocation(input)

	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = "jobopportunities"
	}

	metadata := map[string]string{
		"upstream_source": source,
	}

	if value := strings.TrimSpace(input.FirstSeenAt); value != "" {
		metadata["first_seen_at"] = value
	}

	if value := strings.TrimSpace(input.LastVerifiedAt); value != "" {
		metadata["last_verified_at"] = value
	}

	return Job{
		ID:          strings.TrimSpace(input.ID),
		Source:      source,
		SourceJobID: strings.TrimSpace(input.ID),

		Title:       strings.TrimSpace(input.Title),
		Company:     strings.TrimSpace(input.Company),
		Description: strings.TrimSpace(input.Description),

		Location:        location,
		CountryCode:     normalizedProviderCountryCode(input, location),
		WorkArrangement: normalizeWorkArrangement(input.Remote),
		EmploymentType:  strings.TrimSpace(input.EmploymentType),

		SalaryMin:      input.SalaryMin,
		SalaryMax:      input.SalaryMax,
		SalaryCurrency: strings.TrimSpace(input.SalaryCurrency),
		SalaryPeriod:   strings.TrimSpace(input.SalaryPeriod),

		PostedAt: parseProviderTime(input.PostedAt),

		IsActive: providerJobIsActive(input.Status),

		ApplyURL: strings.TrimSpace(input.ApplyURL),

		Metadata: metadata,
	}
}

func normalizedProviderLocation(
	input jobOpportunitiesJob,
) string {
	// Prefer the provider's explicit normalized location.
	if location := strings.TrimSpace(input.Location); location != "" {
		return location
	}

	// Fall back only to actual geographic fields.
	// Never use the provider's "remote" field as a location.
	parts := make([]string, 0, 3)

	appendLocationPart := func(value string) {
		value = strings.TrimSpace(value)

		if value == "" {
			return
		}

		for _, existing := range parts {
			if strings.EqualFold(existing, value) {
				return
			}
		}

		parts = append(parts, value)
	}

	appendLocationPart(input.City)
	appendLocationPart(input.Region)
	appendLocationPart(input.Country)

	return strings.Join(parts, ", ")
}

func normalizedProviderCountryCode(
	input jobOpportunitiesJob,
	location string,
) string {
	/*
		If the provider gives us an explicit location containing a
		recognized US state or territory abbreviation, that location is
		more specific than a conflicting country value.

		Example observed in production:
			location = "Miami, FL"
			country  = "GB"

		In that case the normalized country is US.

		We intentionally do not infer US merely from a city name.
	*/
	if locationContainsUSRegion(location) {
		return "US"
	}

	// The structured region field can also provide a reliable US signal
	// when the explicit location field is absent or less specific.
	if isUSRegionCode(input.Region) {
		return "US"
	}

	return normalizeCountryCode(input.Country)
}

func locationContainsUSRegion(location string) bool {
	location = strings.TrimSpace(location)
	if location == "" {
		return false
	}

	/*
		Tokenize punctuation and whitespace so common forms such as:
			Miami, FL
			Miami FL
			Miami, FL 33101
			Austin, TX, US
		can be recognized without matching state abbreviations inside
		ordinary words.
	*/
	fields := strings.FieldsFunc(
		strings.ToUpper(location),
		func(r rune) bool {
			switch r {
			case ' ', ',', ';', '/', '\\', '|', '(', ')', '[', ']':
				return true
			default:
				return false
			}
		},
	)

	for _, field := range fields {
		field = strings.Trim(field, ".:-_")

		if isUSRegionCode(field) {
			return true
		}
	}

	return false
}

func isUSRegionCode(value string) bool {
	value = strings.ToUpper(strings.TrimSpace(value))

	switch value {
	case
		"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE",
		"FL", "GA", "HI", "ID", "IL", "IN", "IA", "KS",
		"KY", "LA", "ME", "MD", "MA", "MI", "MN", "MS",
		"MO", "MT", "NE", "NV", "NH", "NJ", "NM", "NY",
		"NC", "ND", "OH", "OK", "OR", "PA", "RI", "SC",
		"SD", "TN", "TX", "UT", "VT", "VA", "WA", "WV",
		"WI", "WY", "DC", "PR", "VI", "GU", "AS", "MP":
		return true

	default:
		return false
	}
}

func parseProviderTime(value string) *time.Time {
	value = strings.TrimSpace(value)

	if value == "" {
		return nil
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	}

	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return &parsed
		}
	}

	return nil
}

func normalizeCountryCode(value string) string {
	value = strings.TrimSpace(value)

	if len(value) == 2 {
		return strings.ToUpper(value)
	}

	return ""
}

func providerJobIsActive(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))

	switch status {
	case "", "live", "active", "open":
		return true
	default:
		return false
	}
}

func requestedWorkArrangement(
	request SearchRequest,
) string {
	selected := 0
	value := ""

	if request.Remote {
		selected++
		value = "remote"
	}

	if request.Hybrid {
		selected++
		value = "hybrid"
	}

	if request.Onsite {
		selected++
		value = "on_site"
	}

	// If multiple arrangements are acceptable, don't restrict the
	// upstream search to only one of them.
	if selected != 1 {
		return ""
	}

	return value
}

func normalizeWorkArrangement(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "remote":
		return "remote"

	case "hybrid":
		return "hybrid"

	case "on_site", "onsite", "on-site":
		return "onsite"

	default:
		return ""
	}
}
