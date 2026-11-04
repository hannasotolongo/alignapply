package candidate

import (
	"regexp"
	"strings"
)

// DraftItem is one suggested career-profile entry from a résumé.
type DraftItem struct {
	Category EvidenceCategory
	Text     string
}

const draftMixedSection EvidenceCategory = "\x00mixed"

var skillListSplitRE = regexp.MustCompile(`\s*[,;|·]\s*`)

// DraftCareerProfile suggests career-profile entries from résumé text, for
// the user to review before saving.
//
// Unlike ExtractProfile (which deliberately fans each line out into several
// categories, and adds the whole résumé as skill evidence, to maximize
// matcher recall), the draft follows the résumé's own section headings and
// places every line in exactly one section, so the editor shows a clean,
// non-duplicated profile. Skill sections are split into individual skills.
// Lines before the first recognized heading (name, contact details) are
// skipped, because they are not evidence.
//
// Résumés with no recognizable headings fall back to the extractor's
// per-line classification, keeping only each line's first category.
func DraftCareerProfile(resumeText string) (summary string, items []DraftItem) {
	units := draftLines(resumeText)

	seen := make(map[string]struct{})
	add := func(category EvidenceCategory, text string) {
		text = cleanEvidenceText(text)
		if text == "" || len(text) > 1000 {
			return
		}
		key := string(category) + "\x00" + strings.ToLower(text)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		items = append(items, DraftItem{Category: category, Text: text})
	}

	var summaryParts []string
	current := EvidenceCategory("")
	sawHeading := false

	for _, line := range units {
		if section, ok := resumeSectionCategory(line); ok {
			current = section
			if section == "" {
				// Mixed headings such as "Licenses and Certifications":
				// decide each line by its own wording.
				current = draftMixedSection
			}
			sawHeading = true
			continue
		}

		switch current {
		case "":
			continue

		case draftMixedSection:
			category := EvidenceCertification
			switch {
			case licenseRE.MatchString(line):
				category = EvidenceLicense
			case certificationRE.MatchString(line):
				category = EvidenceCertification
			}
			add(category, line)

		case EvidenceSummary:
			summaryParts = append(summaryParts, line)

		case EvidenceSkill:
			for _, skill := range skillListSplitRE.Split(line, -1) {
				if len(skill) <= 80 {
					add(EvidenceSkill, skill)
				}
			}

		default:
			add(current, line)
		}
	}

	if !sawHeading {
		for _, line := range units {
			classified := classifyEvidenceUnit(line)
			if len(classified) > 0 {
				add(classified[0].Category, line)
			}
		}
	}

	return strings.Join(summaryParts, " "), items
}

func draftLines(text string) []string {
	text = strings.NewReplacer(
		"\r\n", "\n", "\r", "\n",
		"•", "\n", "▪", "\n", "●", "\n", "◦", "\n", "‣", "\n",
	).Replace(text)

	lines := make([]string, 0)
	for _, line := range strings.Split(text, "\n") {
		if line = cleanEvidenceText(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
