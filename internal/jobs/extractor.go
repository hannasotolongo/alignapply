package jobs

import (
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type extractionSection struct {
	Category   RequirementCategory
	Importance RequirementImportance
	Accept     bool
}

type headingDefinition struct {
	Name    string
	Section extractionSection
}

type headingMatch struct {
	Start   int
	End     int
	Name    string
	Section extractionSection
}

type sectionSpan struct {
	Heading string
	Text    string
	Section extractionSection
}

var (
	htmlTagRE = regexp.MustCompile(`<[^>]*>`)

	yearsRE = regexp.MustCompile(
		`(?i)\b(?:\d+\s+plus|\d+\+?|\d+\s*[-–—]\s*\d+)\s*(?:years?|yrs?)\b`,
	)

	experienceRE = regexp.MustCompile(
		`(?i)\b(?:experience|experienced|exp)\b`,
	)

	educationRE = regexp.MustCompile(
		`(?i)\b(high school|ged|diploma|associate(?:'|’)?s?\s+degree|bachelor(?:'|’)?s?|master(?:'|’)?s?|doctorate|ph\.?d\.?|degree)\b`,
	)

	licenseRE = regexp.MustCompile(
		`(?i)\b(driver(?:'|’)?s?\s+licen[cs]e|driver\s+licen[cs]e|licensure|licensed)\b`,
	)

	certificationRE = regexp.MustCompile(
		`(?i)\b(certification|certified|certificate)\b`,
	)

	travelRE = regexp.MustCompile(
		`(?i)\b(travel|relocat(?:e|ion))\b`,
	)

	physicalRE = regexp.MustCompile(
		`(?i)\b(lift|lifting|pounds?|lbs?|standing|walking|sitting|carry|carrying|physical demands?|physical requirements?)\b`,
	)

	skillRE = regexp.MustCompile(
		`(?i)\b(skill|skills|ability|knowledge|proficient|proficiency|communication|communicate|computer|customer service|negotiation|negotiate|leadership|relationship[- ]building|work independently|problem[- ]solving|microsoft office|excel|salesforce|crm|bilingual|organizational|organization)\b`,
	)

	preferredRE = regexp.MustCompile(
		`(?i)\b(preferred|a plus|is a plus|desired|nice[- ]to[- ]have)\b`,
	)

	numericOnlyRE = regexp.MustCompile(`^\d+$`)
)

func makeSection(
	category RequirementCategory,
	importance RequirementImportance,
	accept bool,
) extractionSection {
	return extractionSection{
		Category:   category,
		Importance: importance,
		Accept:     accept,
	}
}

var acceptedHeadings = []headingDefinition{
	// Natural-language and tool/skill qualification sections seen across ATS providers.
	{
		Name:    "we are looking for a person who has",
		Section: makeSection(RequirementSkill, RequirementRequired, true),
	},
	{
		Name:    "your toolbox",
		Section: makeSection(RequirementSkill, RequirementRequired, true),
	},
	{
		Name:    "additional skills",
		Section: makeSection(RequirementSkill, RequirementRequired, true),
	},
	// Explicit experience requirements.
	{
		Name:    "preferred years of work experience",
		Section: makeSection(RequirementExperience, RequirementPreferred, true),
	},
	{
		Name:    "minimum years of work experience",
		Section: makeSection(RequirementExperience, RequirementRequired, true),
	},
	{
		Name:    "preferred leadership experience",
		Section: makeSection(RequirementExperience, RequirementPreferred, true),
	},
	{
		Name:    "minimum leadership experience",
		Section: makeSection(RequirementExperience, RequirementRequired, true),
	},

	// General qualification sections.
	{
		Name:    "preferred qualifications",
		Section: makeSection(RequirementOther, RequirementPreferred, true),
	},
	{
		Name:    "desired qualifications",
		Section: makeSection(RequirementOther, RequirementPreferred, true),
	},
	{
		Name:    "minimum qualifications",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},
	{
		Name:    "required qualifications",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},
	{
		Name:    "basic qualifications",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},
	{
		Name:    "preferred requirements",
		Section: makeSection(RequirementOther, RequirementPreferred, true),
	},
	{
		Name:    "minimum requirements",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},
	{
		Name:    "required requirements",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},
	{
		Name:    "preferred requirements and qualifications",
		Section: makeSection(RequirementOther, RequirementPreferred, true),
	},
	{
		Name:    "requirements and qualifications",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},

	// Education.
	{
		Name:    "preferred education",
		Section: makeSection(RequirementEducation, RequirementPreferred, true),
	},
	{
		Name:    "minimum education",
		Section: makeSection(RequirementEducation, RequirementRequired, true),
	},
	{
		Name:    "required education",
		Section: makeSection(RequirementEducation, RequirementRequired, true),
	},
	{
		Name:    "education requirements",
		Section: makeSection(RequirementEducation, RequirementRequired, true),
	},
	{
		Name:    "education/experience",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},
	{
		Name:    "education and experience",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},

	// Experience.
	{
		Name:    "preferred experience",
		Section: makeSection(RequirementExperience, RequirementPreferred, true),
	},
	{
		Name:    "minimum experience",
		Section: makeSection(RequirementExperience, RequirementRequired, true),
	},
	{
		Name:    "required experience",
		Section: makeSection(RequirementExperience, RequirementRequired, true),
	},
	{
		Name:    "experience requirements",
		Section: makeSection(RequirementExperience, RequirementRequired, true),
	},
	{
		Name:    "experience required",
		Section: makeSection(RequirementExperience, RequirementRequired, true),
	},

	// Skills, knowledge, abilities, and competencies.
	{
		Name:    "technical skills",
		Section: makeSection(RequirementSkill, RequirementRequired, true),
	},
	{
		Name:    "required skills",
		Section: makeSection(RequirementSkill, RequirementRequired, true),
	},
	{
		Name:    "preferred skills",
		Section: makeSection(RequirementSkill, RequirementPreferred, true),
	},
	{
		Name:    "skills and qualifications",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},
	{
		Name:    "knowledge skills and abilities",
		Section: makeSection(RequirementSkill, RequirementRequired, true),
	},
	{
		Name:    "knowledge skills abilities",
		Section: makeSection(RequirementSkill, RequirementRequired, true),
	},
	{
		Name:    "knowledge and skills",
		Section: makeSection(RequirementSkill, RequirementRequired, true),
	},
	{
		Name:    "competencies",
		Section: makeSection(RequirementSkill, RequirementRequired, true),
	},

	// Candidate-profile style qualification headings.
	{
		Name:    "who you are",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},
	{
		Name:    "what you bring",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},
	{
		Name:    "what you need",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},
	{
		Name:    "what we're looking for",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},
	{
		Name:    "what we’re looking for",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},
	{
		Name:    "ideal candidate",
		Section: makeSection(RequirementOther, RequirementPreferred, true),
	},

	// Credentials.
	{
		Name:    "certifications",
		Section: makeSection(RequirementCertification, RequirementRequired, true),
	},
	{
		Name:    "certification requirements",
		Section: makeSection(RequirementCertification, RequirementRequired, true),
	},
	{
		Name:    "licenses",
		Section: makeSection(RequirementLicense, RequirementRequired, true),
	},
	{
		Name:    "license requirements",
		Section: makeSection(RequirementLicense, RequirementRequired, true),
	},
	{
		Name:    "licenses and certifications",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},

	// Physical and travel requirements.
	{
		Name:    "physical requirements",
		Section: makeSection(RequirementPhysical, RequirementRequired, true),
	},
	{
		Name:    "physical demands",
		Section: makeSection(RequirementPhysical, RequirementRequired, true),
	},
	{
		Name:    "travel requirements",
		Section: makeSection(RequirementTravel, RequirementRequired, true),
	},

	// Broad fallbacks intentionally last.
	{
		Name:    "qualifications",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},
	{
		Name:    "requirements",
		Section: makeSection(RequirementOther, RequirementRequired, true),
	},
}

var rejectedHeadings = []string{
	// Responsibilities and duties.
	"additional job responsibilities include",
	"additional job responsibilities",
	"position responsibilities",
	"primary responsibilities",
	"key responsibilities",
	"core responsibilities",
	"essential responsibilities",
	"job responsibilities",
	"responsibilities",
	"essential functions",
	"essential duties",
	"primary duties",
	"key duties",
	"job duties",
	"duties",
	"what you will do",
	"what you'll do",
	"what you’ll do",
	"what you will be doing",
	"what you'll be doing",
	"what you’ll be doing",
	"day to day",
	"day-to-day",

	// Role description and organizational context.
	"direct manager/direct reports",
	"position purpose",
	"position overview",
	"role overview",
	"job overview",
	"position summary",
	"role summary",
	"job summary",
	"job description",
	"about the position",
	"about the role",

	// Work environment and logistics.
	"working conditions",
	"work environment",
	"work schedule",
	"schedule",
	"hours",
	"location",

	// Compensation and benefits.
	"benefits",
	"our benefits",
	"what we offer",
	"benefits and perks",
	"perks and benefits",
	"compensation and benefits",
	"compensation",
	"salary range",
	"pay range",
	"salary",
	"pay",

	// Company and marketing content.
	"company description",
	"company overview",
	"about the company",
	"about us",
	"who we are",
	"our company",
	"our mission",
	"our culture",
	"why work here",
	"why work with us",
	"why join us",
	"why join",

	// Application/contact/recruiting content.
	"how to apply",
	"application process",
	"application instructions",
	"contact information",
	"recruiter contact",
	"recruiting contact",

	// Legal and boilerplate.
	"equal employment opportunity",
	"equal opportunity statement",
	"equal opportunity employer",
	"equal opportunity",
	"eeo statement",
	"eeo",
	"disclaimer",
}

func EnrichJob(job Job) Job {
	job.Requirements = ExtractRequirements(job.Description)

	job.Requirements = ValidateRequirements(
		job.Requirements,
		job.Description,
	)

	rebuildLegacyRequirementFields(&job)

	return job
}

func ExtractRequirements(description string) []Requirement {
	text := prepareRequirementText(description)
	if text == "" {
		return []Requirement{}
	}

	spans := extractSectionSpans(text)
	if len(spans) == 0 {
		return []Requirement{}
	}

	requirements := make([]Requirement, 0)

	for _, span := range spans {
		if !span.Section.Accept {
			continue
		}

		atoms := atomizeSection(span)

		for _, atom := range atoms {
			addRequirement(
				&requirements,
				atom,
				span.Heading,
				span.Section,
			)
		}
	}

	return requirements
}

func extractSectionSpans(text string) []sectionSpan {
	matches := findAllSectionHeadings(text)

	if len(matches) == 0 {
		return nil
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Start == matches[j].Start {
			return matches[i].End > matches[j].End
		}

		return matches[i].Start < matches[j].Start
	})

	filtered := make([]headingMatch, 0, len(matches))

	for _, match := range matches {
		if len(filtered) == 0 {
			filtered = append(filtered, match)
			continue
		}

		last := filtered[len(filtered)-1]

		if match.Start < last.End {
			continue
		}

		filtered = append(filtered, match)
	}

	spans := make([]sectionSpan, 0)

	for i, match := range filtered {
		end := len(text)

		if i+1 < len(filtered) {
			end = filtered[i+1].Start
		}

		body := strings.TrimSpace(text[match.End:end])
		body = truncateMarketingProse(body)

		if !match.Section.Accept || body == "" {
			continue
		}

		spans = append(spans, sectionSpan{
			Heading: match.Name,
			Text:    body,
			Section: match.Section,
		})
	}

	return spans
}

func findAllSectionHeadings(text string) []headingMatch {
	matches := make([]headingMatch, 0)

	for _, definition := range acceptedHeadings {
		matches = append(
			matches,
			findHeadingMatches(
				text,
				definition.Name,
				definition.Section,
			)...,
		)
	}

	rejectedSection := makeSection(
		RequirementOther,
		RequirementRequired,
		false,
	)

	for _, heading := range rejectedHeadings {
		matches = append(
			matches,
			findHeadingMatches(
				text,
				heading,
				rejectedSection,
			)...,
		)
	}

	return matches
}

func findHeadingMatches(
	text string,
	name string,
	section extractionSection,
) []headingMatch {
	lower := strings.ToLower(text)
	needle := strings.ToLower(name)

	result := make([]headingMatch, 0)
	offset := 0

	for offset < len(lower) {
		index := strings.Index(lower[offset:], needle)

		if index < 0 {
			break
		}

		start := offset + index
		nameEnd := start + len(needle)

		if !validHeadingStart(lower, start) {
			offset = nameEnd
			continue
		}

		end, ok := validHeadingEnd(text, start, nameEnd, name)

		if !ok {
			offset = nameEnd
			continue
		}

		result = append(result, headingMatch{
			Start:   start,
			End:     end,
			Name:    name,
			Section: section,
		})

		offset = end

		if offset <= start {
			offset = nameEnd
		}
	}

	return result
}

func validHeadingStart(text string, start int) bool {
	if start == 0 {
		return true
	}

	previous := rune(text[start-1])

	return unicode.IsSpace(previous) ||
		previous == '.' ||
		previous == ';' ||
		previous == ':' ||
		previous == ')' ||
		previous == ']' ||
		previous == '•' ||
		previous == '▪' ||
		previous == '●' ||
		previous == '◦' ||
		previous == '‣'
}

func validHeadingEnd(text string, start int, nameEnd int, name string) (int, bool) {
	if nameEnd >= len(text) {
		return nameEnd, true
	}

	i := nameEnd

	// Providers frequently normalize headings into forms such as:
	//
	// Qualifications:
	// Qualifications •
	// Qualifications  •
	//
	// Permit horizontal whitespace before the delimiter.
	for i < len(text) {
		switch text[i] {
		case ' ', '\t':
			i++
		default:
			goto delimiter
		}
	}

delimiter:
	if i >= len(text) {
		return i, true
	}

	switch text[i] {
	case ':':
		return i + 1, true

	case '\n', '\r':
		return i, true
	}

	remainder := text[i:]

	for _, bullet := range []string{
		"•",
		"▪",
		"●",
		"◦",
		"‣",
	} {
		if strings.HasPrefix(remainder, bullet) {
			return i + len(bullet), true
		}
	}

	// Some providers flatten section headings into a single line, for example:
	// "Qualifications Excellent communication..." or
	// "Compensation Competitive pay...". Allow this only for headings that
	// commonly appear as standalone provider section labels. Word-boundary
	// checks in validHeadingStart plus the heading whitelist keep this scoped.
	if i > nameEnd &&
		allowsFlattenedHeading(name) &&
		looksLikePlausibleFlattenedHeading(text, start, i, name) {
		return i, true
	}

	return nameEnd, false
}

func looksLikePlausibleFlattenedHeading(
	text string,
	start int,
	bodyStart int,
	name string,
) bool {
	if start < 0 || bodyStart < 0 || bodyStart > len(text) {
		return false
	}

	body := strings.TrimSpace(text[bodyStart:])
	if body == "" {
		return true
	}

	normalized := normalizeHeading(name)

	// Rejected headings are section terminators. Providers frequently flatten
	// them onto the same line as their body, so recognizing them is necessary
	// to stop qualification extraction before responsibilities, compensation,
	// benefits, legal text, and other non-qualification content.
	for _, rejected := range rejectedHeadings {
		if normalized == normalizeHeading(rejected) {
			return true
		}
	}

	// Broad qualification headings must be followed by text that looks like
	// candidate evidence. This permits provider-flattened forms such as
	// "Qualifications Excellent interpersonal..." without treating an
	// ordinary occurrence of "requirements" inside prose as a section.
	if isLikelyRequirementStart(body) {
		return true
	}

	lower := strings.ToLower(body)

	prefixes := []string{
		"excellent ",
		"strong ",
		"superior ",
		"outstanding ",
		"proven ",
		"demonstrated ",
		"self-starter",
		"self starter",
		"ability ",
		"knowledge ",
		"proficiency ",
		"proficient ",
		"bachelor",
		"master",
		"doctorate",
		"associate degree",
		"high school",
		"ged",
		"experience ",
		"inside sales ",
		"must ",
		"required ",
		"minimum ",
		"preferred ",
		"valid ",
		"licensed ",
		"certified ",
		"certification ",
		"willingness ",
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}

	return false
}

func allowsFlattenedHeading(name string) bool {
	switch normalizeHeading(name) {
	case
		// Qualification sections.
		"qualifications",
		"requirements",
		"preferred qualifications",
		"desired qualifications",
		"minimum qualifications",
		"required qualifications",
		"basic qualifications",
		"preferred requirements",
		"minimum requirements",
		"required requirements",
		"requirements and qualifications",
		"preferred requirements and qualifications",

		// Education and experience.
		"education experience",
		"education and experience",
		"education requirements",
		"preferred education",
		"minimum education",
		"required education",
		"experience requirements",
		"experience required",
		"preferred experience",
		"minimum experience",
		"required experience",

		// Skills and candidate profile.
		"technical skills",
		"required skills",
		"preferred skills",
		"skills and qualifications",
		"knowledge skills and abilities",
		"knowledge skills abilities",
		"knowledge and skills",
		"competencies",
		"who you are",
		"what you bring",
		"what you need",
		"what we're looking for",
		"what we’re looking for",
		"ideal candidate",
		"we are looking for a person who has",
		"your toolbox",
		"additional skills",

		// Credentials / physical / travel.
		"certifications",
		"certification requirements",
		"licenses",
		"license requirements",
		"licenses and certifications",
		"physical requirements",
		"physical demands",
		"travel requirements",

		// Section terminators.
		"responsibilities",
		"job responsibilities",
		"key responsibilities",
		"primary responsibilities",
		"core responsibilities",
		"essential responsibilities",
		"position responsibilities",
		"additional job responsibilities",
		"additional job responsibilities include",
		"essential functions",
		"job duties",
		"duties",
		"what you will do",
		"what you'll do",
		"what you’ll do",
		"what you will be doing",
		"what you'll be doing",
		"what you’ll be doing",
		"benefits",
		"our benefits",
		"what we offer",
		"benefits and perks",
		"perks and benefits",
		"compensation",
		"compensation and benefits",
		"salary",
		"salary range",
		"pay",
		"pay range",
		"work environment",
		"working conditions",
		"about the company",
		"about us",
		"company description",
		"company overview",
		"how to apply",
		"application process",
		"contact information",
		"equal employment opportunity",
		"equal opportunity statement",
		"equal opportunity employer",
		"equal opportunity",
		"eeo statement",
		"eeo",
		"disclaimer":
		return true

	default:
		return false
	}
}

func atomizeSection(span sectionSpan) []string {
	text := strings.TrimSpace(span.Text)

	if text == "" {
		return nil
	}

	if isExplicitNoneValue(text) {
		return nil
	}

	if isYearsOfWorkExperienceHeading(span.Heading) {
		if number, ok := leadingNumericValue(text); ok {
			return []string{
				strconv.Itoa(number) + " years of work experience",
			}
		}
	}

	if isLeadershipExperienceHeading(span.Heading) &&
		isExplicitNoneValue(text) {
		return nil
	}

	if span.Section.Category == RequirementEducation {
		text = cleanRequirementText(text)

		if isExplicitNoneValue(text) || !validRequirementText(text) {
			return nil
		}

		return []string{text}
	}

	text = normalizeListSeparators(text)

	chunks := strings.Split(text, "\n")
	result := make([]string, 0)

	for _, chunk := range chunks {
		chunk = cleanRequirementText(chunk)

		if chunk == "" ||
			isExplicitNoneValue(chunk) ||
			isMarketingOrCompanyProse(chunk) {
			continue
		}

		result = append(
			result,
			atomizeFlattenedChunk(
				chunk,
				span.Section,
			)...,
		)
	}

	return uniqueAtomicStrings(result)
}

func normalizeListSeparators(text string) string {
	// Convert provider list delimiters into explicit boundaries.
	// The replacement preserves the surrounding word boundary rather
	// than concatenating text on either side of a bullet or semicolon.
	return strings.NewReplacer(
		"\r\n", "\n",
		"\r", "\n",
		"•", "\n",
		"▪", "\n",
		"●", "\n",
		"◦", "\n",
		"‣", "\n",
		"\t", " ",
	).Replace(text)
}

func atomizeFlattenedChunk(
	text string,
	section extractionSection,
) []string {
	text = cleanRequirementText(text)

	if text == "" ||
		isExplicitNoneValue(text) ||
		isMarketingOrCompanyProse(text) {
		return nil
	}

	boundaries := semanticBoundaries(text, section)

	if len(boundaries) <= 1 {
		if validRequirementText(text) {
			return []string{text}
		}

		return nil
	}

	result := make([]string, 0, len(boundaries))

	for i, start := range boundaries {
		end := len(text)

		if i+1 < len(boundaries) {
			end = boundaries[i+1]
		}

		value := cleanRequirementText(text[start:end])

		if value == "" ||
			isExplicitNoneValue(value) ||
			isMarketingOrCompanyProse(value) ||
			!validRequirementText(value) {
			continue
		}

		result = append(result, value)
	}

	return result
}

func semanticBoundaries(
	text string,
	section extractionSection,
) []int {
	if strings.TrimSpace(text) == "" {
		return []int{0}
	}

	positions := []int{0}

	addYearExpressionBoundaries(&positions, text)
	addSentenceBoundaries(&positions, text)
	addExplicitClauseBoundaries(&positions, text, section)
	addRequirementGrammarBoundaries(&positions, text, section)
	addCategoryTransitionBoundaries(&positions, text)

	return normalizedBoundaryPositions(
		positions,
		len(text),
	)
}

func addExplicitClauseBoundaries(
	positions *[]int,
	text string,
	section extractionSection,
) {
	for index, r := range text {
		if r != ';' {
			continue
		}

		start := index + 1
		for start < len(text) && unicode.IsSpace(rune(text[start])) {
			start++
		}
		if start >= len(text) {
			continue
		}

		left := cleanRequirementText(text[:index])
		right := cleanRequirementText(text[start:])
		if !validRequirementText(left) || !validRequirementText(right) {
			continue
		}

		lower := strings.ToLower(right)
		independentPrefixes := []string{
			"ability ", "ability to ", "knowledge ", "knowledge of ",
			"experience ", "experience with ", "experience in ",
			"proficiency ", "proficient ", "familiarity ", "familiarity with ",
			"strong ", "excellent ", "demonstrated ", "proven ",
			"valid ", "licensed ", "certified ", "certification ",
			"willingness ", "must ", "required ", "preferred ",
		}

		independent := false
		for _, prefix := range independentPrefixes {
			if strings.HasPrefix(lower, prefix) {
				independent = true
				break
			}
		}
		if !independent && isLikelyRequirementStart(right) {
			independent = true
		}
		if independent {
			*positions = append(*positions, start)
		}
	}
}

func addCategoryTransitionBoundaries(
	positions *[]int,
	text string,
) {
	words := wordStartPositions(text)

	for _, start := range words {
		if start <= 0 || start >= len(text) {
			continue
		}

		if !isSafeRequirementBoundary(text, start) ||
			startsInsideNumericRange(text, start) {
			continue
		}

		before := strings.TrimSpace(text[:start])
		after := strings.TrimSpace(text[start:])

		if before == "" || after == "" {
			continue
		}

		// The preceding clause must contain an education qualification.
		if !educationRE.MatchString(before) {
			continue
		}

		// The new clause must independently contain experience evidence.
		experienceIndex := experienceRE.FindStringIndex(after)

		if experienceIndex == nil {
			continue
		}

		// Do not look through the end of one qualification to borrow
		// "experience" from a later qualification.
		//
		// Example:
		//   Bachelor's Degree with emphasis in Life Sciences,
		//   Medicine, or Business preferred Experience selling...
		//
		// "Sciences" is still part of the education requirement because
		// "preferred" closes that qualification before the later
		// experience requirement begins.
		beforeExperience := after[:experienceIndex[0]]

		if preferredRE.MatchString(beforeExperience) {
			continue
		}

		lowerAfter := strings.ToLower(after)

		if strings.Contains(lowerAfter, "equivalent experience") ||
			strings.Contains(lowerAfter, "equivalent practical experience") ||
			strings.Contains(lowerAfter, "equivalent professional experience") ||
			strings.Contains(lowerAfter, "equivalent work experience") {
			continue
		}

		// Do not split in the middle of a degree phrase such as:
		// "degree in Business", "Communications or Marketing", etc.
		if !previousTextLooksComplete(before) {
			continue
		}

		// A category transition should begin like a real new clause.
		// Capitalization is useful here because flattened providers often
		// remove bullets/newlines while preserving the original clause case.
		runes := []rune(after)
		if len(runes) == 0 || !unicode.IsUpper(runes[0]) {
			continue
		}

		// Require the suffix itself to classify as experience. This prevents
		// unrelated capitalized words inside an education phrase from
		// becoming boundaries merely because "experience" occurs later.
		if classifyRequirement(
			after,
			RequirementOther,
		) != RequirementExperience {
			continue
		}

		*positions = append(*positions, start)

		// Once the education -> experience transition is found, later
		// splitting is handled by the normal boundary passes.
		return
	}
}

func addYearExpressionBoundaries(
	positions *[]int,
	text string,
) {
	matches := yearsRE.FindAllStringIndex(text, -1)

	for _, match := range matches {
		start := match[0]

		// Do not create a boundary inside a numeric range.
		//
		// Example:
		//   0 - 2 years of experience
		//
		// "2 years" is part of the range, not a new requirement.
		if startsInsideNumericRange(text, start) {
			continue
		}

		after := strings.ToLower(
			text[start:minInt(len(text), start+60)],
		)

		if strings.Contains(after, "years of age") {
			continue
		}

		if start > 0 &&
			isSafeRequirementBoundary(text, start) {
			before := strings.TrimSpace(text[:start])
			if leadingYearsModifier(before) {
				continue
			}
			*positions = append(*positions, start)
		}
	}
}

func leadingYearsModifier(before string) bool {
	lower := strings.ToLower(cleanRequirementText(before))
	if lower == "" {
		return false
	}

	modifiers := []string{
		"around",
		"approximately",
		"about",
		"at least",
		"minimum",
		"minimum of",
		"more than",
		"over",
		"up to",
		"preferred",
		"required",
	}

	for _, modifier := range modifiers {
		if lower == modifier || strings.HasSuffix(lower, " "+modifier) {
			return true
		}
	}

	return false
}

func startsInsideNumericRange(
	text string,
	start int,
) bool {
	if start <= 0 || start > len(text) {
		return false
	}

	// Inspect the characters immediately before the numeric expression.
	// Providers commonly emit all of these:
	//
	//   0 - 2 years
	//   0-2 years
	//   0–2 years
	//   0—2 years
	//
	// The second number must never become a separate requirement.
	before := strings.TrimSpace(text[:start])

	if before == "" {
		return false
	}

	runes := []rune(before)

	i := len(runes) - 1
	for i >= 0 && unicode.IsSpace(runes[i]) {
		i--
	}

	if i < 0 {
		return false
	}

	if runes[i] != '-' &&
		runes[i] != '–' &&
		runes[i] != '—' {
		return false
	}

	i--

	for i >= 0 && unicode.IsSpace(runes[i]) {
		i--
	}

	if i < 0 || !unicode.IsDigit(runes[i]) {
		return false
	}

	for i >= 0 && unicode.IsDigit(runes[i]) {
		i--
	}

	// A digit immediately before the range separator is sufficient.
	// Earlier text is irrelevant; it may contain modifiers such as
	// "minimum" or other qualification prose.
	return true
}

func addSentenceBoundaries(
	positions *[]int,
	text string,
) {
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '.', '!', '?':
			next := nextNonSpaceIndex(text, i+1)

			if next <= 0 || next >= len(text) {
				continue
			}

			if !isLikelyRequirementStart(
				text[next:],
			) {
				continue
			}

			*positions = append(*positions, next)
		}
	}
}

func addRequirementGrammarBoundaries(
	positions *[]int,
	text string,
	section extractionSection,
) {
	words := wordStartPositions(text)

	for _, start := range words {
		if start == 0 {
			continue
		}

		if !isSafeRequirementBoundary(text, start) {
			continue
		}

		if startsInsideNumericRange(text, start) {
			continue
		}

		remaining := text[start:]

		if match := yearsRE.FindStringIndex(remaining); match != nil && match[0] == 0 && leadingYearsModifier(text[:start]) {
			continue
		}

		if !isLikelyRequirementStart(remaining) {
			continue
		}

		// A word that looks like a requirement start can also be the
		// natural completion of the requirement immediately before it.
		//
		// Examples:
		//   Strong communication skills
		//   3 years of relevant experience
		//   CPA certification
		//
		// Those must remain intact.
		if candidateCompletesPreviousRequirement(
			text,
			start,
		) {
			continue
		}

		segmentStart := previousBoundaryStart(*positions, start)
		left := cleanRequirementText(text[segmentStart:start])

		if !previousTextLooksComplete(left) {
			continue
		}

		// A flattened split is valid only when the immediately preceding
		// segment can stand as a qualification on its own. Looking only at
		// that local segment (rather than all text before the candidate) keeps
		// an earlier valid requirement from making a later fragment appear
		// complete.
		if classifyRequirement(left, RequirementOther) == RequirementOther {
			continue
		}

		*positions = append(*positions, start)
	}
	addEmbeddedPreferenceBoundaries(
		positions,
		text,
	)
	if section.Importance == RequirementRequired {
		addRepeatedRequirementMarker(
			positions,
			text,
			"must ",
		)

		addRepeatedRequirementMarker(
			positions,
			text,
			"required ",
		)
	}
}

func previousBoundaryStart(positions []int, start int) int {
	best := 0

	for _, position := range positions {
		if position >= 0 && position < start && position > best {
			best = position
		}
	}

	return best
}

func addEmbeddedPreferenceBoundaries(
	positions *[]int,
	text string,
) {
	lower := strings.ToLower(text)

	markers := []string{
		" as the world's largest",
		" as the world’s largest",
		" all of our associates",
		" is a leading global provider",
		" we are a leading global provider",
		" we are the leading provider",
		" preferred ",
		" desired ",
		" a plus ",
		" nice-to-have ",
		" nice to have ",
	}

	for _, marker := range markers {
		offset := 0

		for offset < len(lower) {
			index := strings.Index(
				lower[offset:],
				marker,
			)

			if index < 0 {
				break
			}

			markerStart := offset + index
			next := markerStart + len(marker)

			if next < len(text) {
				next = nextNonSpaceIndex(
					text,
					next,
				)

				if next < len(text) &&
					isLikelyRequirementStart(text[next:]) {
					*positions = append(
						*positions,
						next,
					)
				}
			}

			offset = markerStart + len(marker)
		}
	}
}
func candidateCompletesPreviousRequirement(
	text string,
	start int,
) bool {
	if start <= 0 || start >= len(text) {
		return false
	}

	before := strings.ToLower(
		strings.TrimSpace(text[:start]),
	)

	after := strings.ToLower(
		strings.TrimSpace(text[start:]),
	)

	if before == "" || after == "" {
		return false
	}

	first := firstWord(after)

	switch first {
	case "skills", "skill":
		return precedingPhraseAcceptsSkillNoun(before)

	case "experience":
		return precedingPhraseAcceptsExperienceNoun(before)

	case "certification", "certified":
		return precedingPhraseAcceptsCertificationNoun(before)

	case "license", "licence", "licensed":
		return precedingPhraseAcceptsLicenseNoun(before)
	}

	return false
}

func firstWord(text string) string {
	fields := strings.Fields(text)

	if len(fields) == 0 {
		return ""
	}

	return strings.Trim(
		fields[0],
		" \t\r\n.,;:!?()[]{}",
	)
}

func precedingPhraseAcceptsSkillNoun(
	text string,
) bool {
	fields := strings.Fields(text)

	if len(fields) == 0 {
		return false
	}

	last := strings.Trim(
		fields[len(fields)-1],
		" \t\r\n.,;:!?()[]{}",
	)

	// These commonly function as modifiers immediately before
	// "skill" or "skills".
	return last == "communication" ||
		last == "interpersonal" ||
		last == "technical" ||
		last == "computer" ||
		last == "organizational" ||
		last == "leadership" ||
		last == "negotiation" ||
		last == "analytical" ||
		last == "problem-solving" ||
		last == "writing"
}

func precedingPhraseAcceptsExperienceNoun(
	text string,
) bool {
	fields := strings.Fields(text)

	if len(fields) == 0 {
		return false
	}

	// A preceding years expression is a strong indication that
	// "experience" completes the current requirement rather than
	// beginning another one.
	windowStart := 0

	if len(fields) > 10 {
		windowStart = len(fields) - 10
	}

	window := strings.Join(
		fields[windowStart:],
		" ",
	)

	if yearsRE.MatchString(window) {
		return true
	}

	last := strings.Trim(
		fields[len(fields)-1],
		" \t\r\n.,;:!?()[]{}",
	)

	switch last {
	case "relevant",
		"professional",
		"industry",
		"management",
		"sales",
		"service",
		"customer-service",
		"technical",
		"clinical",
		"engineering",
		"recruiting",
		"strong",
		"extensive",
		"proven",
		"demonstrated",
		"prior",
		"previous",
		"hands-on",
		"handson":
		return true
	}

	return false
}

func precedingPhraseAcceptsCertificationNoun(
	text string,
) bool {
	fields := strings.Fields(text)

	if len(fields) == 0 {
		return false
	}

	last := strings.Trim(
		fields[len(fields)-1],
		" \t\r\n.,;:!?()[]{}",
	)

	// Short uppercase-style credentials frequently precede the word
	// certification after provider normalization.
	if len(last) >= 2 && len(last) <= 10 {
		return true
	}

	return false
}

func precedingPhraseAcceptsLicenseNoun(
	text string,
) bool {
	fields := strings.Fields(text)

	if len(fields) == 0 {
		return false
	}

	last := strings.Trim(
		fields[len(fields)-1],
		" \t\r\n.,;:!?()[]{}",
	)

	switch last {
	case "driver's",
		"drivers",
		"driver",
		"professional",
		"valid":
		return true
	}

	return false
}
func looksLikeCredentialStart(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))

	if len(fields) < 2 {
		return false
	}

	token := strings.Trim(
		fields[0],
		" \t\r\n.,;:!?()[]{}",
	)

	if len(token) < 2 || len(token) > 10 {
		return false
	}

	hasLetter := false

	for _, r := range token {
		if unicode.IsLetter(r) {
			hasLetter = true

			if !unicode.IsUpper(r) {
				return false
			}

			continue
		}

		if unicode.IsDigit(r) ||
			r == '-' ||
			r == '/' {
			continue
		}

		return false
	}

	if !hasLetter {
		return false
	}

	next := strings.ToLower(
		strings.Trim(
			fields[1],
			" \t\r\n.,;:!?()[]{}",
		),
	)

	switch next {
	case "certification",
		"certified",
		"license",
		"licence",
		"credential":
		return true
	default:
		return false
	}
}
func isLikelyRequirementStart(text string) bool {
	lower := strings.ToLower(
		strings.TrimSpace(text),
	)

	if lower == "" {
		return false
	}

	if looksLikeCredentialStart(
		strings.TrimSpace(text),
	) {
		return true
	}

	prefixes := []string{
		"ability ",
		"ability to ",
		"bachelor",
		"master",
		"doctorate",
		"ph.d",
		"phd",
		"associate degree",
		"high school",
		"ged",
		"experience ",
		"knowledge ",
		"proficiency ",
		"proficient ",
		"skill ",
		"skills ",
		"strong ",
		"excellent ",
		"demonstrated ",
		"proven ",
		"valid ",
		"licensed ",
		"certified ",
		"certification ",
		"willingness ",
		"must ",
		"required ",
		"minimum ",
		"comfortable ",
		"self-motivated ",
		"self starter ",
		"self-starter ",
		"reliable ",
		"superior ",
		"outstanding ",
		"established ",
		"excellence ",
		"documentation ",
		"inside sales ",
	}

	for _, article := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(lower, article) {
			return isLikelyRequirementStart(strings.TrimSpace(lower[len(article):]))
		}
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}

	if yearsRE.MatchString(lower) {
		match := yearsRE.FindStringIndex(lower)

		if match != nil && match[0] == 0 {
			return true
		}
	}

	return false
}

func previousTextLooksComplete(text string) bool {
	text = cleanRequirementText(text)

	if text == "" {
		return false
	}

	// A colon normally introduces the content that follows it:
	// "Communication: Excellent written..."
	// "Cloud Exposure: Familiarity with..."
	// The label is not an independent qualification.
	if strings.HasSuffix(strings.TrimSpace(text), ":") {
		return false
	}

	lower := strings.ToLower(text)
	fields := strings.Fields(lower)

	if len(fields) == 0 {
		return false
	}

	last := strings.Trim(
		fields[len(fields)-1],
		" \t\r\n.,;:!?()[]{}",
	)

	// These tokens leave the preceding phrase grammatically or
	// semantically incomplete. A following requirement-looking word
	// therefore belongs to the same qualification.
	incompleteEndings := map[string]bool{
		"and":          true,
		"or":           true,
		"with":         true,
		"without":      true,
		"of":           true,
		"to":           true,
		"for":          true,
		"in":           true,
		"on":           true,
		"including":    true,
		"such":         true,
		"as":           true,
		"the":          true,
		"a":            true,
		"an":           true,
		"possess":      true,
		"possesses":    true,
		"have":         true,
		"has":          true,
		"also":         true,
		"any":          true,
		"some":         true,
		"strong":       true,
		"excellent":    true,
		"superior":     true,
		"outstanding":  true,
		"foundational": true,
		"basic":        true,
		"deep":         true,
		"extensive":    true,
		"proven":       true,
		"demonstrated": true,
		"prior":        true,
		"previous":     true,
		"relevant":     true,
		"professional": true,
	}

	if incompleteEndings[last] {
		return false
	}

	return true
}

func isSafeRequirementBoundary(
	text string,
	start int,
) bool {
	if start <= 0 || start >= len(text) {
		return false
	}

	previous := rune(text[start-1])

	if unicode.IsLetter(previous) ||
		unicode.IsDigit(previous) {
		return false
	}

	return true
}

func wordStartPositions(text string) []int {
	result := make([]int, 0)

	inWord := false

	for i, r := range text {
		isWord := unicode.IsLetter(r) ||
			unicode.IsDigit(r)

		if isWord && !inWord {
			result = append(result, i)
		}

		inWord = isWord
	}

	return result
}

func nextNonSpaceIndex(
	text string,
	start int,
) int {
	for i := start; i < len(text); i++ {
		if !unicode.IsSpace(rune(text[i])) {
			return i
		}
	}

	return len(text)
}

func addRepeatedRequirementMarker(
	positions *[]int,
	text string,
	marker string,
) {
	lower := strings.ToLower(text)
	needle := strings.ToLower(marker)

	offset := 0
	count := 0

	for offset < len(lower) {
		index := strings.Index(
			lower[offset:],
			needle,
		)

		if index < 0 {
			return
		}

		start := offset + index

		if start > 0 &&
			isSafeRequirementBoundary(text, start) {
			if count > 0 {
				*positions = append(
					*positions,
					start,
				)
			}

			count++
		}

		offset = start + len(needle)
	}
}

func normalizedBoundaryPositions(
	positions []int,
	textLength int,
) []int {
	sort.Ints(positions)

	result := make([]int, 0, len(positions))

	for _, position := range positions {
		if position < 0 ||
			position >= textLength {
			continue
		}

		if len(result) > 0 &&
			result[len(result)-1] == position {
			continue
		}

		result = append(result, position)
	}

	if len(result) == 0 ||
		result[0] != 0 {
		result = append([]int{0}, result...)
	}

	return result
}

func addRequirement(
	result *[]Requirement,
	text string,
	heading string,
	section extractionSection,
) {
	text = cleanRequirementText(text)

	if text == "" ||
		isExplicitNoneValue(text) ||
		isMarketingOrCompanyProse(text) ||
		!validRequirementText(text) {
		return
	}

	if section.Category == RequirementExperience &&
		isYearsOfWorkExperienceHeading(heading) {
		if number, ok := leadingNumericValue(text); ok {
			text = strconv.Itoa(number) +
				" years of work experience"
		}
	}

	importance := section.Importance

	if preferredRE.MatchString(text) {
		importance = RequirementPreferred
	}

	category := classifyRequirement(
		text,
		section.Category,
	)

	*result = appendRequirementUnique(
		*result,
		Requirement{
			Text:       text,
			Category:   category,
			Importance: importance,
		},
	)
}

func classifyRequirement(
	text string,
	fallback RequirementCategory,
) RequirementCategory {
	lower := strings.ToLower(text)

	if strings.Contains(lower, "years of age") {
		return RequirementOther
	}

	switch {
	case licenseRE.MatchString(text):
		return RequirementLicense

	case certificationRE.MatchString(text):
		return RequirementCertification

	case travelRE.MatchString(text):
		return RequirementTravel

	case physicalRE.MatchString(text):
		return RequirementPhysical

	case educationRE.MatchString(text):
		return RequirementEducation

	case experienceRE.MatchString(text):
		return RequirementExperience

	case skillRE.MatchString(text):
		return RequirementSkill

	case fallback != "":
		return fallback

	default:
		return RequirementOther
	}
}

func isYearsOfWorkExperienceHeading(
	heading string,
) bool {
	normalized := normalizeHeading(heading)

	return normalized == "minimum years of work experience" ||
		normalized == "preferred years of work experience"
}

func isLeadershipExperienceHeading(
	heading string,
) bool {
	normalized := normalizeHeading(heading)

	return normalized == "minimum leadership experience" ||
		normalized == "preferred leadership experience"
}

func leadingNumericValue(text string) (int, bool) {
	fields := strings.Fields(
		cleanRequirementText(text),
	)

	if len(fields) == 0 {
		return 0, false
	}

	value := strings.TrimSpace(fields[0])

	if !numericOnlyRE.MatchString(value) {
		return 0, false
	}

	number, err := strconv.Atoi(value)

	if err != nil {
		return 0, false
	}

	return number, true
}

func isExplicitNoneValue(text string) bool {
	lower := strings.ToLower(
		cleanRequirementText(text),
	)

	values := []string{
		"none",
		"n/a",
		"na",
		"not applicable",
		"no additional",
		"no additional education",
		"no previous leadership",
		"no previous leadership experience",
		"no experience required",
		"no prior experience required",
		"none required",
		"not required",
	}

	for _, value := range values {
		if lower == value {
			return true
		}
	}

	return false
}

func truncateMarketingProse(text string) string {
	lower := strings.ToLower(text)
	cut := len(text)

	markers := []string{
		" as the world's largest",
		" as the world’s largest",
		" all of our associates",
		" is a leading global provider",
		" we are a leading global provider",
		" we are the leading provider",
		" equal opportunity employer",
		" equal employment opportunity",
		" benefits:",
		" our benefits:",
		" what we offer:",
		" compensation:",
		" salary range:",
		" pay range:",
		" how to apply:",
		" application process:",
		" company overview:",
		" company description:",
		" about the company:",
		" about us:",
	}

	for _, marker := range markers {
		if index := strings.Index(lower, marker); index >= 0 && index < cut {
			cut = index
		}
	}

	if index := genericLifeAtBoundary(lower); index >= 0 && index < cut {
		cut = index
	}

	return strings.TrimSpace(text[:cut])
}

func genericLifeAtBoundary(lower string) int {
	searchFrom := 0
	for searchFrom < len(lower) {
		relative := strings.Index(lower[searchFrom:], "life at ")
		if relative < 0 {
			return -1
		}
		index := searchFrom + relative
		if index == 0 {
			return index
		}
		prefix := lower[:index]
		trimmed := strings.TrimRight(prefix, " \t\r\n.:;!?-–—•▪●◦‣")
		if trimmed == "" || len(trimmed) < len(prefix) {
			return index
		}
		searchFrom = index + len("life at ")
	}
	return -1
}

func isMarketingOrCompanyProse(text string) bool {
	lower := strings.ToLower(cleanRequirementText(text))
	if lower == "" {
		return false
	}

	prefixes := []string{
		"as the world's largest", "as the world’s largest", "all of our associates",
		"we are a leading global provider", "is a leading global provider", "we are the leading provider",
		"about the company", "about us", "company overview", "company description",
		"our mission", "our culture", "join our team", "why work here", "why work for",
		"why join", "life at ", "equal opportunity employer", "equal employment opportunity",
		"benefits", "our benefits", "what we offer", "compensation", "salary range", "pay range",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func validRequirementText(text string) bool {
	text = cleanRequirementText(text)

	if text == "" ||
		isExplicitNoneValue(text) ||
		isMarketingOrCompanyProse(text) {
		return false
	}

	if numericOnlyRE.MatchString(text) {
		return false
	}

	lower := strings.ToLower(text)

	badExactFragments := []string{
		"must be",
		"avail",
		"professional demeanor and",
		"strong leadership and",
		"the knowledge, skills and abilities typically acquired through the completion of a",
		"the knowledge, skills and abilitiestypically acquired through the completion of a",

		// Structural labels are not qualifications.
		"required",
		"requirements",
		"preferred",
		"preferred qualifications",
		"desired",
		"nice to have",
		"nice-to-have",
		"additional skills",
		"additional qualifications",
		"minimum qualifications",
		"qualifications",
		"communication",
		"communication :",
		"initiative & drive",
		"initiative & drive :",
		"front-end collaboration",
		"front-end collaboration :",
		"physical & office/site presence",
		"physical and office/site presence",

		// Incomplete atomization fragments.
		"foundational",
		"strong",
		"excellent",
		"superior",
		"outstanding",
		"extensive",
		"proven",
		"demonstrated",
		"any",
		"your",
		"and priorities",
	}

	for _, fragment := range badExactFragments {
		if lower == fragment {
			return false
		}
	}

	if len(text) > 500 {
		return false
	}

	return true
}

func rebuildLegacyRequirementFields(job *Job) {
	job.MinimumQualifications = nil
	job.PreferredQualifications = nil
	job.Skills = nil
	job.ExperienceRequirements = nil
	job.EducationRequirements = nil
	job.LicenseRequirements = nil
	job.CertificationRequirements = nil
	job.PhysicalRequirements = nil
	job.TravelRequirements = nil

	for _, requirement := range job.Requirements {
		if requirement.Importance == RequirementPreferred {
			job.PreferredQualifications = appendUniqueString(
				job.PreferredQualifications,
				requirement.Text,
			)
		} else {
			job.MinimumQualifications = appendUniqueString(
				job.MinimumQualifications,
				requirement.Text,
			)
		}

		switch requirement.Category {
		case RequirementSkill:
			job.Skills = appendUniqueString(
				job.Skills,
				requirement.Text,
			)

		case RequirementExperience:
			job.ExperienceRequirements = appendUniqueString(
				job.ExperienceRequirements,
				requirement.Text,
			)

		case RequirementEducation:
			job.EducationRequirements = appendUniqueString(
				job.EducationRequirements,
				requirement.Text,
			)

		case RequirementLicense:
			job.LicenseRequirements = appendUniqueString(
				job.LicenseRequirements,
				requirement.Text,
			)

		case RequirementCertification:
			job.CertificationRequirements = appendUniqueString(
				job.CertificationRequirements,
				requirement.Text,
			)

		case RequirementPhysical:
			job.PhysicalRequirements = appendUniqueString(
				job.PhysicalRequirements,
				requirement.Text,
			)

		case RequirementTravel:
			job.TravelRequirements = appendUniqueString(
				job.TravelRequirements,
				requirement.Text,
			)
		}
	}
}

func prepareRequirementText(text string) string {
	text = html.UnescapeString(text)

	text = strings.NewReplacer(
		"<br>", "\n",
		"<br/>", "\n",
		"<br />", "\n",
		"</p>", "\n",
		"</div>", "\n",
		"</li>", "\n",
		"<li>", "\n",
		"</ul>", "\n",
		"</ol>", "\n",
		"\r\n", "\n",
		"\r", "\n",
	).Replace(text)

	text = htmlTagRE.ReplaceAllString(text, " ")

	// Keep bullets visible as structural delimiters.
	text = strings.NewReplacer(
		"•", " • ",
		"▪", " ▪ ",
		"●", " ● ",
		"◦", " ◦ ",
		"‣", " ‣ ",
	).Replace(text)

	return strings.TrimSpace(text)
}

func cleanRequirementText(text string) string {
	text = strings.TrimSpace(text)

	text = strings.TrimLeftFunc(
		text,
		func(r rune) bool {
			return unicode.IsSpace(r) ||
				r == '-' ||
				r == '*' ||
				r == '•' ||
				r == '▪' ||
				r == '●' ||
				r == '◦' ||
				r == '‣'
		},
	)

	text = strings.TrimSpace(text)

	for {
		trimmed := strings.TrimRight(text, ".;")

		if trimmed == text {
			break
		}

		text = strings.TrimSpace(trimmed)
	}

	return strings.Join(
		strings.Fields(text),
		" ",
	)
}

func normalizeHeading(text string) string {
	text = strings.ToLower(
		strings.TrimSpace(
			strings.TrimSuffix(
				strings.TrimSpace(text),
				":",
			),
		),
	)

	text = strings.NewReplacer(
		",", "",
		"/", " ",
		"-", " ",
	).Replace(text)

	return strings.Join(
		strings.Fields(text),
		" ",
	)
}

func appendRequirementUnique(
	requirements []Requirement,
	requirement Requirement,
) []Requirement {
	requirement.Text = cleanRequirementText(
		requirement.Text,
	)

	if requirement.Text == "" {
		return requirements
	}

	key := normalizedRequirementKey(
		requirement.Text,
	)

	for _, existing := range requirements {
		if normalizedRequirementKey(existing.Text) == key &&
			existing.Category == requirement.Category &&
			existing.Importance == requirement.Importance {
			return requirements
		}
	}

	return append(requirements, requirement)
}

func appendUniqueString(
	values []string,
	value string,
) []string {
	value = cleanRequirementText(value)

	if value == "" {
		return values
	}

	key := normalizedRequirementKey(value)

	for _, existing := range values {
		if normalizedRequirementKey(existing) == key {
			return values
		}
	}

	return append(values, value)
}

func uniqueAtomicStrings(
	values []string,
) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{})

	for _, value := range values {
		value = cleanRequirementText(value)

		if value == "" {
			continue
		}

		key := normalizedRequirementKey(value)

		if _, exists := seen[key]; exists {
			continue
		}

		seen[key] = struct{}{}
		result = append(result, value)
	}

	return result
}

func normalizedRequirementKey(text string) string {
	return strings.ToLower(
		strings.Join(
			strings.Fields(text),
			" ",
		),
	)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}

	return b
}
