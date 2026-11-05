package matching

import (
	"sync/atomic"
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

type countingVerifier struct{ calls *int64 }

func (v countingVerifier) Verify(requirement, evidence string) evidenceResult {
	atomic.AddInt64(v.calls, 1)
	return missingEvidence()
}

func TestMatchFastSkipsAIAndCaches(t *testing.T) {
	var calls int64
	SetEvidenceVerifier(countingVerifier{&calls})
	defer SetEvidenceVerifier(nil)

	resume := "Alex Rivera\nMarketing Manager, Brightline Foods, 2019 - 2025\n- Managed a team of 4 marketers\n- Ran A/B tests in HubSpot and Google Analytics\nB.S. Marketing, University of Florida"
	request := jobs.SearchRequest{ResumeText: resume}
	job := jobs.EnrichJob(jobs.Job{ID: "fast-1", Description: marketingPosting + " Experience with Salesforce Marketing Cloud."})

	first := MatchFast(request, job)
	if calls != 0 {
		t.Fatalf("fast scoring called the AI verifier %d times", calls)
	}
	if first.MatchPercentage == 0 {
		t.Fatalf("expected a score, got %+v", first)
	}
	again := MatchFast(request, job)
	if again.MatchPercentage != first.MatchPercentage || again.MatchLevel != first.MatchLevel {
		t.Fatalf("cached score differs: %d vs %d", again.MatchPercentage, first.MatchPercentage)
	}
	if key1, key2 := matchCacheKey(scorer{fast: true}, request, job), matchCacheKey(scorer{fast: true}, jobs.SearchRequest{ResumeText: resume + "\nSQL"}, job); key1 == key2 {
		t.Fatal("different résumés must not share a cached score")
	}

	Match(request, job)
	if calls == 0 {
		t.Fatal("full scoring (Fit Check) should still use the AI verifier")
	}
}
