package jobs

import "testing"

func TestExtractionQualityGoodForStructuredPosting(t *testing.T) {
	job := Job{
		Description: "Structured job posting",
		Requirements: []Requirement{
			{
				Text:       "3 years of backend engineering experience",
				Category:   RequirementExperience,
				Importance: RequirementRequired,
			},
			{
				Text:       "Strong communication skills",
				Category:   RequirementSkill,
				Importance: RequirementRequired,
			},
			{
				Text:       "Bachelor's degree",
				Category:   RequirementEducation,
				Importance: RequirementRequired,
			},
			{
				Text:       "AWS experience preferred",
				Category:   RequirementExperience,
				Importance: RequirementPreferred,
			},
		},
	}

	quality := EvaluateExtractionQuality(job)

	if quality.Status != ExtractionQualityGood {
		t.Fatalf(
			"expected good extraction quality, got %q",
			quality.Status,
		)
	}

	if !quality.Scorable {
		t.Fatal("expected structured posting to be scorable")
	}

	if quality.Score < 55 {
		t.Fatalf(
			"expected quality score >= 55, got %d",
			quality.Score,
		)
	}
}

func TestExtractionQualityInsufficientWithoutRequirements(t *testing.T) {
	job := Job{
		Description:  "A job posting with no extracted qualifications.",
		Requirements: nil,
	}

	quality := EvaluateExtractionQuality(job)

	if quality.Status != ExtractionQualityInsufficient {
		t.Fatalf(
			"expected insufficient quality, got %q",
			quality.Status,
		)
	}

	if quality.Scorable {
		t.Fatal("posting without requirements must not be scorable")
	}
}

func TestExtractionQualityInsufficientWithoutDescription(t *testing.T) {
	job := Job{
		Description: "",
		Requirements: []Requirement{
			{
				Text:       "Go experience",
				Category:   RequirementSkill,
				Importance: RequirementRequired,
			},
		},
	}

	quality := EvaluateExtractionQuality(job)

	if quality.Scorable {
		t.Fatal("posting without a description must not be scorable")
	}
}

func TestExtractionQualityLimitedForSingleStrongRequirement(t *testing.T) {
	job := Job{
		Description: "Minimum Qualifications: 3 years of experience.",
		Requirements: []Requirement{
			{
				Text:       "3 years of experience",
				Category:   RequirementExperience,
				Importance: RequirementRequired,
			},
		},
	}

	quality := EvaluateExtractionQuality(job)

	if quality.Status != ExtractionQualityLimited {
		t.Fatalf(
			"expected limited extraction quality, got %q",
			quality.Status,
		)
	}

	if !quality.Scorable {
		t.Fatal(
			"single explicit strong qualification should remain scorable with limited quality",
		)
	}
}

func TestExtractionQualityRejectsUnknownRequirementData(t *testing.T) {
	job := Job{
		Description: "Qualifications are present.",
		Requirements: []Requirement{
			{
				Text:       "Unknown requirement",
				Category:   RequirementCategory("invalid"),
				Importance: RequirementImportance("invalid"),
			},
		},
	}

	quality := EvaluateExtractionQuality(job)

	if quality.Scorable {
		t.Fatal("invalid structured requirement must not be scorable")
	}

	if quality.RequirementCount != 0 {
		t.Fatalf(
			"expected zero usable requirements, got %d",
			quality.RequirementCount,
		)
	}
}

func TestExtractionQualityPreferredOnlyPostingCanBeScorable(t *testing.T) {
	job := Job{
		Description: "Preferred Qualifications",
		Requirements: []Requirement{
			{
				Text:       "Salesforce experience preferred",
				Category:   RequirementExperience,
				Importance: RequirementPreferred,
			},
			{
				Text:       "Bachelor's degree preferred",
				Category:   RequirementEducation,
				Importance: RequirementPreferred,
			},
			{
				Text:       "Strong communication skills preferred",
				Category:   RequirementSkill,
				Importance: RequirementPreferred,
			},
		},
	}

	quality := EvaluateExtractionQuality(job)

	if !quality.Scorable {
		t.Fatal(
			"posting with multiple explicit preferred qualifications should be scorable",
		)
	}
}

func TestExtractionQualityOtherOnlyEvidenceIsNotScorable(t *testing.T) {
	job := Job{
		Description: "Qualifications",
		Requirements: []Requirement{
			{
				Text:       "Professional demeanor",
				Category:   RequirementOther,
				Importance: RequirementRequired,
			},
		},
	}

	quality := EvaluateExtractionQuality(job)

	if quality.Scorable {
		t.Fatal(
			"other-only evidence should not produce an evidence-match score",
		)
	}
}

func TestExtractionQualityRejectsBoilerplateAsQualificationEvidence(t *testing.T) {
	job := Job{
		Description: "Job posting with malformed extracted content.",
		Requirements: []Requirement{
			{
				Text: "This description outlines the basic information for the " +
					"position noted. This is not a comprehensive listing of responsibilities.",
				Category:   RequirementEducation,
				Importance: RequirementRequired,
			},
			{
				Text:       "Contact recruiter@example.com to apply.",
				Category:   RequirementOther,
				Importance: RequirementRequired,
			},
		},
	}

	quality := EvaluateExtractionQuality(job)

	if quality.Scorable {
		t.Fatalf(
			"boilerplate extraction must not be scorable: %#v",
			quality,
		)
	}

	if quality.Status != ExtractionQualityInsufficient {
		t.Fatalf(
			"expected insufficient extraction quality, got %q",
			quality.Status,
		)
	}

	if quality.RequirementCount != 0 {
		t.Fatalf(
			"expected suspicious boilerplate to contribute zero usable requirements, got %d",
			quality.RequirementCount,
		)
	}
}

func TestExtractionQualityIgnoresContaminationWhenReliableEvidenceRemains(t *testing.T) {
	job := Job{
		Description: "Job posting containing qualifications and unrelated boilerplate.",
		Requirements: []Requirement{
			{
				Text:       "5 years of backend engineering experience",
				Category:   RequirementExperience,
				Importance: RequirementRequired,
			},
			{
				Text:       "Strong SQL skills",
				Category:   RequirementSkill,
				Importance: RequirementRequired,
			},
			{
				Text:       "Bachelor's degree",
				Category:   RequirementEducation,
				Importance: RequirementRequired,
			},
			{
				Text:       "Contact recruiter@example.com to apply.",
				Category:   RequirementOther,
				Importance: RequirementRequired,
			},
		},
	}

	quality := EvaluateExtractionQuality(job)

	if !quality.Scorable {
		t.Fatalf(
			"reliable qualification evidence should remain scorable despite isolated contamination: %#v",
			quality,
		)
	}

	if quality.RequirementCount != 3 {
		t.Fatalf(
			"expected three usable requirements, got %d",
			quality.RequirementCount,
		)
	}

	if quality.RequiredRequirementCount != 3 {
		t.Fatalf(
			"expected three usable required qualifications, got %d",
			quality.RequiredRequirementCount,
		)
	}
}
