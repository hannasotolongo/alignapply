package matching

import (
	"regexp"
	"strings"

	"github.com/hannasotolongo/casemade-backend/internal/candidate"
)

// Degrees form a ladder: a bachelor's degree also meets "high school diploma
// or GED", and a master's meets "bachelor's degree". Credentials are
// otherwise matched word for word, which marked an associate degree as not
// meeting "High school diploma required".

const (
	levelNone = iota
	levelHighSchool
	levelAssociate
	levelBachelor
	levelMaster
	levelDoctorate
)

var degreeLevelPatterns = []struct {
	level   int
	pattern *regexp.Regexp
}{
	{levelDoctorate, regexp.MustCompile(`(?i)\b(ph\.?\s?d|doctorate|doctoral|doctor of|d\.?n\.?p|ed\.?d|j\.?d|m\.?d|pharm\.?d)\b`)},
	{levelMaster, regexp.MustCompile(`(?i)\b(master'?s?|m\.?s\.?n?|m\.?a\.?|m\.?b\.?a|m\.?ed|m\.?eng|m\.?s\.?w|graduate degree)\b`)},
	{levelBachelor, regexp.MustCompile(`(?i)\b(bachelor'?s?|baccalaureate|b\.?s\.?n?|b\.?a\.?|b\.?sc|b\.?eng|undergraduate degree|(?:four|4)[- ]year (?:college )?degree)\b`)},
	{levelAssociate, regexp.MustCompile(`(?i)\b(associate'?s?(?: of| degree| in)|a\.?a\.?s?|a\.?s\.?n?|a\.?d\.?n|(?:two|2)[- ]year degree)\b`)},
	{levelHighSchool, regexp.MustCompile(`(?i)\b(high school|h\.?s\.? diploma|ged|g\.e\.d|hiset|secondary school|secondary education)\b`)},
}

// requiredDegreeLevel is the lowest degree a requirement accepts ("High
// school diploma or GED" -> high school; "Bachelor's or Master's" ->
// bachelor's), or levelNone.
func requiredDegreeLevel(text string) int {
	lowest := levelNone
	for _, entry := range degreeLevelPatterns {
		if entry.pattern.MatchString(text) && (lowest == levelNone || entry.level < lowest) {
			lowest = entry.level
		}
	}
	return lowest
}

// highestDegreeLevel is the highest degree named in the candidate's
// education lines.
func highestDegreeLevel(evidence []candidate.Evidence) int {
	highest := levelNone
	for _, item := range evidence {
		for _, entry := range degreeLevelPatterns {
			if entry.level > highest && entry.pattern.MatchString(item.Text) {
				highest = entry.level
			}
		}
	}
	return highest
}

// degreeFieldPattern finds a required field of study: "degree in nursing",
// "Bachelor's in Computer Science".
var degreeFieldPattern = regexp.MustCompile(`(?i)\b(?:degree|diploma|bachelor'?s?|master'?s?|associate'?s?|b\.?s\.?|b\.?a\.?|m\.?s\.?)\s+(?:of \w+ )?(?:in|of)\s+(\w[\w\s,&/-]*)`)

// evaluateDegreeLadder settles education requirements the ladder can
// decide: a higher degree meets a lower one when no particular field of
// study is asked for (a high school requirement never names one). handled
// is false when the field must still be checked word for word.
func evaluateDegreeLadder(evidence []candidate.Evidence, requirement string) (evidenceResult, bool) {
	required := requiredDegreeLevel(requirement)
	if required == levelNone {
		return evidenceResult{}, false
	}
	have := highestDegreeLevel(evidence)
	if have < required {
		return evidenceResult{}, false
	}
	if required == levelHighSchool {
		return supportedEvidence(), true
	}
	if field := degreeFieldPattern.FindStringSubmatch(requirement); field != nil {
		words := strings.ToLower(strings.TrimSpace(field[1]))
		if !strings.HasPrefix(words, "a related field") && !strings.HasPrefix(words, "related field") && !strings.HasPrefix(words, "any field") {
			return evidenceResult{}, false
		}
	}
	return supportedEvidence(), true
}
