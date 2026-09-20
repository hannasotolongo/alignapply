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

// capabilityFamilies broadens evidence matching beyond exact wording.
// Each family contains terms/phrases that commonly demonstrate the same
// underlying capability. This layer is intentionally conservative: it helps
// retrieve semantically related evidence, but hard constraints such as years,
// degrees, licenses, and certifications are still verified separately.
var capabilityFamilies = [][]string{
	{"distributed systems", "distributed services", "distributed system", "microservices", "service oriented", "service-oriented"},
	{"reliability", "reliable systems", "fault tolerant", "fault-tolerant", "high availability", "highly available", "resilience", "resilient", "recovery", "failover"},
	{"backend", "backend engineering", "backend development", "server side", "server-side", "api development", "api engineering", "web services"},
	{"rest api", "restful api", "http api", "web api", "api endpoint", "api endpoints"},
	{"concurrency", "concurrent", "parallel processing", "multithreading", "multi-threading", "goroutine", "goroutines"},
	{"persistence", "persistent storage", "database", "datastore", "data store", "state management", "durable state"},
	{"cloud", "cloud infrastructure", "cloud computing", "aws", "azure", "gcp"},
	{"containers", "containerization", "containerized", "docker", "kubernetes", "k8s"},
	{"orchestration", "container orchestration", "kubernetes", "k8s"},
	{"ci/cd", "continuous integration", "continuous delivery", "continuous deployment", "deployment pipeline", "build pipeline"},
	{"observability", "monitoring", "metrics", "logging", "tracing", "telemetry"},
	{"machine learning", "ml", "deep learning", "neural network", "neural networks"},
	{"artificial intelligence", "ai", "machine learning", "ml"},
	{"llm", "large language model", "large language models", "language model", "language models"},
	{"computer vision", "image recognition", "image processing", "vision model", "vision models"},
	{"distributed training", "multi gpu", "multi-gpu", "nccl", "data parallel", "model parallel"},
	{"gpu", "gpu computing", "cuda", "accelerator", "accelerators"},
	{"infrastructure as code", "iac", "terraform"},
	{"database", "databases", "sql", "mysql", "postgresql", "postgres", "relational database"},
	{"streaming", "real time", "real-time", "event driven", "event-driven", "message stream", "websocket", "websockets"},
	{"messaging", "message queue", "message queues", "event driven", "event-driven", "pub sub", "publish subscribe", "broker"},
	{"testing", "automated testing", "unit testing", "integration testing", "test automation"},
	{"security", "application security", "cybersecurity", "secure systems", "vulnerability"},
	{"scalability", "scalable", "scale", "high throughput", "high-throughput", "performance"},
	{"low latency", "low-latency", "latency sensitive", "latency-sensitive", "real time", "real-time"},
	{"data engineering", "data pipeline", "data pipelines", "etl", "data processing"},
	{"version control", "source control", "git", "github"},
	{"agile", "scrum", "sprint", "sprints"},
	{"leadership", "technical leadership", "mentoring", "mentor", "leading teams", "team lead"},
	{"communication", "communicate", "cross functional", "cross-functional", "stakeholder", "stakeholders", "collaboration", "collaborative"},
}

// semanticCoverage compares underlying capabilities rather than requiring the
// employer and candidate to use identical vocabulary. It combines lexical
// evidence with concept-family evidence. The result remains evidence-based:
// no family match is possible unless the candidate's actual text expresses a
// member of the same capability family.
func semanticCoverage(requirement string, evidence string) float64 {
	lexical := tokenCoverage(requirement, evidence)
	reqConcepts := capabilityConcepts(requirement)
	if len(reqConcepts) == 0 {
		return lexical
	}

	evidenceConcepts := capabilityConcepts(evidence)
	if len(evidenceConcepts) == 0 {
		return lexical
	}

	matched := 0
	for concept := range reqConcepts {
		if _, ok := evidenceConcepts[concept]; ok {
			matched++
		}
	}

	conceptCoverage := float64(matched) / float64(len(reqConcepts))

	// A piece of evidence can legitimately express more semantic concepts than
	// the requirement. For example, "Kubernetes" expresses both containerization
	// and orchestration, while "container orchestration" may express only the
	// orchestration family. Coverage is requirement-directed, so extra concepts
	// in the evidence must not reduce support.
	//
	// Conversely, a requirement that expresses multiple distinct concepts still
	// requires those concepts to be demonstrated independently; one overlapping
	// concept cannot satisfy the whole requirement.

	// Concept equivalence can establish strong support even when wording is
	// different. Lexical evidence still contributes when it is stronger.
	if conceptCoverage > lexical {
		return conceptCoverage
	}
	return lexical
}

func capabilityConcepts(text string) map[int]struct{} {
	normalized := semanticNormalize(text)
	result := make(map[int]struct{})

	if normalized == "" {
		return result
	}

	for index, family := range capabilityFamilies {
		for _, phrase := range family {
			if containsNormalizedPhrase(normalized, semanticNormalize(phrase)) {
				result[index] = struct{}{}
				break
			}
		}
	}

	return result
}

// containsNormalizedPhrase matches a normalized capability phrase on token
// boundaries. Padding both sides prevents short capability names from matching
// inside unrelated words while still allowing punctuation-normalized phrases
// such as "Kubernetes." at the end of a sentence.
func containsNormalizedPhrase(text string, phrase string) bool {
	text = strings.TrimSpace(text)
	phrase = strings.TrimSpace(phrase)

	if text == "" || phrase == "" {
		return false
	}

	haystack := " " + text + " "
	needle := " " + phrase + " "

	return strings.Contains(haystack, needle)
}

// semanticNormalize canonicalizes text for capability-phrase detection. The
// general normalize function intentionally preserves periods for identifiers
// and versions, but sentence-final punctuation must not prevent a semantic
// capability match ("Kubernetes." -> "kubernetes").
func semanticNormalize(text string) string {
	normalized := normalize(text)
	fields := strings.Fields(normalized)

	for index, field := range fields {
		fields[index] = strings.Trim(field, ".")
	}

	return strings.Join(fields, " ")
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
	combinedEvidence := make(
		[]string,
		0,
		len(relevant),
	)

	for _, evidence := range relevant {
		evidenceText := normalize(evidence.Text)

		if evidenceText == "" {
			continue
		}

		combinedEvidence = append(
			combinedEvidence,
			evidenceText,
		)

		coverage := semanticCoverage(
			requirementText,
			evidenceText,
		)

		if coverage > bestCoverage {
			bestCoverage = coverage
		}
	}

	// A profile can demonstrate one requirement across multiple evidence
	// statements. Evaluate the union of relevant evidence as well as each
	// individual statement.
	if len(combinedEvidence) > 1 {
		aggregateCoverage := semanticCoverage(
			requirementText,
			strings.Join(
				combinedEvidence,
				" ",
			),
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

		coverage := semanticCoverage(
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
