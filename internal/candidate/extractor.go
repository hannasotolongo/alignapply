package candidate

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	yearExperienceRE = regexp.MustCompile(
		`(?i)\b(?:\d+\+?|\d+\s*[-–—]\s*\d+)\s*(?:years?|yrs?)\b`,
	)

	educationRE = regexp.MustCompile(
		`(?i)\b(?:high school|ged|associate(?:'|’)?s?|bachelor(?:'|’)?s?|master(?:'|’)?s?|doctorate|ph\.?d\.?|degree)\b`,
	)

	certificationRE = regexp.MustCompile(
		`(?i)\b(?:certification|certified|certificate)\b`,
	)

	licenseRE = regexp.MustCompile(
		`(?i)\b(?:license|licence|licensed|licensure)\b`,
	)

	experienceLanguageRE = regexp.MustCompile(
		`(?i)\b(?:experience|experienced|developed|built|implemented|designed|engineered|managed|led|created|deployed|trained|integrated|maintained|optimized|analysed|analyzed|researched|coordinated|supported|administered|delivered|operated|owned|tested|configured|performed|provided|worked|counseled|counselled|identified|sourced|measured|redesigned|improved|evaluated|advised|educated|taught|recruited|contacted|screened|mentored|consulted|supervised)\b`,
	)

	skillLanguageRE = regexp.MustCompile(
		`(?i)\b(?:skills?|proficient|proficiency|knowledge|familiarity|experienced with|experience with|technologies|tools|languages|frameworks|platforms|systems|software|applications)\b`,
	)
)

func ExtractProfile(resumeText string) Profile {
	raw := strings.TrimSpace(resumeText)

	profile := Profile{
		RawResumeText:  raw,
		Skills:         []Evidence{},
		Experience:     []Evidence{},
		Education:      []Evidence{},
		Certifications: []Evidence{},
		Licenses:       []Evidence{},
		Projects:       []Evidence{},
		Summary:        []Evidence{},
		AllEvidence:    []Evidence{},
	}

	if raw == "" {
		return profile
	}

	for _, unit := range resumeEvidenceUnits(raw) {
		for _, evidence := range classifyEvidenceUnit(unit) {
			addEvidence(&profile, evidence)
		}
	}

	// Preserve the complete résumé as searchable skill evidence so literal
	// technologies, tools, platforms, and professional capabilities are not
	// lost when résumé formatting collapses into a paragraph.
	//
	// Do NOT add the entire résumé as experience evidence. Experience evidence
	// must remain locally scoped so that unrelated dates or year counts from
	// education or other sections cannot satisfy an experience requirement.
	addEvidence(&profile, Evidence{
		Text:     raw,
		Category: EvidenceSkill,
		Source:   raw,
	})

	return profile
}

func resumeEvidenceUnits(text string) []string {
	text = strings.NewReplacer(
		"\r\n", "\n",
		"\r", "\n",
		"•", "\n",
		"▪", "\n",
		"●", "\n",
		"◦", "\n",
		"‣", "\n",
	).Replace(text)

	result := make([]string, 0)

	for _, line := range strings.Split(text, "\n") {
		line = cleanEvidenceText(line)

		if line == "" {
			continue
		}

		result = appendEvidenceUnitUnique(result, line)

		// Preserve the complete line, but also expose sentence-level evidence.
		// This supports résumés that arrive as paragraphs rather than bullets.
		for _, sentence := range splitEvidenceSentences(line) {
			sentence = cleanEvidenceText(sentence)

			if sentence == "" || sentence == line {
				continue
			}

			result = appendEvidenceUnitUnique(result, sentence)
		}
	}

	return result
}

func splitEvidenceSentences(text string) []string {
	result := make([]string, 0)
	start := 0

	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '.', '!', '?':
			end := i + 1

			value := cleanEvidenceText(text[start:end])
			if value != "" {
				result = append(result, value)
			}

			start = end
		}
	}

	if start < len(text) {
		value := cleanEvidenceText(text[start:])
		if value != "" {
			result = append(result, value)
		}
	}

	if len(result) == 0 {
		return []string{cleanEvidenceText(text)}
	}

	return result
}

func classifyEvidenceUnit(text string) []Evidence {
	text = cleanEvidenceText(text)

	if text == "" {
		return nil
	}

	result := make([]Evidence, 0, 5)

	if licenseRE.MatchString(text) {
		result = appendEvidenceUnique(
			result,
			newEvidence(text, EvidenceLicense),
		)
	}

	if certificationRE.MatchString(text) {
		result = appendEvidenceUnique(
			result,
			newEvidence(text, EvidenceCertification),
		)
	}

	if educationRE.MatchString(text) {
		result = appendEvidenceUnique(
			result,
			newEvidence(text, EvidenceEducation),
		)
	}

	isEducationEvidence := educationRE.MatchString(text)

	if !isEducationEvidence &&
		(yearExperienceRE.MatchString(text) ||
			experienceLanguageRE.MatchString(text)) {

		result = appendEvidenceUnique(
			result,
			newEvidence(text, EvidenceExperience),
		)
	}

	if looksLikeSkillEvidence(text) {
		result = appendEvidenceUnique(
			result,
			newEvidence(text, EvidenceSkill),
		)
	}

	return result
}

func newEvidence(
	text string,
	category EvidenceCategory,
) Evidence {
	return Evidence{
		Text:     text,
		Category: category,
		Source:   text,
	}
}

func looksLikeSkillEvidence(text string) bool {
	text = cleanEvidenceText(text)

	if text == "" {
		return false
	}

	if skillLanguageRE.MatchString(text) {
		return true
	}

	// Compact lists are common in résumé skills sections.
	if strings.Contains(text, ",") ||
		strings.Contains(text, " | ") {

		return len(splitSkillCandidates(text)) >= 2
	}

	// Technical/professional evidence is often written as normal prose:
	// "Built distributed services in Go"
	// "Managed enterprise accounts"
	// "Performed medication reconciliation"
	//
	// A demonstrated action plus meaningful content is useful evidence even
	// when the résumé does not contain a literal "Skills:" heading.
	if experienceLanguageRE.MatchString(text) &&
		meaningfulEvidenceWordCount(text) >= 3 {
		return true
	}

	return false
}

func splitSkillCandidates(text string) []string {
	text = strings.ReplaceAll(text, " | ", ",")

	parts := strings.Split(text, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		part = cleanEvidenceText(part)

		if part == "" {
			continue
		}

		result = append(result, part)
	}

	return result
}

func meaningfulEvidenceWordCount(text string) int {
	count := 0

	for _, field := range strings.Fields(text) {
		field = strings.TrimFunc(
			field,
			func(r rune) bool {
				return !unicode.IsLetter(r) &&
					!unicode.IsDigit(r)
			},
		)

		if field != "" {
			count++
		}
	}

	return count
}

func appendEvidenceUnitUnique(
	values []string,
	value string,
) []string {
	value = cleanEvidenceText(value)

	if value == "" {
		return values
	}

	key := strings.ToLower(value)

	for _, existing := range values {
		if strings.ToLower(cleanEvidenceText(existing)) == key {
			return values
		}
	}

	return append(values, value)
}

func addEvidence(
	profile *Profile,
	evidence Evidence,
) {
	evidence.Text = cleanEvidenceText(evidence.Text)
	evidence.Source = cleanEvidenceText(evidence.Source)

	if evidence.Text == "" {
		return
	}

	profile.AllEvidence = appendEvidenceUnique(
		profile.AllEvidence,
		evidence,
	)

	switch evidence.Category {
	case EvidenceSkill:
		profile.Skills = appendEvidenceUnique(
			profile.Skills,
			evidence,
		)

	case EvidenceExperience:
		profile.Experience = appendEvidenceUnique(
			profile.Experience,
			evidence,
		)

	case EvidenceEducation:
		profile.Education = appendEvidenceUnique(
			profile.Education,
			evidence,
		)

	case EvidenceCertification:
		profile.Certifications = appendEvidenceUnique(
			profile.Certifications,
			evidence,
		)

	case EvidenceLicense:
		profile.Licenses = appendEvidenceUnique(
			profile.Licenses,
			evidence,
		)

	case EvidenceProject:
		profile.Projects = appendEvidenceUnique(
			profile.Projects,
			evidence,
		)

	case EvidenceSummary:
		profile.Summary = appendEvidenceUnique(
			profile.Summary,
			evidence,
		)
	}
}

func appendEvidenceUnique(
	values []Evidence,
	value Evidence,
) []Evidence {
	key := evidenceKey(value)

	for _, existing := range values {
		if evidenceKey(existing) == key {
			return values
		}
	}

	return append(values, value)
}

func evidenceKey(evidence Evidence) string {
	return string(evidence.Category) +
		":" +
		strings.ToLower(
			strings.Join(
				strings.Fields(evidence.Text),
				" ",
			),
		)
}

func cleanEvidenceText(text string) string {
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

	return strings.Join(
		strings.Fields(text),
		" ",
	)
}
