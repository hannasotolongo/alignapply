package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

// A search's ranked, scored matches are kept for a while so "Load more"
// shows the next page right away, without searching or scoring again.
const (
	// Each kept job carries its description, so memory is capped at about
	// 40 searches x 300 jobs (roughly 100 MB on a small server).
	matchPageTTL        = time.Hour
	matchPageMaxEntries = 40
	maxMatchesKept      = 300
	maxPageSize         = 50
)

type matchPage struct {
	matches []jobs.JobMatch
	at      time.Time
}

type matchPageCache struct {
	mu      sync.Mutex
	entries map[string]matchPage
}

var searchPages = &matchPageCache{entries: map[string]matchPage{}}

// matchPageKey identifies one user's search, whatever page they ask for.
func matchPageKey(userID string, request jobs.SearchRequest) string {
	request.Offset, request.PageSize = 0, 0
	encoded, _ := json.Marshal(request)
	sum := sha256.Sum256(append([]byte(userID+"|"), encoded...))
	return hex.EncodeToString(sum[:])
}

func (c *matchPageCache) get(key string) ([]jobs.JobMatch, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || time.Since(entry.at) > matchPageTTL {
		return nil, false
	}
	return entry.matches, true
}

func (c *matchPageCache) put(key string, matches []jobs.JobMatch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= matchPageMaxEntries {
		for k, entry := range c.entries {
			if time.Since(entry.at) > matchPageTTL || len(c.entries) >= matchPageMaxEntries {
				delete(c.entries, k)
			}
		}
	}
	c.entries[key] = matchPage{matches: matches, at: time.Now()}
}

// writeMatchPage sends one page of the ranked matches. The total and the
// next page's offset go in headers, so the body stays a plain list.
func writeMatchPage(w http.ResponseWriter, all []jobs.JobMatch, offset, size int) {
	if size > maxPageSize {
		size = maxPageSize
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(all) {
		offset = len(all)
	}
	end := min(offset+size, len(all))
	w.Header().Set("X-Jane-Total", strconv.Itoa(len(all)))
	w.Header().Set("X-Jane-Next-Offset", strconv.Itoa(end))
	w.Header().Set("Access-Control-Expose-Headers", "X-Jane-Total, X-Jane-Next-Offset")
	writeJSON(w, http.StatusOK, all[offset:end])
}
