package candidate

import (
	"regexp"
	"strings"
)

// credentialAbbreviations maps common résumé abbreviations to the wording job
// postings use. Expansion only appends the spelled-out form next to an
// abbreviation the candidate actually wrote, so it never adds a credential.
var credentialAbbreviations = []struct {
	abbreviation string
	expansion    string
}{
	{"BSN", "Bachelor of Science in Nursing"},
	{"MSN", "Master of Science in Nursing"},
	{"ADN", "Associate Degree in Nursing"},
	{"ASN", "Associate of Science in Nursing"},
	{"DNP", "Doctor of Nursing Practice"},
	{"RN", "Registered Nurse"},
	{"LPN", "Licensed Practical Nurse"},
	{"LVN", "Licensed Vocational Nurse"},
	{"CNA", "Certified Nursing Assistant"},
	{"NP", "Nurse Practitioner"},
	{"APRN", "Advanced Practice Registered Nurse"},
	{"BLS", "Basic Life Support"},
	{"ACLS", "Advanced Cardiovascular Life Support"},
	{"PALS", "Pediatric Advanced Life Support"},
	{"NRP", "Neonatal Resuscitation Program"},
	{"TNCC", "Trauma Nursing Core Course"},
	{"CCRN", "Critical Care Registered Nurse"},
	{"EHR", "Electronic Health Records"},
	{"ICU", "Intensive Care Unit"},
	{"PACU", "Post-Anesthesia Care Unit"},
	{"BS", "Bachelor of Science"},
	{"BA", "Bachelor of Arts"},
	{"MBA", "Master of Business Administration"},
	{"PhD", "Doctor of Philosophy"},
	{"CPA", "Certified Public Accountant"},
	{"PMP", "Project Management Professional"},
}

var abbreviationPatterns = func() []*regexp.Regexp {
	patterns := make([]*regexp.Regexp, len(credentialAbbreviations))
	for i, entry := range credentialAbbreviations {
		// Case-sensitive and bounded so "or"/"er"/"ms" in prose don't match;
		// allow dotted forms like "B.S.N." and "B.S.".
		dotted := strings.Join(strings.Split(entry.abbreviation, ""), `\.?`)
		patterns[i] = regexp.MustCompile(`(^|[^A-Za-z])(` + dotted + `\.?)([^A-Za-z]|$)`)
	}
	return patterns
}()

// ExpandCredentialAbbreviations appends the spelled-out meaning of known
// abbreviations so requirement matching recognizes them.
func ExpandCredentialAbbreviations(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	lower := strings.ToLower(text)
	var additions []string
	for i, entry := range credentialAbbreviations {
		if strings.Contains(lower, strings.ToLower(entry.expansion)) {
			continue
		}
		if abbreviationPatterns[i].MatchString(text) {
			additions = append(additions, entry.expansion+" ("+entry.abbreviation+")")
		}
	}
	if len(additions) == 0 {
		return text
	}
	return text + "\n" + strings.Join(additions, "; ")
}
