package jobs

import (
	"regexp"
	"strings"
)

type ExtractionQualityStatus string

const (
	ExtractionQualityGood         ExtractionQualityStatus = "good"
	ExtractionQualityLimited      ExtractionQualityStatus = "limited"
	ExtractionQualityInsufficient ExtractionQualityStatus = "insufficient"
)

type ExtractionQuality struct {
	Status ExtractionQualityStatus `json:"status"`

	Score int `json:"score"`

	Scorable bool `json:"scorable"`

	RequirementCount          int `json:"requirementCount"`
	RequiredRequirementCount  int `json:"requiredRequirementCount"`
	PreferredRequirementCount int `json:"preferredRequirementCount"`

	CategoryCount int `json:"categoryCount"`

	Reasons []string `json:"reasons,omitempty"`
}

var suspiciousExtractionPatterns = []*regexp.Regexp{
	// Application and recruiting instructions.
	regexp.MustCompile(
		`(?i)\b(?:apply|application)\b.*\b(?:email|contact|recruiter|resume|résumé)\b`,
	),

	// Email addresses should not become qualifications.
	regexp.MustCompile(
		`(?i)\b[\w.%+\-]+@[\w.\-]+\.[a-z]{2,}\b`,
	),

	// Equal-opportunity and accommodation boilerplate.
	regexp.MustCompile(
		`(?i)\b(?:equal opportunity|equal employment opportunity|eeo|reasonable accommodation)\b`,
	),

	// Job-description disclaimers.
	regexp.MustCompile(
		`(?i)\b(?:this description|this position description|this job description)\b.*\b(?:outlines|listing|comprehensive|duties|responsibilities|activities)\b`,
	),

	// Common statements explaining that duties may change.
	regexp.MustCompile(
		`(?i)\b(?:duties|responsibilities|activities)\b.*\b(?:may change|subject to change)\b`,
	),

	// Recruiter/contact details that occasionally appear at the end
	// of syndicated job descriptions.
	regexp.MustCompile(
		`(?i)\b(?:recruiter|recruiting contact|contact information)\b`,
	),

	// Phone/contact lines should not influence qualification quality.
	regexp.MustCompile(
		`(?i)^\s*(?:mobile|phone|telephone|tel|fax)\s*:`,
	),

	// Courtesy/sign-off text from recruiter-posted descriptions.
	regexp.MustCompile(
		`(?i)^\s*(?:thank you|thanks|sincerely|best regards|regards)\s*[,!.]?\s*$`,
	),
}

func EvaluateExtractionQuality(job Job) ExtractionQuality {
	requirements := job.Requirements

	quality := ExtractionQuality{
		Status:   ExtractionQualityInsufficient,
		Score:    0,
		Scorable: false,
		Reasons:  make([]string, 0),
	}

	if strings.TrimSpace(job.Description) == "" {
		quality.Reasons = append(
			quality.Reasons,
			"Job posting does not contain a usable description.",
		)

		return quality
	}

	if len(requirements) == 0 {
		quality.Reasons = append(
			quality.Reasons,
			"No reliable structured qualifications were extracted from the posting.",
		)

		return quality
	}

	categories := make(map[RequirementCategory]struct{})

	validCount := 0
	requiredCount := 0
	preferredCount := 0
	strongSignalCount := 0
	suspiciousCount := 0

	for _, requirement := range requirements {
		text := strings.TrimSpace(requirement.Text)

		if text == "" {
			continue
		}

		if suspiciousExtractedRequirement(text) {
			suspiciousCount++
			continue
		}

		if !isKnownRequirementCategory(requirement.Category) {
			continue
		}

		if !isKnownRequirementImportance(requirement.Importance) {
			continue
		}

		// Your Fit measures candidate qualifications, not job logistics or
		// uncategorized posting text. This rule is occupation-agnostic.
		if !isCandidateFitQualification(requirement.Category) {
			continue
		}

		validCount++

		categories[requirement.Category] = struct{}{}

		switch requirement.Importance {
		case RequirementRequired:
			requiredCount++

		case RequirementPreferred:
			preferredCount++
		}

		if isStrongQualificationSignal(requirement) {
			strongSignalCount++
		}
	}

	quality.RequirementCount = validCount
	quality.RequiredRequirementCount = requiredCount
	quality.PreferredRequirementCount = preferredCount
	quality.CategoryCount = len(categories)

	if suspiciousCount > 0 {
		quality.Reasons = append(
			quality.Reasons,
			"Some extracted text resembled application instructions, contact information, legal boilerplate, or non-qualification posting content.",
		)
	}

	if validCount == 0 {
		quality.Reasons = append(
			quality.Reasons,
			"Extracted qualification data did not contain usable requirements.",
		)

		return quality
	}

	score := 0

	// Requirement volume.
	switch {
	case validCount >= 5:
		score += 35

	case validCount >= 3:
		score += 30

	case validCount == 2:
		score += 22

	case validCount == 1:
		score += 12
	}

	// Required qualifications are especially valuable because they
	// represent the employer's explicit minimum expectations.
	switch {
	case requiredCount >= 3:
		score += 25

	case requiredCount >= 1:
		score += 20

	case preferredCount > 0:
		score += 10
	}

	// Multiple qualification categories provide better evidence that
	// extraction captured the structure of the posting rather than one
	// isolated phrase.
	switch {
	case len(categories) >= 3:
		score += 20

	case len(categories) == 2:
		score += 15

	case len(categories) == 1:
		score += 8
	}

	// Strong signals are requirements whose category normally carries
	// meaningful candidate evidence: experience, education, skills,
	// licenses, certifications, travel, or physical requirements.
	switch {
	case strongSignalCount >= 3:
		score += 20

	case strongSignalCount >= 1:
		score += 15
	}

	// A posting whose extracted output is dominated by suspicious content
	// must not receive a confident quality score. This is intentionally
	// generic: it measures extraction contamination rather than relying on
	// provider- or company-specific rules.
	if suspiciousCount > 0 && suspiciousCount >= validCount {
		score = 0
	}

	if score > 100 {
		score = 100
	}

	quality.Score = score

	switch {
	case validCount >= 2 &&
		strongSignalCount >= 1 &&
		score >= 55:

		quality.Status = ExtractionQualityGood
		quality.Scorable = true

	case validCount >= 1 &&
		strongSignalCount >= 1 &&
		score >= 35:

		quality.Status = ExtractionQualityLimited
		quality.Scorable = true

	default:
		quality.Status = ExtractionQualityInsufficient
		quality.Scorable = false
	}

	if requiredCount == 0 {
		quality.Reasons = append(
			quality.Reasons,
			"No explicit required qualifications were identified.",
		)
	}

	if len(categories) == 1 {
		quality.Reasons = append(
			quality.Reasons,
			"Qualification evidence is concentrated in a single category.",
		)
	}

	if strongSignalCount == 0 {
		quality.Reasons = append(
			quality.Reasons,
			"No strong qualification signals were identified.",
		)
	}

	if !quality.Scorable {
		quality.Reasons = append(
			quality.Reasons,
			"Structured qualification evidence is insufficient for a reliable evidence-match score.",
		)
	}

	return quality
}

func suspiciousExtractedRequirement(text string) bool {
	text = strings.TrimSpace(text)

	if text == "" {
		return true
	}

	for _, pattern := range suspiciousExtractionPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}

	return false
}

func isKnownRequirementCategory(
	category RequirementCategory,
) bool {
	switch category {
	case RequirementSkill,
		RequirementExperience,
		RequirementEducation,
		RequirementLicense,
		RequirementCertification,
		RequirementPhysical,
		RequirementTravel,
		RequirementOther:

		return true

	default:
		return false
	}
}

func isKnownRequirementImportance(
	importance RequirementImportance,
) bool {
	switch importance {
	case RequirementRequired,
		RequirementPreferred:

		return true

	default:
		return false
	}
}

func isStrongQualificationSignal(
	requirement Requirement,
) bool {
	return isCandidateFitQualification(requirement.Category)
}
