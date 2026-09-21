package matching

import (
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

func TestMatchUsesCategoryAwareCandidateEvidence(t *testing.T) {
	request := jobs.SearchRequest{
		ResumeText: `
Experience
Built backend services in Go.
Developed distributed systems and APIs.

Education
Bachelor's Degree in Biology

Skills
Go, Python, SQL, Kubernetes
`,
	}

	job := jobs.Job{
		ID:          "job-1",
		Title:       "Backend Engineer",
		Company:     "Example",
		Description: "Backend engineering position.",
		Requirements: []jobs.Requirement{
			{
				Text:       "Go skills",
				Category:   jobs.RequirementSkill,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Backend engineering experience",
				Category:   jobs.RequirementExperience,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Bachelor's degree",
				Category:   jobs.RequirementEducation,
				Importance: jobs.RequirementRequired,
			},
		},
	}

	result := Match(request, job)

	if result.MatchLevel == "Insufficient Evidence" {
		t.Fatalf(
			"expected scorable match, got %#v",
			result,
		)
	}

	if result.MatchPercentage <= 0 {
		t.Fatalf(
			"expected positive evidence match, got %d",
			result.MatchPercentage,
		)
	}
}

func TestMatchDoesNotUseUnrelatedYearsFromResume(t *testing.T) {
	request := jobs.SearchRequest{
		ResumeText: `
Experience
Built backend APIs in Go.

Education
Completed a 6 year academic program.
`,
	}

	job := jobs.Job{
		ID:          "job-2",
		Title:       "Backend Engineer",
		Company:     "Example",
		Description: "Backend role requiring significant experience.",
		Requirements: []jobs.Requirement{
			{
				Text:       "5 years of backend experience",
				Category:   jobs.RequirementExperience,
				Importance: jobs.RequirementRequired,
			},
		},
	}

	result := Match(request, job)

	if len(result.SupportedRequirements) != 0 {
		t.Fatalf(
			"unrelated education years must not satisfy experience requirement: %#v",
			result.SupportedRequirements,
		)
	}
}

func TestMatchUsesRelevantExperienceYears(t *testing.T) {
	request := jobs.SearchRequest{
		ResumeText: `
Experience
6 years of backend engineering experience building APIs and services.
`,
	}

	job := jobs.Job{
		ID:          "job-3",
		Title:       "Backend Engineer",
		Company:     "Example",
		Description: "Backend engineering role.",
		Requirements: []jobs.Requirement{
			{
				Text:       "5 years of backend engineering experience",
				Category:   jobs.RequirementExperience,
				Importance: jobs.RequirementRequired,
			},
		},
	}

	result := Match(request, job)

	if len(result.SupportedRequirements) != 1 {
		t.Fatalf(
			"expected experience requirement to be supported, got %#v",
			result,
		)
	}
}

func TestMatchReturnsInsufficientEvidenceForPoorJobExtraction(t *testing.T) {
	request := jobs.SearchRequest{
		ResumeText: `
Built backend systems in Go.
`,
	}

	job := jobs.Job{
		ID:           "job-4",
		Title:        "Engineer",
		Company:      "Example",
		Description:  "Join our amazing team.",
		Requirements: nil,
	}

	result := Match(request, job)

	if result.MatchLevel != "Insufficient Evidence" {
		t.Fatalf(
			"expected insufficient evidence, got %q",
			result.MatchLevel,
		)
	}

	if result.MatchPercentage != 0 {
		t.Fatalf(
			"insufficient extraction must not produce a positive percentage",
		)
	}
}

func TestMatchReturnsInsufficientEvidenceForEmptyResume(t *testing.T) {
	request := jobs.SearchRequest{}

	job := jobs.Job{
		ID:          "job-5",
		Title:       "Account Manager",
		Company:     "Example",
		Description: "Account management role.",
		Requirements: []jobs.Requirement{
			{
				Text:       "Customer relationship experience",
				Category:   jobs.RequirementExperience,
				Importance: jobs.RequirementRequired,
			},
		},
	}

	result := Match(request, job)

	if result.MatchLevel != "Insufficient Evidence" {
		t.Fatalf(
			"expected insufficient résumé evidence, got %q",
			result.MatchLevel,
		)
	}
}

func TestMatchSupportsNonTechnicalCandidate(t *testing.T) {
	request := jobs.SearchRequest{
		ResumeText: `
Experience
Managed customer relationships and client accounts.
Developed sales campaigns and maintained CRM records.

Education
Bachelor's Degree in Marketing

Skills
Communication, Salesforce, Microsoft Excel
`,
	}

	job := jobs.Job{
		ID:          "job-6",
		Title:       "Account Manager",
		Company:     "Example",
		Description: "Client account management position.",
		Requirements: []jobs.Requirement{
			{
				Text:       "Customer relationship experience",
				Category:   jobs.RequirementExperience,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Communication skills",
				Category:   jobs.RequirementSkill,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Bachelor's degree",
				Category:   jobs.RequirementEducation,
				Importance: jobs.RequirementRequired,
			},
		},
	}

	result := Match(request, job)

	if result.MatchLevel == "Insufficient Evidence" {
		t.Fatal(
			"nontechnical candidate should be evaluated normally",
		)
	}

	if result.MatchPercentage <= 0 {
		t.Fatalf(
			"expected positive nontechnical match, got %d",
			result.MatchPercentage,
		)
	}
}

func TestMissingRequiredQualificationsPreventStrongMatchWhenAllMissing(
	t *testing.T,
) {
	request := jobs.SearchRequest{
		ResumeText: `
Experience
Managed retail customer accounts.

Skills
Communication, Microsoft Excel
`,
	}

	job := jobs.Job{
		ID:          "job-7",
		Title:       "Licensed Professional",
		Company:     "Example",
		Description: "Licensed professional position.",
		Requirements: []jobs.Requirement{
			{
				Text:       "Registered Nurse license",
				Category:   jobs.RequirementLicense,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Bachelor's degree in Nursing",
				Category:   jobs.RequirementEducation,
				Importance: jobs.RequirementRequired,
			},
		},
	}

	result := Match(request, job)

	if result.MatchLevel != "Reach" {
		t.Fatalf(
			"expected Reach when all required qualifications are missing, got %q",
			result.MatchLevel,
		)
	}
}

func TestPreferredRequirementsReceiveLowerWeight(t *testing.T) {
	request := jobs.SearchRequest{
		ResumeText: `
Experience
Built backend engineering systems in Go.

Skills
Go, SQL
`,
	}

	job := jobs.Job{
		ID:          "job-8",
		Title:       "Backend Engineer",
		Company:     "Example",
		Description: "Backend engineering role.",
		Requirements: []jobs.Requirement{
			{
				Text:       "Backend engineering experience",
				Category:   jobs.RequirementExperience,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Kubernetes skills preferred",
				Category:   jobs.RequirementSkill,
				Importance: jobs.RequirementPreferred,
			},
		},
	}

	result := Match(request, job)

	if result.MatchPercentage <= 50 {
		t.Fatalf(
			"supported required qualification should dominate missing preferred qualification, got %d",
			result.MatchPercentage,
		)
	}
}

func TestTokenCoverageIgnoresQualificationBoilerplate(t *testing.T) {
	tests := []struct {
		name        string
		requirement string
		evidence    string
		minCoverage float64
	}{
		{
			name:        "Go exposure",
			requirement: "exposure to Go is a plus",
			evidence:    "built backend systems in Go and Python",
			minCoverage: 1.0,
		},
		{
			name:        "AWS familiarity",
			requirement: "familiarity with AWS is a plus",
			evidence:    "deployed services on AWS",
			minCoverage: 1.0,
		},
		{
			name:        "SQL experience",
			requirement: "experience using SQL",
			evidence:    "built services backed by SQL databases",
			minCoverage: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenCoverage(tt.requirement, tt.evidence)

			if got < tt.minCoverage {
				t.Fatalf(
					"tokenCoverage(%q, %q) = %.2f; want >= %.2f",
					tt.requirement,
					tt.evidence,
					got,
					tt.minCoverage,
				)
			}
		})
	}
}

func TestTokenCoverageDoesNotTreatOneOfManyCapabilitiesAsFullSupport(t *testing.T) {
	got := tokenCoverage(
		"Go Kubernetes Terraform AWS",
		"built backend systems in Go",
	)

	if got >= 0.75 {
		t.Fatalf(
			"coverage = %.2f; one matching capability must not fully support a multi-capability requirement",
			got,
		)
	}
}

func TestMatchCanCombineEvidenceAcrossResumeStatements(t *testing.T) {
	job := jobs.Job{
		ID:      "aggregate-evidence",
		Title:   "Software Engineer",
		Company: "Example",
		Description: `
Requirements

Familiarity with AWS and containerization tools like Docker and Kubernetes.
Experience building backend services.
`,
		IsActive: true,
		Requirements: []jobs.Requirement{
			{
				Text:       "Familiarity with AWS and containerization tools like Docker and Kubernetes",
				Category:   jobs.RequirementSkill,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Experience building backend services",
				Category:   jobs.RequirementExperience,
				Importance: jobs.RequirementRequired,
			},
		},
	}

	request := jobs.SearchRequest{
		ResumeText: "Deployed backend services on AWS. Built containerized systems using Docker and Kubernetes.",
	}

	result := Match(request, job)

	if result.MatchLevel == "Insufficient Evidence" {
		t.Fatalf(
			"test fixture did not pass extraction quality gate: %+v",
			result,
		)
	}

	target := "Familiarity with AWS and containerization tools like Docker and Kubernetes"

	found := false

	for _, requirement := range result.SupportedRequirements {
		if requirement == target {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf(
			"expected aggregate cloud/container requirement to be supported; got supported=%v partial=%v missing=%v",
			result.SupportedRequirements,
			result.PartialRequirements,
			result.MissingRequirements,
		)
	}
}

func TestSemanticCoverageMatchesEquivalentCapabilityLanguage(t *testing.T) {
	tests := []struct {
		name        string
		requirement string
		evidence    string
		minCoverage float64
	}{
		{
			name:        "high availability from fault tolerance and recovery",
			requirement: "Experience building highly available services",
			evidence:    "Implemented fault-tolerant services with automated recovery and failover.",
			minCoverage: 1.0,
		},
		{
			name:        "container orchestration from Kubernetes",
			requirement: "Experience with container orchestration",
			evidence:    "Deployed production workloads on Kubernetes.",
			minCoverage: 1.0,
		},
		{
			name:        "continuous integration from CI/CD",
			requirement: "Knowledge of continuous integration",
			evidence:    "Built CI/CD pipelines for automated builds and deployments.",
			minCoverage: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := semanticCoverage(tt.requirement, tt.evidence)
			if got < tt.minCoverage {
				t.Fatalf(
					"semanticCoverage(%q, %q) = %.2f; want >= %.2f",
					tt.requirement,
					tt.evidence,
					got,
					tt.minCoverage,
				)
			}
		})
	}
}

func TestSemanticCoverageDoesNotMatchUnrelatedCapabilities(t *testing.T) {
	tests := []struct {
		requirement string
		evidence    string
	}{
		{
			requirement: "Experience with Kubernetes",
			evidence:    "Managed customer accounts and sales campaigns.",
		},
		{
			requirement: "Experience with machine learning",
			evidence:    "Built SQL reporting dashboards for finance teams.",
		},
		{
			requirement: "Experience with application security",
			evidence:    "Developed marketing campaigns and maintained CRM records.",
		},
	}

	for _, tt := range tests {
		got := semanticCoverage(tt.requirement, tt.evidence)
		if got >= 0.40 {
			t.Fatalf(
				"unrelated evidence produced semantic coverage %.2f for requirement %q and evidence %q",
				got,
				tt.requirement,
				tt.evidence,
			)
		}
	}
}

func TestMatchRealisticBackendEngineerEvidence(t *testing.T) {
	request := jobs.SearchRequest{
		ResumeText: `
Software Engineer

Experience
Built backend systems in Go and Python.
Designed distributed systems, REST APIs, and concurrent services.
Built trading infrastructure and GPU workload scheduling systems.
Deployed containerized services using Docker and Kubernetes.
Worked with SQL, AWS, Linux, and CI/CD pipelines.
Built machine learning infrastructure using PyTorch and distributed training.
Performed reliability testing of distributed services.
`,
	}

	job := jobs.Job{
		ID:          "realistic-backend-match",
		Title:       "Backend Software Engineer",
		Company:     "Example",
		Description: "Backend software engineering role.",
		IsActive:    true,
		Requirements: []jobs.Requirement{
			{
				Text:       "Experience developing backend services in Go",
				Category:   jobs.RequirementExperience,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Experience building distributed systems",
				Category:   jobs.RequirementExperience,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Experience with AWS",
				Category:   jobs.RequirementSkill,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Experience with Docker and Kubernetes",
				Category:   jobs.RequirementSkill,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Experience building REST APIs",
				Category:   jobs.RequirementExperience,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Python experience",
				Category:   jobs.RequirementSkill,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Experience with SQL",
				Category:   jobs.RequirementSkill,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Experience with Ruby on Rails",
				Category:   jobs.RequirementSkill,
				Importance: jobs.RequirementPreferred,
			},
		},
	}

	result := Match(request, job)

	if result.MatchLevel == "Insufficient Evidence" {
		t.Fatalf(
			"realistic backend job should be scorable: %+v",
			result,
		)
	}

	if len(result.SupportedRequirements) == 0 {
		t.Fatalf(
			"expected overlapping backend evidence to produce supported requirements: %+v",
			result,
		)
	}

	supportedExpected := []string{
		"Experience developing backend services in Go",
		"Experience building distributed systems",
		"Experience with AWS",
		"Experience with Docker and Kubernetes",
		"Python experience",
		"Experience with SQL",
	}

	for _, expected := range supportedExpected {
		found := false

		for _, supported := range result.SupportedRequirements {
			if supported == expected {
				found = true
				break
			}
		}

		if !found {
			t.Errorf(
				"expected %q to be supported; supported=%v partial=%v missing=%v",
				expected,
				result.SupportedRequirements,
				result.PartialRequirements,
				result.MissingRequirements,
			)
		}
	}

	partialExpected := []string{
		"Experience building REST APIs",
	}

	for _, expected := range partialExpected {
		found := false

		for _, partial := range result.PartialRequirements {
			if partial == expected {
				found = true
				break
			}
		}

		if !found {
			t.Errorf(
				"expected %q to be partially supported; supported=%v partial=%v missing=%v",
				expected,
				result.SupportedRequirements,
				result.PartialRequirements,
				result.MissingRequirements,
			)
		}
	}

	unsupportedPreferred := "Experience with Ruby on Rails"

	for _, supported := range result.SupportedRequirements {
		if supported == unsupportedPreferred {
			t.Fatalf(
				"unsupported preferred requirement was incorrectly marked supported: %q",
				unsupportedPreferred,
			)
		}
	}

	if result.MatchPercentage <= 0 {
		t.Fatalf(
			"expected positive evidence-match percentage, got %d",
			result.MatchPercentage,
		)
	}
}
