package jobs

import (
	"strings"
	"testing"
	"time"
)

func TestPipelineRejectsInactiveJobs(t *testing.T) {
	pipeline := NewPipeline()

	input := []Job{
		testPipelineJob(
			"active",
			"Backend Engineer",
			"Example",
			"Miami, FL",
		),
		testPipelineJob(
			"inactive",
			"Backend Engineer",
			"Other",
			"Miami, FL",
		),
	}

	input[1].IsActive = false

	result := pipeline.Process(
		SearchRequest{},
		input,
	)

	if len(result) != 1 {
		t.Fatalf(
			"expected one active job, got %d",
			len(result),
		)
	}

	if result[0].ID != "active" {
		t.Fatalf(
			"expected active job, got %q",
			result[0].ID,
		)
	}
}

func TestPipelineRejectsInvalidJobs(t *testing.T) {
	pipeline := NewPipeline()

	valid := testPipelineJob(
		"valid",
		"Account Manager",
		"Example",
		"Miami, FL",
	)

	invalid := valid
	invalid.ID = "invalid"
	invalid.ApplyURL = ""

	result := pipeline.Process(
		SearchRequest{},
		[]Job{valid, invalid},
	)

	if len(result) != 1 {
		t.Fatalf(
			"expected invalid posting to be removed, got %d jobs",
			len(result),
		)
	}
}

func TestPipelineFiltersKnownLocationMismatch(t *testing.T) {
	pipeline := NewPipeline()

	miami := testPipelineJob(
		"miami",
		"Account Manager",
		"Example",
		"Miami, FL",
	)

	chicago := testPipelineJob(
		"chicago",
		"Account Manager",
		"Other",
		"Chicago, IL",
	)

	result := pipeline.Process(
		SearchRequest{
			Location: "Miami, FL",
		},
		[]Job{miami, chicago},
	)

	if len(result) != 1 ||
		result[0].ID != "miami" {
		t.Fatalf(
			"expected only Miami job, got %#v",
			result,
		)
	}
}

func TestPipelineAllowsRemoteOutsideRequestedLocation(
	t *testing.T,
) {
	pipeline := NewPipeline()

	job := testPipelineJob(
		"remote",
		"Software Engineer",
		"Example",
		"New York, NY",
	)

	job.WorkArrangement = "remote"

	result := pipeline.Process(
		SearchRequest{
			Location: "Miami, FL",
			Remote:   true,
		},
		[]Job{job},
	)

	if len(result) != 1 {
		t.Fatal(
			"remote job should remain eligible when remote is accepted",
		)
	}
}

func TestPipelineFiltersWorkArrangement(t *testing.T) {
	pipeline := NewPipeline()

	remote := testPipelineJob(
		"remote",
		"Engineer",
		"Example",
		"Miami, FL",
	)
	remote.WorkArrangement = "remote"

	onsite := testPipelineJob(
		"onsite",
		"Engineer",
		"Other",
		"Miami, FL",
	)
	onsite.WorkArrangement = "onsite"

	result := pipeline.Process(
		SearchRequest{
			Location: "Miami, FL",
			Remote:   true,
		},
		[]Job{remote, onsite},
	)

	if len(result) != 1 ||
		result[0].ID != "remote" {
		t.Fatalf(
			"expected only remote job, got %#v",
			result,
		)
	}
}

func TestPipelineAllowsUnknownWorkArrangement(t *testing.T) {
	pipeline := NewPipeline()

	job := testPipelineJob(
		"unknown-arrangement",
		"Engineer",
		"Example",
		"Miami, FL",
	)

	job.WorkArrangement = ""

	result := pipeline.Process(
		SearchRequest{
			Remote: true,
		},
		[]Job{job},
	)

	if len(result) != 1 {
		t.Fatal(
			"unknown arrangement must not be falsely classified as mismatch",
		)
	}
}

func TestPipelineFiltersEmploymentType(t *testing.T) {
	pipeline := NewPipeline()

	fullTime := testPipelineJob(
		"full",
		"Engineer",
		"Example",
		"Miami, FL",
	)
	fullTime.EmploymentType = "full-time"

	partTime := testPipelineJob(
		"part",
		"Engineer",
		"Other",
		"Miami, FL",
	)
	partTime.EmploymentType = "part-time"

	result := pipeline.Process(
		SearchRequest{
			EmploymentType: []string{"full-time"},
		},
		[]Job{fullTime, partTime},
	)

	if len(result) != 1 ||
		result[0].ID != "full" {
		t.Fatalf(
			"expected full-time job only, got %#v",
			result,
		)
	}
}

func TestPipelineFiltersKnownSalaryBelowMinimum(t *testing.T) {
	pipeline := NewPipeline()

	low := testPipelineJob(
		"low",
		"Engineer",
		"Example",
		"Miami, FL",
	)

	lowMin := 70000.0
	lowMax := 90000.0

	low.SalaryMin = &lowMin
	low.SalaryMax = &lowMax

	high := testPipelineJob(
		"high",
		"Engineer",
		"Other",
		"Miami, FL",
	)

	highMin := 110000.0
	highMax := 140000.0

	high.SalaryMin = &highMin
	high.SalaryMax = &highMax

	result := pipeline.Process(
		SearchRequest{
			MinimumSalary: "$100k",
		},
		[]Job{low, high},
	)

	if len(result) != 1 ||
		result[0].ID != "high" {
		t.Fatalf(
			"expected salary-compatible job only, got %#v",
			result,
		)
	}
}

func TestPipelineKeepsUnknownSalary(t *testing.T) {
	pipeline := NewPipeline()

	job := testPipelineJob(
		"unknown-salary",
		"Engineer",
		"Example",
		"Miami, FL",
	)

	result := pipeline.Process(
		SearchRequest{
			MinimumSalary: "100000",
		},
		[]Job{job},
	)

	if len(result) != 1 {
		t.Fatal(
			"unknown salary must remain eligible",
		)
	}
}

func TestPipelineDeduplicatesSameProviderIdentity(
	t *testing.T,
) {
	pipeline := NewPipeline()

	first := testPipelineJob(
		"one",
		"Engineer",
		"Example",
		"Miami, FL",
	)
	first.Source = "provider"
	first.SourceJobID = "123"

	second := first
	second.ID = "two"

	result := pipeline.Process(
		SearchRequest{},
		[]Job{first, second},
	)

	if len(result) != 1 {
		t.Fatalf(
			"expected duplicate source identity once, got %d",
			len(result),
		)
	}
}

func TestPipelineEnrichesRequirements(t *testing.T) {
	pipeline := NewPipeline()

	job := testPipelineJob(
		"enrich",
		"Account Manager",
		"Example",
		"Miami, FL",
	)

	job.Description = `
Qualifications
3 years of account management experience.
Strong communication skills.
Bachelor's degree.
`

	result := pipeline.Process(
		SearchRequest{},
		[]Job{job},
	)

	if len(result) != 1 {
		t.Fatalf(
			"expected one job, got %d",
			len(result),
		)
	}

	if len(result[0].Requirements) == 0 {
		t.Fatal(
			"pipeline must enrich normalized jobs before matching",
		)
	}
}

func TestPipelineOrdersHigherQualityExtractionFirst(
	t *testing.T,
) {
	pipeline := NewPipeline()

	weak := testPipelineJob(
		"weak",
		"Engineer",
		"Alpha",
		"Miami, FL",
	)
	weak.Description = `
Qualifications
Professional demeanor.
`

	strong := testPipelineJob(
		"strong",
		"Engineer",
		"Beta",
		"Miami, FL",
	)
	strong.Description = `
Qualifications
3 years of engineering experience.
Strong communication skills.
Bachelor's degree.
`

	result := pipeline.Process(
		SearchRequest{},
		[]Job{weak, strong},
	)

	if len(result) != 2 {
		t.Fatalf(
			"expected two jobs, got %d",
			len(result),
		)
	}

	if result[0].ID != "strong" {
		t.Fatalf(
			"expected stronger structured posting first, got %q",
			result[0].ID,
		)
	}
}

func TestPipelineUsesPostingDateAsSecondaryOrdering(
	t *testing.T,
) {
	pipeline := NewPipeline()

	oldJob := testPipelineJob(
		"old",
		"Engineer",
		"Alpha",
		"Miami, FL",
	)

	newJob := testPipelineJob(
		"new",
		"Engineer",
		"Beta",
		"Miami, FL",
	)

	description := `
Qualifications
3 years of engineering experience.
Strong communication skills.
Bachelor's degree.
`

	oldJob.Description = description
	newJob.Description = description

	oldTime := time.Date(
		2026, 9, 1,
		0, 0, 0, 0,
		time.UTC,
	)

	newTime := time.Date(
		2026, 9, 15,
		0, 0, 0, 0,
		time.UTC,
	)

	oldJob.PostedAt = &oldTime
	newJob.PostedAt = &newTime

	result := pipeline.Process(
		SearchRequest{},
		[]Job{oldJob, newJob},
	)

	if len(result) != 2 {
		t.Fatalf(
			"expected two jobs, got %d",
			len(result),
		)
	}

	if result[0].ID != "new" {
		t.Fatalf(
			"expected newer equivalent-quality posting first, got %q",
			result[0].ID,
		)
	}
}

func TestParseMinimumSalaryFormats(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
	}{
		{"100000", 100000},
		{"$100,000", 100000},
		{"100k", 100000},
		{"$125K", 125000},
	}

	for _, test := range tests {
		actual, ok := parseMinimumSalary(test.input)

		if !ok {
			t.Fatalf(
				"expected %q to parse",
				test.input,
			)
		}

		if actual != test.expected {
			t.Fatalf(
				"expected %q = %.0f, got %.0f",
				test.input,
				test.expected,
				actual,
			)
		}
	}
}

func testPipelineJob(
	id string,
	title string,
	company string,
	location string,
) Job {
	return Job{
		ID:          id,
		Source:      "test",
		SourceJobID: id,

		Title:       title,
		Company:     company,
		Description: "Qualifications: Strong communication skills.",

		Location: location,

		IsActive: true,

		ApplyURL: "https://example.com/jobs/" + id,
	}
}

func TestPipelineNormalizesJobDescriptionFormatting(t *testing.T) {
	pipeline := NewPipeline()

	job := testPipelineJob(
		"description-formatting",
		"Software Engineer",
		"Example",
		"Miami, FL",
	)

	job.Description = "About the role\r\n\r\nBuild backend systems&nbsp;in Go.<br>Work with APIs and distributed systems."

	result := pipeline.Process(
		SearchRequest{},
		[]Job{job},
	)

	if len(result) != 1 {
		t.Fatalf("expected one job, got %d", len(result))
	}

	description := result[0].Description

	if strings.Contains(description, "&nbsp;") {
		t.Fatalf("HTML entity remained in description: %q", description)
	}

	if strings.Contains(description, "<br>") {
		t.Fatalf("HTML markup remained in description: %q", description)
	}

	if !strings.Contains(
		description,
		"Build backend systems in Go.",
	) {
		t.Fatalf(
			"expected complete sentence to be preserved, got %q",
			description,
		)
	}

	if !strings.Contains(
		description,
		"Work with APIs and distributed systems.",
	) {
		t.Fatalf(
			"expected second complete sentence to be preserved, got %q",
			description,
		)
	}
}

func TestNormalizeJobDescriptionDoesNotRewriteWords(t *testing.T) {
	input := "Design GPU workload scheduling systems.\nBuild REST APIs in Go."

	got := normalizeJobDescription(input)

	if !strings.Contains(
		got,
		"Design GPU workload scheduling systems.",
	) {
		t.Fatalf("description wording changed unexpectedly: %q", got)
	}

	if !strings.Contains(
		got,
		"Build REST APIs in Go.",
	) {
		t.Fatalf("description wording changed unexpectedly: %q", got)
	}
}

func TestJobTitleMatchesTargetRole(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		title    string
		expected bool
	}{
		{"exact", "Software Engineer", "Software Engineer", true},
		{"seniority", "Software Engineer", "Senior Software Engineer", true},
		{"staff", "Software Engineer", "Staff Software Engineer, Infrastructure", true},
		{"backend", "Backend Engineer", "Senior Backend Software Engineer", true},
		{"mechanical rejected", "Software Engineer", "Mechanical Engineer", false},
		{"civil rejected", "Software Engineer", "Civil Engineer", false},
		{"account executive rejected", "Software Engineer", "Account Executive", false},
		{"data analyst rejected", "Software Engineer", "Data Analyst", false},
		{"data engineer", "Data Engineer", "Senior Data Engineer", true},
		{"ml engineer", "Machine Learning Engineer", "Senior Machine Learning Engineer", true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := jobTitleMatchesTargetRole(test.role, test.title)

			if actual != test.expected {
				t.Fatalf(
					"jobTitleMatchesTargetRole(%q, %q) = %v; want %v",
					test.role,
					test.title,
					actual,
					test.expected,
				)
			}
		})
	}
}

func TestRemoteJobRejectsExplicitCountryMismatch(t *testing.T) {
	request := SearchRequest{
		Location:    "Miami, FL",
		CountryCode: "US",
		Remote:      true,
	}

	tests := []struct {
		name     string
		job      Job
		expected bool
	}{
		{
			name: "US remote accepted",
			job: Job{
				Location:        "Remote / USA",
				CountryCode:     "US",
				WorkArrangement: "remote",
			},
			expected: true,
		},
		{
			name: "Colombia remote rejected",
			job: Job{
				Location:        "Remote / Colombia",
				CountryCode:     "CO",
				WorkArrangement: "remote",
			},
			expected: false,
		},
		{
			name: "Brazil remote rejected",
			job: Job{
				Location:        "Remote / Brazil",
				CountryCode:     "BR",
				WorkArrangement: "remote",
			},
			expected: false,
		},
		{
			name: "unknown remote country remains eligible",
			job: Job{
				Location:        "Remote",
				WorkArrangement: "remote",
			},
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := matchesLocationPreference(request, test.job)

			if actual != test.expected {
				t.Fatalf(
					"matchesLocationPreference() = %v; want %v",
					actual,
					test.expected,
				)
			}
		})
	}
}
