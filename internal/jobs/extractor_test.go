package jobs

import (
	"strings"
	"testing"
)

func TestExtractsStandardStructuredRequirements(t *testing.T) {
	description := `
Minimum Qualifications:
- 2 years sales experience
- High school diploma or GED
- Excellent verbal and written communication skills
- Customer service
- Negotiation skills

Preferred Qualifications:
- 5 years outside sales experience
- Bachelor's degree

Travel Requirements:
Typically requires overnight travel 20% to 50% of the time.

Physical Requirements:
Must be able to lift up to 20 pounds.
`

	requirements := ExtractRequirements(description)

	assertRequirement(
		t,
		requirements,
		"2 years sales experience",
		RequirementExperience,
		RequirementRequired,
	)

	assertRequirement(
		t,
		requirements,
		"High school diploma or GED",
		RequirementEducation,
		RequirementRequired,
	)

	assertRequirement(
		t,
		requirements,
		"Excellent verbal and written communication skills",
		RequirementSkill,
		RequirementRequired,
	)

	assertRequirement(
		t,
		requirements,
		"Customer service",
		RequirementSkill,
		RequirementRequired,
	)

	assertRequirement(
		t,
		requirements,
		"Negotiation skills",
		RequirementSkill,
		RequirementRequired,
	)

	assertRequirement(
		t,
		requirements,
		"5 years outside sales experience",
		RequirementExperience,
		RequirementPreferred,
	)

	assertRequirement(
		t,
		requirements,
		"Bachelor's degree",
		RequirementEducation,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"overnight travel",
		RequirementTravel,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"lift",
		RequirementPhysical,
		RequirementRequired,
	)
}

func TestExtractsHomeDepotStyleInlineRequirements(t *testing.T) {
	description := `
Minimum Qualifications: Must be 18 years of age or older Must be legally permitted to work in the United States
Preferred Qualifications: Working knowledge of Microsoft Office Suite 5 years of professional work experience 2 years account management/sales management experience Strong leadership and negotiation skills; ability to persuade or influence others Excellent communication skills (verbal, written)
Minimum Education: The knowledge, skills and abilities typically acquired through the completion of a high school diploma and/or GED.
Preferred Education: No additional education
Travel Requirements: Typically requires overnight travel 20% to 50% of the time.
Certifications: None
`

	requirements := ExtractRequirements(description)

	assertRequirementContaining(
		t,
		requirements,
		"Microsoft Office",
		RequirementSkill,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"5 years of professional work experience",
		RequirementExperience,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"2 years account management",
		RequirementExperience,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"negotiation",
		RequirementSkill,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"communication",
		RequirementSkill,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"high school diploma",
		RequirementEducation,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"overnight travel",
		RequirementTravel,
		RequirementRequired,
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"no additional education",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"certifications: none",
	)

	assertNoRequirementExact(
		t,
		requirements,
		"None",
	)
}

func TestExtractsIQVIAStyleRequirements(t *testing.T) {
	description := `
Minimum Qualifications:
Bachelor's degree required
0 - 2 years business experience
Ability to travel as required
Valid Driver's License

Preferred Qualifications:
2+ years of relevant experience
Life Sciences degree preferred
Experience selling products or services to healthcare professionals
`

	requirements := ExtractRequirements(description)

	assertRequirementContaining(
		t,
		requirements,
		"Bachelor",
		RequirementEducation,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"0 - 2 years",
		RequirementExperience,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"travel",
		RequirementTravel,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"Driver",
		RequirementLicense,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"2+ years",
		RequirementExperience,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"Life Sciences degree",
		RequirementEducation,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"selling",
		RequirementExperience,
		RequirementPreferred,
	)
}

func TestExtractsCareerPlugQualificationsWithoutCompanyProse(t *testing.T) {
	description := `
About the Company:
We are a growing organization committed to serving our customers and communities.

Qualifications:
Excellent communication, negotiation, and relationship-building skills
Ability to work independently
Bachelor's degree in Business, Marketing, or related field is a plus

Why Work Here:
Join our team and enjoy a supportive culture with opportunities for growth.
`

	requirements := ExtractRequirements(description)

	assertRequirementContaining(
		t,
		requirements,
		"communication",
		RequirementSkill,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"work independently",
		RequirementSkill,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"Bachelor",
		RequirementEducation,
		RequirementPreferred,
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"growing organization",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"supportive culture",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"Why Work Here",
	)
}

func TestDoesNotExtractResponsibilitiesAsQualifications(t *testing.T) {
	description := `
Responsibilities:
Develop new customer relationships
Meet monthly sales targets
Maintain accurate CRM records
Travel throughout assigned territory

Qualifications:
3 years of sales experience
Excellent communication skills
`

	requirements := ExtractRequirements(description)

	assertNoRequirementContaining(
		t,
		requirements,
		"Develop new customer relationships",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"monthly sales targets",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"CRM records",
	)

	assertRequirementContaining(
		t,
		requirements,
		"3 years of sales experience",
		RequirementExperience,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"communication skills",
		RequirementSkill,
		RequirementRequired,
	)
}

func TestDoesNotCreateRequirementsFromExplicitNoneValues(t *testing.T) {
	description := `
Certifications: None
Preferred Education: No additional education
Minimum Leadership Experience: No previous leadership experience
Preferred Leadership Experience: No previous leadership experience
`

	requirements := ExtractRequirements(description)

	if len(requirements) != 0 {
		t.Fatalf(
			"expected no requirements, got %#v",
			requirements,
		)
	}
}

func TestPreservesOriginalDescription(t *testing.T) {
	description := `
Qualifications:
2 years sales experience
Excellent communication skills
`

	job := Job{
		ID:          "job-1",
		Title:       "Sales Representative",
		Company:     "Example Company",
		Description: description,
		ApplyURL:    "https://example.com/apply",
		IsActive:    true,
	}

	enriched := EnrichJob(job)

	if enriched.Description != description {
		t.Fatalf(
			"description changed during enrichment\nwant: %q\ngot:  %q",
			description,
			enriched.Description,
		)
	}
}

func TestRequirementsAreAtomic(t *testing.T) {
	description := `
Qualifications:
2 years sales experience
Bachelor's degree
Excellent communication skills
`

	requirements := ExtractRequirements(description)

	if len(requirements) != 3 {
		t.Fatalf(
			"expected exactly 3 atomic requirements, got %d: %#v",
			len(requirements),
			requirements,
		)
	}

	assertRequirement(
		t,
		requirements,
		"2 years sales experience",
		RequirementExperience,
		RequirementRequired,
	)

	assertRequirement(
		t,
		requirements,
		"Bachelor's degree",
		RequirementEducation,
		RequirementRequired,
	)

	assertRequirement(
		t,
		requirements,
		"Excellent communication skills",
		RequirementSkill,
		RequirementRequired,
	)
}

func TestLegacyFieldsAreDerivedFromRequirements(t *testing.T) {
	description := `
Minimum Qualifications:
2 years sales experience
Excellent communication skills
High school diploma or GED
Valid Driver's License

Preferred Qualifications:
Bachelor's degree
5 years outside sales experience

Travel Requirements:
Travel up to 25% of the time.

Physical Requirements:
Must be able to lift 20 pounds.

Certifications:
PMP certification
`

	job := EnrichJob(
		Job{
			ID:          "job-legacy",
			Title:       "Account Manager",
			Company:     "Example Company",
			Description: description,
			ApplyURL:    "https://example.com/apply",
			IsActive:    true,
		},
	)

	assertStringSliceContains(
		t,
		job.ExperienceRequirements,
		"2 years sales experience",
	)

	assertStringSliceContains(
		t,
		job.Skills,
		"Excellent communication skills",
	)

	assertStringSliceContains(
		t,
		job.EducationRequirements,
		"High school diploma",
	)

	assertStringSliceContains(
		t,
		job.LicenseRequirements,
		"Driver",
	)

	assertStringSliceContains(
		t,
		job.EducationRequirements,
		"Bachelor",
	)

	assertStringSliceContains(
		t,
		job.ExperienceRequirements,
		"5 years outside sales experience",
	)

	assertStringSliceContains(
		t,
		job.TravelRequirements,
		"Travel up to 25%",
	)

	assertStringSliceContains(
		t,
		job.PhysicalRequirements,
		"lift 20 pounds",
	)

	assertStringSliceContains(
		t,
		job.CertificationRequirements,
		"PMP certification",
	)

	if len(job.MinimumQualifications) == 0 {
		t.Fatal(
			"expected minimumQualifications to be derived from canonical requirements",
		)
	}

	if len(job.PreferredQualifications) == 0 {
		t.Fatal(
			"expected preferredQualifications to be derived from canonical requirements",
		)
	}
}

// Regression test based on the structure of the live Home Depot posting.
// Age requirements must not become years-of-experience requirements.
func TestAgeRequirementIsNotWorkExperience(t *testing.T) {
	description := `
Minimum Qualifications: Must be 18 years of age or older Must be legally permitted to work in the United States
`

	requirements := ExtractRequirements(description)

	assertNoRequirementExact(
		t,
		requirements,
		"Must be",
	)

	for _, requirement := range requirements {
		if strings.Contains(
			strings.ToLower(requirement.Text),
			"18 years of age",
		) &&
			requirement.Category == RequirementExperience {
			t.Fatalf(
				"age requirement incorrectly classified as experience: %#v",
				requirement,
			)
		}
	}
}

// Numeric values from "Minimum Years of Work Experience: 2" must
// become meaningful experience requirements rather than raw "2".
func TestNumericYearsHeadingsProduceMeaningfulRequirements(t *testing.T) {
	description := `
Minimum Years of Work Experience: 2
Preferred Years of Work Experience: 5
`

	requirements := ExtractRequirements(description)

	assertNoRequirementExact(
		t,
		requirements,
		"2",
	)

	assertNoRequirementExact(
		t,
		requirements,
		"5",
	)

	assertRequirementContaining(
		t,
		requirements,
		"2 years",
		RequirementExperience,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"5 years",
		RequirementExperience,
		RequirementPreferred,
	)
}

// The extractor must not split one coherent skill phrase into fragments.
func TestDoesNotSplitLeadershipAndNegotiationPhrase(t *testing.T) {
	description := `
Preferred Qualifications: Strong leadership and negotiation skills; ability to persuade or influence others
`

	requirements := ExtractRequirements(description)

	assertRequirement(
		t,
		requirements,
		"Strong leadership and negotiation skills",
		RequirementSkill,
		RequirementPreferred,
	)

	assertRequirement(
		t,
		requirements,
		"ability to persuade or influence others",
		RequirementSkill,
		RequirementPreferred,
	)

	assertNoRequirementExact(
		t,
		requirements,
		"Strong leadership and",
	)
}

// Multiple flattened experience requirements must be separated instead
// of being merged with unrelated qualification prose.
func TestSeparatesFlattenedExperienceRequirements(t *testing.T) {
	description := `
Preferred Qualifications: 5 years of professional work experience 2 years account management/sales management experience 2 plus years home improvement or home building industry experience Successful professional growth in a high-paced retail environment
`

	requirements := ExtractRequirements(description)

	assertRequirementContaining(
		t,
		requirements,
		"5 years of professional work experience",
		RequirementExperience,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"2 years account management",
		RequirementExperience,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"2 plus years home improvement",
		RequirementExperience,
		RequirementPreferred,
	)

	for _, requirement := range requirements {
		lower := strings.ToLower(requirement.Text)

		if strings.Contains(
			lower,
			"account management",
		) &&
			strings.Contains(
				lower,
				"successful professional growth",
			) {
			t.Fatalf(
				"experience requirement contains unrelated merged prose: %#v",
				requirement,
			)
		}
	}
}

// A descriptive prefix attached to a real education requirement should
// not become a separate skill requirement.
func TestEducationPrefixDoesNotBecomeSeparateSkill(t *testing.T) {
	description := `
Minimum Education: The knowledge, skills and abilities typically acquired through the completion of a high school diploma and/or GED.
`

	requirements := ExtractRequirements(description)

	assertRequirementContaining(
		t,
		requirements,
		"high school diploma",
		RequirementEducation,
		RequirementRequired,
	)

	assertNoRequirementExact(
		t,
		requirements,
		"The knowledge, skills and abilities typically acquired through the completion of a",
	)
}

// Once extraction reaches company/marketing prose, that prose must not
// leak into the qualification set.
func TestCompanyMarketingProseDoesNotLeakIntoRequirements(t *testing.T) {
	description := `
Minimum Education: High school diploma or GED
Preferred Education: No additional education
Minimum Years of Work Experience: 2
Preferred Years of Work Experience: 5
Minimum Leadership Experience: No previous leadership experience
Preferred Leadership Experience: No previous leadership experience
Certifications: None
Competencies: Action Oriented Being Resilient Persuades Builds Networks Communicates Effectively Customer Focus Drives Results
As the world's largest home improvement specialty retailer, we operate over 2,200 retail stores across North America.
All of our associates have one thing in mind — helping our customers build and improve their homes and businesses.
`

	requirements := ExtractRequirements(description)

	assertNoRequirementContaining(
		t,
		requirements,
		"world's largest",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"world’s largest",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"All of our associates",
	)

	for _, requirement := range requirements {
		if strings.Contains(
			strings.ToLower(requirement.Text),
			"helping our customers build",
		) {
			t.Fatalf(
				"company marketing prose leaked into requirements: %#v",
				requirement,
			)
		}
	}
}

// This fixture combines the important extraction patterns from the live
// Home Depot response into one production-style regression test.
func TestHomeDepotProductionStyleExtraction(t *testing.T) {
	description := `
Travel Requirements: Typically requires overnight travel 20% to 50% of the time.
Physical Requirements: Most of the time is spent sitting in the same position or standing/walking or there is some requirement to lift or handle material or equipment of moderate weight (8-20 pounds).
Working Conditions: Typically in a comfortable environment.
Minimum Qualifications: Must be 18 years of age or older Must be legally permitted to work in the United States
Preferred Qualifications: Working knowledge of Microsoft Office Suite 5 years of professional work experience 2 years account management/sales management experience 2 plus years home improvement or home building industry experience Strong leadership and negotiation skills; ability to persuade or influence others Excellent communication skills (verbal, written)
Minimum Education: The knowledge, skills and abilities typically acquired through the completion of a high school diploma and/or GED.
Preferred Education: No additional education
Minimum Years of Work Experience: 2
Preferred Years of Work Experience: 5
Minimum Leadership Experience: No previous leadership experience
Preferred Leadership Experience: No previous leadership experience
Certifications: None
Competencies: Action Oriented Being Resilient Persuades Builds Networks Communicates Effectively Customer Focus Drives Results
As the world's largest home improvement specialty retailer, we operate over 2,200 retail stores across North America.
All of our associates have one thing in mind — helping our customers build and improve their homes and businesses.
`

	requirements := ExtractRequirements(description)

	assertRequirementContaining(
		t,
		requirements,
		"overnight travel",
		RequirementTravel,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"lift",
		RequirementPhysical,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"Microsoft Office",
		RequirementSkill,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"5 years of professional work experience",
		RequirementExperience,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"2 years account management",
		RequirementExperience,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"2 plus years home improvement",
		RequirementExperience,
		RequirementPreferred,
	)

	assertRequirement(
		t,
		requirements,
		"Strong leadership and negotiation skills",
		RequirementSkill,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"communication skills",
		RequirementSkill,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"high school diploma",
		RequirementEducation,
		RequirementRequired,
	)

	assertNoRequirementExact(
		t,
		requirements,
		"Must be",
	)

	assertNoRequirementExact(
		t,
		requirements,
		"2",
	)

	assertNoRequirementExact(
		t,
		requirements,
		"5",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"no additional education",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"no previous leadership experience",
	)

	assertNoRequirementExact(
		t,
		requirements,
		"None",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"All of our associates",
	)

	for _, requirement := range requirements {
		if strings.Contains(
			strings.ToLower(requirement.Text),
			"18 years of age",
		) &&
			requirement.Category == RequirementExperience {
			t.Fatalf(
				"age requirement incorrectly classified as experience: %#v",
				requirement,
			)
		}
	}
}

func assertRequirement(
	t *testing.T,
	requirements []Requirement,
	text string,
	category RequirementCategory,
	importance RequirementImportance,
) {
	t.Helper()

	for _, requirement := range requirements {
		if strings.EqualFold(
			strings.TrimSpace(requirement.Text),
			strings.TrimSpace(text),
		) &&
			requirement.Category == category &&
			requirement.Importance == importance {
			return
		}
	}

	t.Fatalf(
		"expected requirement %q category=%q importance=%q, got %#v",
		text,
		category,
		importance,
		requirements,
	)
}

func assertRequirementContaining(
	t *testing.T,
	requirements []Requirement,
	text string,
	category RequirementCategory,
	importance RequirementImportance,
) {
	t.Helper()

	needle := strings.ToLower(
		strings.TrimSpace(text),
	)

	for _, requirement := range requirements {
		if strings.Contains(
			strings.ToLower(requirement.Text),
			needle,
		) &&
			requirement.Category == category &&
			requirement.Importance == importance {
			return
		}
	}

	t.Fatalf(
		"expected requirement containing %q category=%q importance=%q, got %#v",
		text,
		category,
		importance,
		requirements,
	)
}

func assertNoRequirementContaining(
	t *testing.T,
	requirements []Requirement,
	text string,
) {
	t.Helper()

	needle := strings.ToLower(
		strings.TrimSpace(text),
	)

	for _, requirement := range requirements {
		if strings.Contains(
			strings.ToLower(requirement.Text),
			needle,
		) {
			t.Fatalf(
				"did not expect requirement containing %q, got %#v",
				text,
				requirement,
			)
		}
	}
}

func assertNoRequirementExact(
	t *testing.T,
	requirements []Requirement,
	text string,
) {
	t.Helper()

	for _, requirement := range requirements {
		if strings.EqualFold(
			strings.TrimSpace(requirement.Text),
			strings.TrimSpace(text),
		) {
			t.Fatalf(
				"did not expect requirement %q, got %#v",
				text,
				requirement,
			)
		}
	}
}

func assertStringSliceContains(
	t *testing.T,
	values []string,
	text string,
) {
	t.Helper()

	needle := strings.ToLower(
		strings.TrimSpace(text),
	)

	for _, value := range values {
		if strings.Contains(
			strings.ToLower(value),
			needle,
		) {
			return
		}
	}

	t.Fatalf(
		"expected slice to contain %q, got %#v",
		text,
		values,
	)
}
func TestFlattenedIQVIAProductionDescriptionExtractsQualificationSections(t *testing.T) {
	description := `The Associate Sales Representative supports sales activities and customer relationships. Job Duties: Expand the sales of client products and convert competitive products. Conduct sales presentations. Required Qualifications: Bachelor’s degree 0 - 2 years business exp The ability to travel (50-75%) and/or relocate to an assigned geography as needed Valid Driver’s License issued the United States Preferred Qualifications: 2+ years of professional experience Established business planning and forecasting experience Bachelor’s Degree with emphasis in Life Sciences, Medicine, or Business preferred Experience selling in a new or changed sales channel Strong desire to learn and grow professionally Excellence in process management and organizational agility Documentation of successful sales performance The ability to work in an operating room LI-CES IQVIA is a leading global provider of clinical research services.`

	requirements := ExtractRequirements(description)

	assertRequirementContaining(
		t,
		requirements,
		"Bachelor",
		RequirementEducation,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"0 - 2 years business exp",
		RequirementExperience,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"travel",
		RequirementTravel,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"Driver",
		RequirementLicense,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"2+ years of professional experience",
		RequirementExperience,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"Life Sciences",
		RequirementEducation,
		RequirementPreferred,
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"leading global provider",
	)
}

func TestFlattenedDiocesanProductionDescriptionKeepsRequirementsAtomic(t *testing.T) {
	description := `Job description Diocesan is seeking an outside sales representative. Responsibilities • Generate and develop new customer leads to increase revenue. • Build and foster existing relationships. Qualifications • Coachable with a strong work ethic. • Availability to travel overnight on a regular basis. • Basic computer knowledge is a must. • Professional demeanor and ability to effectively communicate in person, over the phone, and through email. • Must be bilingual in English and Spanish. • Comfortable working in the Catholic Church environment. • Valid Driver’s License Benefits • Medical, dental, optical, and prescription coverage. About Diocesan We are the leading provider of Catholic communication solutions.`

	requirements := ExtractRequirements(description)

	assertRequirement(
		t,
		requirements,
		"Availability to travel overnight on a regular basis",
		RequirementTravel,
		RequirementRequired,
	)

	assertRequirement(
		t,
		requirements,
		"Professional demeanor and ability to effectively communicate in person, over the phone, and through email",
		RequirementSkill,
		RequirementRequired,
	)

	assertRequirement(
		t,
		requirements,
		"Valid Driver’s License",
		RequirementLicense,
		RequirementRequired,
	)

	assertNoRequirementExact(
		t,
		requirements,
		"Avail",
	)

	assertNoRequirementExact(
		t,
		requirements,
		"Professional demeanor and",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"Medical, dental",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"leading provider of Catholic",
	)
}

func TestFlattenedHomeDepotProductionDescriptionFindsEmbeddedSections(t *testing.T) {
	description := `With a career at The Home Depot, you can be yourself and also be part of something bigger. Position Purpose: The Outside Sales Representative is responsible for driving incremental sales growth. Key Responsibilities: Cultivate new sales relationships and develop strategies. Direct Manager/Direct Reports: This Position typically reports to the Pro Sales Manager. Travel Requirements: Typically requires overnight travel 20% to 50% of the time. Physical Requirements: Most of the time is spent sitting in the same position or standing/walking or there is some requirement to lift or handle material or equipment of moderate weight (8-20 pounds). Working Conditions: Typically in a comfortable environment. Minimum Qualifications: Must be 18 years of age or older Must be legally permitted to work in the United States Preferred Qualifications: Working knowledge of Microsoft Office Suite 5 years of professional work experience 2 years account management/sales management experience 2 plus years home improvement or home building industry experience Strong leadership and negotiation skills; ability to persuade or influence others Excellent communication skills (verbal, written) and able to communicate globally Minimum Education: The knowledge, skills and abilities typically acquired through the completion of a high school diploma and/or GED. Preferred Education: No additional education Minimum Years of Work Experience: 2 Preferred Years of Work Experience: 5 Minimum Leadership Experience: No previous leadership experience Preferred Leadership Experience: No previous leadership experience Certifications: None Competencies: Action Oriented Being Resilient Persuades Builds Networks Communicates Effectively Customer Focus Drives Results As the world’s largest home improvement specialty retailer, we operate over 2,200 retail stores across North America. All of our associates have one thing in mind — helping our customers build and improve their homes and businesses.`

	requirements := ExtractRequirements(description)

	if len(requirements) == 0 {
		t.Fatal("expected embedded qualification sections to produce requirements")
	}

	assertRequirementContaining(
		t,
		requirements,
		"overnight travel",
		RequirementTravel,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"lift",
		RequirementPhysical,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"Microsoft Office",
		RequirementSkill,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"5 years of professional work experience",
		RequirementExperience,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"2 years account management",
		RequirementExperience,
		RequirementPreferred,
	)

	assertRequirement(
		t,
		requirements,
		"Strong leadership and negotiation skills",
		RequirementSkill,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"high school diploma",
		RequirementEducation,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"2 years of work experience",
		RequirementExperience,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"5 years of work experience",
		RequirementExperience,
		RequirementPreferred,
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"All of our associates",
	)

	assertNoRequirementExact(
		t,
		requirements,
		"2",
	)

	assertNoRequirementExact(
		t,
		requirements,
		"5",
	)

	for _, requirement := range requirements {
		if strings.Contains(
			strings.ToLower(requirement.Text),
			"18 years of age",
		) &&
			requirement.Category == RequirementExperience {
			t.Fatalf(
				"age requirement incorrectly classified as experience: %#v",
				requirement,
			)
		}
	}
}
func TestCareerPlugQualificationsStopBeforeCompensation(t *testing.T) {
	description := `Benefits: Flexible schedule Opportunity for advancement Competitive salary Free uniforms We’re Hiring! Sales Representative for Window Cleaning Services Are you a motivated, people-oriented individual with a passion for sales and quality service? Responsibilities • Conduct door-to-door sales. • Engage with potential customers, explain service benefits, and close deals. Requirements • Strong communication and interpersonal skills. • Self-motivated with a goal-oriented mindset. • Ability to work independently and manage time effectively. • Reliable transportation preferred (not required). • Valid Driver’s License required. • Willingness to work in the field and visit various businesses. Compensation Competitive pay structure with two available plans: • Base salary $600/week with 10% commission • Base salary $400/week with 15% commission for candidates who prefer a higher commission model. Note: The full base salary applies when weekly activity expectations are achieved. If you're a driven salesperson who enjoys meeting new people and closing deals, we want to hear from you!`

	requirements := ExtractRequirements(description)

	assertRequirement(
		t,
		requirements,
		"Strong communication and interpersonal skills",
		RequirementSkill,
		RequirementRequired,
	)

	assertRequirement(
		t,
		requirements,
		"Valid Driver’s License required",
		RequirementLicense,
		RequirementRequired,
	)

	assertRequirement(
		t,
		requirements,
		"Willingness to work in the field and visit various businesses",
		RequirementOther,
		RequirementRequired,
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"Compensation",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"Base salary",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"commission",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"weekly activity expectations",
	)
}

func TestCareerPlugProductionQualificationsAndEducationExtractCorrectly(t *testing.T) {
	description := `The Business Development Representative will be responsible for developing new business relationships. Responsibilities Communicate with prospective clients and/or their executive assistants via phone, email and/or social media Identify and target candidates through ongoing industry research Generate leads and research candidates Promote and track all client development campaigns Utilize CRM to track and manage sales pipeline Support the development of company collateral Qualifications Excellent interpersonal and organizational communication skills Self-starter who can seek out needs and information without directives Superior writing and proofreading skills. Must possess the ability to write and edit text to appropriately and proactively anticipate the needs of the audience Ability to work independently as well as part of a team Ability to multi-task and perform in several capacities Knowledge of CRM and other tracking software, including Salesforce, Luxor, Salesloft, LinkedIn, Zoominfo, and Microsoft Excel preferred Outstanding project management, scheduling, and research skills Education/Experience Bachelors degree in Business, Communications or Marketing Inside sales, lead generation and/or recruiting experience preferred Work Environment and Physical Demands While performing the responsibilities of this position, reasonable accommodations may be made to enable people with disabilities to perform the essential functions of this role. While performing the duties of this position, the employee is occasionally required to travel. Conclusion This position description is intended to convey information essential to understanding the scope of the position and it is not intended to be an exhaustive list of skills, efforts, duties, responsibilities or working conditions associated with this position. Equal Opportunity Statement The employer is an Equal Employment Opportunity Employer.`

	requirements := ExtractRequirements(description)

	if len(requirements) == 0 {
		t.Fatal(
			"expected CareerPlug production description to produce structured requirements",
		)
	}

	assertRequirementContaining(
		t,
		requirements,
		"interpersonal",
		RequirementSkill,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"Salesforce",
		RequirementSkill,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"Bachelors degree",
		RequirementEducation,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"Inside sales",
		RequirementExperience,
		RequirementPreferred,
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"reasonable accommodations",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"Equal Employment Opportunity",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"scope of the position",
	)
}
func TestGenericAtomizerSplitsFlattenedQualifications(t *testing.T) {
	job := Job{
		Description: `
Qualifications:
Strong communication skills Ability to work independently Bachelor's degree 3 years of relevant experience
`,
	}

	got := EnrichJob(job)

	expected := []string{
		"Strong communication skills",
		"Ability to work independently",
		"Bachelor's degree",
		"3 years of relevant experience",
	}

	assertRequirementTextsContain(t, got.Requirements, expected)
}

func TestGenericAtomizerPreservesNaturalCompoundRequirement(t *testing.T) {
	job := Job{
		Description: `
Qualifications:
Strong communication and interpersonal skills.
`,
	}

	got := EnrichJob(job)

	if len(got.Requirements) != 1 {
		t.Fatalf(
			"expected one compound requirement, got %d: %#v",
			len(got.Requirements),
			got.Requirements,
		)
	}

	if got.Requirements[0].Text !=
		"Strong communication and interpersonal skills" {
		t.Fatalf(
			"unexpected requirement: %q",
			got.Requirements[0].Text,
		)
	}
}

func TestGenericAtomizerSplitsRepeatedExperienceRequirements(t *testing.T) {
	job := Job{
		Description: `
Minimum Qualifications:
2 years of customer service experience 5 years of management experience
`,
	}

	got := EnrichJob(job)

	expected := []string{
		"2 years of customer service experience",
		"5 years of management experience",
	}

	assertRequirementTextsContain(t, got.Requirements, expected)
}

func TestGenericAtomizerStopsAtRejectedCompensationSection(t *testing.T) {
	job := Job{
		Description: `
Required Qualifications:
Bachelor's degree
Excellent communication skills

Compensation:
The actual base pay offered may vary based on location, experience, and schedule.
Incentive plans, bonuses, and other forms of compensation may be offered.
`,
	}

	got := EnrichJob(job)

	for _, requirement := range got.Requirements {
		lower := strings.ToLower(requirement.Text)

		if strings.Contains(lower, "base pay") ||
			strings.Contains(lower, "incentive") ||
			strings.Contains(lower, "bonuses") ||
			strings.Contains(lower, "compensation") {
			t.Fatalf(
				"compensation leaked into requirements: %#v",
				got.Requirements,
			)
		}
	}

	expected := []string{
		"Bachelor's degree",
		"Excellent communication skills",
	}

	assertRequirementTextsContain(t, got.Requirements, expected)
}

func TestGenericAtomizerRejectsIncompleteConnectorFragment(t *testing.T) {
	job := Job{
		Description: `
Qualifications:
Valid driver's license and
`,
	}

	got := EnrichJob(job)

	for _, requirement := range got.Requirements {
		if strings.HasSuffix(
			strings.ToLower(requirement.Text),
			" and",
		) {
			t.Fatalf(
				"incomplete connector fragment survived: %#v",
				got.Requirements,
			)
		}
	}
}

func TestGenericAtomizerWorksAcrossQualificationCategories(t *testing.T) {
	job := Job{
		Description: `
Requirements:
Valid driver's license CPA certification Ability to lift 50 pounds Willingness to travel Bachelor's degree 4 years of relevant experience Strong communication skills
`,
	}

	got := EnrichJob(job)

	if len(got.Requirements) < 6 {
		t.Fatalf(
			"expected multiple qualification categories, got %d: %#v",
			len(got.Requirements),
			got.Requirements,
		)
	}

	categories := map[RequirementCategory]bool{}

	for _, requirement := range got.Requirements {
		categories[requirement.Category] = true
	}

	expectedCategories := []RequirementCategory{
		RequirementLicense,
		RequirementCertification,
		RequirementPhysical,
		RequirementTravel,
		RequirementEducation,
		RequirementExperience,
		RequirementSkill,
	}

	for _, category := range expectedCategories {
		if !categories[category] {
			t.Errorf(
				"expected category %q; requirements: %#v",
				category,
				got.Requirements,
			)
		}
	}
}

func assertRequirementTextsContain(
	t *testing.T,
	requirements []Requirement,
	expected []string,
) {
	t.Helper()

	actual := make(map[string]bool)

	for _, requirement := range requirements {
		actual[requirement.Text] = true
	}

	for _, value := range expected {
		if !actual[value] {
			t.Errorf(
				"missing requirement %q; got %#v",
				value,
				requirements,
			)
		}
	}
}
func TestQualificationExtractionStopsAtResponsibilitiesSection(t *testing.T) {
	description := `
Qualifications
3 years of backend engineering experience.
Strong communication skills.
Bachelor's degree.

Responsibilities
Implement solutions for customer requirements.
Provide system support.
Coordinate project activities.
Maintain documentation.
`

	requirements := ExtractRequirements(description)

	assertRequirementContaining(
		t,
		requirements,
		"3 years",
		RequirementExperience,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"communication",
		RequirementSkill,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"Bachelor",
		RequirementEducation,
		RequirementRequired,
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"Implement solutions",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"Provide system support",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"Coordinate project activities",
	)
}

func TestQualificationExtractionStopsAtApplicationContactSection(t *testing.T) {
	description := `
Minimum Qualifications
5 years of account management experience.
Bachelor's degree in Business.

How to Apply
Send your resume to the recruiter.
Contact recruiting@example.com for additional information.
`

	requirements := ExtractRequirements(description)

	assertRequirementContaining(
		t,
		requirements,
		"5 years",
		RequirementExperience,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"Bachelor",
		RequirementEducation,
		RequirementRequired,
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"Send your resume",
	)

	assertNoRequirementContaining(
		t,
		requirements,
		"recruiting@example.com",
	)
}

func TestNaturalLanguageQualificationHeading(t *testing.T) {
	description := `We are looking for a person who has
Track record of owning services in production.
Experience defining and operating against SLOs/SLIs.
Comfort operating in modern cloud environments (e.g., AWS/GCP), containerized workloads, and CI/CD pipelines.
Life at Example
We offer competitive benefits.`
	r := ExtractRequirements(description)
	assertRequirementContaining(t, r, "owning services", RequirementSkill, RequirementRequired)
	assertRequirementContaining(t, r, "SLOs/SLIs", RequirementExperience, RequirementRequired)
	assertRequirementContaining(t, r, "AWS/GCP", RequirementSkill, RequirementRequired)
	assertNoRequirementContaining(t, r, "competitive benefits")
}

func TestYourToolboxHeading(t *testing.T) {
	description := `Your Toolbox:
Bachelor's degree in Computer Science or equivalent practical experience.
8+ years of software engineering experience.
Strong knowledge of distributed systems.`
	r := ExtractRequirements(description)
	assertRequirementContaining(t, r, "Bachelor", RequirementEducation, RequirementRequired)
	assertRequirementContaining(t, r, "8+ years", RequirementExperience, RequirementRequired)
	assertRequirementContaining(t, r, "distributed systems", RequirementSkill, RequirementRequired)
}

func TestAroundYearsStaysAtomic(t *testing.T) {
	r := ExtractRequirements(`Qualifications: Around 5 years of experience building backend systems.`)
	assertRequirementContaining(t, r, "Around 5 years", RequirementExperience, RequirementRequired)
	assertNoRequirementExact(t, r, "Around")
}

func TestSemicolonSubclausesDoNotBecomeConnectorFragments(t *testing.T) {
	r := ExtractRequirements(`Qualifications: Strong technical background in enterprise platforms; integrations with external systems; security setup; and report writing.`)
	assertNoRequirementExact(t, r, "and report writing")
	for _, req := range r {
		if strings.HasPrefix(strings.ToLower(req.Text), "and ") {
			t.Fatalf("connector-start fragment survived: %#v", r)
		}
	}
}

func TestGenericLifeAtTerminatesQualifications(t *testing.T) {
	r := ExtractRequirements(`Qualifications: Strong communication skills. 3 years of relevant experience. Life at Example We provide medical, dental, and retirement benefits.`)
	assertRequirementContaining(t, r, "communication", RequirementSkill, RequirementRequired)
	assertRequirementContaining(t, r, "3 years", RequirementExperience, RequirementRequired)
	assertNoRequirementContaining(t, r, "medical, dental")
}

func TestAdditionalSkillsHeadingKeepsCoherentBullets(t *testing.T) {
	r := ExtractRequirements("Additional Skills:\nTrains less senior associates on defined procedures and standards.\nStrong written communication skills.")
	assertRequirementContaining(t, r, "Trains less senior associates", RequirementSkill, RequirementRequired)
	assertNoRequirementExact(t, r, "Trains less senior")
	assertNoRequirementExact(t, r, "associates on defined procedures and standards")
}

func TestFlattenedRequiredPreferredAndLifeAtBoundaries(t *testing.T) {
	r := ExtractRequirements(`Requirements 3 years of backend engineering experience Strong communication skills Preferred Qualifications Experience with Kubernetes preferred Life at Example Great benefits and culture.`)
	assertRequirementContaining(t, r, "3 years", RequirementExperience, RequirementRequired)
	assertRequirementContaining(t, r, "Kubernetes", RequirementExperience, RequirementPreferred)
	assertNoRequirementContaining(t, r, "Great benefits")
}

func TestMixedEducationAndExperienceWithUnicodeRange(t *testing.T) {
	r := ExtractRequirements(`Qualifications: Bachelor's degree in Computer Science or equivalent practical experience. 2–5 years of backend engineering experience.`)
	assertRequirementContaining(t, r, "Bachelor", RequirementEducation, RequirementRequired)
	assertRequirementContaining(t, r, "2–5 years", RequirementExperience, RequirementRequired)
}
func TestValidateRequirementsRejectsStructurallyContaminatedAtoms(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{
			name: "multiple flattened sentences",
			text: "Experience supporting commercial construction projects. Excellent written and verbal communication skills. Ability to manage multiple priorities in a fast-paced environment.",
		},
		{
			name: "embedded qualification section",
			text: "Experience with project coordination and scheduling Preferred Qualifications: Experience with construction management software and field operations",
		},
		{
			name: "large flattened requirement sequence",
			text: "Experience with project management; Strong communication skills; Ability to manage multiple priorities; Proficiency with construction management software and scheduling tools",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requirement := Requirement{
				Text:       tt.text,
				Category:   RequirementExperience,
				Importance: RequirementRequired,
			}

			got := ValidateRequirements(
				[]Requirement{requirement},
				tt.text,
			)

			if len(got) != 0 {
				t.Fatalf(
					"expected contaminated requirement to be rejected, got %#v",
					got,
				)
			}
		})
	}
}

func TestValidateRequirementsPreservesNormalAtomicRequirements(t *testing.T) {
	tests := []Requirement{
		{
			Text:       "Excellent written and verbal communication skills",
			Category:   RequirementSkill,
			Importance: RequirementRequired,
		},
		{
			Text:       "1-4 years of experience supporting commercial construction projects or completion of a relevant internship or co-op program",
			Category:   RequirementExperience,
			Importance: RequirementRequired,
		},
		{
			Text:       "Experience with Procore, Bluebeam, Excel, or similar construction management software is preferred",
			Category:   RequirementSkill,
			Importance: RequirementPreferred,
		},
		{
			Text:       "Ability to manage multiple priorities in a fast-paced construction environment",
			Category:   RequirementSkill,
			Importance: RequirementRequired,
		},
	}

	source := strings.Join([]string{
		tests[0].Text,
		tests[1].Text,
		tests[2].Text,
		tests[3].Text,
	}, "\n")

	got := ValidateRequirements(tests, source)

	if len(got) != len(tests) {
		t.Fatalf(
			"expected %d legitimate requirements to survive, got %d: %#v",
			len(tests),
			len(got),
			got,
		)
	}
}
func TestExtractRequirementsClassifiesYearsWithoutExperienceWord(t *testing.T) {
	description := `
Requirements

• 0-2 years in software engineering, with an interest in back-end and full-stack development across modern web platforms.
`

	requirements := ExtractRequirements(description)

	assertRequirementContaining(
		t,
		requirements,
		"0-2 years",
		RequirementExperience,
		RequirementRequired,
	)
}

func TestExtractRequirementsClassifiesTechnicalCapabilityAsSkill(t *testing.T) {
	description := `
Requirements

• .NET / API Foundations: Some exposure to building scalable services using .NET and an eagerness to learn API design with GraphQL and REST.
`

	requirements := ExtractRequirements(description)

	assertRequirementContaining(
		t,
		requirements,
		".NET / API Foundations",
		RequirementSkill,
		RequirementRequired,
	)
}

func TestExtractRequirementsIgnoresExperienceSectionIntro(t *testing.T) {
	description := `
Additional Skills

While this role is primarily back-end/full-stack focused, experience in these areas is a plus:

• CMS & Platform Engineering: Any experience with or interest in CMS platforms, with Optimizely a nice-to-have.
`

	requirements := ExtractRequirements(description)

	for _, requirement := range requirements {
		if strings.Contains(
			strings.ToLower(requirement.Text),
			"experience in these areas is a plus",
		) {
			t.Fatalf(
				"expected section-intro prose to be ignored, got requirement: %+v",
				requirement,
			)
		}
	}
}

func TestExtractsModernATSQualificationHeadings(t *testing.T) {
	description := `
What You'll Bring:
- 3+ years of software engineering experience
- Experience building backend services in Go

Nice to Haves:
- Experience with vector databases
- Experience with OpenSearch

Bonus Points:
- Experience with Neo4j
`

	requirements := ExtractRequirements(description)

	assertRequirementContaining(
		t,
		requirements,
		"software engineering",
		RequirementExperience,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"backend services",
		RequirementExperience,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"vector databases",
		RequirementExperience,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"OpenSearch",
		RequirementExperience,
		RequirementPreferred,
	)

	assertRequirementContaining(
		t,
		requirements,
		"Neo4j",
		RequirementExperience,
		RequirementPreferred,
	)
}

func TestExtractsGreatFitHeading(t *testing.T) {
	description := `
What Makes You a Great Fit:
- Strong experience developing distributed systems
- Proficiency with Go and cloud infrastructure
`

	requirements := ExtractRequirements(description)

	assertRequirementContaining(
		t,
		requirements,
		"distributed systems",
		RequirementExperience,
		RequirementRequired,
	)

	assertRequirementContaining(
		t,
		requirements,
		"Go",
		RequirementSkill,
		RequirementRequired,
	)
}

func TestUniversalExtractorRecognizesRealWorldQualificationHeadings(t *testing.T) {
	tests := []struct {
		name        string
		description string
		contains    []string
	}{
		{
			name: "recipe for success",
			description: `
Recipe for Success – apply now if this sounds like you!

• Bachelor's degree in a related field
• 2+ years of relevant experience
• Strong analytical and modeling skills
• Advanced Excel capability

Benefits:
Medical, dental and vision coverage.
`,
			contains: []string{
				"Bachelor",
				"2+ years",
				"analytical",
				"Excel",
			},
		},
		{
			name: "knowledge skills and abilities punctuation",
			description: `
Knowledge, Skills and Abilities:

• Bachelor's degree in related field
• Minimum of two years of related experience
• Strong analytical and communication skills
• Proficiency in Microsoft Excel

Pay Range:
Competitive salary.
`,
			contains: []string{
				"Bachelor",
				"two years",
				"analytical",
				"Excel",
			},
		},
		{
			name: "your background",
			description: `
Your Background

Bachelor's degree required
3-7 years of relevant professional experience
Strong analytical and problem-solving skills
Ability to analyze complex data sets using R and Python

What We Offer:
Comprehensive benefits package.
`,
			contains: []string{
				"Bachelor",
				"3-7 years",
				"analytical",
				"complex data",
			},
		},
		{
			name: "what youll need",
			description: `
What You'll Need:

High school diploma or equivalent
Three years of relevant experience
Strong written and verbal communication skills

Benefits:
Paid time off.
`,
			contains: []string{
				"high school",
				"Three years",
				"communication",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requirements := ExtractRequirements(test.description)

			if len(requirements) == 0 {
				t.Fatalf(
					"expected qualification requirements, got none",
				)
			}

			for _, expected := range test.contains {
				found := false

				for _, requirement := range requirements {
					if strings.Contains(
						strings.ToLower(requirement.Text),
						strings.ToLower(expected),
					) {
						found = true
						break
					}
				}

				if !found {
					t.Fatalf(
						"expected extracted requirement containing %q; got %#v",
						expected,
						requirements,
					)
				}
			}
		})
	}
}

func TestUniversalUnheadedRequirementFallback(t *testing.T) {
	description := `
We are building a growing team and looking for someone who can contribute immediately.

Bachelor's degree in a related field
3 years of relevant experience
Strong analytical and communication skills
Proficiency with industry-standard tools

You will work with cross-functional teams and support day-to-day business operations.
`

	requirements := ExtractRequirements(description)

	if len(requirements) == 0 {
		t.Fatal("expected unheaded qualification requirements, got none")
	}

	foundEducation := false
	foundExperience := false
	foundSkill := false

	for _, requirement := range requirements {
		switch requirement.Category {
		case RequirementEducation:
			foundEducation = true
		case RequirementExperience:
			foundExperience = true
		case RequirementSkill:
			foundSkill = true
		}
	}

	if !foundEducation {
		t.Errorf("expected education requirement, got %#v", requirements)
	}

	if !foundExperience {
		t.Errorf("expected experience requirement, got %#v", requirements)
	}

	if !foundSkill {
		t.Errorf("expected skill requirement, got %#v", requirements)
	}
}

func TestUniversalRequirementBoundariesStayClean(t *testing.T) {
	description := `
Requirements:
Bachelor's degree in a quantitative or analytical discipline
0-3 years of experience in an analytical role
Strong Excel skills and comfort working with large, imperfect datasets
Ability to read a P&L and reason about what drives the numbers
Clear written and verbal communication

Why This Role
This is an exciting opportunity to make an impact.

Interview Policy & Privacy Notice
Applicants may be asked to participate in interviews.

Our Commitment to Employees
We provide competitive benefits.
`

	requirements := ExtractRequirements(description)

	if len(requirements) == 0 {
		t.Fatal("expected qualification requirements")
	}

	var texts []string
	for _, r := range requirements {
		texts = append(texts, r.Text)
	}

	joined := strings.Join(texts, "\n")
	lower := strings.ToLower(joined)

	if !strings.Contains(joined, "Strong Excel skills") {
		t.Fatalf("expected Excel requirement to remain intact: %#v", texts)
	}

	for _, bad := range []string{
		"why this role",
		"interview policy",
		"privacy notice",
		"commitment to employees",
		"competitive benefits",
	} {
		if strings.Contains(lower, bad) {
			t.Fatalf("non-qualification content leaked into requirements: %q: %#v", bad, texts)
		}
	}
}
