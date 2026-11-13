package matching

import (
	"regexp"
	"strings"
	"time"

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
	s scorer,
	profile candidate.Profile,
	requirement jobs.Requirement,
) (evidenceResult, bool) {
	switch requirement.Category {
	case jobs.RequirementLicense, jobs.RequirementCertification:
		// Résumés often combine "Licenses & Certifications", so either
		// section can hold the credential.
		return evaluateExplicitCredential(
			credentialEvidence(profile),
			requirement.Text,
		), true

	case jobs.RequirementEducation:
		education := withRawLines(profile.Education, profile, true)
		if result, handled := evaluateDegreeLadder(education, requirement.Text); handled {
			return result, true
		}
		return evaluateExplicitCredential(
			education,
			requirement.Text,
		), true

	case jobs.RequirementExperience:
		// Explicit years are a hard constraint. Do not allow the normal
		// semantic requirement path to handle them.
		if _, hasYears :=
			extractRequiredYears(requirement.Text); hasYears {

			return evaluateExperienceYears(
				s,
				withRawLines(profile.Experience, profile, false),
				requirement.Text,
				careerYears(profile, time.Now()),
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
	s scorer,
	evidence []candidate.Evidence,
	requirement string,
	datedCareerYears int,
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
			trailingClause(yearsPattern.ReplaceAllString(
				requirement,
				"",
			)),
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
		return experienceFromDates(s, evidence, requirementWithoutYears, requiredYears, datedCareerYears)
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
		s.retriever().Retrieve(
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
			s.verifier().Verify(
				requirementWithoutYears,
				text,
			)

		if result.Level == evidenceMissing {
			continue
		}

		// The years only count in full when this statement is clearly the
		// same kind of work. "5 years of B2B SaaS sales" half-matches
		// "software engineering" (SaaS is software) but isn't it.
		if candidateYears >= requiredYears && result.Level == evidenceSupported {
			return supportedEvidence()
		}
		if candidateYears >= requiredYears {
			return partialEvidence()
		}

		// Relevant explicit experience was found, but the candidate's stated
		// duration is below the required duration.
		if datedCareerYears >= requiredYears && result.Level == evidenceSupported {
			return supportedEvidence()
		}
		return partialEvidence()
	}

	return experienceFromDates(s, evidence, requirementWithoutYears, requiredYears, datedCareerYears)
}

// experienceFromDates handles the common résumé that lists date ranges rather
// than "N years of ...": if the résumé's work history shows relevant
// experience, its dated length counts toward the required years.
func experienceFromDates(
	s scorer,
	evidence []candidate.Evidence,
	requirementWithoutYears string,
	requiredYears int,
	datedCareerYears int,
) evidenceResult {
	if datedCareerYears <= 0 {
		return missingEvidence()
	}

	texts := make([]string, 0, len(evidence))
	for _, item := range evidence {
		text := normalize(item.Text)
		if text != "" && !educationLikeEvidence(text) {
			texts = append(texts, text)
		}
	}
	if len(texts) == 0 {
		return missingEvidence()
	}

	retrieved := s.retriever().Retrieve(requirementWithoutYears, texts, 5)
	combined := make([]string, 0, len(retrieved))
	for _, item := range retrieved {
		if text := strings.TrimSpace(item.Evidence); text != "" {
			combined = append(combined, text)
		}
	}
	if len(combined) == 0 {
		combined = texts
		if len(combined) > 8 {
			combined = combined[:8]
		}
	}
	joined := strings.Join(combined, "\n")

	relevance := verifyEvidenceDeterministically(requirementWithoutYears, joined)
	if relevance.Level != evidenceSupported {
		relevance = s.verifier().Verify(requirementWithoutYears, joined)
	}

	switch {
	case relevance.Level == evidenceMissing:
		return missingEvidence()
	case relevance.Level == evidenceSupported && datedCareerYears >= requiredYears:
		return supportedEvidence()
	default:
		return partialEvidence()
	}
}

func credentialEvidence(profile candidate.Profile) []candidate.Evidence {
	evidence := make([]candidate.Evidence, 0, len(profile.Licenses)+len(profile.Certifications)+len(profile.Education))
	evidence = append(evidence, profile.Licenses...)
	evidence = append(evidence, profile.Certifications...)
	// Résumé parsers don't always file credential lines under a credential
	// heading ("BLS, ACLS (AHA)" can land in skills), so also include any
	// line that names a credential explicitly.
	for _, item := range profile.AllEvidence {
		if credentialLine.MatchString(item.Text) {
			evidence = append(evidence, item)
		}
	}
	return evidence
}

// credentialFiller words describe a credential requirement without
// identifying which credential it is.
var credentialFiller = map[string]struct{}{
	"required": {}, "require": {}, "requires": {}, "preferred": {}, "active": {}, "current": {}, "currently": {},
	"valid": {}, "unrestricted": {}, "good": {}, "standing": {}, "must": {}, "have": {}, "hold": {}, "possess": {},
	"certification": {}, "certifications": {}, "certified": {}, "certificate": {}, "license": {}, "licensed": {},
	"licenses": {}, "licensure": {}, "card": {}, "state": {}, "upon": {}, "hire": {}, "within": {}, "days": {},
	"months": {}, "obtain": {}, "maintain": {}, "eligible": {}, "eligibility": {}, "compact": {}, "multistate": {},
	"multi": {}, "american": {}, "heart": {}, "association": {}, "aha": {}, "red": {}, "cross": {}, "both": {},
	"degree": {}, "degrees": {}, "field": {}, "fields": {}, "area": {}, "discipline": {}, "equivalent": {}, "accredited": {},
	"college": {}, "university": {}, "school": {}, "program": {}, "major": {}, "concentration": {}, "similar": {},
}

func credentialTokens(text string) []string {
	text = candidate.CanonicalCredentials(text)
	tokens := meaningfulTokens(normalize(text))
	kept := tokens[:0]
	for _, token := range tokens {
		if _, filler := credentialFiller[token]; !filler {
			kept = append(kept, token)
		}
	}
	return kept
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

	requirementTokens := credentialTokens(requirement)
	if len(requirementTokens) == 0 {
		return missingEvidence()
	}

	best := 0.0

	for _, item := range evidence {
		itemTokens := impliedCredentials(credentialTokens(item.Text))
		matched := 0
		for _, token := range requirementTokens {
			if tokenDemonstrated(token, itemTokens) {
				matched++
			}
		}
		coverage := float64(matched) / float64(len(requirementTokens))

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

var credentialLine = regexp.MustCompile(`(?i)(licens|certif|\b(bls|acls|pals|nrp|tncc|ccrn|cpr|cna|lpn|lvn|rn|aprn|np|cpa|pmp|cdl)\b)`)

// impliedCredentials adds credentials that another one includes: BLS, ACLS,
// PALS and NRP courses all certify CPR.
func impliedCredentials(tokens []string) []string {
	for _, token := range tokens {
		switch token {
		case "bls", "acls", "pals", "nrp":
			return append(tokens, "cpr")
		}
	}
	return tokens
}

// trailingClausePattern finds where a years requirement stops naming the
// kind of work and starts describing the person: "1+ years in customer
// service or call center support, with a proven ability to handle…".
var trailingClausePattern = regexp.MustCompile(`(?i)(,\s*(with|including|ideally|preferably|where|and a|and an)\b|;|\s+with a (proven|demonstrated|strong)\b)`)

func trailingClause(text string) string {
	if location := trailingClausePattern.FindStringIndex(text); location != nil && location[0] > 12 {
		return text[:location[0]]
	}
	return text
}
