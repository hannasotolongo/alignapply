package jobs

import (
	"strings"
	"unicode"
)

// ValidateRequirements is the final quality gate between extraction
// and matching.
func ValidateRequirements(
	requirements []Requirement,
	sourceText string,
) []Requirement {
	if len(requirements) == 0 {
		return []Requirement{}
	}

	validated := make([]Requirement, 0, len(requirements))

	for _, requirement := range requirements {
		requirement.Text = cleanRequirementText(requirement.Text)

		if !validCanonicalRequirement(requirement, sourceText) {
			continue
		}

		validated = appendValidatedRequirement(
			validated,
			requirement,
		)
	}

	return validated
}

func validCanonicalRequirement(
	requirement Requirement,
	sourceText string,
) bool {
	if requirement.Text == "" {
		return false
	}

	if !validRequirementCategory(requirement.Category) {
		return false
	}

	if !validRequirementImportance(requirement.Importance) {
		return false
	}

	if !validRequirementText(requirement.Text) {
		return false
	}

	if isIncompleteRequirementFragment(requirement.Text) {
		return false
	}

	if containsRejectedSectionContent(requirement.Text) {
		return false
	}

	if sourceText != "" &&
		!requirementTraceableToSource(requirement.Text, sourceText) {
		return false
	}

	return true
}

func validRequirementCategory(
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

func validRequirementImportance(
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

// requirementTraceableToSource ensures validation never invents
// qualification text. Formatting differences are ignored, but words
// are not added, replaced, corrected, or inferred.
func requirementTraceableToSource(
	requirement string,
	source string,
) bool {
	requirementKey := requirementTraceabilityKey(requirement)
	sourceKey := requirementTraceabilityKey(source)

	if requirementKey == "" || sourceKey == "" {
		return false
	}

	return strings.Contains(sourceKey, requirementKey)
}

func requirementTraceabilityKey(text string) string {
	text = strings.ToLower(text)

	var builder strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}

	return builder.String()
}

// isIncompleteRequirementFragment rejects structurally broken atoms.
// Short qualifications such as CPA or RN remain valid.
func isIncompleteRequirementFragment(text string) bool {
	text = cleanRequirementText(text)

	if text == "" {
		return true
	}

	lower := strings.ToLower(text)
	fields := strings.Fields(lower)

	if len(fields) == 0 {
		return true
	}

	// A qualification ending with a connector is structurally incomplete.
	// This is generic and does not depend on a company, provider, job,
	// industry, or particular qualification phrase.
	last := strings.Trim(
		fields[len(fields)-1],
		" \t\r\n.,;:!?()[]{}",
	)

	switch last {
	case "and",
		"or",
		"with",
		"including":
		return true
	}

	// Handle compound connectors that may survive tokenization.
	if strings.HasSuffix(lower, "and/or") ||
		strings.HasSuffix(lower, "such as") {
		return true
	}

	// Section headings themselves are structural metadata,
	// not candidate requirements.
	normalized := normalizeHeading(lower)

	switch normalized {
	case "qualification",
		"qualifications",
		"requirement",
		"requirements",
		"required",
		"desired",
		"preferred",
		"minimum",
		"additional skills",
		"technical skills",
		"required skills",
		"preferred skills",
		"skills and qualifications",
		"knowledge skills and abilities",
		"knowledge skills abilities",
		"knowledge and skills",
		"preferred qualifications",
		"minimum qualifications",
		"required qualifications",
		"basic qualifications",
		"preferred requirements",
		"minimum requirements",
		"required requirements",
		"education",
		"experience",
		"education experience",
		"skills",
		"competencies",
		"certifications",
		"physical requirements",
		"physical demands",
		"travel requirements":
		return true
	}

	return false
}

// containsRejectedSectionContent provides a defensive boundary if
// malformed provider text leaks non-qualification prose into extraction.
func containsRejectedSectionContent(text string) bool {
	lower := strings.ToLower(
		cleanRequirementText(text),
	)

	if lower == "" {
		return true
	}

	markers := []string{
		"equal opportunity employer",
		"affirmative action employer",
		"all qualified applicants will receive consideration",
		"without regard to race",
		"reasonable accommodations may be made",
		"position description is intended to convey",
		"not intended to be an exhaustive list",
		"base salary applies",
		"base pay offered may vary",
		"incentive plans, bonuses",
		"other forms of compensation",
		"health and welfare",
		"weekly activity expectations",
		"this description outlines",
		"this job description outlines",
		"not a comprehensive listing",
		"not a comprehensive list",
		"duties, responsibilities and activities may change",
		"duties responsibilities and activities may change",
		"thank you,",
		"| recruiter |",
		"recruiter |",
		"mobile:",
		"phone:",
		"telephone:",
		"fax:",
		"life at ",
		"visit ",
		"follow us on social media",
	}

	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	return false
}

func appendValidatedRequirement(
	requirements []Requirement,
	requirement Requirement,
) []Requirement {
	requirement.Text = cleanRequirementText(
		requirement.Text,
	)

	if requirement.Text == "" {
		return requirements
	}

	key := requirementTraceabilityKey(
		requirement.Text,
	)

	if key == "" {
		return requirements
	}

	for _, existing := range requirements {
		if requirementTraceabilityKey(existing.Text) == key &&
			existing.Category == requirement.Category &&
			existing.Importance == requirement.Importance {
			return requirements
		}
	}

	return append(requirements, requirement)
}
