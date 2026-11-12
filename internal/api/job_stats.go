package api

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
)

// Live job numbers for the homepage: how many jobs are open in Jane's own
// database, and how many she found this week and today, everywhere and in
// a visitor's state. Counts are real and cached for a few minutes, so the
// homepage never waits on the database.

const jobStatsTTL = 10 * time.Minute

type jobStatsEntry struct {
	counts  repository.JobCounts
	expires time.Time
}

var jobStats = struct {
	mu      sync.Mutex
	entries map[string]jobStatsEntry
	running map[string]bool
}{entries: map[string]jobStatsEntry{}, running: map[string]bool{}}

type jobStatsResponse struct {
	All   repository.JobCounts  `json:"all"`
	State string                `json:"state,omitempty"`
	Near  *repository.JobCounts `json:"near,omitempty"`
}

func (s *Server) handleJobStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	if s.crawledJobs == nil {
		writeError(w, http.StatusServiceUnavailable, "job numbers aren't available")
		return
	}
	all, ok := s.cachedJobCounts(r.Context(), "")
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "job numbers aren't ready yet")
		return
	}
	response := jobStatsResponse{All: all}

	place := strings.TrimSpace(r.URL.Query().Get("place"))
	if place != "" && utf8.RuneCountInString(place) <= 100 {
		if code := jobs.StateOfPlace(place); code != "" {
			if near, ok := s.cachedJobCounts(r.Context(), code); ok {
				response.State = jobs.StateName(code)
				response.Near = &near
			}
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=120")
	writeJSON(w, http.StatusOK, response)
}

// cachedJobCounts returns counts for a state ("" for everywhere). A stale
// entry is returned at once and refreshed in the background.
func (s *Server) cachedJobCounts(ctx context.Context, state string) (repository.JobCounts, bool) {
	jobStats.mu.Lock()
	entry, ok := jobStats.entries[state]
	fresh := ok && time.Now().Before(entry.expires)
	if ok && !fresh && !jobStats.running[state] {
		jobStats.running[state] = true
		go s.refreshJobCounts(state)
	}
	jobStats.mu.Unlock()
	if ok {
		return entry.counts, true
	}

	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	counts, err := s.crawledJobs.CountJobs(queryCtx, statePattern(state))
	if err != nil {
		log.Printf("[job-stats] count %q: %v", state, err)
		return repository.JobCounts{}, false
	}
	jobStats.mu.Lock()
	jobStats.entries[state] = jobStatsEntry{counts: counts, expires: time.Now().Add(jobStatsTTL)}
	jobStats.mu.Unlock()
	return counts, true
}

func (s *Server) refreshJobCounts(state string) {
	defer func() {
		jobStats.mu.Lock()
		delete(jobStats.running, state)
		jobStats.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	counts, err := s.crawledJobs.CountJobs(ctx, statePattern(state))
	if err != nil {
		log.Printf("[job-stats] refresh %q: %v", state, err)
		return
	}
	jobStats.mu.Lock()
	jobStats.entries[state] = jobStatsEntry{counts: counts, expires: time.Now().Add(jobStatsTTL)}
	jobStats.mu.Unlock()
}

// statePattern matches job locations in a state ("" matches everywhere).
func statePattern(state string) string { return jobs.StatePattern(state) }
