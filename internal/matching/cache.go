package matching

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

// matchCache remembers recent scores so a repeat search, or the same job
// showing up in a new search, is instant. A score depends only on the
// candidate's evidence and the job itself, so that's the key.
type matchCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	max     int
	order   *list.List // front = most recently used
	entries map[string]*list.Element
}

type cachedMatch struct {
	key     string
	match   jobs.JobMatch
	expires time.Time
}

var scoreCache = newMatchCache(3000, 12*time.Hour)

func newMatchCache(max int, ttl time.Duration) *matchCache {
	return &matchCache{ttl: ttl, max: max, order: list.New(), entries: make(map[string]*list.Element)}
}

func matchCacheKey(s scorer, request jobs.SearchRequest, job jobs.Job) string {
	hash := sha256.New()
	mode := "full"
	if s.fast {
		mode = "fast"
	}
	hash.Write([]byte(mode))
	evidence, _ := json.Marshal(struct {
		Profile any
		Resume  string
	}{request.CareerProfile, request.ResumeText})
	hash.Write(evidence)
	posting, _ := json.Marshal(job)
	hash.Write(posting)
	return hex.EncodeToString(hash.Sum(nil))
}

func (c *matchCache) get(key string) (jobs.JobMatch, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return jobs.JobMatch{}, false
	}
	entry := element.Value.(*cachedMatch)
	if time.Now().After(entry.expires) {
		c.order.Remove(element)
		delete(c.entries, key)
		return jobs.JobMatch{}, false
	}
	c.order.MoveToFront(element)
	return entry.match, true
}

func (c *matchCache) put(key string, match jobs.JobMatch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.entries[key]; ok {
		element.Value.(*cachedMatch).match = match
		element.Value.(*cachedMatch).expires = time.Now().Add(c.ttl)
		c.order.MoveToFront(element)
		return
	}
	c.entries[key] = c.order.PushFront(&cachedMatch{key: key, match: match, expires: time.Now().Add(c.ttl)})
	for c.order.Len() > c.max {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.entries, oldest.Value.(*cachedMatch).key)
	}
}

func cachedScore(s scorer, request jobs.SearchRequest, job jobs.Job) jobs.JobMatch {
	key := matchCacheKey(s, request, job)
	if match, ok := scoreCache.get(key); ok {
		return match
	}
	match := scoreJob(s, request, job)
	scoreCache.put(key, match)
	return match
}
