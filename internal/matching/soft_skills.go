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
		}
	}
	return requirement
}
