package matching

import (
	"github.com/hannasotolongo/casemade-backend/internal/candidate"
	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

// evaluateHardConstraint prevents semantic similarity from becoming proof of
// credentials or duration. These requirements require evidence from the
// corresponding verified profile category.
func evaluateHardConstraint(
	profile candidate.Profile,
	requirement jobs.Requirement,
) (evidenceResult, bool) {
	switch requirement.Category {
	case jobs.RequirementLicense:
		return evaluateExplicitCredential(profile.Licenses, requirement.Text), true

	case jobs.RequirementCertification:
		return evaluateExplicitCredential(profile.Certifications, requirement.Text), true

	case jobs.RequirementEducation:
		return evaluateExplicitCredential(profile.Education, requirement.Text), true

	case jobs.RequirementExperience:
		if _, hasYears := extractRequiredYears(requirement.Text); hasYears {
			result, _ := evaluateExperienceEvidence(
				profile.Experience,
				normalize(requirement.Text),
			)
			return result, true
		}
	}

	return evidenceResult{}, false
}

// Credentials are intentionally checked lexically inside their verified
// evidence category. A semantic model must never infer a license, degree, or
// certification from related work.
func evaluateExplicitCredential(
	evidence []candidate.Evidence,
	requirement string,
) evidenceResult {
	requirement = normalize(requirement)
	if requirement == "" || len(evidence) == 0 {
		return missingEvidence()
	}

	best := 0.0
	for _, item := range evidence {
		coverage := tokenCoverage(requirement, normalize(item.Text))
		if coverage > best {
			best = coverage
		}
	}

	switch {
	case best >= 0.75:
		return supportedEvidence()
	case best >= 0.40:
		return partialEvidence()
	default:
		return missingEvidence()
	}
}
