package jobs

import (
	"testing"
	"time"
)

func TestNewLeverProviderFiltersEmployers(t *testing.T) {
	employers := []ATSEmployer{
		{
			Name:     "Lever Company",
			Provider: "lever",
			Site:     "lever-company",
		},
		{
			Name:     "Greenhouse Company",
			Provider: "greenhouse",
			Site:     "greenhouse-company",
		},
		{
			Name:     "Missing Site",
			Provider: "lever",
			Site:     "",
		},
	}

	provider := NewLeverProvider(employers)

	if len(provider.employers) != 1 {
		t.Fatalf(
			"expected 1 valid Lever employer, got %d",
			len(provider.employers),
		)
	}

	if provider.employers[0].Name != "Lever Company" {
		t.Fatalf(
			"unexpected employer: %q",
			provider.employers[0].Name,
		)
	}
}

func TestNormalizeLeverPosting(t *testing.T) {
	posting := leverPosting{
		ID:            "job-123",
		Text:          "Backend Software Engineer",
		Description:   "<p>Build reliable backend systems.</p>",
		ApplyURL:      "https://example.com/apply/job-123",
		CreatedAt:     1_700_000_000_000,
		WorkplaceType: "hybrid",
	}

	posting.Categories.Location = "Miami, FL"
	posting.Categories.Commitment = "Full-time"
	posting.Categories.Team = "Engineering"

	posting.SalaryRange = &struct {
		Min      float64 `json:"min"`
		Max      float64 `json:"max"`
		Currency string  `json:"currency"`
		Interval string  `json:"interval"`
	}{
		Min:      100000,
		Max:      140000,
		Currency: "USD",
		Interval: "year",
	}

	job := normalizeLeverPosting(
		ATSEmployer{
			Name:     "Example Company",
			Provider: "lever",
			Site:     "example-company",
		},
		posting,
	)

	if job.ID != "job-123" {
		t.Fatalf("unexpected ID: %q", job.ID)
	}

	if job.Title != "Backend Software Engineer" {
		t.Fatalf("unexpected title: %q", job.Title)
	}

	if job.Company != "Example Company" {
		t.Fatalf("unexpected company: %q", job.Company)
	}

	if job.Location != "Miami, FL" {
		t.Fatalf("unexpected location: %q", job.Location)
	}

	if job.WorkArrangement != "hybrid" {
		t.Fatalf(
			"unexpected work arrangement: %q",
			job.WorkArrangement,
		)
	}

	if job.EmploymentType != "Full-time" {
		t.Fatalf(
			"unexpected employment type: %q",
			job.EmploymentType,
		)
	}

	if job.ApplyURL != "https://example.com/apply/job-123" {
		t.Fatalf(
			"unexpected apply URL: %q",
			job.ApplyURL,
		)
	}

	if job.SalaryMin == nil || *job.SalaryMin != 100000 {
		t.Fatal("expected salary minimum 100000")
	}

	if job.SalaryMax == nil || *job.SalaryMax != 140000 {
		t.Fatal("expected salary maximum 140000")
	}

	if job.SalaryCurrency != "USD" {
		t.Fatalf(
			"unexpected salary currency: %q",
			job.SalaryCurrency,
		)
	}

	if job.PostedAt == nil {
		t.Fatal("expected posting date")
	}

	expected := time.UnixMilli(
		1_700_000_000_000,
	).UTC()

	if !job.PostedAt.Equal(expected) {
		t.Fatalf(
			"unexpected posting date: %v",
			job.PostedAt,
		)
	}

	if job.Source != "lever" {
		t.Fatalf("unexpected source: %q", job.Source)
	}

	if !job.IsActive {
		t.Fatal("expected Lever posting to be active")
	}
}

func TestBuildLeverDescriptionPreservesUsefulContent(t *testing.T) {
	posting := leverPosting{
		Description: "<p>Build distributed systems.</p>",
		Additional:  "<p>Experience with cloud infrastructure.</p>",
	}

	posting.Lists = []struct {
		Text    string `json:"text"`
		Content string `json:"content"`
	}{
		{
			Text:    "Qualifications",
			Content: "<ul><li>Experience with Go</li><li>Experience with Kubernetes</li></ul>",
		},
	}

	description := buildLeverDescription(posting)

	expectedParts := []string{
		"Build distributed systems.",
		"Qualifications",
		"Experience with Go",
		"Experience with Kubernetes",
		"Experience with cloud infrastructure.",
	}

	for _, expected := range expectedParts {
		if !containsNormalizedText(
			description,
			expected,
		) {
			t.Fatalf(
				"description missing %q:\n%s",
				expected,
				description,
			)
		}
	}
}

func TestLeverPostingMatchesSearch(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		title    string
		expected bool
	}{
		{
			name:     "exact role",
			role:     "Software Engineer",
			title:    "Software Engineer",
			expected: true,
		},
		{
			name:     "seniority variation",
			role:     "Software Engineer",
			title:    "Senior Software Engineer",
			expected: true,
		},
		{
			name:     "shared meaningful token",
			role:     "Backend Engineer",
			title:    "Senior Backend Software Engineer",
			expected: true,
		},
		{
			name:     "unrelated role",
			role:     "Software Engineer",
			title:    "Account Executive",
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				actual := leverPostingMatchesSearch(
					SearchRequest{
						TargetRole: test.role,
					},
					Job{
						Title: test.title,
					},
				)

				if actual != test.expected {
					t.Fatalf(
						"expected %v, got %v",
						test.expected,
						actual,
					)
				}
			},
		)
	}
}

func containsNormalizedText(
	value string,
	expected string,
) bool {
	value = normalizePreferenceText(value)
	expected = normalizePreferenceText(expected)

	return len(expected) > 0 &&
		len(value) >= len(expected) &&
		containsString(value, expected)
}

func containsString(
	value string,
	expected string,
) bool {
	for i := 0; i+len(expected) <= len(value); i++ {
		if value[i:i+len(expected)] == expected {
			return true
		}
	}

	return false
}

func TestInferLeverCountryCode(t *testing.T) {
	tests := []struct {
		location string
		expected string
	}{
		{"Remote / USA", "US"},
		{"Remote / Colombia", "CO"},
		{"Remote / Brazil", "BR"},
		{"Remote / Canada", "CA"},
		{"Remote / United Kingdom", "GB"},
		{"Miami, Florida", "US"},
		{"Unknown Remote Location", ""},
	}

	for _, test := range tests {
		actual := inferLeverCountryCode(test.location)

		if actual != test.expected {
			t.Fatalf(
				"inferLeverCountryCode(%q) = %q; want %q",
				test.location,
				actual,
				test.expected,
			)
		}
	}
}
