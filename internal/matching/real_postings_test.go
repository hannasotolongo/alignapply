package matching

import (
	"strings"
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

// Cases found by scoring real Greenhouse postings against real-looking
// résumés (Nov 2026).

func scoreOne(resume string, requirements ...jobs.Requirement) jobs.JobMatch {
	return Match(jobs.SearchRequest{ResumeText: resume}, jobs.Job{ID: "t", Title: "Test role", Company: "X", Description: "x", Requirements: requirements})
}

func required(category jobs.RequirementCategory, text string) jobs.Requirement {
	return jobs.Requirement{Text: text, Category: category, Importance: jobs.RequirementRequired}
}

const salesResume = `Chris Nguyen
Account executive with 5 years of B2B SaaS sales experience.
EXPERIENCE
Account Executive, Mid-Market — HubSpot, Austin TX 2020 – Present
- Closed $1.4M in new annual recurring revenue in 2024, 128% of quota
- Run full sales cycle from prospecting to close
EDUCATION
BBA Marketing, Texas State University, 2020`

const engineerResume = `Maya Torres
Software engineer with 5 years building backend services in Go and Python.
EXPERIENCE
Software Engineer, Bright Ledger 2020 – Present
- Built REST APIs in Go and PostgreSQL
- Built React and TypeScript features for the billing dashboard
EDUCATION
B.S. Computer Science, University of South Florida, 2020`

func TestSalesYearsDontCountAsSoftwareEngineeringYears(t *testing.T) {
	match := scoreOne(salesResume, required(jobs.RequirementExperience, "5+ years of software engineering experience"))
	if containsRequirement(match.SupportedRequirements, "5+ years of software engineering experience") {
		t.Fatalf("5 years of SaaS sales must not meet 5 years of software engineering; got %+v", match)
	}
}

func TestBBAMeetsBachelorsDegree(t *testing.T) {
	match := scoreOne(salesResume, required(jobs.RequirementEducation, "Bachelor's Degree or equivalent years of experience"))
	if len(match.SupportedRequirements) != 1 {
		t.Fatalf("a BBA is a bachelor's degree; got %+v", match)
	}
}

func TestLanguagesAreChecked(t *testing.T) {
	resume := "Customer support specialist. Bilingual English and Spanish. Zendesk, Salesforce."
	zendesk := required(jobs.RequirementSkill, "Experience with Zendesk")
	if match := scoreOne(resume, zendesk, required(jobs.RequirementOther, "Fluency in Spanish, both spoken and written")); !containsRequirement(match.SupportedRequirements, "Fluency in Spanish, both spoken and written") {
		t.Fatalf("bilingual Spanish should meet fluency in Spanish; got %+v", match)
	}
	if match := scoreOne(engineerResume, zendesk, required(jobs.RequirementOther, "Fluency in Spanish, both spoken and written")); containsRequirement(match.SupportedRequirements, "Fluency in Spanish, both spoken and written") {
		t.Fatalf("no Spanish on the résumé; got %+v", match)
	}
}

func TestImpliedSkills(t *testing.T) {
	match := scoreOne(engineerResume,
		required(jobs.RequirementSkill, "Proficiency in object-oriented programming"),
		required(jobs.RequirementOther, "You are an expert in React/React Native with a solid grasp of Python backend development"),
	)
	// React Native isn't on the résumé, so the second one is a partial match.
	if len(match.SupportedRequirements) != 1 || len(match.PartialRequirements) != 1 {
		t.Fatalf("Go/Python imply OOP, and React + Python is on the résumé; got supported=%v partial=%v missing=%v",
			match.SupportedRequirements, match.PartialRequirements, match.MissingRequirements)
	}
	if match := scoreOne(salesResume, required(jobs.RequirementSkill, "Proficiency in object-oriented programming")); len(match.SupportedRequirements) != 0 {
		t.Fatalf("a sales résumé implies no programming; got %+v", match)
	}
}

func TestLeadershipWordsCountOnce(t *testing.T) {
	match := scoreOne(salesResume, required(jobs.RequirementExperience, "Experience leading engineering organizations through managers, including managing managers and staff-level engineers"))
	if len(match.SupportedRequirements) != 0 {
		t.Fatalf("running a sales cycle isn't leading engineering managers; got %+v", match)
	}
}

func TestPostingWithoutRequirementsIsScoredOnTitle(t *testing.T) {
	nurse := "Aisha Brown, RN, BSN\nRegistered Nurse, Pediatric Unit 2021 – Present\n- Care for pediatric patients\nBachelor of Science in Nursing, 2019"
	job := jobs.Job{ID: "rn", Title: "1:1 Adult & Pediatric Nurse, RN", Company: "Bayada", Description: "Weekly pay. PTO. Flexible scheduling."}
	match := Match(jobs.SearchRequest{ResumeText: nurse}, job)
	if match.MatchPercentage == 0 || match.MatchPercentage > 75 || match.MatchLevel == "Strong Applicant" {
		t.Fatalf("a nurse on a nurse posting with no listed requirements: want a capped title score; got %d%% %s", match.MatchPercentage, match.MatchLevel)
	}
	if !strings.Contains(match.Explanation, "job title") {
		t.Fatalf("the explanation should say the score comes from the title: %q", match.Explanation)
	}
	if other := Match(jobs.SearchRequest{ResumeText: engineerResume}, job); other.MatchPercentage > 20 {
		t.Fatalf("an engineer on a nurse posting: got %d%%", other.MatchPercentage)
	}
}
