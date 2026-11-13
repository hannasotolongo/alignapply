package jobs

import (
	"context"
	"log"
	"sync/atomic"
)

// StorageLimitBytes is the size of the database plan (set from
// DATABASE_STORAGE_GB at startup; 5 GB by default). Jane never grows the
// database or changes the plan: when it gets close to full she stops
// keeping extra postings and says so in the logs, and the owner decides
// whether to pay for more room.
var StorageLimitBytes int64 = 5 << 30

const (
	// Above this share of the plan, jobs abroad are kept as a short
	// record only (US jobs are unaffected).
	foreignPostingsStopAt = 0.80
	storageWarnAt         = 0.90
)

var foreignPostingsOff atomic.Bool

func foreignPostingsAllowed() bool { return !foreignPostingsOff.Load() }

// DatabaseSizer is a store that can report how much space it uses.
type DatabaseSizer interface {
	DatabaseBytes(ctx context.Context) (int64, error)
}

// checkStorage logs how full the database is and turns full postings
// abroad off above 80%.
func checkStorage(ctx context.Context, store BoardStore) {
	sizer, ok := store.(DatabaseSizer)
	if !ok || StorageLimitBytes <= 0 {
		return
	}
	used, err := sizer.DatabaseBytes(ctx)
	if err != nil {
		log.Printf("[storage] couldn't read the database size: %v", err)
		return
	}
	share := float64(used) / float64(StorageLimitBytes)
	log.Printf("[storage] database uses %.2f GB of %.0f GB (%.0f%%)", float64(used)/(1<<30), float64(StorageLimitBytes)/(1<<30), share*100)
	switch {
	case share >= foreignPostingsStopAt:
		if !foreignPostingsOff.Swap(true) {
			log.Printf("[storage] WARNING: database is %.0f%% full. Jane stopped keeping full postings outside the US so it doesn't fill up. More room needs a bigger database plan (the owner's decision).", share*100)
		}
		if share >= storageWarnAt {
			log.Printf("[storage] WARNING: database is %.0f%% full and will stop saving jobs when it's full.", share*100)
		}
	default:
		foreignPostingsOff.Store(false)
	}
}
