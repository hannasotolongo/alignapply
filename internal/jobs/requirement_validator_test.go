package jobs

import "testing"

func TestValidateRequirementsPreservesValidRequirement(t *testing.T) {
	source := `
Qualifications:
Excellent communication and interpersonal skills.
`

	input := []Requirement{
		{
			Text:       "Excellent communication and interpersonal skills",
			Category:   RequirementSkill,
			Importance: RequirementRequired,
		},
	}

	got := ValidateRequirements(input, source)

	if len(got) != 1 {
		t.Fatalf(
			"expected 1 requirement, got %d: %#v",
			len(got),
			got,
		)
	}

	if got[0].Text !=
		"Excellent communication and interpersonal skills" {
		t.Fatalf(
			"unexpected requirement: %q",
			got[0].Text,
		)
	}
}

func TestValidateRequirementsPreservesShortRealQualification(t *testing.T) {
	source := `
Requirements:
CPA
`

	input := []Requirement{
		{
			Text:       "CPA",
			Category:   RequirementCertification,
			Importance: RequirementRequired,
		},
	}

	got := ValidateRequirements(input, source)

	if len(got) != 1 {
		t.Fatalf(
			"expected short real qualification to survive, got %#v",
			got,
		)
	}
}

func TestValidateRequirementsRejectsTrailingConnectorFragment(t *testing.T) {
	source := `
Qualifications:
Valid driver's license and reliable transportation required.
`

	input := []Requirement{
		{
			Text:       "Valid driver's license and",
			Category:   RequirementLicense,
			Importance: RequirementRequired,
		},
	}

	got := ValidateRequirements(input, source)

	if len(got) != 0 {
		t.Fatalf(
			"expected broken fragment to be rejected, got %#v",
			got,
		)
	}
}

func TestValidateRequirementsRejectsSectionLabel(t *testing.T) {
	source := `
Qualifications:
Excellent communication skills.
`

	input := []Requirement{
		{
			Text:       "Qualifications",
			Category:   RequirementOther,
			Importance: RequirementRequired,
		},
	}

	got := ValidateRequirements(input, source)

	if len(got) != 0 {
		t.Fatalf(
			"expected section label to be rejected, got %#v",
			got,
		)
	}
}

func TestValidateRequirementsRejectsCompensationContamination(t *testing.T) {
	source := `
Required Qualifications:
Bachelor's degree.

Compensation:
The actual base pay offered may vary based on job-related
qualifications such as knowledge, skills, education, and experience;
location; and/or schedule. Incentive plans, bonuses, and other forms
of compensation may be offered.
`

	input := []Requirement{
		{
			Text:       "Bachelor's degree",
			Category:   RequirementEducation,
			Importance: RequirementRequired,
		},
		{
			Text:       "The actual base pay offered may vary based on job-related qualifications such as knowledge, skills, education, and experience",
			Category:   RequirementExperience,
			Importance: RequirementRequired,
		},
		{
			Text:       "Incentive plans, bonuses, and other forms of compensation may be offered",
			Category:   RequirementOther,
			Importance: RequirementRequired,
		},
	}

	got := ValidateRequirements(input, source)

	if len(got) != 1 {
		t.Fatalf(
			"expected only the real qualification, got %d: %#v",
			len(got),
			got,
		)
	}

	if got[0].Text != "Bachelor's degree" {
		t.Fatalf(
			"unexpected surviving requirement: %q",
			got[0].Text,
		)
	}
}

func TestValidateRequirementsRequiresSourceTraceability(t *testing.T) {
	source := `
Qualifications:
Excellent communication skills.
`

	input := []Requirement{
		{
			Text:       "Five years of enterprise sales experience",
			Category:   RequirementExperience,
			Importance: RequirementRequired,
		},
	}

	got := ValidateRequirements(input, source)

	if len(got) != 0 {
		t.Fatalf(
			"expected invented requirement to be rejected, got %#v",
			got,
		)
	}
}

func TestValidateRequirementsIgnoresWhitespaceForTraceability(t *testing.T) {
	source := `
Qualifications:
Strong communication
and interpersonal skills.
`

	input := []Requirement{
		{
			Text:       "Strong communication and interpersonal skills",
			Category:   RequirementSkill,
			Importance: RequirementRequired,
		},
	}

	got := ValidateRequirements(input, source)

	if len(got) != 1 {
		t.Fatalf(
			"expected whitespace-normalized source match, got %#v",
			got,
		)
	}
}

func TestValidateRequirementsDeduplicatesCanonicalRequirements(t *testing.T) {
	source := `
Qualifications:
Strong communication skills.
`

	input := []Requirement{
		{
			Text:       "Strong communication skills",
			Category:   RequirementSkill,
			Importance: RequirementRequired,
		},
		{
			Text:       "Strong communication skills.",
			Category:   RequirementSkill,
			Importance: RequirementRequired,
		},
	}

	got := ValidateRequirements(input, source)

	if len(got) != 1 {
		t.Fatalf(
			"expected duplicate requirements to collapse, got %d: %#v",
			len(got),
			got,
		)
	}
}

func TestEnrichJobRebuildsLegacyFieldsFromValidatedRequirements(t *testing.T) {
	job := Job{
		Description: `
Qualifications:
Strong communication skills.
Valid driver's license and

Compensation:
The actual base pay offered may vary.
`,
	}

	got := EnrichJob(job)

	for _, requirement := range got.Requirements {
		if requirement.Text == "Valid driver's license and" {
			t.Fatalf(
				"broken requirement survived validation: %#v",
				got.Requirements,
			)
		}
	}

	for _, value := range got.MinimumQualifications {
		if value == "Valid driver's license and" {
			t.Fatalf(
				"legacy fields were rebuilt from unvalidated data: %#v",
				got.MinimumQualifications,
			)
		}
	}
}
