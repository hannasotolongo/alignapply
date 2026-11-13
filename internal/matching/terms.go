package matching

import (
	"regexp"
	"strings"
)

// Job postings and résumés name the same thing in different ways: "Med
// surg" / "medical-surgical", "ER" / "emergency department", "EHR" / "Epic".
// canonicalTerms rewrites those to one shared token before words are
// compared, on both sides, so the match doesn't depend on spelling. Only
// true equivalents are listed; a brand that implies a category ("Epic" is an
// EHR) adds the category and keeps the brand.
var termRewrites = func() []struct {
	pattern *regexp.Regexp
	replace string
} {
	rules := []struct{ pattern, replace string }{
		// Healthcare
		{`\bmed(?:ical)?[\s/-]*surg(?:ical|ery)?\b`, " medsurg "},
		{`\bintensive care(?: units?)?\b`, " icu "},
		{`\bcritical care\b`, " critical-care icu "},
		{`\bemergency (?:room|department|dept|medicine)\b`, " er "},
		{`\b(?:electronic (?:health|medical) records?|ehr|emr)\b`, " ehr "},
		{`\b(epic|cerner|meditech|athenahealth|eclinicalworks)\b`, " $1 ehr "},
		{`\btele(?:metry)?[\s/-]*(?:unit|monitoring)?\b`, " telemetry "},
		{`\b(?:labor (?:and|&) delivery|l\s*&\s*d)\b`, " labor-delivery "},
		{`\bactivities of daily living\b`, " adl "},
		{`\bvital signs\b`, " vitals "},
		{`\bpatient care\b`, " patient-care "},
		{`\bacute[\s-]+care\b`, " acute "},
		// Office and admin
		{`\b(?:microsoft|ms)\s*office(?:\s*(?:suite|365))?\b|\bmicrosoft 365\b|\boffice 365\b`, " msoffice "},
		{`\bdata entry\b`, " data-entry "},
		// One token each, so "customer" and "service" from different lines
		// ("customer billing dashboard", "backend services") can't add up.
		{`\b(?:customer|client|member|guest)[\s-]+(?:service|support|care|experience)\b`, " customerservice "},
		{`\b(?:call|contact)[\s-]+(?:center|centre)\b`, " callcenter customerservice "},
		{`\bfront desk\b`, " front-desk reception "},
		{`\breceptionist\b`, " reception "},
		// Teaching
		{`\blesson plan(?:s|ning)?\b`, " lesson-plans "},
		{`\bclassroom management\b`, " classroom-management "},
		{`\b(?:k-12|k12|kindergarten through (?:12th|twelfth) grade)\b`, " k12 "},
		{`\b(?:teaching (?:certificate|certification|license|credential)|certified teacher|teacher certification)\b`, " teaching-certificate "},
		// Software
		{`\bjs\b`, " javascript "},
		{`\bts\b`, " typescript "},
		{`\bnode(?:\.js|js)\b`, " node "},
		{`\breact(?:\.js|js)\b`, " react "},
		{`\bk8s\b`, " kubernetes "},
		{`\b(?:ci\s*/\s*cd|continuous integration(?:\s*(?:and|/)\s*continuous (?:delivery|deployment))?)\b`, " cicd "},
		{`\brest(?:ful)?\s*apis?\b`, " rest api "},
		{`\bgolang\b`, " go "},
		{`\bamazon web services\b`, " aws "},
		{`\bgoogle cloud(?: platform)?\b`, " gcp "},
		{`\bmachine learning\b`, " machine-learning "},
	}
	compiled := make([]struct {
		pattern *regexp.Regexp
		replace string
	}, len(rules))
	for i, rule := range rules {
		compiled[i].pattern = regexp.MustCompile(rule.pattern)
		compiled[i].replace = rule.replace
	}
	return compiled
}()

// canonicalTerms applies termRewrites to lowercase text.
func canonicalTerms(text string) string {
	for _, rule := range termRewrites {
		text = rule.pattern.ReplaceAllString(text, rule.replace)
	}
	return text
}

// logisticsPattern finds requirements a résumé can't show either way: a
// driver's license, a car, insurance, screenings, schedules. They're checked
// by the employer during hiring, so they don't count for or against fit.
var logisticsPattern = regexp.MustCompile(`(?i)\b(driver'?s?'? licen[cs]e|driving record|reliable (?:transportation|vehicle)|own (?:transportation|vehicle|car)|auto(?:mobile)? insurance|vehicle insurance|proof of insurance|background (?:check|screening)|drug (?:screen|test)|tb (?:test|screening)|immunization|vaccinat|able to (?:lift|stand|sit|work weekends|work nights)|lift up to|work (?:weekends|nights|holidays|overtime|on-call)|on[- ]call|authorized to work|work authorization|sponsorship|18 years|at least 18|physically demanding|physical activity|walk(?:ing)?/stand|stand(?:ing)? for (?:long|extended)|extended periods|kneel|crouch|climb|bend(?:ing)?|squat|stoop|reach with|manual dexterity|visual acuity|hearing|frequent driving|operate a (?:vehicle|forklift)|read,? write,? (?:and )?speak|speak,? read|lbs|pounds)\b`)

func isLogisticsRequirement(text string) bool {
	return logisticsPattern.MatchString(text)
}

// alternativesPattern splits "Med surg, ICU, ER, or acute experience" into
// its alternatives; any one of them meets the requirement.
var alternativesPattern = regexp.MustCompile(`\s*(?:,\s*(?:or\s+)?|\s+or\s+|\s*/\s*)`)

// requirementAlternatives returns the alternatives in an "A, B, or C"
// requirement, or nil when it isn't one. Each alternative is short, so a
// long sentence that happens to contain "or" isn't split.
// slashBetweenWords finds "software engineering/production infrastructure".
var slashBetweenWords = regexp.MustCompile(`[a-z]{3,}\s*/\s*[a-z]{3,}`)

func requirementAlternatives(requirement string) []string {
	lower := " " + strings.ToLower(requirement) + " "
	if !strings.Contains(lower, " or ") && !slashBetweenWords.MatchString(lower) {
		return nil
	}
	parts := alternativesPattern.Split(strings.TrimSpace(strings.ToLower(requirement)), -1)
	if len(parts) < 2 {
		return nil
	}
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if len(strings.Fields(part)) > 4 {
			return nil
		}
		kept = append(kept, part)
	}
	if len(kept) < 2 {
		return nil
	}
	return kept
}
