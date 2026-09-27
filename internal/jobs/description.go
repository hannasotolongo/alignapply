package jobs

import (
	"html"
	"regexp"
	"strings"
)

var (
	descriptionBreakRE = regexp.MustCompile(`(?i)<\s*br\s*/?\s*>`)
	descriptionBlockRE = regexp.MustCompile(
		`(?i)</?\s*(?:p|div|li|ul|ol|h[1-6]|section|article)\b[^>]*>`,
	)
	descriptionTagRE   = regexp.MustCompile(`<[^>]+>`)
	descriptionSpaceRE = regexp.MustCompile(`[ \t]+`)
	descriptionBlankRE = regexp.MustCompile(`\n{3,}`)
)

// normalizeJobDescription cleans provider formatting without rewriting,
// summarizing, or inventing job-posting content.
//
// The original wording remains authoritative. This function only repairs
// transport/display artifacts such as HTML markup, entities, line endings,
// repeated whitespace, and paragraph boundaries.
func normalizeJobDescription(value string) string {
	value = strings.TrimSpace(value)

	if value == "" {
		return ""
	}

	// Preserve structural HTML boundaries before stripping remaining tags.
	value = descriptionBreakRE.ReplaceAllString(value, "\n")
	value = descriptionBlockRE.ReplaceAllString(value, "\n")
	value = descriptionTagRE.ReplaceAllString(value, " ")

	value = html.UnescapeString(value)

	value = strings.NewReplacer(
		"\r\n", "\n",
		"\r", "\n",
		"\u00a0", " ",
		"\u200b", "",
		"\u2028", "\n",
		"\u2029", "\n",
	).Replace(value)

	lines := strings.Split(value, "\n")
	cleaned := make([]string, 0, len(lines))

	for _, line := range lines {
		line = descriptionSpaceRE.ReplaceAllString(line, " ")
		line = strings.TrimSpace(line)

		if line == "" {
			if len(cleaned) > 0 && cleaned[len(cleaned)-1] != "" {
				cleaned = append(cleaned, "")
			}

			continue
		}

		cleaned = append(cleaned, line)
	}

	value = strings.Join(cleaned, "\n")
	value = descriptionBlankRE.ReplaceAllString(value, "\n\n")

	return strings.TrimSpace(value)
}
