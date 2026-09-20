package matching

import (
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

func TestFitCategoryBestFitRequiresBroadRequiredSupport(t *testing.T) {
	request := jobs.SearchRequest{ResumeText: `
Experience
6 years of backend engineering experience building Go APIs and services.

Education
Bachelor's degree in Biology

Skills
Go, SQL, AWS
`}

	job := jobs.Job{
		ID:          "fit-best",
		Title:       "Backend Engineer",
		Company:     "Example",
		Description: "Backend engineering role.",
		Requirements: []jobs.Requirement{
			{Text: "5 years of backend engineering experience", Category: jobs.RequirementExperience, Importance: jobs.RequirementRequired},
			{Text: "Go skills", Category: jobs.RequirementSkill, Importance: jobs.RequirementRequired},
			{Text: "SQL skills", Category: jobs.RequirementSkill, Importance: jobs.RequirementRequired},
			{Text: "Bachelor's degree", Category: jobs.RequirementEducation, Importance: jobs.RequirementRequired},
		},
	}

	result := Match(request, job)
	if result.MatchLevel != "Best Fit" {
		t.Fatalf("expected Best Fit, got %q: %+v", result.MatchLevel, result)
	}
}

func TestFitCategoryMissingRequiredLicenseForcesReach(t *testing.T) {
	request := jobs.SearchRequest{ResumeText: `
Experience
Managed client relationships and healthcare accounts for 8 years.

Education
Bachelor's degree

Skills
Communication, account management, Excel
`}

	job := jobs.Job{
		ID:          "fit-license-gap",
		Title:       "Licensed Professional",
		Company:     "Example",
		Description: "Client-facing licensed role.",
		Requirements: []jobs.Requirement{
			{Text: "Client relationship experience", Category: jobs.RequirementExperience, Importance: jobs.RequirementRequired},
			{Text: "Communication skills", Category: jobs.RequirementSkill, Importance: jobs.RequirementRequired},
			{Text: "Active state professional license", Category: jobs.RequirementLicense, Importance: jobs.RequirementRequired},
		},
	}

	result := Match(request, job)
	if result.MatchLevel != "Reach" {
		t.Fatalf("missing required license must force Reach, got %q: %+v", result.MatchLevel, result)
	}
}

func TestFitCategoryMissingExplicitRequiredYearsForcesReach(t *testing.T) {
	request := jobs.SearchRequest{ResumeText: `
Experience
Built backend APIs in Go.

Skills
Go, SQL, Docker
`}

	job := jobs.Job{
		ID:          "fit-years-gap",
		Title:       "Senior Backend Engineer",
		Company:     "Example",
		Description: "Senior backend engineering role.",
		Requirements: []jobs.Requirement{
			{Text: "7 years of backend engineering experience", Category: jobs.RequirementExperience, Importance: jobs.RequirementRequired},
			{Text: "Go skills", Category: jobs.RequirementSkill, Importance: jobs.RequirementRequired},
			{Text: "SQL skills", Category: jobs.RequirementSkill, Importance: jobs.RequirementRequired},
		},
	}

	result := Match(request, job)
	if result.MatchLevel != "Reach" {
		t.Fatalf("missing explicit required years must force Reach, got %q: %+v", result.MatchLevel, result)
	}
}

func TestFitCategoryPreferredGapDoesNotBlockBestFit(t *testing.T) {
	request := jobs.SearchRequest{ResumeText: `
Experience
5 years of backend engineering experience building Go services.

Education
Bachelor's degree

Skills
Go, SQL
`}

	job := jobs.Job{
		ID:          "fit-preferred",
		Title:       "Backend Engineer",
		Company:     "Example",
		Description: "Backend engineering role.",
		Requirements: []jobs.Requirement{
			{Text: "5 years of backend engineering experience", Category: jobs.RequirementExperience, Importance: jobs.RequirementRequired},
			{Text: "Go skills", Category: jobs.RequirementSkill, Importance: jobs.RequirementRequired},
			{Text: "Bachelor's degree", Category: jobs.RequirementEducation, Importance: jobs.RequirementRequired},
			{Text: "Ruby on Rails", Category: jobs.RequirementSkill, Importance: jobs.RequirementPreferred},
		},
	}

	result := Match(request, job)
	if result.MatchLevel != "Best Fit" {
		t.Fatalf("preferred-only gap should not block Best Fit, got %q: %+v", result.MatchLevel, result)
	}
}

func TestFitCategoryPartialRequiredEvidenceIsGoodFit(t *testing.T) {
	request := jobs.SearchRequest{ResumeText: `
Experience
Built backend services in Go and worked on distributed applications.

Skills
Go, SQL
`}

	job := jobs.Job{
		ID:          "fit-good",
		Title:       "Backend Engineer",
		Company:     "Example",
		Description: "Backend engineering role.",
		Requirements: []jobs.Requirement{
			{Text: "Go skills", Category: jobs.RequirementSkill, Importance: jobs.RequirementRequired},
			{Text: "SQL skills", Category: jobs.RequirementSkill, Importance: jobs.RequirementRequired},
			{Text: "Distributed systems architecture", Category: jobs.RequirementSkill, Importance: jobs.RequirementRequired},
		},
	}

	result := Match(request, job)
	if result.MatchLevel != "Good Fit" {
		t.Fatalf("expected Good Fit for substantial but incomplete required evidence, got %q: %+v", result.MatchLevel, result)
	}
}
