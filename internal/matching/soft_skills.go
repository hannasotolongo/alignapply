package matching

import (
	"regexp"
	"strings"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

// Soft skills ("excellent communication", "detail-oriented", "team player")
// can't be verified from a résumé, and résumés rarely state them outright.
// Scoring them as missing made qualified candidates look like poor fits, so
// they're left out of the match score. Interviews are where they're judged.
var softSkillPattern = regexp.MustCompile(`(?i)\b(communication|interpersonal|organi[sz]ational|organi[sz]ed|time[- ]management|team[- ]?(player|work|oriented|environment)|work(s)? well|collaborat\w*|fast[- ]paced|detail[- ]oriented|attention to detail|critical[- ]thinking|problem[- ]solving|multi[- ]?task\w*|self[- ]?(motivated|starter|directed)|motivated|positive attitude|work ethic|work independently|independently|flexib\w+|adaptab\w+|professionalism|professional demeanor|punctual\w*|reliab\w+|dependab\w+|compassion\w*|empath\w+|customer[- ]focused|people skills|willing(ness)? to learn|eager to learn|enthusias\w+|integrity|initiative|prioriti[sz]\w*)\b`)

// hardSignal marks requirement text that is clearly a verifiable
// qualification even when it mentions a soft-skill word.
var hardSignal = regexp.MustCompile(`(?i)(\d+\+?\s*(years?|yrs?)|licen[sc]|certif|degree|bachelor|master|diploma|\b(rn|bsn|bls|acls|epic|cerner|sql|python|excel|salesforce|software|systems?)\b)`)

func isSoftSkillRequirement(requirement jobs.Requirement) bool {
	switch requirement.Category {
	case jobs.RequirementLicense, jobs.RequirementCertification, jobs.RequirementEducation:
		return false
	}
	text := strings.TrimSpace(requirement.Text)
	if text == "" || hardSignal.MatchString(text) {
		return false
	}
	return softSkillPattern.MatchString(text)
}

var (
	licenseText       = regexp.MustCompile(`(?i)\blicen[sc](e|ed|ure)\b`)
	certificationText = regexp.MustCompile(`(?i)\bcertifi(ed|cation|cations|cate)\b`)
)

// normalizeRequirementCategory fixes credentials the extractor filed as
// "other" or "skill" ("Active Florida RN license required"), so they're
// scored and checked as the hard requirements they are.
func normalizeRequirementCategory(requirement jobs.Requirement) jobs.Requirement {
	switch requirement.Category {
	case jobs.RequirementOther, jobs.RequirementSkill:
		switch {
		case licenseText.MatchString(requirement.Text):
			requirement.Category = jobs.RequirementLicense
		case certificationText.MatchString(requirement.Text):
			requirement.Category = jobs.RequirementCertification
		case requirement.Category == jobs.RequirementOther && languagePattern.MatchString(requirement.Text):
			// "Fluency in Spanish": a résumé shows a language plainly.
			requirement.Category = jobs.RequirementSkill
		case requirement.Category == jobs.RequirementOther && namesCapabilities(requirement.Text):
			// "You are an expert in React with a solid grasp of Python" or
			// "Track record of exceeding quota and closing customers" read
			// as "other" but name real, checkable work.
			requirement.Category = jobs.RequirementSkill
		}
	}
	return requirement
}

// traitWords describe personality, not work a résumé can show.
var traitWords = map[string]struct{}{
	"mindset": {}, "attitude": {}, "passion": {}, "passionate": {}, "motivated": {}, "curious": {},
	"hungry": {}, "coachable": {}, "collaborative": {}, "ego": {}, "sense": {}, "empathy": {},
	"approach": {}, "team": {}, "first": {}, "humble": {}, "driven": {}, "self": {}, "starter": {},
	"pragmatic": {}, "growth": {}, "ownership": {}, "mentality": {}, "love": {}, "enjoy": {},
	"thrive": {}, "eager": {}, "excited": {}, "values": {}, "integrity": {}, "autonomy": {},
	"ambiguity": {}, "consistently": {}, "bonus": {}, "maintain": {}, "degree": {},
	// Application and posting wording, not work.
	"notice": {}, "applying": {}, "apply": {}, "applicants": {}, "position": {}, "location": {},
	"role": {}, "company": {}, "job": {}, "opportunity": {}, "remote": {}, "hybrid": {}, "onsite": {},
}

// namesCapabilities reports requirement text that names at least two
// concrete things a résumé could show (tools, skills, kinds of work), and
// isn't a soft skill, a personality trait or a logistics line.
func namesCapabilities(text string) bool {
	if isLogisticsRequirement(text) || companyProse.MatchString(text) || isSoftSkillRequirement(jobs.Requirement{Category: jobs.RequirementOther, Text: text}) {
		return false
	}
	count := 0
	for _, token := range meaningfulTokens(text) {
		if _, trait := traitWords[token]; trait {
			continue
		}
		count++
	}
	return count >= 3
}

var languagePattern = regexp.MustCompile(`(?i)\b(spanish|french|german|italian|portuguese|mandarin|cantonese|chinese|japanese|korean|vietnamese|tagalog|arabic|russian|hindi|urdu|polish|haitian creole|creole|asl|american sign language)\b`)

// companyProse is the employer talking about itself or the law, which can
// end up in a requirements list: "At Toast, our employees are…", "Toast is
// committed to an inclusive hiring process", "it is unlawful in
// Massachusetts…".
var companyProse = regexp.MustCompile(`(?i)(^(at|apply|for roles|join)\b|\b(we|our|us|we're|we’re)\b|committed to|unlawful|equal opportunity|accommodation|hiring process|lie detector|benefits|salary|compensation|pay range|apply today|click here)`)
