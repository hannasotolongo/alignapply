package candidate

import (
	"regexp"
	"sort"
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

var inlineAbbreviationPatterns = func() []*regexp.Regexp {
	patterns := make([]*regexp.Regexp, len(credentialAbbreviations))
	for i, entry := range credentialAbbreviations {
		dotted := strings.Join(strings.Split(entry.abbreviation, ""), `\.?`)
		patterns[i] = regexp.MustCompile(`(?i)(^|[^A-Za-z])` + dotted + `\.?([^A-Za-z]|$)`)
	}
	return patterns
}()

// ReplaceCredentialAbbreviations rewrites known abbreviations to their full
// wording ("Active FL RN license" -> "Active FL Registered Nurse license"),
// case-insensitively, so credentials compare word for word.
func ReplaceCredentialAbbreviations(text string) string {
	for i, entry := range credentialAbbreviations {
		replacement := "${1}" + strings.ToLower(entry.expansion) + "${2}"
		// Apply twice so adjacent abbreviations ("BLS ACLS") both expand.
		text = inlineAbbreviationPatterns[i].ReplaceAllString(text, replacement)
		text = inlineAbbreviationPatterns[i].ReplaceAllString(text, replacement)
	}
	return text
}

// extraCredentialPhrases are other common ways postings and résumés spell
// a credential.
var extraCredentialPhrases = map[string][]string{
	"bsn": {"bachelor's degree in nursing", "bachelors degree in nursing", "bachelor degree in nursing", "bachelor of nursing", "baccalaureate degree in nursing", "bachelor's in nursing", "bachelors in nursing"},
	"msn": {"master's degree in nursing", "masters degree in nursing", "master of nursing"},
	"adn": {"associate's degree in nursing", "associates degree in nursing", "associate degree nursing"},
	"rn":  {"registered nurse license", "registered professional nurse"},
	"bls": {"basic life support", "cpr/bls", "bls/cpr"},
}

type phraseAlias struct {
	pattern *regexp.Regexp
	token   string
	length  int
}

var canonicalPhrases = func() []phraseAlias {
	var aliases []phraseAlias
	add := func(phrase, token string) {
		words := strings.Fields(strings.ToLower(phrase))
		for i, word := range words {
			words[i] = regexp.QuoteMeta(word)
		}
		aliases = append(aliases, phraseAlias{
			pattern: regexp.MustCompile(`(?i)(^|[^a-z0-9])` + strings.Join(words, `[\s,/-]+`) + `([^a-z0-9]|$)`),
			token:   token,
			length:  len(phrase),
		})
	}
	for _, entry := range credentialAbbreviations {
		token := strings.ToLower(entry.abbreviation)
		add(entry.expansion, token)
		// Dotted forms: "B.S.N." -> "bsn".
		dotted := strings.Join(strings.Split(token, ""), `\.`) + `\.?`
		aliases = append(aliases, phraseAlias{
			pattern: regexp.MustCompile(`(?i)(^|[^a-z0-9])` + dotted + `([^a-z0-9]|$)`),
			token:   token,
			length:  len(token),
		})
	}
	for token, phrases := range extraCredentialPhrases {
		for _, phrase := range phrases {
			add(phrase, token)
		}
	}
	// Longest phrases first: "Critical Care Registered Nurse" must become
	// "ccrn" before "Registered Nurse" can become "rn".
	sort.SliceStable(aliases, func(i, j int) bool { return aliases[i].length > aliases[j].length })
	return aliases
}()

// CanonicalCredentials rewrites credential phrases to one short token each,
// so "Critical Care Registered Nurse" and "CCRN" compare as the same single
// credential, and never as the loose words "critical" or "nurse".
func CanonicalCredentials(text string) string {
	text = strings.ToLower(text)
	for _, alias := range canonicalPhrases {
		for i := 0; i < 2; i++ {
			text = alias.pattern.ReplaceAllString(text, "${1} "+alias.token+" ${2}")
		}
	}
	return text
}
