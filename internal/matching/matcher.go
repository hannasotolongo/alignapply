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
			"CaseMade could not extract enough reliable qualification evidence from this posting to calculate an evidence-match score."

		return result
	}

	profile := candidate.ExtractProfile(request.ResumeText)

	if strings.TrimSpace(profile.RawResumeText) == "" {
		result.MatchPercentage = 0
		result.MatchLevel = "Insufficient Evidence"
		result.Explanation =
			"CaseMade does not have enough résumé evidence to calculate an evidence-match score."

		return result
	}

	totalWeight := 0.0
	earnedWeight := 0.0

	requiredCount := 0
	requiredMissing := 0

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

		if requirement.Importance == jobs.RequirementRequired {
			requiredCount++
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

		case evidencePartial:
			result.PartialRequirements = appendUnique(
				result.PartialRequirements,
				text,
			)

		default:
			result.MissingRequirements = appendUnique(
				result.MissingRequirements,
				text,
			)

			if requirement.Importance == jobs.RequirementRequired {
				requiredMissing++
			}
		}
	}

	if totalWeight == 0 {
		result.MatchPercentage = 0
		result.MatchLevel = "Insufficient Evidence"
		result.Explanation =
			"CaseMade could not identify enough usable qualification evidence to calculate an evidence-match score."

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

	result.MatchLevel = determineMatchLevel(
		percentage,
		requiredCount,
		requiredMissing,
	)

	result.Explanation = buildExplanation(
		percentage,
		result.MatchLevel,
		len(result.SupportedRequirements),
		len(result.PartialRequirements),
		len(result.MissingRequirements),
		requiredCount,
		requiredMissing,
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

	relevant := relevantEvidence(
		profile,
		requirement.Category,
	)

	if len(relevant) == 0 {
		return missingEvidence()
	}

	if requirement.Category == jobs.RequirementExperience {
		if result, ok := evaluateExperienceEvidence(
			relevant,
			requirementText,
		); ok {
			return result
		}
	}

	bestCoverage := 0.0
	combinedEvidence := make([]string, 0, len(relevant))

	for _, evidence := range relevant {
		evidenceText := normalize(evidence.Text)

		if evidenceText == "" {
			continue
		}

		combinedEvidence = append(
			combinedEvidence,
			evidenceText,
		)

		coverage := tokenCoverage(
			requirementText,
			evidenceText,
		)

		if coverage > bestCoverage {
			bestCoverage = coverage
		}
	}

	// A résumé can demonstrate one requirement across multiple evidence
	// statements. Evaluate the union of relevant evidence as well as each
	// individual statement. This prevents legitimate evidence from being
	// lost merely because it appears in separate résumé bullets or sentences.
	if len(combinedEvidence) > 1 {
		aggregateCoverage := tokenCoverage(
			requirementText,
			strings.Join(combinedEvidence, " "),
		)

		if aggregateCoverage > bestCoverage {
			bestCoverage = aggregateCoverage
		}
	}

	switch {
	case bestCoverage >= 0.75:
		return supportedEvidence()

	case bestCoverage >= 0.40:
		return partialEvidence()

	default:
		return missingEvidence()
	}
}

func relevantEvidence(
	profile candidate.Profile,
	category jobs.RequirementCategory,
) []candidate.Evidence {
	switch category {
	case jobs.RequirementSkill:
		return combineEvidence(
			profile.Skills,
			profile.Experience,
		)

	case jobs.RequirementExperience:
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

	requirementWithoutYears :=
		yearsPattern.ReplaceAllString(
			requirement,
			"",
		)

	bestContextCoverage := 0.0
	bestRelevantYears := 0

	for _, item := range evidence {
		text := normalize(item.Text)

		if text == "" {
			continue
		}

		coverage := tokenCoverage(
			requirementWithoutYears,
			text,
		)

		if coverage > bestContextCoverage {
			bestContextCoverage = coverage
		}

		if coverage >= 0.40 {
			years := extractLargestYears(text)

			if years > bestRelevantYears {
				bestRelevantYears = years
			}
		}
	}

	if !hasYears {
		return evidenceResult{}, false
	}

	if bestContextCoverage < 0.40 {
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
	requirementTokens := meaningfulTokens(requirement)

	if len(requirementTokens) == 0 {
		return 0
	}

	evidenceTokens := meaningfulTokens(evidence)

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
		strings.HasPrefix(longer, shorter)
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
		if strings.HasSuffix(token, suffix) &&
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
	base := categoryWeight(requirement.Category)

	if requirement.Importance == jobs.RequirementPreferred {
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
	match := yearsPattern.FindStringSubmatch(text)

	if len(match) < 2 {
		return 0, false
	}

	value, err := strconv.Atoi(match[1])

	if err != nil {
		return 0, false
	}

	return value, true
}

func extractLargestYears(text string) int {
	matches := yearsPattern.FindAllString(
		text,
		-1,
	)

	largest := 0

	for _, match := range matches {
		number := numberPattern.FindString(match)

		if number == "" {
			continue
		}

		value, err := strconv.Atoi(number)

		if err != nil {
			continue
		}

		if value > largest {
			largest = value
		}
	}

	return largest
}

func determineMatchLevel(
	percentage int,
	requiredCount int,
	requiredMissing int,
) string {
	if requiredCount > 0 &&
		requiredMissing == requiredCount {
		return "Stretch"
	}

	if percentage >= 70 {
		return "Strong Match"
	}

	if percentage >= 40 {
		return "Moderate Match"
	}

	return "Stretch"
}

func buildExplanation(
	percentage int,
	level string,
	supported int,
	partial int,
	missing int,
	requiredCount int,
	requiredMissing int,
) string {
	switch {
	case supported == 0 &&
		partial == 0:
		return fmt.Sprintf(
			"%s (%d%% evidence match). The résumé does not clearly demonstrate the qualifications extracted from this posting.",
			level,
			percentage,
		)

	case requiredCount > 0 &&
		requiredMissing > 0:
		return fmt.Sprintf(
			"%s (%d%% evidence match). The résumé clearly supports %d requirement(s), partially supports %d, and does not clearly demonstrate %d. Of the required qualifications, %d of %d are not clearly demonstrated.",
			level,
			percentage,
			supported,
			partial,
			missing,
			requiredMissing,
			requiredCount,
		)

	default:
		return fmt.Sprintf(
			"%s (%d%% evidence match). The résumé clearly supports %d requirement(s), partially supports %d, and does not clearly demonstrate %d.",
			level,
			percentage,
			supported,
			partial,
			missing,
		)
	}
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

	text = nonAlphanumericPattern.ReplaceAllString(
		text,
		" ",
	)

	fields := strings.Fields(text)

	result := make([]string, 0, len(fields))
	seen := make(map[string]struct{})

	for _, token := range fields {
		token = strings.TrimSpace(token)

		if len(token) < 2 {
			continue
		}

		if _, stop := stopWords[token]; stop {
			continue
		}

		if _, exists := seen[token]; exists {
			continue
		}

		seen[token] = struct{}{}
		result = append(result, token)
	}

	sort.Strings(result)

	return result
}

func tokenSet(text string) map[string]struct{} {
	result := make(map[string]struct{})

	for _, token := range meaningfulTokens(text) {
		result[token] = struct{}{}
	}

	return result
}

func appendUnique(
	values []string,
	value string,
) []string {
	value = strings.TrimSpace(value)

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

	return append(values, value)
}
