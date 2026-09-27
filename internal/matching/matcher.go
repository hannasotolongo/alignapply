package matching

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	// being verified simply because the resume uses a different action verb.
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
	// Career Profile here.
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

	// Evaluate independent requirements concurrently.
	//
	// A bounded worker pool prevents a large job posting from creating an
	// unbounded number of Ollama requests. Results are stored by requirement
	// index so aggregation remains deterministic.
	type requirementEvaluation struct {
		index    int
		text     string
		weight   float64
		required bool
		critical bool
		evidence evidenceResult
		valid    bool
	}

	evaluations := make(
		[]requirementEvaluation,
		len(job.Requirements),
	)

	const maxConcurrentRequirements = 6

	work := make(chan int)
	var wg sync.WaitGroup

	workerCount := maxConcurrentRequirements

	if len(job.Requirements) < workerCount {
		workerCount = len(job.Requirements)
	}

	for worker := 0; worker < workerCount; worker++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for index := range work {
				requirement := job.Requirements[index]

				text := strings.TrimSpace(
					requirement.Text,
				)

				if text == "" {
					continue
				}

				weight := requirementWeight(
					requirement,
				)

				if weight <= 0 {
					continue
				}

				isRequired :=
					requirement.Importance ==
						jobs.RequirementRequired

				isCriticalRequired :=
					isRequired &&
						isCriticalRequirement(
							requirement,
						)

				evidence :=
					evaluateRequirementEvidence(
						profile,
						requirement,
					)

				evaluations[index] =
					requirementEvaluation{
						index:    index,
						text:     text,
						weight:   weight,
						required: isRequired,
						critical: isCriticalRequired,
						evidence: evidence,
						valid:    true,
					}
			}
		}()
	}

	for index := range job.Requirements {
		work <- index
	}

	close(work)
	wg.Wait()

	totalWeight := 0.0
	earnedWeight := 0.0

	requiredCount := 0
	requiredSupported := 0
	requiredPartial := 0
	requiredMissing := 0

	criticalRequiredCount := 0
	criticalRequiredNotSupported := 0

	// Aggregate in requirement order. This keeps output deterministic even
	// though evaluation itself is concurrent.
	for _, evaluation := range evaluations {
		if !evaluation.valid {
			continue
		}

		totalWeight += evaluation.weight
		earnedWeight +=
			evaluation.weight *
				evaluation.evidence.Score

		if evaluation.required {
			requiredCount++
		}

		if evaluation.critical {
			criticalRequiredCount++
		}

		switch evaluation.evidence.Level {
		case evidenceSupported:
			result.SupportedRequirements =
				appendUnique(
					result.SupportedRequirements,
					evaluation.text,
				)

			if evaluation.required {
				requiredSupported++
			}

		case evidencePartial:
			result.PartialRequirements =
				appendUnique(
					result.PartialRequirements,
					evaluation.text,
				)

			if evaluation.required {
				requiredPartial++
			}

			if evaluation.critical {
				criticalRequiredNotSupported++
			}

		default:
			result.MissingRequirements =
				appendUnique(
					result.MissingRequirements,
					evaluation.text,
				)

			if evaluation.required {
				requiredMissing++
			}

			if evaluation.critical {
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

	// Explicit years of experience remain a hard duration constraint.
	if requirement.Category == jobs.RequirementExperience {
		if _, hasYears := extractRequiredYears(requirementText); hasYears {
			// Duration must come from locally scoped experience evidence.
			// Skills, summaries, education dates, and unrelated résumé dates
			// must never establish years of experience.
			result, _ := evaluateExperienceEvidence(
				profile.Experience,
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
			evidenceTexts = append(
				evidenceTexts,
				text,
			)
		}
	}

	if len(evidenceTexts) == 0 {
		return missingEvidence()
	}

	// Embeddings are retrieval only. A cosine similarity score is never
	// converted directly into Supported, Partial, or Missing.
	retrieved := currentSemanticRetriever().Retrieve(
		requirementText,
		evidenceTexts,
		5,
	)

	if len(retrieved) == 0 {
		return missingEvidence()
	}

	// Retrieval identifies the strongest candidate evidence. The deterministic
	// verifier gets the first opportunity to decide the requirement. Only
	// ambiguous evidence is sent to the model-backed verifier.
	//
	// This is important for local Ollama deployments: an obvious lexical match
	// should not consume a Qwen generation. Semantic retrieval still remains
	// responsible for finding cross-domain or differently worded evidence, and
	// those ambiguous cases can fall through to Qwen.
	combined := make(
		[]string,
		0,
		len(retrieved),
	)

	for _, retrievedEvidence := range retrieved {
		text := strings.TrimSpace(
			retrievedEvidence.Evidence,
		)

		if text == "" {
			continue
		}

		combined = append(
			combined,
			text,
		)
	}

	if len(combined) == 0 {
		return missingEvidence()
	}

	combinedEvidence := strings.Join(
		combined,
		"\n",
	)

	deterministicResult := verifyEvidenceDeterministically(
		requirementText,
		combinedEvidence,
	)

	if deterministicResult.Level == evidenceSupported {
		return deterministicResult
	}

	// Missing and partial lexical coverage can still represent a valid
	// semantic match (for example, "patient counseling" vs "patient
	// education"). Let the model resolve those genuinely ambiguous cases.
	return currentEvidenceVerifier().Verify(
		requirementText,
		combinedEvidence,
	)
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
		if tokenDemonstrated(
			requirementToken,
			evidenceTokens,
		) {
			matched++
		}
	}

	if matched == 0 {
		return missingEvidence()
	}

	if matched == len(requirementTokens) {
		return supportedEvidence()
	}

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
		return combineEvidence(
			profile.Skills,
			profile.Experience,
			profile.Projects,
			profile.Summary,
		)

	case jobs.RequirementExperience:
		// Experience requirements without an explicit duration can be
		// demonstrated by experience, projects, skills, or summary evidence.
		//
		// Explicit years-of-experience requirements are narrowed back to
		// profile.Experience in evaluateRequirementEvidence so unrelated
		// résumé dates cannot satisfy a duration constraint.
		return combineEvidence(
			profile.Experience,
			profile.Projects,
			profile.Skills,
			profile.Summary,
		)

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

			result = append(
				result,
				evidence,
			)
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
			evidenceTexts = append(
				evidenceTexts,
				text,
			)
		}
	}

	if len(evidenceTexts) == 0 {
		return missingEvidence(), true
	}

	retrieved := currentSemanticRetriever().Retrieve(
		requirementWithoutYears,
		evidenceTexts,
		5,
	)

	if len(retrieved) == 0 {
		return missingEvidence(), true
	}

	// For explicit years-of-experience requirements, the semantic context
	// still needs to be verified before any duration can count.
	//
	// Bundle retrieved evidence so the model performs one semantic
	// verification instead of one inference per evidence statement.
	combined := make(
		[]string,
		0,
		len(retrieved),
	)

	for _, retrievedEvidence := range retrieved {
		text := strings.TrimSpace(
			retrievedEvidence.Evidence,
		)

		if text == "" {
			continue
		}

		combined = append(
			combined,
			text,
		)
	}

	if len(combined) == 0 {
		return missingEvidence(), true
	}

	combinedEvidence := strings.Join(
		combined,
		"\n",
	)

	contextResult := verifyEvidenceDeterministically(
		requirementWithoutYears,
		combinedEvidence,
	)

	if contextResult.Level != evidenceSupported {
		// A duration requirement can still use semantically equivalent
		// evidence, so ambiguous lexical coverage is delegated to Qwen.
		contextResult = currentEvidenceVerifier().Verify(
			requirementWithoutYears,
			combinedEvidence,
		)
	}

	if contextResult.Level == evidenceMissing {
		return missingEvidence(), true
	}

	bestRelevantYears := 0

	// Duration is still evaluated deterministically. The model does not invent
	// or estimate years that are not explicitly present in candidate evidence.
	for _, retrievedEvidence := range retrieved {
		years := extractLargestYears(
			retrievedEvidence.Evidence,
		)

		if years > bestRelevantYears {
			bestRelevantYears = years
		}
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
// words, a shared lexical stem can also establish a match.
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

	if requiredMissing == 0 &&
		supportedRatio >= 0.75 &&
		requiredPartial <= 1 {

		return "Best Fit"
	}

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
