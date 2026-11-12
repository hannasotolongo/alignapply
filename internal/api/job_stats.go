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
)

// Live job numbers for the homepage, from Jane's own saved jobs: how many
// are open, and how many were posted (by the employer's own date) in the
// last 7 days and the last day, everywhere and per state. The server counts
// everything in the background every half hour, so a visitor's lookup is
// instant and never touches the database.

const jobStatsRefresh = 30 * time.Minute

type jobCounts struct {
	Open        int `json:"open"`
	NewThisWeek int `json:"newThisWeek"`
	NewToday    int `json:"newToday"`
}

func (c *jobCounts) add(posted *time.Time, now time.Time) {
	c.Open++
	if posted == nil || posted.After(now.Add(time.Hour)) {
		return
	}
	if posted.After(now.Add(-7 * 24 * time.Hour)) {
		c.NewThisWeek++
	}
	if posted.After(now.Add(-24 * time.Hour)) {
		c.NewToday++
	}
}

type jobStatsSnapshot struct {
	all    jobCounts
	states map[string]jobCounts
}

var jobStats = struct {
	mu       sync.RWMutex
	snapshot *jobStatsSnapshot
}{}

type jobStatsResponse struct {
	All   jobCounts  `json:"all"`
	State string     `json:"state,omitempty"`
	Near  *jobCounts `json:"near,omitempty"`
}

// GET /api/v1/stats/jobs[?place=Tampa, FL]
func (s *Server) handleJobStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	jobStats.mu.RLock()
	snapshot := jobStats.snapshot
	jobStats.mu.RUnlock()
	if snapshot == nil {
		writeError(w, http.StatusServiceUnavailable, "job numbers aren't ready yet")
		return
	}
	response := jobStatsResponse{All: snapshot.all}
	place := strings.TrimSpace(r.URL.Query().Get("place"))
	if place != "" && utf8.RuneCountInString(place) <= 100 {
		if code := jobs.StateOfPlace(place); code != "" {
			counts := snapshot.states[code]
			response.State = jobs.StateName(code)
			response.Near = &counts
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=120")
	writeJSON(w, http.StatusOK, response)
}

// RunJobStats keeps the homepage's live counts current until ctx ends.
func (s *Server) RunJobStats(ctx context.Context) {
	if s.crawledJobs == nil {
		return
	}
	for {
		s.refreshJobStats(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(jobStatsRefresh):
		}
	}
}

func (s *Server) refreshJobStats(ctx context.Context) {
	started := time.Now()
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	now := time.Now()
	snapshot := &jobStatsSnapshot{states: map[string]jobCounts{}}
	err := s.crawledJobs.EachOpenJob(queryCtx, func(location string, posted *time.Time) {
		snapshot.all.add(posted, now)
		for _, code := range jobs.StatesIn(location) {
			counts := snapshot.states[code]
			counts.add(posted, now)
			snapshot.states[code] = counts
		}
	})
	if err != nil {
		log.Printf("[job-stats] refresh: %v", err)
		return
	}
	jobStats.mu.Lock()
	jobStats.snapshot = snapshot
	jobStats.mu.Unlock()
	log.Printf("[job-stats] %d open jobs, %d posted this week, %d in the last day, %d states (%s)",
		snapshot.all.Open, snapshot.all.NewThisWeek, snapshot.all.NewToday, len(snapshot.states), time.Since(started).Round(time.Millisecond))
}
