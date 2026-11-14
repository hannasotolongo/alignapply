package api

import (
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

func TestNearbyTrialMatchesRemote(t *testing.T) {
	miles := 12.0
	ranked := []jobs.JobMatch{
		{ID: "a", Location: "Remote", WorkArrangement: "remote"},
		{ID: "b", Location: "Remote - US"},
		{ID: "c", Location: "Tampa, FL", DistanceMiles: &miles},
	}
	if got := nearbyTrialMatches(ranked, "Remote"); len(got) != 2 {
		t.Fatalf("Remote search kept %d jobs, want the 2 remote ones", len(got))
	}
	// Tampa has one job nearby: the remote ones top it up.
	if got := nearbyTrialMatches(ranked, "Tampa, FL"); len(got) != 3 || got[0].ID != "c" {
		t.Fatalf("Tampa search kept %+v, want the Tampa job first, then remote ones", got)
	}
}
