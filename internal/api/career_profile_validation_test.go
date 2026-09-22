package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateCareerProfileRequestValid(t *testing.T) {
	request := careerProfileRequest{
		Headline:   "Backend Software Engineer",
		Summary:    "Backend and distributed systems engineer.",
		TargetRole: "Software Engineer",
		Location:   "Miami, FL",
		Evidence: []careerProfileEvidenceRequest{
			{
				Category:   "experience",
				EntryIndex: 0,
				Text:       "Built reliable Go backend services.",
				Source:     "career_profile.experience",
			},
			{
				Category:   "skill",
				EntryIndex: 0,
				Text:       "Go",
				Source:     "career_profile.skills",
			},
		},
	}

	if err := validateCareerProfileRequest(request); err != nil {
		t.Fatalf("expected valid request, got error: %v", err)
	}
}

func TestNormalizeCareerProfileRequest(t *testing.T) {
	request := careerProfileRequest{
		Headline:   "  Backend Engineer  ",
		Summary:    "  Distributed systems  ",
		TargetRole: "  Software Engineer  ",
		Location:   "  Miami, FL  ",
		Evidence: []careerProfileEvidenceRequest{
			{
				Category:   "  EXPERIENCE  ",
				EntryIndex: 0,
				Text:       "  Built Go services.  ",
				Source:     "  career_profile.experience  ",
			},
		},
	}

	normalizeCareerProfileRequest(&request)

	if request.Headline != "Backend Engineer" {
		t.Fatalf("unexpected headline: %q", request.Headline)
	}

	if request.Summary != "Distributed systems" {
		t.Fatalf("unexpected summary: %q", request.Summary)
	}

	if request.TargetRole != "Software Engineer" {
		t.Fatalf("unexpected target role: %q", request.TargetRole)
	}

	if request.Location != "Miami, FL" {
		t.Fatalf("unexpected location: %q", request.Location)
	}

	if len(request.Evidence) != 1 {
		t.Fatalf("expected 1 evidence item, got %d", len(request.Evidence))
	}

	item := request.Evidence[0]

	if item.Category != "experience" {
		t.Fatalf("unexpected category: %q", item.Category)
	}

	if item.Text != "Built Go services." {
		t.Fatalf("unexpected evidence text: %q", item.Text)
	}

	if item.Source != "career_profile.experience" {
		t.Fatalf("unexpected source: %q", item.Source)
	}
}

func TestValidateCareerProfileRequestRejectsInvalidCategory(t *testing.T) {
	request := careerProfileRequest{
		Evidence: []careerProfileEvidenceRequest{
			{
				Category:   "hobby",
				EntryIndex: 0,
				Text:       "Built software.",
				Source:     "career_profile.hobbies",
			},
		},
	}

	err := validateCareerProfileRequest(request)
	if err == nil {
		t.Fatal("expected invalid category error")
	}

	if !strings.Contains(err.Error(), "category is invalid") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCareerProfileRequestRejectsNegativeEntryIndex(t *testing.T) {
	request := careerProfileRequest{
		Evidence: []careerProfileEvidenceRequest{
			{
				Category:   "experience",
				EntryIndex: -1,
				Text:       "Built software.",
				Source:     "career_profile.experience",
			},
		},
	}

	err := validateCareerProfileRequest(request)
	if err == nil {
		t.Fatal("expected negative entry index error")
	}

	if !strings.Contains(err.Error(), "entryIndex") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCareerProfileRequestRejectsMissingText(t *testing.T) {
	request := careerProfileRequest{
		Evidence: []careerProfileEvidenceRequest{
			{
				Category:   "skill",
				EntryIndex: 0,
				Text:       "",
				Source:     "career_profile.skills",
			},
		},
	}

	err := validateCareerProfileRequest(request)
	if err == nil {
		t.Fatal("expected missing text error")
	}

	if !strings.Contains(err.Error(), "text is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCareerProfileRequestRejectsMissingSource(t *testing.T) {
	request := careerProfileRequest{
		Evidence: []careerProfileEvidenceRequest{
			{
				Category:   "skill",
				EntryIndex: 0,
				Text:       "Go",
				Source:     "",
			},
		},
	}

	err := validateCareerProfileRequest(request)
	if err == nil {
		t.Fatal("expected missing source error")
	}

	if !strings.Contains(err.Error(), "source is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCareerProfileRequestRejectsDuplicateEvidence(t *testing.T) {
	request := careerProfileRequest{
		Evidence: []careerProfileEvidenceRequest{
			{
				Category:   "skill",
				EntryIndex: 0,
				Text:       "Go",
				Source:     "career_profile.skills",
			},
			{
				Category:   "skill",
				EntryIndex: 0,
				Text:       "Go",
				Source:     "career_profile.skills",
			},
		},
	}

	err := validateCareerProfileRequest(request)
	if err == nil {
		t.Fatal("expected duplicate evidence error")
	}

	if !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeCareerProfileRequestRejectsUnknownFields(t *testing.T) {
	httpRequest := httptest.NewRequest(
		"PUT",
		"/api/v1/career-profile",
		strings.NewReader(`{
			"headline": "Backend Engineer",
			"unknownField": "not allowed"
		}`),
	)

	var request careerProfileRequest

	err := decodeCareerProfileRequest(
		httpRequest,
		&request,
	)

	if err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestDecodeCareerProfileRequestRejectsTrailingJSON(t *testing.T) {
	httpRequest := httptest.NewRequest(
		"PUT",
		"/api/v1/career-profile",
		strings.NewReader(
			`{"headline":"Backend Engineer"} {"headline":"Second"}`,
		),
	)

	var request careerProfileRequest

	err := decodeCareerProfileRequest(
		httpRequest,
		&request,
	)

	if err == nil {
		t.Fatal("expected trailing JSON error")
	}
}

func TestValidateCareerProfileRequestAcceptsAllSupportedCategories(t *testing.T) {
	categories := []string{
		"skill",
		"experience",
		"education",
		"certification",
		"license",
		"project",
		"summary",
	}

	evidence := make(
		[]careerProfileEvidenceRequest,
		0,
		len(categories),
	)

	for index, category := range categories {
		evidence = append(
			evidence,
			careerProfileEvidenceRequest{
				Category:   category,
				EntryIndex: index,
				Text:       "Evidence for " + category,
				Source:     "career_profile." + category,
			},
		)
	}

	request := careerProfileRequest{
		Evidence: evidence,
	}

	if err := validateCareerProfileRequest(request); err != nil {
		t.Fatalf(
			"expected all supported categories to validate: %v",
			err,
		)
	}
}
