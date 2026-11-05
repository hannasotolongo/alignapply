package matching

import (
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

const marketingPosting = "Marketing Manager. Requirements: 5+ years of marketing experience. Experience managing a team. Hands-on with HubSpot and Google Analytics. Bachelor's degree in marketing or related field. Nice to have: SQL."

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// The job-title line ("Marketing Manager, Brightline Foods, 2019 - 2025") and
// the degree line carry the words that prove the requirement; they must count.
func TestDatedTitleLineProvesYears(t *testing.T) {
	resume := "Alex Rivera\nMarketing Manager, Brightline Foods, 2019 - 2025\n- Led paid social and email campaigns with a $400K annual budget\n- Managed a team of 4 marketers\n- Ran A/B tests in HubSpot and Google Analytics\nB.S. Marketing, University of Florida"
	m := Match(jobs.SearchRequest{ResumeText: resume}, jobs.EnrichJob(jobs.Job{ID: "mkt", Description: marketingPosting}))
	if !contains(m.SupportedRequirements, "5+ years of marketing experience") {
		t.Fatalf("7 years as Marketing Manager not counted: %+v", m)
	}
	if !contains(m.SupportedRequirements, "Bachelor's degree in marketing or related field") {
		t.Fatalf("B.S. Marketing not counted: %+v", m)
	}
	if m.MatchLevel == "Reach" {
		t.Fatalf("qualified marketer scored Reach")
	}
}

func TestUnrelatedYearsDoNotCount(t *testing.T) {
	resume := "Sam Lee\nShift Lead, Blue Bottle Coffee, 2018 - 2025\n- Trained new baristas\n- Opened and closed the store\nB.A. History, Ohio State University"
	m := Match(jobs.SearchRequest{ResumeText: resume}, jobs.EnrichJob(jobs.Job{ID: "mkt", Description: marketingPosting}))
	if contains(m.SupportedRequirements, "5+ years of marketing experience") {
		t.Fatalf("coffee shift lead credited with marketing years")
	}
	if contains(m.SupportedRequirements, "Bachelor's degree in marketing or related field") {
		t.Fatalf("history degree credited as marketing degree")
	}
}
