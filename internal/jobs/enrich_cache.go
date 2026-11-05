package jobs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"sync"
)

// Per-job pipeline logs are thousands of lines per search and slow the
// server down; set JANE_DEBUG_PIPELINE=1 to turn them back on.
var pipelineDebug = os.Getenv("JANE_DEBUG_PIPELINE") != ""

func pipelineDebugf(format string, args ...any) {
	if pipelineDebug {
		log.Printf(format, args...)
	}
}

// Reading a posting's requirements is the slowest part of a search, and the
// same postings come back search after search (job listings are cached for
// hours), so the result is remembered by the posting's content.
var (
	enrichMu    sync.Mutex
	enrichCache = make(map[string]Job)
)

const enrichCacheMax = 5000

func enrichCached(job Job) Job {
	raw, _ := json.Marshal(job)
	sum := sha256.Sum256(raw)
	key := hex.EncodeToString(sum[:])

	enrichMu.Lock()
	cached, ok := enrichCache[key]
	enrichMu.Unlock()
	if ok {
		return cached
	}

	enriched := EnrichJob(job)

	enrichMu.Lock()
	if len(enrichCache) >= enrichCacheMax {
		// Simple bound: start over rather than track recency.
		enrichCache = make(map[string]Job)
	}
	enrichCache[key] = enriched
	enrichMu.Unlock()
	return enriched
}
