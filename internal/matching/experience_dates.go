package matching

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/candidate"
)

// Résumés show experience as date ranges ("2019 – Present",
// "12/2023 – 09/2026", "Jan 2020 - Mar 2023"), not as "5 years of ...".
// careerYears measures work history from those ranges so a requirement like
// "2+ years of acute care" isn't marked unproven just because the résumé
// lists dates instead of a sentence.

var monthNames = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "sept": 9, "oct": 10, "nov": 11, "dec": 12,
}

const datePart = `(?:(?:(jan|feb|mar|apr|may|jun|jul|aug|sept?|oct|nov|dec)[a-z]*\.?\s+)|(\d{1,2})\s*/\s*)?((?:19|20)\d{2})`

var dateRangePattern = regexp.MustCompile(`(?i)` + datePart + `\s*(?:-|–|—|to|until)\s*(?:` + datePart + `|(present|current|now|today))`)

// degreeLine marks lines about a degree; their dates are school, not work.
var degreeLine = regexp.MustCompile(`(?i)\b(bachelor|master|associate'?s? degree|b\.?s\.?n?|b\.?a\.?|m\.?s\.?n?|mba|ph\.?d|gpa|graduat|degree|diploma|coursework)\b`)

// institutionLine names a school. Alone it suggests schooling, but teachers,
// school nurses and university staff work at schools: a job title on the
// same line makes it work.
var institutionLine = regexp.MustCompile(`(?i)\b(university|college|school|academy|isd|student)\b`)

var jobTitleWord = regexp.MustCompile(`(?i)\b(teacher|teaching|instructor|professor|lecturer|tutor|para(?:professional|educator)?|aide|counselor|librarian|principal|coach|nurse|rn|lpn|cna|therapist|technician|assistant|coordinator|manager|director|specialist|analyst|engineer|developer|administrator|officer|representative|clerk|secretary|receptionist|advisor|adviser|supervisor|custodian|driver|cook|chef|scientist|researcher|intern)\b`)

// isEducationLine reports whether a line's dates are school rather than work.
func isEducationLine(line string) bool {
	if degreeLine.MatchString(line) {
		return true
	}
	return institutionLine.MatchString(line) && !jobTitleWord.MatchString(line)
}

type monthSpan struct{ start, end int } // months since year 0

func monthIndex(year, month int) int { return year*12 + (month - 1) }

func parseRangeEnd(monthName, monthNum, year string, defaultMonth int) (int, bool) {
	y, err := strconv.Atoi(year)
	if err != nil {
		return 0, false
	}
	month := defaultMonth
	if monthName != "" {
		if m, ok := monthNames[strings.ToLower(monthName)[:3]]; ok {
			month = m
		}
		if strings.HasPrefix(strings.ToLower(monthName), "sept") {
			month = 9
		}
	} else if monthNum != "" {
		if m, err := strconv.Atoi(monthNum); err == nil && m >= 1 && m <= 12 {
			month = m
		}
	}
	return monthIndex(y, month), true
}

func spansFromText(text string, now time.Time) []monthSpan {
	var spans []monthSpan
	current := monthIndex(now.Year(), int(now.Month()))
	for _, line := range strings.Split(text, "\n") {
		if isEducationLine(line) {
			continue
		}
		for _, m := range dateRangePattern.FindAllStringSubmatch(line, -1) {
			start, ok := parseRangeEnd(m[1], m[2], m[3], 1)
			if !ok {
				continue
			}
			var end int
			if m[7] != "" {
				end = current
			} else {
				end, ok = parseRangeEnd(m[4], m[5], m[6], 12)
				if !ok {
					continue
				}
			}
			if end > current {
				end = current
			}
			if end < start || end-start > 50*12 {
				continue
			}
			spans = append(spans, monthSpan{start, end})
		}
	}
	return spans
}

// careerYears returns whole years of work covered by résumé date ranges,
// counting overlapping jobs once.
func careerYears(profile candidate.Profile, now time.Time) int {
	var text strings.Builder
	for _, item := range profile.Experience {
		text.WriteString(item.Text)
		text.WriteByte('\n')
	}
	spans := spansFromText(text.String(), now)
	if len(spans) == 0 {
		spans = spansFromText(profile.RawResumeText, now)
	}
	if len(spans) == 0 {
		return 0
	}

	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	total := 0
	cur := spans[0]
	for _, span := range spans[1:] {
		if span.start <= cur.end+1 {
			if span.end > cur.end {
				cur.end = span.end
			}
			continue
		}
		total += cur.end - cur.start + 1
		cur = span
	}
	total += cur.end - cur.start + 1
	return total / 12
}

// rawResumeLines returns résumé lines the profile extractor tends to drop:
// job-title lines carrying dates ("Marketing Manager, Brightline Foods,
// 2019 – 2025") for work, or degree lines ("B.S. Marketing, University of
// Florida") for school. Without them a requirement like "5+ years of
// marketing experience" or "Bachelor's in marketing" loses the very words
// that prove it.
func rawResumeLines(profile candidate.Profile, education bool) []candidate.Evidence {
	var lines []candidate.Evidence
	for _, line := range strings.Split(profile.RawResumeText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || len(line) > 200 {
			continue
		}
		if education {
			// Degree lines and school names both help prove a degree.
			if degreeLine.MatchString(line) || institutionLine.MatchString(line) {
				lines = append(lines, candidate.Evidence{Text: line, Source: "resume"})
			}
			continue
		}
		if !isEducationLine(line) && dateRangePattern.MatchString(line) {
			lines = append(lines, candidate.Evidence{Text: line, Source: "resume"})
		}
	}
	return lines
}

func withRawLines(evidence []candidate.Evidence, profile candidate.Profile, education bool) []candidate.Evidence {
	extra := rawResumeLines(profile, education)
	if len(extra) == 0 {
		return evidence
	}
	seen := make(map[string]bool, len(evidence))
	for _, item := range evidence {
		seen[normalize(item.Text)] = true
	}
	out := append([]candidate.Evidence(nil), evidence...)
	for _, item := range extra {
		if !seen[normalize(item.Text)] {
			out = append(out, item)
		}
	}
	return out
}
