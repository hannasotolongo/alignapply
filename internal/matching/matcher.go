package matching

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/hannasotolongo/casemade-backend/internal/candidate"
	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

type evidenceLevel int

const (
	evidenceMissing evidenceLevel = iota
	evidencePartial
	evidenceSupported
)

type evidenceResult struct {
	Level evidenceLevel
	Score float64
}

var (
	yearsPattern = regexp.MustCompile(
		`(?i)\b(\d+)\s*(?:\+|-\s*\d+)?\s*(?:years?|yrs?)\b`,
	)

	numberPattern = regexp.MustCompile(`\d+`)

	nonAlphanumericPattern = regexp.MustCompile(
		`[^a-z0-9+#/]+`,
	)
)

var stopWords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "as": {},
	"at": {}, "be": {}, "been": {}, "being": {}, "by": {},
	"for": {}, "from": {}, "has": {}, "have": {}, "in": {},
	"into": {}, "is": {}, "it": {}, "of": {}, "on": {},
	"or": {}, "our": {}, "that": {}, "the": {}, "their": {},
	"this": {}, "to": {}, "with": {}, "you": {}, "your": {},
	"required": {}, "preferred": {}, "minimum": {}, "must": {},
	"should": {}, "candidate": {}, "candidates": {},

	// Qualification-language words describe how an employer phrases a
	// requirement, but do not themselves identify the capability being
	// requested. Excluding them prevents sentence wording from dominating
	// evidence comparison.
	"ability": {}, "abilities": {},
	"experience": {}, "experienced": {},
	"exposure": {},
	"familiar": {}, "familiarity": {},
	"knowledge":     {},
	"understanding": {},
	"proficiency":   {}, "proficient": {},
	"skill": {}, "skills": {},
	"strong": {}, "excellent": {}, "solid": {},
	"demonstrated": {}, "proven": {},
	"track": {}, "record": {},
	"plus": {}, "bonus": {},
	"some": {}, "any": {},
	"interest": {}, "interested": {},
	"coursework":  {},
	"comfortable": {}, "comfort": {},
	"working": {}, "work": {},
	"using": {}, "use": {},

	// Generic action verbs commonly wrap the actual capability in job
	// requirements. They should not prevent direct capability evidence from
	// being verified simply because the resume uses a different action verb
	// (for example, "developing backend services" vs "built backend services").
	"build": {}, "building": {}, "built": {},
	"develop": {}, "developing": {}, "developed": {},

	"related": {}, "relevant": {},
	"professional": {},
	"practical":    {},

	// Structural comparison words introduce examples, categories, or
	// qualification phrasing but are not independently required evidence.
	"like":         {},
	"such":         {},
	"including":    {},
	"include":      {},
	"includes":     {},
	"tool":         {},
	"tools":        {},
	"technology":   {},
	"technologies": {},
	"platform":     {},
	"platforms":    {},
	"area":         {},
	"areas":        {},
	"concept":      {},
	"concepts":     {},
	"foundation":   {},
	"foundations":  {},
}

func Match(
	request jobs.SearchRequest,
	job jobs.Job,
) jobs.JobMatch {
	result := newJobMatch(job)

	quality := jobs.EvaluateExtractionQuality(job)

	if !quality.Scorable {
		result.MatchPercentage = 0
		result.MatchLevel = "Insufficient Evidence"
		result.Explanation =
			"AlignApply could not extract enough reliable qualification evidence from this posting to classify the fit."

		return result
	}

	var profile candidate.Profile

	// Career Profile is the canonical evidence source for Your Fit.
	//
	// ResumeText remains a legacy fallback while existing clients migrate.
	// We intentionally do not merge an individual resume into a populated
	// Career Profile here. Resume evidence will later power Resume Fit and
	// Best Resume independently from the user's overall Career Profile.
	if request.CareerProfile.HasEvidence() {
		profile = candidate.BuildProfile(
			"",
			request.CareerProfile,
		)
	} else {
		profile = candidate.ExtractProfile(
			request.ResumeText,
		)
	}

	if len(profile.AllEvidence) == 0 {
		result.MatchPercentage = 0
		result.MatchLevel = "Insufficient Evidence"
		result.Explanation =
			"AlignApply does not have enough candidate evidence to classify the fit."

		return result
	}

	totalWeight := 0.0
	earnedWeight := 0.0

	requiredCount := 0
	requiredSupported := 0
	requiredPartial := 0
	requiredMissing := 0

	criticalRequiredCount := 0
	criticalRequiredNotSupported := 0

	for _, requirement := range job.Requirements {
		text := strings.TrimSpace(requirement.Text)

		if text == "" {
			continue
		}

		weight := requirementWeight(requirement)

		if weight <= 0 {
			continue
		}

		totalWeight += weight

		isRequired :=
			requirement.Importance == jobs.RequirementRequired

		isCriticalRequired :=
			isRequired &&
				isCriticalRequirement(requirement)

		if isRequired {
			requiredCount++
		}

		if isCriticalRequired {
			criticalRequiredCount++
		}

		evidence := evaluateRequirementEvidence(
			profile,
			requirement,
		)

		earnedWeight += weight * evidence.Score

		switch evidence.Level {
		case evidenceSupported:
			result.SupportedRequirements = appendUnique(
				result.SupportedRequirements,
				text,
			)

			if isRequired {
				requiredSupported++
			}

		case evidencePartial:
			result.PartialRequirements = appendUnique(
				result.PartialRequirements,
				text,
			)

			if isRequired {
				requiredPartial++
			}

			if isCriticalRequired {
				criticalRequiredNotSupported++
			}

		default:
			result.MissingRequirements = appendUnique(
				result.MissingRequirements,
				text,
			)

			if isRequired {
				requiredMissing++
			}

			if isCriticalRequired {
				criticalRequiredNotSupported++
			}
		}
	}

	if totalWeight == 0 {
		result.MatchPercentage = 0
		result.MatchLevel = "Insufficient Evidence"
		result.Explanation =
			"AlignApply could not identify enough usable qualification evidence to classify the fit."

		return result
	}

	percentage := int(
		math.Round(
			(earnedWeight / totalWeight) * 100,
		),
	)

	if percentage < 0 {
		percentage = 0
	}

	if percentage > 100 {
		percentage = 100
	}

	result.MatchPercentage = percentage

	result.MatchLevel = determineFitCategory(
		requiredCount,
		requiredSupported,
		requiredPartial,
		requiredMissing,
		criticalRequiredCount,
		criticalRequiredNotSupported,
	)

	result.Explanation = buildFitExplanation(
		result.MatchLevel,
		len(result.SupportedRequirements),
		len(result.PartialRequirements),
		len(result.MissingRequirements),
		requiredCount,
		requiredSupported,
		requiredPartial,
		requiredMissing,
		criticalRequiredNotSupported,
	)

	return result
}

func newJobMatch(job jobs.Job) jobs.JobMatch {
	return jobs.JobMatch{
		ID:          job.ID,
		Title:       job.Title,
		Company:     job.Company,
		Description: job.Description,

		Location:        job.Location,
		CountryCode:     job.CountryCode,
		WorkArrangement: job.WorkArrangement,
		EmploymentType:  job.EmploymentType,

		SalaryMin:      job.SalaryMin,
		SalaryMax:      job.SalaryMax,
		SalaryCurrency: job.SalaryCurrency,
		SalaryPeriod:   job.SalaryPeriod,

		PostedAt:  job.PostedAt,
		ExpiresAt: job.ExpiresAt,

		Requirements: job.Requirements,

		MinimumQualifications:   job.MinimumQualifications,
		PreferredQualifications: job.PreferredQualifications,
		Skills:                  job.Skills,

		ExperienceRequirements:    job.ExperienceRequirements,
		EducationRequirements:     job.EducationRequirements,
		LicenseRequirements:       job.LicenseRequirements,
		CertificationRequirements: job.CertificationRequirements,
		PhysicalRequirements:      job.PhysicalRequirements,
		TravelRequirements:        job.TravelRequirements,

		SupportedRequirements: []string{},
		PartialRequirements:   []string{},
		MissingRequirements:   []string{},

		ApplyURL:  job.ApplyURL,
		SourceURL: job.SourceURL,
		Source:    job.Source,
	}
}

func evaluateRequirementEvidence(
	profile candidate.Profile,
	requirement jobs.Requirement,
) evidenceResult {
	requirementText := normalize(requirement.Text)

	if requirementText == "" {
		return missingEvidence()
	}

	// Hard constraints remain authoritative and are evaluated before semantic
	// retrieval. Similarity must never override a missing license, degree,
	// certification, or other structured requirement.
	if result, handled := evaluateHardConstraint(profile, requirement); handled {
		return result
	}

	relevant := relevantEvidence(
		profile,
		requirement.Category,
	)

	if len(relevant) == 0 {
		return missingEvidence()
	}

	// Explicit years of experience remain a hard duration constraint. Semantic
	// retrieval may help locate the relevant experience statement, but the
	// duration itself is evaluated deterministically.
	if requirement.Category == jobs.RequirementExperience {
		if _, hasYears := extractRequiredYears(requirementText); hasYears {
			result, _ := evaluateExperienceEvidence(
				relevant,
				requirementText,
			)
			return result
		}
	}

	evidenceTexts := make(
		[]string,
		0,
		len(relevant),
	)

	for _, item := range relevant {
		text := normalize(item.Text)
		if text != "" {
			evidenceTexts = append(evidenceTexts, text)
		}
	}

	if len(evidenceTexts) == 0 {
		return missingEvidence()
	}

	// Embeddings are retrieval only. We intentionally do not convert a cosine
	// score into Supported/Partial/Missing.
	retrieved := currentSemanticRetriever().Retrieve(
		requirementText,
		evidenceTexts,
		5,
	)

	if len(retrieved) == 0 {
		return missingEvidence()
	}

	best := missingEvidence()

	for _, candidate := range retrieved {
		result := currentEvidenceVerifier().Verify(
			requirementText,
			candidate.Evidence,
		)

		if result.Level > best.Level {
			best = result
		}

		if best.Level == evidenceSupported {
			return best
		}
	}

	// Multiple statements may jointly demonstrate a requirement. Retrieval
	// chooses the evidence to inspect; verification still makes the decision.
	if len(retrieved) > 1 {
		combined := make([]string, 0, len(retrieved))
		for _, candidate := range retrieved {
			combined = append(combined, candidate.Evidence)
		}

		result := currentEvidenceVerifier().Verify(
			requirementText,
			strings.Join(combined, " "),
		)

		if result.Level > best.Level {
			best = result
		}
	}

	return best
}

// verifyEvidenceDeterministically is the conservative verifier boundary used
// until a model-backed evidence verifier is configured.
//
// Crucially, it does not inspect embedding similarity. A semantic retrieval
// score means "inspect this evidence", not "this requirement is proven".
func verifyEvidenceDeterministically(
	requirement string,
	evidence string,
) evidenceResult {
	requirementTokens := meaningfulTokens(requirement)
	if len(requirementTokens) == 0 {
		return missingEvidence()
	}

	evidenceTokens := meaningfulTokens(evidence)

	matched := 0
	for _, requirementToken := range requirementTokens {
		if tokenDemonstrated(requirementToken, evidenceTokens) {
			matched++
		}
	}

	if matched == 0 {
		return missingEvidence()
	}

	// Verification is based on demonstrated requirement concepts, not on the
	// embedding retrieval score. For short requirements, every meaningful
	// concept must be present. This correctly treats:
	//
	//   "building distributed systems"
	//   "designed distributed systems ..."
	//
	// as direct evidence even though "building" and "designed" are different
	// action verbs. The capability itself ("distributed systems") is explicit.
	if matched == len(requirementTokens) {
		return supportedEvidence()
	}

	// Requirements often contain an action verb plus the actual capability.
	// When a two-concept requirement has one directly demonstrated capability,
	// retain partial support rather than treating semantic retrieval as proof.
	if matched*2 >= len(requirementTokens) {
		return partialEvidence()
	}

	return missingEvidence()
}

func relevantEvidence(
	profile candidate.Profile,
	category jobs.RequirementCategory,
) []candidate.Evidence {
	switch category {
	case jobs.RequirementSkill:
		// Skills may be demonstrated directly or through professional
		// experience, projects, or user-confirmed summary evidence.
		return combineEvidence(
			profile.Skills,
			profile.Experience,
			profile.Projects,
			profile.Summary,
		)

	case jobs.RequirementExperience:
		// Experience requirements deliberately use only professional
		// experience evidence. Project durations and summary text must not
		// satisfy professional years-of-experience requirements.
		return profile.Experience

	case jobs.RequirementEducation:
		return profile.Education

	case jobs.RequirementCertification:
		return profile.Certifications

	case jobs.RequirementLicense:
		return profile.Licenses

	case jobs.RequirementTravel,
		jobs.RequirementPhysical,
		jobs.RequirementOther:
		return profile.AllEvidence

	default:
		return profile.AllEvidence
	}
}

func combineEvidence(
	groups ...[]candidate.Evidence,
) []candidate.Evidence {
	result := make([]candidate.Evidence, 0)
	seen := make(map[string]struct{})

	for _, group := range groups {
		for _, evidence := range group {
			key := string(evidence.Category) +
				":" +
				normalize(evidence.Text)

			if _, exists := seen[key]; exists {
				continue
			}

			seen[key] = struct{}{}
			result = append(result, evidence)
		}
	}

	return result
}

func evaluateExperienceEvidence(
	evidence []candidate.Evidence,
	requirement string,
) (evidenceResult, bool) {
	requiredYears, hasYears :=
		extractRequiredYears(requirement)

	if !hasYears {
		return evidenceResult{}, false
	}

	requirementWithoutYears :=
		yearsPattern.ReplaceAllString(
			requirement,
			"",
		)

	evidenceTexts := make(
		[]string,
		0,
		len(evidence),
	)

	for _, item := range evidence {
		text := normalize(item.Text)
		if text != "" {
			evidenceTexts = append(evidenceTexts, text)
		}
	}

	if len(evidenceTexts) == 0 {
		return missingEvidence(), true
	}

	// Retrieval narrows the professional-experience evidence we inspect.
	// Retrieval score itself never proves that the experience is relevant.
	retrieved := currentSemanticRetriever().Retrieve(
		requirementWithoutYears,
		evidenceTexts,
		5,
	)

	bestContext := evidenceMissing
	bestRelevantYears := 0

	for _, candidate := range retrieved {
		contextResult := currentEvidenceVerifier().Verify(
			requirementWithoutYears,
			candidate.Evidence,
		)

		if contextResult.Level > bestContext {
			bestContext = contextResult.Level
		}

		// Only evidence independently verified as at least partial may
		// contribute years toward the hard duration requirement.
		if contextResult.Level >= evidencePartial {
			years := extractLargestYears(candidate.Evidence)
			if years > bestRelevantYears {
				bestRelevantYears = years
			}
		}
	}

	if bestContext == evidenceMissing {
		return missingEvidence(), true
	}

	if bestRelevantYears == 0 {
		return partialEvidence(), true
	}

	if bestRelevantYears >= requiredYears {
		return supportedEvidence(), true
	}

	return partialEvidence(), true
}

func tokenCoverage(
	requirement string,
	evidence string,
) float64 {
	requirementTokens :=
		meaningfulTokens(requirement)

	if len(requirementTokens) == 0 {
		return 0
	}

	evidenceTokens :=
		meaningfulTokens(evidence)

	matched := 0

	for _, requirementToken := range requirementTokens {

		if tokenDemonstrated(
			requirementToken,
			evidenceTokens,
		) {
			matched++
		}
	}

	return float64(matched) /
		float64(len(requirementTokens))
}

// tokenDemonstrated performs conservative lexical matching.
//
// Exact token equality remains the primary rule. For longer alphabetic
// words, a shared lexical stem can also establish a match. This handles
// ordinary morphological variants such as:
//
//	containerization <-> containerized
//	deployment       <-> deploying
//
// Short identifiers and technical tokens such as Go, C++, AWS, SQL,
// CI/CD, version numbers, and acronyms are never stem-matched.
func tokenDemonstrated(
	requirementToken string,
	evidenceTokens []string,
) bool {
	for _, evidenceToken := range evidenceTokens {
		if requirementToken == evidenceToken {
			return true
		}

		if lexicalVariantMatch(
			requirementToken,
			evidenceToken,
		) {
			return true
		}
	}

	return false
}

func lexicalVariantMatch(
	left string,
	right string,
) bool {
	if len(left) < 7 || len(right) < 7 {
		return false
	}

	if !alphabeticToken(left) ||
		!alphabeticToken(right) {
		return false
	}

	leftStem := lexicalStem(left)
	rightStem := lexicalStem(right)

	if len(leftStem) < 6 ||
		len(rightStem) < 6 {
		return false
	}

	if leftStem == rightStem {
		return true
	}

	shorter := leftStem
	longer := rightStem

	if len(shorter) > len(longer) {
		shorter, longer = longer, shorter
	}

	return len(shorter) >= 7 &&
		strings.HasPrefix(
			longer,
			shorter,
		)
}

func lexicalStem(token string) string {
	suffixes := []string{
		"ization",
		"isation",
		"ational",
		"ation",
		"ition",
		"ments",
		"ment",
		"ingly",
		"edly",
		"ing",
		"ized",
		"ised",
		"ize",
		"ise",
		"ers",
		"er",
		"ed",
	}

	for _, suffix := range suffixes {
		if strings.HasSuffix(
			token,
			suffix,
		) &&
			len(token)-len(suffix) >= 6 {

			return strings.TrimSuffix(
				token,
				suffix,
			)
		}
	}

	return token
}

func alphabeticToken(token string) bool {
	for _, r := range token {
		if r < 'a' || r > 'z' {
			return false
		}
	}

	return token != ""
}

func supportedEvidence() evidenceResult {
	return evidenceResult{
		Level: evidenceSupported,
		Score: 1,
	}
}

func partialEvidence() evidenceResult {
	return evidenceResult{
		Level: evidencePartial,
		Score: 0.5,
	}
}

func missingEvidence() evidenceResult {
	return evidenceResult{
		Level: evidenceMissing,
		Score: 0,
	}
}

func requirementWeight(
	requirement jobs.Requirement,
) float64 {
	base :=
		categoryWeight(
			requirement.Category,
		)

	if requirement.Importance ==
		jobs.RequirementPreferred {

		return base * 0.5
	}

	return base
}

func categoryWeight(
	category jobs.RequirementCategory,
) float64 {
	switch category {
	case jobs.RequirementLicense:
		return 3.5

	case jobs.RequirementCertification:
		return 3.5

	case jobs.RequirementExperience:
		return 3.0

	case jobs.RequirementEducation:
		return 3.0

	case jobs.RequirementSkill:
		return 2.5

	case jobs.RequirementTravel:
		return 1.5

	case jobs.RequirementPhysical:
		return 1.5

	default:
		return 1.0
	}
}

func extractRequiredYears(
	text string,
) (int, bool) {
	match :=
		yearsPattern.FindStringSubmatch(
			text,
		)

	if len(match) < 2 {
		return 0, false
	}

	value, err :=
		strconv.Atoi(match[1])

	if err != nil {
		return 0, false
	}

	return value, true
}

func extractLargestYears(text string) int {
	matches :=
		yearsPattern.FindAllString(
			text,
			-1,
		)

	largest := 0

	for _, match := range matches {
		number :=
			numberPattern.FindString(match)

		if number == "" {
			continue
		}

		value, err :=
			strconv.Atoi(number)

		if err != nil {
			continue
		}

		if value > largest {
			largest = value
		}
	}

	return largest
}

func isCriticalRequirement(
	requirement jobs.Requirement,
) bool {
	if requirement.Importance !=
		jobs.RequirementRequired {

		return false
	}

	switch requirement.Category {
	case jobs.RequirementLicense,
		jobs.RequirementCertification,
		jobs.RequirementEducation:

		return true

	case jobs.RequirementExperience:
		_, hasExplicitYears :=
			extractRequiredYears(
				requirement.Text,
			)

		return hasExplicitYears

	default:
		return false
	}
}

// determineFitCategory intentionally classifies fit from required-qualification
// outcomes rather than from the numeric evidence score. MatchPercentage remains
// populated for API compatibility and internal diagnostics, but it is not the
// source of truth for the user-facing category.
func determineFitCategory(
	requiredCount int,
	requiredSupported int,
	requiredPartial int,
	requiredMissing int,
	criticalRequiredCount int,
	criticalRequiredNotSupported int,
) string {
	if requiredCount == 0 {
		return "Good Fit"
	}

	// A required license, certification, education requirement, or explicit
	// years-of-experience requirement that is not fully supported is a
	// material gap. We do not allow unrelated matching skills to wash it out.
	if criticalRequiredNotSupported > 0 {
		return "Reach"
	}

	supportedRatio :=
		float64(requiredSupported) /
			float64(requiredCount)

	demonstratedRatio :=
		float64(
			requiredSupported+
				requiredPartial,
		) /
			float64(requiredCount)

	// Best Fit is deliberately strict: every required qualification must have
	// at least some evidence, most must be clearly supported, and at most one
	// required qualification may only be partial.
	if requiredMissing == 0 &&
		supportedRatio >= 0.75 &&
		requiredPartial <= 1 {

		return "Best Fit"
	}

	// Good Fit means the candidate demonstrates a meaningful majority of the
	// required qualifications and fewer than half are wholly unsupported.
	if demonstratedRatio >= 0.60 &&
		requiredMissing*2 < requiredCount {

		return "Good Fit"
	}

	_ = criticalRequiredCount

	return "Reach"
}

func buildFitExplanation(
	level string,
	supported int,
	partial int,
	missing int,
	requiredCount int,
	requiredSupported int,
	requiredPartial int,
	requiredMissing int,
	criticalRequiredNotSupported int,
) string {
	if supported == 0 &&
		partial == 0 {

		return fmt.Sprintf(
			"%s. The profile does not clearly demonstrate the qualifications extracted from this posting.",
			level,
		)
	}

	if criticalRequiredNotSupported > 0 {
		return fmt.Sprintf(
			"%s. The profile supports %d requirement(s), partially supports %d, and does not clearly demonstrate %d. %d required high-impact qualification(s) are not fully demonstrated.",
			level,
			supported,
			partial,
			missing,
			criticalRequiredNotSupported,
		)
	}

	if requiredCount > 0 {
		return fmt.Sprintf(
			"%s. Of %d required qualification(s), the profile clearly supports %d, partially supports %d, and does not clearly demonstrate %d. Across all extracted qualifications, %d are supported, %d are partial, and %d are not clearly demonstrated.",
			level,
			requiredCount,
			requiredSupported,
			requiredPartial,
			requiredMissing,
			supported,
			partial,
			missing,
		)
	}

	return fmt.Sprintf(
		"%s. The profile clearly supports %d qualification(s), partially supports %d, and does not clearly demonstrate %d.",
		level,
		supported,
		partial,
		missing,
	)
}

func normalize(text string) string {
	text = strings.ToLower(text)

	text = strings.Map(
		func(r rune) rune {
			switch {
			case unicode.IsLetter(r),
				unicode.IsDigit(r):
				return r

			case r == '+',
				r == '#',
				r == '.',
				r == '/':
				return r

			default:
				return ' '
			}
		},
		text,
	)

	return strings.Join(
		strings.Fields(text),
		" ",
	)
}

func meaningfulTokens(text string) []string {
	text = strings.ToLower(text)

	text =
		nonAlphanumericPattern.ReplaceAllString(
			text,
			" ",
		)

	fields := strings.Fields(text)

	result :=
		make(
			[]string,
			0,
			len(fields),
		)

	seen :=
		make(map[string]struct{})

	for _, token := range fields {
		token =
			strings.TrimSpace(token)

		if len(token) < 2 {
			continue
		}

		if _, stop :=
			stopWords[token]; stop {

			continue
		}

		if _, exists :=
			seen[token]; exists {

			continue
		}

		seen[token] = struct{}{}

		result = append(
			result,
			token,
		)
	}

	sort.Strings(result)

	return result
}

func tokenSet(text string) map[string]struct{} {
	result :=
		make(map[string]struct{})

	for _, token := range meaningfulTokens(text) {

		result[token] = struct{}{}
	}

	return result
}

func appendUnique(
	values []string,
	value string,
) []string {
	value =
		strings.TrimSpace(value)

	if value == "" {
		return values
	}

	for _, existing := range values {
		if strings.EqualFold(
			strings.TrimSpace(existing),
			value,
		) {
			return values
		}
	}

	return append(
		values,
		value,
	)
}
