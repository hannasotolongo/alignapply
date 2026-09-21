package matching

import (
	"strings"

	"github.com/hannasotolongo/casemade-backend/internal/candidate"
	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

// evaluateHardConstraint prevents semantic similarity from becoming proof of
// credentials or duration.
//
// Credentials and explicit years-of-experience requirements are authoritative
// constraints. They must be satisfied by evidence from the appropriate
// candidate category. Semantic similarity may help locate evidence, but it
// cannot manufacture a credential or a number of years.
func evaluateHardConstraint(
	profile candidate.Profile,
	requirement jobs.Requirement,
) (evidenceResult, bool) {
	switch requirement.Category {
	case jobs.RequirementLicense:
		return evaluateExplicitCredential(
			profile.Licenses,
			requirement.Text,
		), true

	case jobs.RequirementCertification:
		return evaluateExplicitCredential(
			profile.Certifications,
			requirement.Text,
		), true

	case jobs.RequirementEducation:
		return evaluateExplicitCredential(
			profile.Education,
			requirement.Text,
		), true

	case jobs.RequirementExperience:
		// Explicit years are a hard constraint. Do not allow the normal
		// semantic requirement path to handle them.
		if _, hasYears :=
			extractRequiredYears(requirement.Text); hasYears {

			return evaluateExperienceYears(
				profile.Experience,
				requirement.Text,
			), true
		}
	}

	return evidenceResult{}, false
}

// evaluateExperienceYears handles requirements such as:
//
//	"5 years of backend engineering experience"
//
// The candidate must have BOTH:
//
//  1. relevant professional-experience evidence; and
//  2. an explicit duration in that same candidate evidence.
//
// The requirement's own number is never considered candidate evidence.
func evaluateExperienceYears(
	evidence []candidate.Evidence,
	requirement string,
) evidenceResult {
	requiredYears, ok :=
		extractRequiredYears(requirement)

	if !ok || requiredYears <= 0 {
		return missingEvidence()
	}

	if len(evidence) == 0 {
		return missingEvidence()
	}

	requirementWithoutYears :=
		normalize(
			yearsPattern.ReplaceAllString(
				requirement,
				"",
			),
		)

	if requirementWithoutYears == "" {
		return missingEvidence()
	}

	// Only professional experience enters this function.
	//
	// Defense in depth: reject anything that looks like education even if an
	// upstream classifier accidentally placed it in Experience.
	candidates := make(
		[]candidate.Evidence,
		0,
		len(evidence),
	)

	for _, item := range evidence {
		text := normalize(item.Text)

		if text == "" {
			continue
		}

		if educationLikeEvidence(text) {
			continue
		}

		// An explicit duration must occur in the candidate evidence itself.
		// This prevents the job requirement's "5 years" from being treated as
		// if it came from the résumé.
		if extractLargestYears(text) == 0 {
			continue
		}

		candidates = append(
			candidates,
			item,
		)
	}

	if len(candidates) == 0 {
		return missingEvidence()
	}

	// Semantic retrieval is used only to find the most relevant candidate
	// evidence. It cannot establish duration by itself.
	evidenceTexts := make(
		[]string,
		0,
		len(candidates),
	)

	for _, item := range candidates {
		text := normalize(item.Text)

		if text != "" {
			evidenceTexts = append(
				evidenceTexts,
				text,
			)
		}
	}

	retrieved :=
		currentSemanticRetriever().Retrieve(
			requirementWithoutYears,
			evidenceTexts,
			5,
		)

	if len(retrieved) == 0 {
		return missingEvidence()
	}

	for _, item := range retrieved {
		text := normalize(item.Evidence)

		if text == "" ||
			educationLikeEvidence(text) {
			continue
		}

		candidateYears :=
			extractLargestYears(text)

		if candidateYears == 0 {
			continue
		}

		// Verification determines whether THIS candidate statement actually
		// demonstrates the required type of experience.
		result :=
			currentEvidenceVerifier().Verify(
				requirementWithoutYears,
				text,
			)

		if result.Level == evidenceMissing {
			continue
		}

		if candidateYears >= requiredYears {
			return supportedEvidence()
		}

		// Relevant explicit experience was found, but the candidate's stated
		// duration is below the required duration.
		return partialEvidence()
	}

	return missingEvidence()
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
		coverage :=
			tokenCoverage(
				requirement,
				normalize(item.Text),
			)

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

func educationLikeEvidence(text string) bool {
	lower := strings.ToLower(
		strings.TrimSpace(text),
	)

	educationMarkers := []string{
		"high school",
		"ged",
		"associate",
		"bachelor",
		"master",
		"doctorate",
		"doctoral",
		"ph.d",
		"phd",
		"degree",
		"academic program",
		"academic",
		"university",
		"college",
		"coursework",
		"graduated",
		"graduation",
	}

	for _, marker := range educationMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	return false
}
