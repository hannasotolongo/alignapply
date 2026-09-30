package candidate

import "testing"

func TestExtractProfileEmptyResume(t *testing.T) {
	profile := ExtractProfile("")

	if len(profile.AllEvidence) != 0 {
		t.Fatalf(
			"expected no evidence, got %#v",
			profile.AllEvidence,
		)
	}
}

func TestExtractProfilePreservesRawResume(t *testing.T) {
	resume := "Backend Engineer\nBuilt distributed services in Go."

	profile := ExtractProfile(resume)

	if profile.RawResumeText != resume {
		t.Fatalf(
			"expected raw resume to be preserved",
		)
	}
}

func TestExtractProfileFindsExperience(t *testing.T) {
	resume := `
Experience
Built distributed backend services in Go.
Implemented durable order processing.
`

	profile := ExtractProfile(resume)

	if len(profile.Experience) < 2 {
		t.Fatalf(
			"expected experience evidence, got %#v",
			profile.Experience,
		)
	}
}

func TestExtractProfileFindsEducation(t *testing.T) {
	resume := `
Education
Bachelor's Degree in Biology
University of Florida
`

	profile := ExtractProfile(resume)

	if !containsEvidence(
		profile.Education,
		"Bachelor",
	) {
		t.Fatalf(
			"expected bachelor's education evidence, got %#v",
			profile.Education,
		)
	}
}

func TestExtractProfileFindsCertification(t *testing.T) {
	resume := `
AWS Certified Solutions Architect
`

	profile := ExtractProfile(resume)

	if len(profile.Certifications) == 0 {
		t.Fatal("expected certification evidence")
	}
}

func TestExtractProfileFindsLicense(t *testing.T) {
	resume := `
Valid Registered Nurse License
`

	profile := ExtractProfile(resume)

	if len(profile.Licenses) == 0 {
		t.Fatal("expected license evidence")
	}
}

func TestExtractProfileFindsSkillList(t *testing.T) {
	resume := `
Skills
Go, Python, SQL, Kubernetes, Docker
`

	profile := ExtractProfile(resume)

	if len(profile.Skills) == 0 {
		t.Fatal("expected skill evidence")
	}
}

func TestExtractProfileSupportsNonTechnicalResume(t *testing.T) {
	resume := `
Experience
Managed customer relationships and coordinated client accounts.
Developed sales campaigns and maintained CRM records.

Education
Bachelor's Degree in Marketing

Skills
Communication, Salesforce, Microsoft Excel
`

	profile := ExtractProfile(resume)

	if len(profile.Experience) == 0 {
		t.Fatal("expected nontechnical experience evidence")
	}

	if len(profile.Education) == 0 {
		t.Fatal("expected nontechnical education evidence")
	}

	if len(profile.Skills) == 0 {
		t.Fatal("expected nontechnical skill evidence")
	}
}

func TestEvidenceIsTraceableToResume(t *testing.T) {
	resume := `
Built distributed backend services in Go.
`

	profile := ExtractProfile(resume)

	for _, evidence := range profile.AllEvidence {
		if evidence.Source == "" {
			t.Fatalf(
				"evidence must retain its source: %#v",
				evidence,
			)
		}
	}
}

func TestEvidenceDeduplicatesRepeatedLines(t *testing.T) {
	resume := `
Built distributed backend services in Go.
Built distributed backend services in Go.
`

	profile := ExtractProfile(resume)

	count := 0

	for _, evidence := range profile.Experience {
		if evidence.Text ==
			"Built distributed backend services in Go." {
			count++
		}
	}

	if count != 1 {
		t.Fatalf(
			"expected duplicate evidence once, got %d",
			count,
		)
	}
}

func containsEvidence(
	values []Evidence,
	substring string,
) bool {
	for _, evidence := range values {
		if containsFold(
			evidence.Text,
			substring,
		) {
			return true
		}
	}

	return false
}

func containsFold(
	text string,
	substring string,
) bool {
	textRunes := []rune(text)
	subRunes := []rune(substring)

	if len(subRunes) == 0 {
		return true
	}

	if len(subRunes) > len(textRunes) {
		return false
	}

	for i := 0; i <= len(textRunes)-len(subRunes); i++ {
		match := true

		for j := range subRunes {
			a := textRunes[i+j]
			b := subRunes[j]

			if 'A' <= a && a <= 'Z' {
				a += 'a' - 'A'
			}

			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}

			if a != b {
				match = false
				break
			}
		}

		if match {
			return true
		}
	}

	return false
}

func TestExtractProfilePreservesUniversalResumeSectionContext(t *testing.T) {
	tests := []struct {
		name     string
		resume   string
		category EvidenceCategory
		contains string
	}{
		{
			name: "finance experience",
			resume: `Professional Experience
Financial Analyst
Performed forecasting, budgeting, variance analysis, and financial modeling.`,
			category: EvidenceExperience,
			contains: "Performed forecasting",
		},
		{
			name: "healthcare experience",
			resume: `Work Experience
Registered Nurse
Provided patient care, medication administration, and patient education.`,
			category: EvidenceExperience,
			contains: "Provided patient care",
		},
		{
			name: "software experience",
			resume: `Employment History
Software Engineer
Built distributed backend services and production APIs.`,
			category: EvidenceExperience,
			contains: "Built distributed backend",
		},
		{
			name: "education context",
			resume: `Education
University of Florida
Bachelor of Science in Biology`,
			category: EvidenceEducation,
			contains: "University of Florida",
		},
		{
			name: "project context",
			resume: `Projects
Order Processing Platform
Built a reliable distributed order-processing system.`,
			category: EvidenceProject,
			contains: "Order Processing Platform",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := ExtractProfile(test.resume)

			var evidence []Evidence

			switch test.category {
			case EvidenceExperience:
				evidence = profile.Experience
			case EvidenceEducation:
				evidence = profile.Education
			case EvidenceProject:
				evidence = profile.Projects
			default:
				t.Fatalf("unsupported test category %q", test.category)
			}

			if !containsEvidence(evidence, test.contains) {
				t.Fatalf(
					"expected %q as %s evidence; got %#v",
					test.contains,
					test.category,
					evidence,
				)
			}
		})
	}
}
