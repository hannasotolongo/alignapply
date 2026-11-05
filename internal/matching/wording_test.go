package matching

import (
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

func TestDifferentWordingStillCounts(t *testing.T) {
	resume := `Jordan Lee
EXPERIENCE
Sales Manager — Acme Corp    Jan 2020 – Present
- Managed a team of 6 account executives and grew annual revenue 25%
- Built relationships with enterprise clients and closed deals over $250K
- Ran weekly pipeline reviews in Salesforce and created forecasting reports in Excel
- Coached new hires during onboarding
Account Executive — Beta Inc    2017 – 2019
- Exceeded quota every quarter selling SaaS software to mid-market companies
EDUCATION
B.A. Business Administration, University of Miami 2017`
	posting := `Regional Sales Manager
Requirements:
- 5+ years of B2B sales experience
- Experience leading and developing a sales team
- Proven track record of driving revenue growth
- Strong client relationship management skills
- Experience with CRM software such as Salesforce or HubSpot
- Data-driven forecasting and pipeline management
- Bachelor's degree in Business or related field
Preferred:
- Experience mentoring and training new sales representatives
- SaaS or software sales experience`
	score := func(r string) jobs.JobMatch {
		return Match(jobs.SearchRequest{ResumeText: r}, jobs.EnrichJob(jobs.Job{ID: "s", Description: posting}))
	}
	strong := score(resume)
	junior := score("Ana Cruz\nEXPERIENCE\nSales Development Rep — Gamma LLC    Jun 2024 – Present\n- Booked meetings for account executives by cold calling\n- Logged activity in HubSpot\nEDUCATION\nB.S. Communications, FIU 2024")
	barista := score("Sam Park\nEXPERIENCE\nBarista — Blue Bottle Coffee    2022 – Present\n- Made espresso drinks and kept the cafe clean\n- Handled the cash register\nEDUCATION\nHigh School Diploma 2021")
	if strong.MatchLevel != "Strong Applicant" || strong.MatchPercentage < 85 {
		t.Fatalf("experienced sales manager worded differently should be strong, got %d%% %s missing=%q", strong.MatchPercentage, strong.MatchLevel, strong.MissingRequirements)
	}
	if junior.MatchLevel != "Reach" || barista.MatchLevel != "Reach" {
		t.Fatalf("junior and barista should stay reaches: %s %d%%, %s %d%%", junior.MatchLevel, junior.MatchPercentage, barista.MatchLevel, barista.MatchPercentage)
	}
	if !(strong.MatchPercentage > junior.MatchPercentage && junior.MatchPercentage > barista.MatchPercentage) {
		t.Fatalf("expected strong > junior > barista, got %d, %d, %d", strong.MatchPercentage, junior.MatchPercentage, barista.MatchPercentage)
	}
}
