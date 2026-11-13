package matching

import (
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/candidate"
	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

func TestStructuredCareerProfileCanMatchWithoutResumeText(t *testing.T) {
	request := jobs.SearchRequest{
		CareerProfile: candidate.CareerProfile{
			Skills: []string{
				"Go",
				"Distributed systems",
			},
		},
	}

	job := structuredProfileTestJob(
		jobs.Requirement{
			Text:       "Go",
			Category:   jobs.RequirementSkill,
			Importance: jobs.RequirementRequired,
		},
	)

	match := Match(request, job)

	if match.MatchLevel == "Insufficient Evidence" {
		t.Fatalf(
			"expected structured career profile to be scorable without resumeText",
		)
	}

	if !containsRequirement(
		match.SupportedRequirements,
		"Go",
	) {
		t.Fatalf(
			"expected Go requirement to be supported; got supported=%v partial=%v missing=%v",
			match.SupportedRequirements,
			match.PartialRequirements,
			match.MissingRequirements,
		)
	}
}

func TestResumeAndCareerProfileBothCount(t *testing.T) {
	// A saved profile is often shorter than the résumé it came from. Both
	// are the user's own words, so a skill on either one counts.
	request := jobs.SearchRequest{
		ResumeText: "Go distributed systems",
		CareerProfile: candidate.CareerProfile{
			Skills: []string{
				"Python",
			},
		},
	}

	for _, skill := range []string{"Go", "Python"} {
		job := structuredProfileTestJob(
			jobs.Requirement{
				Text:       skill,
				Category:   jobs.RequirementSkill,
				Importance: jobs.RequirementRequired,
			},
		)
		match := Match(request, job)
		if !containsRequirement(match.SupportedRequirements, skill) {
			t.Fatalf("%s should count from the résumé or the profile; got supported=%v missing=%v",
				skill, match.SupportedRequirements, match.MissingRequirements)
		}
	}
}

func TestProjectCanSupportSkillRequirement(t *testing.T) {
	request := jobs.SearchRequest{
		CareerProfile: candidate.CareerProfile{
			Projects: []string{
				"Built distributed services in Go",
			},
		},
	}

	job := structuredProfileTestJob(
		jobs.Requirement{
			Text:       "Go",
			Category:   jobs.RequirementSkill,
			Importance: jobs.RequirementRequired,
		},
	)

	match := Match(request, job)

	if !containsRequirement(
		match.SupportedRequirements,
		"Go",
	) {
		t.Fatalf(
			"expected project evidence to support a skill requirement; supported=%v partial=%v missing=%v",
			match.SupportedRequirements,
			match.PartialRequirements,
			match.MissingRequirements,
		)
	}
}

func TestProjectDurationCannotSatisfyProfessionalExperienceYears(t *testing.T) {
	request := jobs.SearchRequest{
		CareerProfile: candidate.CareerProfile{
			Projects: []string{
				"Built Go backend systems for 5 years",
			},
		},
	}

	job := structuredProfileTestJob(
		jobs.Requirement{
			Text:       "5 years of Go backend experience",
			Category:   jobs.RequirementExperience,
			Importance: jobs.RequirementRequired,
		},
	)

	match := Match(request, job)

	if containsRequirement(
		match.SupportedRequirements,
		"5 years of Go backend experience",
	) {
		t.Fatalf(
			"project duration must not satisfy professional experience years",
		)
	}

	if match.MatchLevel != "Reach" {
		t.Fatalf(
			"expected required explicit experience gap to classify as Reach; got %q",
			match.MatchLevel,
		)
	}
}

func TestProfessionalExperienceCanSatisfyExperienceYears(t *testing.T) {
	request := jobs.SearchRequest{
		CareerProfile: candidate.CareerProfile{
			Experience: []string{
				"5 years of Go backend experience building production services",
			},
		},
	}

	job := structuredProfileTestJob(
		jobs.Requirement{
			Text:       "5 years of Go backend experience",
			Category:   jobs.RequirementExperience,
			Importance: jobs.RequirementRequired,
		},
	)

	match := Match(request, job)

	if !containsRequirement(
		match.SupportedRequirements,
		"5 years of Go backend experience",
	) {
		t.Fatalf(
			"expected professional experience evidence to satisfy explicit years requirement; supported=%v partial=%v missing=%v",
			match.SupportedRequirements,
			match.PartialRequirements,
			match.MissingRequirements,
		)
	}
}

func TestLegacyResumeTextStillWorksWithoutCareerProfile(t *testing.T) {
	request := jobs.SearchRequest{
		ResumeText: "Built production services in Go",
	}

	job := structuredProfileTestJob(
		jobs.Requirement{
			Text:       "Go",
			Category:   jobs.RequirementSkill,
			Importance: jobs.RequirementRequired,
		},
	)

	match := Match(request, job)

	if match.MatchLevel == "Insufficient Evidence" {
		t.Fatalf(
			"expected legacy resumeText request to remain scorable",
		)
	}

	if !containsRequirement(
		match.SupportedRequirements,
		"Go",
	) {
		t.Fatalf(
			"expected legacy resume evidence to continue supporting Go",
		)
	}
}

func structuredProfileTestJob(
	requirements ...jobs.Requirement,
) jobs.Job {
	return jobs.Job{
		ID:           "structured-profile-test-job",
		Source:       "test",
		SourceJobID:  "structured-profile-test-job",
		Title:        "Software Engineer",
		Company:      "Test Company",
		Description:  "Test job description.",
		Location:     "Miami, FL",
		IsActive:     true,
		Requirements: requirements,
		ApplyURL:     "https://example.com/apply",
	}
}

func containsRequirement(
	values []string,
	expected string,
) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}

	return false
}
