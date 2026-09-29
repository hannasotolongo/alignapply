package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	leverBaseURL        = "https://api.lever.co/v0/postings"
	leverPageSize       = 100
	leverMaxPages       = 3
	leverMaxConcurrency = 6
)

type LeverProvider struct {
	client    *http.Client
	employers []ATSEmployer
}

func NewLeverProvider(
	employers []ATSEmployer,
) *LeverProvider {
	filtered := make([]ATSEmployer, 0, len(employers))

	for _, employer := range employers {
		if !strings.EqualFold(
			strings.TrimSpace(employer.Provider),
			"lever",
		) {
			continue
		}

		if strings.TrimSpace(employer.Site) == "" {
			continue
		}

		filtered = append(filtered, employer)
	}

	return &LeverProvider{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		employers: filtered,
	}
}

func (p *LeverProvider) Name() string {
	return "lever"
}

type leverPosting struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	Description string `json:"description"`
	HostedURL   string `json:"hostedUrl"`
	ApplyURL    string `json:"applyUrl"`
	CreatedAt   int64  `json:"createdAt"`

	Categories struct {
		Commitment string `json:"commitment"`
		Location   string `json:"location"`
		Team       string `json:"team"`
		Department string `json:"department"`
	} `json:"categories"`

	WorkplaceType string `json:"workplaceType"`

	SalaryRange *struct {
		Min      float64 `json:"min"`
		Max      float64 `json:"max"`
		Currency string  `json:"currency"`
		Interval string  `json:"interval"`
	} `json:"salaryRange"`

	Lists []struct {
		Text    string `json:"text"`
		Content string `json:"content"`
	} `json:"lists"`

	Additional string `json:"additional"`
}

func (p *LeverProvider) Search(
	ctx context.Context,
	request SearchRequest,
) ([]Job, error) {
	if p == nil || len(p.employers) == 0 {
		return []Job{}, nil
	}

	type employerResult struct {
		jobs []Job
		err  error
	}

	results := make(
		chan employerResult,
		len(p.employers),
	)

	sem := make(
		chan struct{},
		leverMaxConcurrency,
	)

	var wg sync.WaitGroup

	for _, employer := range p.employers {
		employer := employer

		wg.Add(1)

		go func() {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() {
					<-sem
				}()

			case <-ctx.Done():
				results <- employerResult{
					err: ctx.Err(),
				}
				return
			}

			found, err := p.searchEmployer(
				ctx,
				employer,
				request,
			)

			results <- employerResult{
				jobs: found,
				err:  err,
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	allJobs := make([]Job, 0)
	var firstErr error

	for result := range results {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}

		allJobs = append(
			allJobs,
			result.jobs...,
		)
	}

	// One broken employer feed should not erase valid jobs returned
	// by the other configured Lever employers.
	if len(allJobs) > 0 {
		return allJobs, nil
	}

	if firstErr != nil {
		return nil, firstErr
	}

	return []Job{}, nil
}

func (p *LeverProvider) searchEmployer(
	ctx context.Context,
	employer ATSEmployer,
	request SearchRequest,
) ([]Job, error) {
	result := make([]Job, 0)

	for page := 0; page < leverMaxPages; page++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		postings, err := p.fetchPage(
			ctx,
			employer.Site,
			page*leverPageSize,
		)
		if err != nil {
			return result, err
		}

		for _, posting := range postings {
			job := normalizeLeverPosting(
				employer,
				posting,
			)

			if !validJob(job) {
				continue
			}

			if !leverPostingMatchesSearch(
				request,
				job,
			) {
				continue
			}

			result = append(
				result,
				job,
			)
		}

		if len(postings) < leverPageSize {
			break
		}
	}

	return result, nil
}

func (p *LeverProvider) fetchPage(
	ctx context.Context,
	site string,
	skip int,
) ([]leverPosting, error) {
	site = strings.TrimSpace(site)

	if site == "" {
		return []leverPosting{}, nil
	}

	endpoint, err := url.Parse(
		leverBaseURL + "/" + url.PathEscape(site),
	)
	if err != nil {
		return nil, err
	}

	query := endpoint.Query()
	query.Set("mode", "json")
	query.Set(
		"limit",
		strconv.Itoa(leverPageSize),
	)
	query.Set(
		"skip",
		strconv.Itoa(skip),
	)

	endpoint.RawQuery = query.Encode()

	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		endpoint.String(),
		nil,
	)
	if err != nil {
		return nil, err
	}

	httpRequest.Header.Set(
		"Accept",
		"application/json",
	)

	response, err := p.client.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 ||
		response.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"lever site %q returned HTTP %d",
			site,
			response.StatusCode,
		)
	}

	var postings []leverPosting

	if err := json.NewDecoder(
		response.Body,
	).Decode(&postings); err != nil {
		return nil, err
	}

	return postings, nil
}

func normalizeLeverPosting(
	employer ATSEmployer,
	input leverPosting,
) Job {
	description := buildLeverDescription(input)

	applyURL := strings.TrimSpace(input.ApplyURL)

	if applyURL == "" {
		applyURL = strings.TrimSpace(
			input.HostedURL,
		)
	}

	var postedAt *time.Time

	if input.CreatedAt > 0 {
		created := time.UnixMilli(
			input.CreatedAt,
		).UTC()

		postedAt = &created
	}

	metadata := map[string]string{
		"ats_provider": "lever",
		"ats_site": strings.TrimSpace(
			employer.Site,
		),
	}

	if team := strings.TrimSpace(
		input.Categories.Team,
	); team != "" {
		metadata["team"] = team
	}

	if department := strings.TrimSpace(
		input.Categories.Department,
	); department != "" {
		metadata["department"] = department
	}

	job := Job{
		ID: strings.TrimSpace(input.ID),

		Source:      "lever",
		SourceJobID: strings.TrimSpace(input.ID),

		Title: strings.TrimSpace(input.Text),
		Company: strings.TrimSpace(
			employer.Name,
		),
		Description: description,

		Location: strings.TrimSpace(
			input.Categories.Location,
		),

		WorkArrangement: normalizeLeverWorkplaceType(
			input.WorkplaceType,
		),

		EmploymentType: strings.TrimSpace(
			input.Categories.Commitment,
		),

		PostedAt: postedAt,

		IsActive: true,

		ApplyURL: applyURL,

		Metadata: metadata,
	}

	if input.SalaryRange != nil {
		minimum := input.SalaryRange.Min
		maximum := input.SalaryRange.Max

		if minimum > 0 {
			job.SalaryMin = &minimum
		}

		if maximum > 0 {
			job.SalaryMax = &maximum
		}

		job.SalaryCurrency = strings.TrimSpace(
			input.SalaryRange.Currency,
		)

		job.SalaryPeriod = strings.TrimSpace(
			input.SalaryRange.Interval,
		)
	}

	return job
}

func buildLeverDescription(
	input leverPosting,
) string {
	parts := make([]string, 0)

	appendPart := func(value string) {
		value = cleanLeverHTML(value)

		if value == "" {
			return
		}

		parts = append(parts, value)
	}

	appendPart(input.Description)

	for _, list := range input.Lists {
		heading := cleanLeverHTML(list.Text)
		content := cleanLeverHTML(list.Content)

		switch {
		case heading != "" && content != "":
			appendPart(
				heading + "\n" + content,
			)

		case heading != "":
			appendPart(heading)

		case content != "":
			appendPart(content)
		}
	}

	appendPart(input.Additional)

	return strings.Join(
		parts,
		"\n\n",
	)
}

func cleanLeverHTML(
	value string,
) string {
	value = strings.TrimSpace(value)

	if value == "" {
		return ""
	}

	replacer := strings.NewReplacer(
		"<br>", "\n",
		"<br/>", "\n",
		"<br />", "\n",
		"</p>", "\n",
		"</li>", "\n",
		"<li>", "• ",
	)

	value = replacer.Replace(value)

	var builder strings.Builder
	inTag := false

	for _, r := range value {
		switch r {
		case '<':
			inTag = true

		case '>':
			inTag = false

		default:
			if !inTag {
				builder.WriteRune(r)
			}
		}
	}

	value = html.UnescapeString(
		builder.String(),
	)

	lines := strings.Split(
		value,
		"\n",
	)

	cleaned := make([]string, 0, len(lines))

	for _, line := range lines {
		line = strings.Join(
			strings.Fields(line),
			" ",
		)

		if line != "" {
			cleaned = append(
				cleaned,
				line,
			)
		}
	}

	return strings.Join(
		cleaned,
		"\n",
	)
}

func normalizeLeverWorkplaceType(
	value string,
) string {
	switch strings.ToLower(
		strings.TrimSpace(value),
	) {
	case "remote":
		return "remote"

	case "hybrid":
		return "hybrid"

	case "on-site",
		"onsite",
		"on_site":
		return "onsite"

	default:
		return ""
	}
}

func leverPostingMatchesSearch(
	request SearchRequest,
	job Job,
) bool {
	return jobTitleMatchesTargetRole(
		request.TargetRole,
		job.Title,
	)
}
