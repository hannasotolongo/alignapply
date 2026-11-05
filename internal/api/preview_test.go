package api

import (
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

func TestPreviewSourcesAreNotScored(t *testing.T) {
	job := jobs.EnrichJob(jobs.Job{ID: "jooble-1", Source: jobs.SourceJooble, Title: "Store Manager", Company: "Target", ApplyURL: "https://x", Description: "Requirements: 3+ years of retail management experience."})
	match := previewOrScore(jobs.SearchRequest{ResumeText: "Store Manager, Target, 2015 - 2025"}, job)
	if match.MatchLevel != previewMatchLevel || match.MatchPercentage != 0 || len(match.SupportedRequirements) != 0 {
		t.Fatalf("preview job was scored: %+v", match)
	}

	full := jobs.EnrichJob(jobs.Job{ID: "gh-1", Source: "greenhouse", Title: "Store Manager", Company: "Target", ApplyURL: "https://x", Description: "Requirements: 3+ years of retail management experience."})
	if previewOrScore(jobs.SearchRequest{ResumeText: "Store Manager, Target, 2015 - 2025"}, full).MatchLevel == previewMatchLevel {
		t.Fatal("full postings must still be scored")
	}

	matches := []jobs.JobMatch{match, {MatchLevel: "Reach", MatchPercentage: 20}}
	sortJobMatches(matches)
	if matches[0].MatchLevel != "Reach" {
		t.Fatal("scored jobs should sort above preview-only jobs")
	}
}
