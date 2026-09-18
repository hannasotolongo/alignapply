package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Provider is the common interface for every live job source CaseMade uses.
//
// Providers are responsible only for retrieving and normalizing jobs.
// They must NOT decide whether a candidate is qualified for a job.
type Provider interface {
	Name() string
	Search(ctx context.Context, request SearchRequest) ([]Job, error)
}

// ProviderError records a failure from one source without preventing
// successful sources from returning jobs.
type ProviderError struct {
	Provider string
	Err      error
}

func (e ProviderError) Error() string {
	return fmt.Sprintf("%s: %v", e.Provider, e.Err)
}

// SearchResult contains the combined normalized jobs returned by all
// available providers.
type SearchResult struct {
	Jobs   []Job
	Errors []ProviderError
}

// MultiProvider searches multiple independent job sources concurrently.
type MultiProvider struct {
	providers []Provider
}

func NewMultiProvider(providers ...Provider) *MultiProvider {
	filtered := make([]Provider, 0, len(providers))

	for _, provider := range providers {
		if provider != nil {
			filtered = append(filtered, provider)
		}
	}

	return &MultiProvider{
		providers: filtered,
	}
}

func (m *MultiProvider) Search(
	ctx context.Context,
	request SearchRequest,
) SearchResult {
	if len(m.providers) == 0 {
		return SearchResult{
			Jobs:   []Job{},
			Errors: []ProviderError{},
		}
	}

	type providerResult struct {
		name string
		jobs []Job
		err  error
	}

	results := make(
		chan providerResult,
		len(m.providers),
	)

	var wg sync.WaitGroup

	for _, provider := range m.providers {
		wg.Add(1)

		go func(p Provider) {
			defer wg.Done()

			jobsFound, err := p.Search(ctx, request)

			results <- providerResult{
				name: p.Name(),
				jobs: jobsFound,
				err:  err,
			}
		}(provider)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	allJobs := make([]Job, 0)
	providerErrors := make([]ProviderError, 0)

	for result := range results {
		if result.err != nil {
			if !errors.Is(result.err, context.Canceled) {
				providerErrors = append(
					providerErrors,
					ProviderError{
						Provider: result.name,
						Err:      result.err,
					},
				)
			}

			continue
		}

		allJobs = append(allJobs, result.jobs...)
	}

	return SearchResult{
		Jobs:   deduplicate(allJobs),
		Errors: providerErrors,
	}
}

// deduplicate removes obvious duplicate postings returned by different
// providers.
//
// This is intentionally industry-agnostic. It does not inspect technical
// skills or assume any particular profession.
func deduplicate(input []Job) []Job {
	seen := make(map[string]struct{})
	result := make([]Job, 0, len(input))

	for _, job := range input {
		if !validJob(job) {
			continue
		}

		key := normalizedKey(
			job.Company,
			job.Title,
			job.Location,
		)

		if _, exists := seen[key]; exists {
			continue
		}

		seen[key] = struct{}{}
		result = append(result, job)
	}

	return result
}

func validJob(job Job) bool {
	return strings.TrimSpace(job.ID) != "" &&
		strings.TrimSpace(job.Title) != "" &&
		strings.TrimSpace(job.Company) != "" &&
		strings.TrimSpace(job.ApplyURL) != ""
}

func normalizedKey(values ...string) string {
	normalized := make(
		[]string,
		0,
		len(values),
	)

	for _, value := range values {
		value = strings.TrimSpace(value)
		value = strings.ToLower(value)
		value = strings.Join(
			strings.Fields(value),
			" ",
		)

		normalized = append(
			normalized,
			value,
		)
	}

	return strings.Join(normalized, "|")
}
