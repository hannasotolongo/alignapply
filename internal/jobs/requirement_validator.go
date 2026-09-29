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

	if isStructurallyContaminatedRequirement(requirement.Text) {
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

// isCandidateFitQualification identifies requirement categories that describe
// evidence about the candidate and can therefore participate in Your Fit.
//
// This is intentionally occupation-agnostic. It does not contain job titles,
// industries, professions, technologies, credentials, or domain vocabulary.
func isCandidateFitQualification(
	category RequirementCategory,
) bool {
	switch category {
	case RequirementSkill,
		RequirementExperience,
		RequirementEducation,
		RequirementLicense,
		RequirementCertification:
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

// isIncompleteRequirementFragment rejects structurally broken atoms while
// preserving genuinely short qualifications such as CPA, RN, SQL, or AWS.
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

	// Reject meaningless one-character atoms caused by broken provider
	// formatting or extraction boundaries.
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 1 && unicode.IsLetter(runes[0]) {
		return true
	}

	// Reject punctuation-only fragments.
	hasLetterOrDigit := false
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasLetterOrDigit = true
			break
		}
	}

	if !hasLetterOrDigit {
		return true
	}

	// Reject requirements that end in a connector and therefore appear
	// to have been cut off before the qualification was complete.
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

	if strings.HasSuffix(lower, "and/or") ||
		strings.HasSuffix(lower, "such as") {
		return true
	}

	// A dangling slash usually means a compound phrase was split at
	// the extraction boundary.
	if strings.HasSuffix(strings.TrimSpace(text), "/") {
		return true
	}

	// Section headings are structural metadata, not candidate requirements.
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
		"degree",
		"degrees",
		"education",
		"experience",
		"education experience",
		"skill",
		"skills",
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
		"competencies",
		"certification",
		"certifications",
		"license",
		"licenses",
		"physical requirements",
		"physical demands",
		"travel requirements":
		return true
	}

	return false
}

// isStructurallyContaminatedRequirement is a conservative final atomicity
// check. It does not attempt to repair or rewrite extracted text.
//
// A canonical requirement should represent one bounded qualification. Some
// providers flatten several originally separate blocks into one line. When
// that happens, extraction can occasionally return a valid qualification
// followed by another qualification, a section transition, or recruiting
// prose as one large atom.
//
// We reject only when there is strong structural evidence that the atom
// contains multiple independent pieces. If the boundary is uncertain, the
// text is left unchanged.
func isStructurallyContaminatedRequirement(text string) bool {
	text = cleanRequirementText(text)

	if text == "" {
		return true
	}

	fields := strings.Fields(text)

	// Ordinary-sized atomic requirements should pass through this
	// defensive check. Other validator rules still apply to them.
	if len(fields) < 18 && len(text) < 140 {
		return false
	}

	lower := strings.ToLower(text)

	// Structural labels embedded after substantive qualification text are
	// strong evidence that provider formatting collapsed a section boundary.
	embeddedSectionTransitions := []string{
		" qualifications:",
		" requirements:",
		" required qualifications:",
		" preferred qualifications:",
		" minimum qualifications:",
		" basic qualifications:",
		" preferred requirements:",
		" required skills:",
		" preferred skills:",
		" technical skills:",
		" additional skills:",
		" education:",
		" experience:",
		" certifications:",
		" licenses:",
		" physical requirements:",
		" physical demands:",
		" travel requirements:",
	}

	for _, marker := range embeddedSectionTransitions {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	// Two internal sentence boundaries represent at least three flattened
	// sentence-like units even when the final unit has no trailing period.
	if completedSentenceCount(text) >= 2 {
		return true
	}

	// Providers frequently preserve semicolons when flattening bullet lists.
	// Three or more meaningful semicolon-delimited clauses should not be
	// scored as one canonical qualification.
	if independentSemicolonClauseCount(text) >= 3 {
		return true
	}

	// For larger atoms, multiple qualification-style starts at structural
	// boundaries are additional evidence that separate requirements were
	// collapsed into one.
	if len(fields) >= 25 || len(text) >= 180 {
		if independentRequirementStartCount(text) >= 2 {
			return true
		}
	}

	return false
}

func independentSemicolonClauseCount(text string) int {
	parts := strings.Split(text, ";")

	if len(parts) < 2 {
		return 1
	}

	count := 0

	for _, part := range parts {
		part = cleanRequirementText(part)

		if part == "" {
			continue
		}

		if validRequirementText(part) {
			count++
		}
	}

	return count
}

func completedSentenceCount(text string) int {
	count := 0

	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '.', '!', '?':
			// Decimal points and similar punctuation inside tokens should not
			// count as sentence boundaries.
			if i+1 < len(text) &&
				!unicode.IsSpace(rune(text[i+1])) {
				continue
			}

			count++
		}
	}

	return count
}

func independentRequirementStartCount(text string) int {
	lower := strings.ToLower(text)

	markers := []string{
		"ability to ",
		"ability ",
		"experience with ",
		"experience in ",
		"experience ",
		"proficiency ",
		"proficient ",
		"familiarity with ",
		"familiarity ",
		"knowledge of ",
		"knowledge ",
		"strong ",
		"excellent ",
		"demonstrated ",
		"proven ",
		"valid ",
		"licensed ",
		"certified ",
		"certification ",
		"willingness to ",
		"willingness ",
		"must ",
		"required ",
		"preferred ",
	}

	count := 0

	for _, marker := range markers {
		searchFrom := 0

		for searchFrom < len(lower) {
			relative := strings.Index(
				lower[searchFrom:],
				marker,
			)

			if relative < 0 {
				break
			}

			index := searchFrom + relative

			// Only count starts occurring at a plausible clause boundary.
			if index == 0 ||
				isStructuralClauseBoundary(lower, index) {
				count++
			}

			searchFrom = index + len(marker)
		}
	}

	return count
}

func isStructuralClauseBoundary(
	text string,
	index int,
) bool {
	if index <= 0 || index > len(text) {
		return index == 0
	}

	i := index - 1

	for i >= 0 && unicode.IsSpace(rune(text[i])) {
		i--
	}

	if i < 0 {
		return true
	}

	switch text[i] {
	case '.', '!', '?', ';', ':':
		return true
	default:
		return false
	}
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
