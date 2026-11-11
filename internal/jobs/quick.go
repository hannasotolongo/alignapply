package jobs

import (
	"context"
	"log"
	"sync"
	"time"
)

// QuickProvider keeps a slow live source from holding up a search. The
// search waits at most `wait` for it; if the source is still working, the
// search goes on without it (Jane's own saved jobs answer it), and the
// source keeps going in the background so its jobs are ready, from the
// cache, for the next search of the same role and place.
type QuickProvider struct {
	inner      Provider
	wait       time.Duration
	ttl        time.Duration
	background time.Duration

	mu      sync.Mutex
	done    map[string]quickEntry
	running map[string]*quickRun
}

type quickEntry struct {
	jobs    []Job
	expires time.Time
}

type quickRun struct {
	finished chan struct{}
	jobs     []Job
	err      error
}

// NewQuickProvider waits up to wait for inner, and keeps its answers for ttl.
func NewQuickProvider(inner Provider, wait, ttl time.Duration) *QuickProvider {
	return &QuickProvider{
		inner:      inner,
		wait:       wait,
		ttl:        ttl,
		background: 90 * time.Second,
		done:       make(map[string]quickEntry),
		running:    make(map[string]*quickRun),
	}
}

func (q *QuickProvider) Name() string { return q.inner.Name() }

func (q *QuickProvider) Search(ctx context.Context, request SearchRequest) ([]Job, error) {
	key := searchCacheKey(request)
	now := time.Now()

	q.mu.Lock()
	if entry, ok := q.done[key]; ok && now.Before(entry.expires) {
		q.mu.Unlock()
		return append([]Job(nil), entry.jobs...), nil
	}
	run, ok := q.running[key]
	if !ok {
		run = &quickRun{finished: make(chan struct{})}
		q.running[key] = run
		q.prune(now)
		go q.fetch(key, request, run)
	}
	q.mu.Unlock()

	timer := time.NewTimer(q.wait)
	defer timer.Stop()
	select {
	case <-run.finished:
		return append([]Job(nil), run.jobs...), run.err
	case <-timer.C:
		log.Printf("[job-quick] %s still working after %s; finishing in the background", q.inner.Name(), q.wait)
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// fetch runs the source on its own clock, not the search's, so a search
// that stops waiting doesn't cut it off.
func (q *QuickProvider) fetch(key string, request SearchRequest, run *quickRun) {
	ctx, cancel := context.WithTimeout(context.Background(), q.background)
	defer cancel()
	started := time.Now()
	jobs, err := q.inner.Search(ctx, request)
	log.Printf("[job-quick] %s returned %d jobs (err=%v) in %s", q.inner.Name(), len(jobs), err, time.Since(started).Round(time.Millisecond))

	q.mu.Lock()
	run.jobs, run.err = jobs, err
	delete(q.running, key)
	if err == nil && len(jobs) > 0 {
		q.done[key] = quickEntry{jobs: jobs, expires: time.Now().Add(q.ttl)}
	}
	q.mu.Unlock()
	close(run.finished)
}

// prune drops expired answers. Called with q.mu held.
func (q *QuickProvider) prune(now time.Time) {
	if len(q.done) < 500 {
		return
	}
	for key, entry := range q.done {
		if now.After(entry.expires) {
			delete(q.done, key)
		}
	}
}
