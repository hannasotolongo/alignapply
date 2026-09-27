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

type semanticTestProvider struct {
	scores map[string]float64
}

func (p semanticTestProvider) Similarity(requirement string, evidence string) (float64, error) {
	// Match() passes canonical matcher text into the semantic layer, while
	// direct semantic tests may use display-form text. Canonicalize both here
	// so the fake provider tests meaning rather than capitalization or
	// punctuation differences.
	requirement = normalize(requirement)
	evidence = normalize(evidence)

	if score, ok := p.scores[semanticTestKey(requirement, evidence)]; ok {
		return score, nil
	}

	for key, score := range p.scores {
		separator := -1
		for i := 0; i < len(key); i++ {
			if key[i] == 0 {
				separator = i
				break
			}
		}

		if separator < 0 {
			continue
		}

		expectedRequirement := key[:separator]
		expectedEvidence := key[separator+1:]

		if requirement == expectedRequirement &&
			containsNormalizedText(evidence, expectedEvidence) {
			return score, nil
		}
	}

	return 0, nil
}

func containsNormalizedText(text string, target string) bool {
	if target == "" {
		return false
	}

	for i := 0; i+len(target) <= len(text); i++ {
		if text[i:i+len(target)] == target {
			return true
		}
	}

	return false
}

func semanticTestKey(requirement string, evidence string) string {
	return normalize(requirement) + "\x00" + normalize(evidence)
}

func semanticRetrievalScore(requirement string, evidence string) float64 {
	candidates := currentSemanticRetriever().Retrieve(
		requirement,
		[]string{evidence},
		1,
	)
	if len(candidates) == 0 {
		return 0
	}
	return candidates[0].Score
}

func TestSemanticRetrievalRanksEquivalentCapabilityLanguage(t *testing.T) {
	previous := currentSemanticRetriever()
	t.Cleanup(func() {
		SetSemanticRetriever(previous)
	})

	tests := []struct {
		name        string
		requirement string
		evidence    string
	}{
		{
			name:        "high availability from fault tolerance and recovery",
			requirement: "Experience building highly available services",
			evidence:    "Implemented fault-tolerant services with automated recovery and failover.",
		},
		{
			name:        "container orchestration from Kubernetes",
			requirement: "Experience with container orchestration",
			evidence:    "Deployed production workloads on Kubernetes.",
		},
		{
			name:        "continuous integration from CI/CD",
			requirement: "Knowledge of continuous integration",
			evidence:    "Built CI/CD pipelines for automated builds and deployments.",
		},
	}

	scores := make(map[string]float64, len(tests))
	for _, tt := range tests {
		scores[semanticTestKey(tt.requirement, tt.evidence)] = 0.90
	}

	SetSemanticRetriever(HybridSemanticRetriever{
		Provider: semanticTestProvider{scores: scores},
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := semanticRetrievalScore(tt.requirement, tt.evidence)
			if got < 0.75 {
				t.Fatalf(
					"semantic retrieval score (%q, %q) = %.2f; want >= 0.75",
					tt.requirement,
					tt.evidence,
					got,
				)
			}
		})
	}
}

func TestSemanticRetrievalDoesNotRankUnrelatedCapabilitiesHighly(t *testing.T) {
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
		got := semanticRetrievalScore(tt.requirement, tt.evidence)
		if got >= 0.40 {
			t.Fatalf(
				"unrelated evidence produced semantic retrieval score %.2f for requirement %q and evidence %q",
				got,
				tt.requirement,
				tt.evidence,
			)
		}
	}
}

func TestMatchRealisticBackendEngineerEvidence(t *testing.T) {
	previous := currentSemanticRetriever()
	t.Cleanup(func() {
		SetSemanticRetriever(previous)
	})

	SetSemanticRetriever(HybridSemanticRetriever{
		Provider: semanticTestProvider{
			scores: map[string]float64{
				semanticTestKey(
					"Experience building distributed systems",
					"Designed distributed systems, REST APIs, and concurrent services.",
				): 0.90,
			},
		},
	})

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
			"expected overlapping backend evidence to produce supported requirements:%+v",
			result,
		)
	}

	supportedExpected := []string{
		"Experience developing backend services in Go",
		"Experience building distributed systems",
		"Experience with AWS",
		"Experience with Docker and Kubernetes",
		"Experience building REST APIs",
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

type evidenceVerifierTestKey struct {
	requirement string
	evidence    string
}

type evidenceTestVerifier struct {
	results map[evidenceVerifierTestKey]evidenceResult
}

func (v evidenceTestVerifier) Verify(requirement string, evidence string) evidenceResult {
	requirement = normalize(requirement)
	evidence = normalize(evidence)

	key := evidenceVerifierTestKey{
		requirement: requirement,
		evidence:    evidence,
	}

	if result, ok := v.results[key]; ok {
		return result
	}

	// Candidate extraction may preserve surrounding section/context text.
	// The fake verifier should recognize the expected atomic evidence inside
	// that extracted text rather than require byte-for-byte equality.
	for expected, result := range v.results {
		if requirement != expected.requirement {
			continue
		}

		if containsNormalizedText(evidence, expected.evidence) ||
			containsNormalizedText(expected.evidence, evidence) {
			return result
		}
	}

	return verifyEvidenceDeterministically(requirement, evidence)
}

func TestMatchCrossDomainEquivalentExperience(t *testing.T) {
	previousRetriever := currentSemanticRetriever()
	previousVerifier := currentEvidenceVerifier()
	t.Cleanup(func() {
		SetSemanticRetriever(previousRetriever)
		SetEvidenceVerifier(previousVerifier)
	})

	tests := []struct {
		name        string
		resumeText  string
		evidence    string
		jobTitle    string
		requirement string
		category    jobs.RequirementCategory
	}{
		{
			name:        "finance financial modeling",
			resumeText:  "Experience\nBuilt valuation and cash-flow models to evaluate investment opportunities.",
			evidence:    "Built valuation and cash-flow models to evaluate investment opportunities.",
			jobTitle:    "Financial Analyst",
			requirement: "Experience with financial modeling",
			category:    jobs.RequirementExperience,
		},
		{
			name:        "healthcare patient education",
			resumeText:  "Experience\nCounseled patients on medications, proper administration, adherence, and potential side effects.",
			evidence:    "Counseled patients on medications, proper administration, adherence, and potential side effects.",
			jobTitle:    "Clinical Care Coordinator",
			requirement: "Experience providing patient education",
			category:    jobs.RequirementExperience,
		},
		{
			name:        "recruiting candidate sourcing",
			resumeText:  "Experience\nIdentified and contacted prospective hires for open technical positions.",
			evidence:    "Identified and contacted prospective hires for open technical positions.",
			jobTitle:    "Technical Recruiter",
			requirement: "Experience sourcing candidates",
			category:    jobs.RequirementExperience,
		},
		{
			name:        "sales account management",
			resumeText:  "Experience\nManaged a portfolio of client accounts and maintained long-term customer relationships.",
			evidence:    "Managed a portfolio of client accounts and maintained long-term customer relationships.",
			jobTitle:    "Account Executive",
			requirement: "Experience managing customer accounts",
			category:    jobs.RequirementExperience,
		},
		{
			name:        "marketing campaign analytics",
			resumeText:  "Experience\nMeasured campaign performance using conversion, engagement, and acquisition metrics.",
			evidence:    "Measured campaign performance using conversion, engagement, and acquisition metrics.",
			jobTitle:    "Marketing Analyst",
			requirement: "Experience analyzing marketing campaign performance",
			category:    jobs.RequirementExperience,
		},
		{
			name:        "operations process improvement",
			resumeText:  "Experience\nRedesigned internal workflows to reduce processing delays and improve team efficiency.",
			evidence:    "Redesigned internal workflows to reduce processing delays and improve team efficiency.",
			jobTitle:    "Operations Analyst",
			requirement: "Experience improving operational processes",
			category:    jobs.RequirementExperience,
		},
	}

	scores := make(map[string]float64, len(tests))
	verificationResults := make(map[evidenceVerifierTestKey]evidenceResult, len(tests))

	for _, tt := range tests {
		scores[semanticTestKey(tt.requirement, tt.evidence)] = 0.90
		verificationResults[evidenceVerifierTestKey{
			requirement: normalize(tt.requirement),
			evidence:    normalize(tt.evidence),
		}] = supportedEvidence()
	}

	SetSemanticRetriever(HybridSemanticRetriever{
		Provider: semanticTestProvider{scores: scores},
	})
	SetEvidenceVerifier(evidenceTestVerifier{results: verificationResults})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := jobs.Job{
				ID:          "cross-domain-" + tt.name,
				Title:       tt.jobTitle,
				Company:     "Example",
				Description: tt.jobTitle + " position.",
				IsActive:    true,
				Requirements: []jobs.Requirement{
					{
						Text:       tt.requirement,
						Category:   tt.category,
						Importance: jobs.RequirementRequired,
					},
				},
			}

			result := Match(
				jobs.SearchRequest{ResumeText: tt.resumeText},
				job,
			)

			if result.MatchLevel == "Insufficient Evidence" {
				t.Fatalf("%s should be scorable: %+v", tt.name, result)
			}

			found := false
			for _, supported := range result.SupportedRequirements {
				if supported == tt.requirement {
					found = true
					break
				}
			}

			if !found {
				t.Fatalf(
					"equivalent %s evidence should support %q; supported=%v partial=%v missing=%v",
					tt.name,
					tt.requirement,
					result.SupportedRequirements,
					result.PartialRequirements,
					result.MissingRequirements,
				)
			}
		})
	}
}

func TestExperienceRequirementCanUseCapabilityEvidence(t *testing.T) {
	request := jobs.SearchRequest{
		ResumeText: "Software engineer experienced with AWS, PostgreSQL, REST APIs, and distributed systems.",
	}

	job := jobs.Job{
		ID:          "capability-as-experience",
		Title:       "Software Engineer",
		Company:     "Example",
		Description: "Cloud backend role.",
		Requirements: []jobs.Requirement{
			{
				Text:       "Experience with AWS",
				Category:   jobs.RequirementExperience,
				Importance: jobs.RequirementRequired,
			},
		},
	}

	result := Match(request, job)

	if len(result.SupportedRequirements) != 1 {
		t.Fatalf(
			"expected capability evidence to support non-duration experience requirement, got %+v",
			result,
		)
	}
}

func TestExperienceYearsDoNotUseSkillOnlyEvidence(t *testing.T) {
	request := jobs.SearchRequest{
		ResumeText: "Skills: AWS, PostgreSQL, REST APIs, Docker, Kubernetes.",
	}

	job := jobs.Job{
		ID:          "duration-remains-strict",
		Title:       "Software Engineer",
		Company:     "Example",
		Description: "Cloud backend role.",
		Requirements: []jobs.Requirement{
			{
				Text:       "5 years of AWS experience",
				Category:   jobs.RequirementExperience,
				Importance: jobs.RequirementRequired,
			},
		},
	}

	result := Match(request, job)

	if len(result.SupportedRequirements) != 0 {
		t.Fatalf(
			"skill-only evidence must not establish years of experience: %+v",
			result,
		)
	}
}

func TestNonQualificationCategoriesDoNotAffectCandidateFit(t *testing.T) {
	request := jobs.SearchRequest{
		ResumeText: `
Software Engineer
Built backend services in Go.
`,
	}

	job := jobs.Job{
		ID:          "metadata-does-not-affect-fit",
		Title:       "Backend Engineer",
		Company:     "Example",
		Description: "Backend role.",
		Requirements: []jobs.Requirement{
			{
				Text:       "Experience building backend services in Go",
				Category:   jobs.RequirementExperience,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Must be able to travel 50 percent",
				Category:   jobs.RequirementTravel,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Must be able to lift 40 pounds",
				Category:   jobs.RequirementPhysical,
				Importance: jobs.RequirementRequired,
			},
			{
				Text:       "Miami, FL",
				Category:   jobs.RequirementOther,
				Importance: jobs.RequirementRequired,
			},
		},
	}

	result := Match(request, job)

	for _, value := range result.MissingRequirements {
		if value == "Miami, FL" ||
			value == "Must be able to travel 50 percent" ||
			value == "Must be able to lift 40 pounds" {

			t.Fatalf(
				"job metadata/non-qualification requirement affected candidate fit: %+v",
				result,
			)
		}
	}
}

func TestFitCategoryUsesApplicantLabels(t *testing.T) {
	strong := determineFitCategory(
		4,
		4,
		0,
		0,
		1,
		0,
	)

	if strong != "Strong Applicant" {
		t.Fatalf(
			"expected Strong Applicant, got %q",
			strong,
		)
	}

	moderate := determineFitCategory(
		4,
		2,
		1,
		1,
		0,
		0,
	)

	if moderate != "Moderate Match" {
		t.Fatalf(
			"expected Moderate Match, got %q",
			moderate,
		)
	}

	reach := determineFitCategory(
		4,
		3,
		0,
		1,
		1,
		1,
	)

	if reach != "Reach" {
		t.Fatalf(
			"expected Reach, got %q",
			reach,
		)
	}
}

func TestRequiredQualificationCategoriesParticipateInFit(t *testing.T) {
	qualificationCategories := []jobs.RequirementCategory{
		jobs.RequirementSkill,
		jobs.RequirementExperience,
		jobs.RequirementEducation,
		jobs.RequirementLicense,
		jobs.RequirementCertification,
	}

	for _, category := range qualificationCategories {
		requirement := jobs.Requirement{
			Text:       "qualification",
			Category:   category,
			Importance: jobs.RequirementRequired,
		}

		if !isQualificationRequirement(requirement) {
			t.Fatalf(
				"expected category %q to participate in candidate fit",
				category,
			)
		}
	}

	excludedCategories := []jobs.RequirementCategory{
		jobs.RequirementPhysical,
		jobs.RequirementTravel,
		jobs.RequirementOther,
	}

	for _, category := range excludedCategories {
		requirement := jobs.Requirement{
			Text:       "posting metadata",
			Category:   category,
			Importance: jobs.RequirementRequired,
		}

		if isQualificationRequirement(requirement) {
			t.Fatalf(
				"category %q must not participate in candidate fit",
				category,
			)
		}
	}
}
