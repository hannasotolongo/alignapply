package jobs

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

// CachedProvider remembers a provider's results for a while, so repeat
// searches (the same role and place, including the automatic search on every
// sign-in) don't spend the job feed's request allowance.
type CachedProvider struct {
	inner      Provider
	ttl        time.Duration
	maxEntries int

	mu      sync.Mutex
	entries map[string]cachedSearch

	hits, misses int
}

type cachedSearch struct {
	jobs    []Job
	expires time.Time
}

func NewCachedProvider(inner Provider, ttl time.Duration) *CachedProvider {
	return &CachedProvider{inner: inner, ttl: ttl, maxEntries: 2000, entries: make(map[string]cachedSearch)}
}

func (c *CachedProvider) Name() string { return c.inner.Name() }

func searchCacheKey(request SearchRequest) string {
	norm := func(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }
	// Work style, salary and employment type are applied after retrieval,
	// so they don't need separate entries.
	return norm(request.TargetRole) + "|" + norm(request.Location) + "|" + strings.ToUpper(strings.TrimSpace(request.CountryCode))
}

func (c *CachedProvider) Search(ctx context.Context, request SearchRequest) ([]Job, error) {
	key := searchCacheKey(request)
	now := time.Now()

	c.mu.Lock()
	if entry, ok := c.entries[key]; ok && now.Before(entry.expires) {
		c.hits++
		hits, misses := c.hits, c.misses
		c.mu.Unlock()
		log.Printf("[job-cache] HIT provider=%s key=%q jobs=%d (hits=%d misses=%d)", c.inner.Name(), key, len(entry.jobs), hits, misses)
		return append([]Job(nil), entry.jobs...), nil
	}
	c.misses++
	c.mu.Unlock()

	jobs, err := c.inner.Search(ctx, request)
	if err != nil || len(jobs) == 0 {
		return jobs, err
	}

	c.mu.Lock()
	if len(c.entries) >= c.maxEntries {
		for k, entry := range c.entries {
			if now.After(entry.expires) || len(c.entries) >= c.maxEntries {
				delete(c.entries, k)
			}
			if len(c.entries) < c.maxEntries*9/10 {
				break
			}
		}
	}
	c.entries[key] = cachedSearch{jobs: append([]Job(nil), jobs...), expires: now.Add(c.ttl)}
	c.mu.Unlock()
	return jobs, nil
}
