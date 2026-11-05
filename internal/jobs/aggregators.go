package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Jooble is a job aggregator with broad coverage (retail, healthcare,
// education, trades, office), which the company job boards don't reach. It
// returns only a short preview of each description, so its jobs are listed
// as previews: Jane shows them but doesn't pretend to score a fit from a
// few sentences.

const SourceJooble = "jooble"

// IsPreviewSource reports whether a source only provides description
// previews.
func IsPreviewSource(source string) bool {
	return source == SourceJooble
}

var (
	snippetTagRE   = regexp.MustCompile(`<[^>]+>`)
	snippetSpaceRE = regexp.MustCompile(`\s+`)
)

func cleanSnippet(value string) string {
	value = html.UnescapeString(snippetTagRE.ReplaceAllString(value, " "))
	return strings.TrimSpace(snippetSpaceRE.ReplaceAllString(value, " "))
}

// ---------------------------------------------------------------------------
// Jooble (free key: jooble.org/api/about)
// ---------------------------------------------------------------------------

type JoobleProvider struct {
	apiKey  string
	baseURL string
	budget  CallBudget
}

// CallBudget keeps a rate-limited source under its request allowance.
type CallBudget interface {
	// Reserve claims one request; false means the allowance is used up.
	Reserve(ctx context.Context) (bool, error)
}

// ErrBudgetExhausted means a source's request allowance is used up for now.
var ErrBudgetExhausted = errors.New("request allowance used up")

// WithBudget makes the provider ask the budget before every request.
func (p *JoobleProvider) WithBudget(budget CallBudget) *JoobleProvider {
	if p != nil {
		p.budget = budget
	}
	return p
}

// MemoryBudget is a CallBudget kept in memory (used when there's no
// database; it resets when the server restarts).
type MemoryBudget struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	calls  []time.Time
}

func NewMemoryBudget(limit int, window time.Duration) *MemoryBudget {
	return &MemoryBudget{limit: limit, window: window}
}

func (b *MemoryBudget) Reserve(context.Context) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cutoff := time.Now().Add(-b.window)
	kept := b.calls[:0]
	for _, at := range b.calls {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	b.calls = kept
	if len(b.calls) >= b.limit {
		return false, nil
	}
	b.calls = append(b.calls, time.Now())
	return true, nil
}

// NewJoobleProvider returns nil unless JOOBLE_API_KEY is set.
func NewJoobleProvider() *JoobleProvider {
	key := strings.TrimSpace(os.Getenv("JOOBLE_API_KEY"))
	if key == "" {
		return nil
	}
	return &JoobleProvider{apiKey: key, baseURL: "https://jooble.org/api/"}
}

func (p *JoobleProvider) Name() string { return SourceJooble }

type joobleResponse struct {
	Jobs []struct {
		ID       json.Number `json:"id"`
		Title    string      `json:"title"`
		Location string      `json:"location"`
		Snippet  string      `json:"snippet"`
		Salary   string      `json:"salary"`
		Type     string      `json:"type"`
		Link     string      `json:"link"`
		Company  string      `json:"company"`
		Updated  string      `json:"updated"`
	} `json:"jobs"`
}

func (p *JoobleProvider) Search(ctx context.Context, request SearchRequest) ([]Job, error) {
	role := strings.TrimSpace(request.TargetRole)
	if p == nil || role == "" {
		return nil, nil
	}
	if p.budget != nil {
		allowed, err := p.budget.Reserve(ctx)
		if err != nil {
			// If the count can't be checked, don't risk going over.
			return nil, fmt.Errorf("jooble: checking request allowance: %w", err)
		}
		if !allowed {
			return nil, ErrBudgetExhausted
		}
	}
	body := map[string]string{"keywords": role, "page": "1", "ResultOnPage": "50"}
	if location := strings.TrimSpace(request.Location); location != "" && !strings.EqualFold(location, "remote") {
		body["location"] = location
		body["radius"] = "40"
	}
	encoded, _ := json.Marshal(body)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+url.PathEscape(p.apiKey), bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	response, err := backupHTTPClient.Do(httpRequest)
	if err != nil {
		// The URL carries the key, so report only that the request failed.
		return nil, fmt.Errorf("jooble: request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("jooble: HTTP %d", response.StatusCode)
	}
	var payload joobleResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("jooble: %w", err)
	}

	jobs := make([]Job, 0, len(payload.Jobs))
	for _, item := range payload.Jobs {
		id := item.ID.String()
		title := cleanSnippet(item.Title)
		company := cleanSnippet(item.Company)
		if id == "" || title == "" || company == "" || item.Link == "" {
			continue
		}
		location := cleanSnippet(item.Location)
		job := Job{
			ID:              "jooble-" + id,
			Source:          SourceJooble,
			SourceJobID:     id,
			Title:           title,
			Company:         company,
			Description:     cleanSnippet(item.Snippet),
			Location:        location,
			WorkArrangement: arrangementFromLocation(location + " " + title),
			EmploymentType:  strings.ToLower(strings.TrimSpace(item.Type)),
			PostedAt:        parseTimeLoose(item.Updated),
			IsActive:        true,
			ApplyURL:        item.Link,
			SourceURL:       item.Link,
		}
		if !strings.Contains(strings.ToLower(location), "remote") && location != "" {
			job.CountryCode = countryFromLocation(location)
		}
		if low, high, ok := parseSalaryRange(item.Salary); ok {
			job.SalaryMin, job.SalaryMax = &low, &high
			lower := strings.ToLower(item.Salary)
			switch {
			case strings.Contains(lower, "hour"):
				job.SalaryPeriod = "hour"
			case strings.Contains(lower, "month"):
				job.SalaryPeriod = "month"
			default:
				job.SalaryPeriod = "year"
			}
			if strings.Contains(item.Salary, "$") {
				job.SalaryCurrency = "USD"
			}
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

var salaryNumberRE = regexp.MustCompile(`\$?\s*(\d[\d,]*(?:\.\d+)?)\s*(k)?`)

// parseSalaryRange reads Jooble's free-text salary ("$55,000 - $65,000",
// "$18 - $22 per hour", "$70k"). Hourly figures are left for the pipeline
// to annualize.
func parseSalaryRange(text string) (float64, float64, bool) {
	matches := salaryNumberRE.FindAllStringSubmatch(text, 2)
	values := make([]float64, 0, 2)
	for _, match := range matches {
		value, err := strconv.ParseFloat(strings.ReplaceAll(match[1], ",", ""), 64)
		if err != nil || value <= 0 {
			continue
		}
		if match[2] != "" {
			value *= 1000
		}
		values = append(values, value)
	}
	switch len(values) {
	case 0:
		return 0, 0, false
	case 1:
		return values[0], values[0], true
	default:
		return values[0], values[1], true
	}
}
