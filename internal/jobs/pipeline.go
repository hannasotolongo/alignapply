package jobs

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMaxPostingAge = 365 * 24 * time.Hour
	maxFuturePostingSkew = 48 * time.Hour
)

type Pipeline struct {
	now           func() time.Time
	maxPostingAge time.Duration
}

func NewPipeline() *Pipeline {
	return &Pipeline{
		now:           time.Now,
		maxPostingAge: defaultMaxPostingAge,
	}
}

// Process takes normalized jobs returned by providers and applies the
// provider-independent rules Jane Careers expects every source to follow.
//
// Provider-specific code should retrieve and normalize data.
// Candidate matching belongs to the matching package.
// This pipeline sits between those layers.
// widenBelow is how few local jobs make Jane look farther out.
const widenBelow = 8

func (p *Pipeline) Process(
	request SearchRequest,
	input []Job,
) []Job {
	result := p.processWithin(request, input)
	if len(result) == 0 && !request.LooseTitles && len(input) > 0 {
		// Nothing matched the whole search. Show the closest titles (ones
		// sharing a meaningful word) instead of an empty page.
		loose := request
		loose.LooseTitles = true
		return p.Process(loose, input)
	}
	if request.RadiusMiles != 0 || len(result) >= widenBelow {
		return withDistances(request, result)
	}
	if _, _, ok := geoLocate(request.Location, "", true); !ok {
		return result
	}
	// Few jobs nearby: look 80 miles out, then 150, then the whole state,
	// stopping as soon as there's a useful list.
	for _, radius := range []float64{80, 150, wholeStateRadius} {
		wider := request
		wider.RadiusMiles = radius
		more := p.processWithin(wider, input)
		if len(more) > len(result) {
			result = more
		}
		if len(result) >= widenBelow {
			break
		}
	}
	return withDistances(request, result)
}

// withDistances records how far each job is from the searched place.
func withDistances(request SearchRequest, jobs []Job) []Job {
	for i := range jobs {
		if miles, _, ok := jobDistance(request, jobs[i]); ok {
			rounded := math.Round(miles)
			jobs[i].DistanceMiles = &rounded
		}
	}
	return jobs
}

func (p *Pipeline) processWithin(
	request SearchRequest,
	input []Job,
) []Job {
	if len(input) == 0 {
		return []Job{}
	}

	result := make([]Job, 0, len(input))
	seen := make(map[string]struct{})

	for _, rawJob := range input {
		pipelineDebugf(
			"[pipeline-debug] CHECK title=%q company=%q location=%q country=%q arrangement=%q employment=%q active=%v apply_url=%q",
			rawJob.Title,
			rawJob.Company,
			rawJob.Location,
			rawJob.CountryCode,
			rawJob.WorkArrangement,
			rawJob.EmploymentType,
			rawJob.IsActive,
			rawJob.ApplyURL,
		)

		if !validPipelineJob(rawJob) {
			pipelineDebugf(
				"[pipeline-debug] REJECT reason=invalid_job title=%q company=%q id=%q apply_url=%q",
				rawJob.Title,
				rawJob.Company,
				rawJob.ID,
				rawJob.ApplyURL,
			)
			continue
		}

		if !rawJob.IsActive {
			pipelineDebugf(
				"[pipeline-debug] REJECT reason=inactive title=%q company=%q",
				rawJob.Title,
				rawJob.Company,
			)
			continue
		}

		if !p.isCurrentJob(rawJob) {
			pipelineDebugf(
				"[pipeline-debug] REJECT reason=not_current title=%q company=%q posted_at=%v expires_at=%v",
				rawJob.Title,
				rawJob.Company,
				rawJob.PostedAt,
				rawJob.ExpiresAt,
			)
			continue
		}

		if !roleMatches(request, rawJob.Title) {
			pipelineDebugf(
				"[pipeline-debug] REJECT reason=role title=%q target_role=%q",
				rawJob.Title,
				request.TargetRole,
			)
			continue
		}

		if !matchesLocationPreference(request, rawJob) {
			pipelineDebugf(
				"[pipeline-debug] REJECT reason=location title=%q requested=%q job_location=%q request_country=%q job_country=%q",
				rawJob.Title,
				request.Location,
				rawJob.Location,
				request.CountryCode,
				rawJob.CountryCode,
			)
			continue
		}

		if !matchesWorkArrangementPreference(request, rawJob) {
			pipelineDebugf(
				"[pipeline-debug] REJECT reason=work_arrangement title=%q arrangement=%q",
				rawJob.Title,
				rawJob.WorkArrangement,
			)
			continue
		}

		if !matchesEmploymentTypePreference(request, rawJob) {
			pipelineDebugf(
				"[pipeline-debug] REJECT reason=employment_type title=%q employment=%q requested=%v",
				rawJob.Title,
				rawJob.EmploymentType,
				request.EmploymentType,
			)
			continue
		}

		if !matchesMinimumSalaryPreference(request, rawJob) {
			pipelineDebugf(
				"[pipeline-debug] REJECT reason=salary title=%q minimum=%q salary_min=%v salary_max=%v",
				rawJob.Title,
				request.MinimumSalary,
				rawJob.SalaryMin,
				rawJob.SalaryMax,
			)
			continue
		}

		key := pipelineDeduplicationKey(rawJob)

		if _, exists := seen[key]; exists {
			pipelineDebugf(
				"[pipeline-debug] REJECT reason=duplicate title=%q key=%q",
				rawJob.Title,
				key,
			)
			continue
		}

		pipelineDebugf(
			"[pipeline-debug] ACCEPT title=%q company=%q",
			rawJob.Title,
			rawJob.Company,
		)

		seen[key] = struct{}{}

		// Normalize provider formatting before requirement extraction and
		// before the description is returned to the client.
		rawJob.Description = normalizeJobDescription(rawJob.Description)

		job := enrichCached(rawJob)

		result = append(result, job)
	}

	sort.SliceStable(
		result,
		func(i, j int) bool {
			return jobSortLess(result[i], result[j])
		},
	)

	return result
}

// isCurrentJob is the centralized freshness policy for normalized jobs.
// Providers remain responsible for faithfully reporting source metadata;
// the pipeline decides whether dated postings are plausible/current enough
// to present to the user.
//
// Unknown dates remain eligible because Jane Careers must not invent a posting
// date or discard a job merely because the provider omitted one.
func (p *Pipeline) isCurrentJob(job Job) bool {
	now := time.Now().UTC()
	maxAge := defaultMaxPostingAge

	if p != nil {
		if p.now != nil {
			now = p.now().UTC()
		}

		if p.maxPostingAge > 0 {
			maxAge = p.maxPostingAge
		}
	}

	if job.ExpiresAt != nil {
		expiresAt := job.ExpiresAt.UTC()

		if expiresAt.Before(now) {
			return false
		}
	}

	if job.PostedAt == nil {
		return true
	}

	postedAt := job.PostedAt.UTC()

	// A substantially future-dated posting is most likely malformed
	// provider metadata. A small tolerance permits clock/time-zone skew.
	if postedAt.After(now.Add(maxFuturePostingSkew)) {
		return false
	}

	if postedAt.Before(now.Add(-maxAge)) {
		return false
	}

	return true
}

func validPipelineJob(job Job) bool {
	if strings.TrimSpace(job.ID) == "" {
		return false
	}

	if strings.TrimSpace(job.Title) == "" {
		return false
	}

	if strings.TrimSpace(job.Company) == "" {
		return false
	}

	if strings.TrimSpace(job.ApplyURL) == "" {
		return false
	}

	return true
}

func matchesRequestedPreferences(
	request SearchRequest,
	job Job,
) bool {
	if !roleMatches(request, job.Title) {
		return false
	}

	if !matchesLocationPreference(request, job) {
		return false
	}

	if !matchesWorkArrangementPreference(request, job) {
		return false
	}

	if !matchesEmploymentTypePreference(request, job) {
		return false
	}

	if !matchesMinimumSalaryPreference(request, job) {
		return false
	}

	return true
}

func matchesLocationPreference(
	request SearchRequest,
	job Job,
) bool {
	requested := normalizePreferenceText(request.Location)

	if requested == "" {
		return true
	}

	// An explicit country mismatch is authoritative, including for remote
	// postings. Remote describes work arrangement; it does not mean the role
	// is available worldwide.
	requestCountry := strings.ToUpper(strings.TrimSpace(request.CountryCode))
	jobCountry := strings.ToUpper(strings.TrimSpace(job.CountryCode))

	// A search for a US city or state is a US search, so a remote role
	// limited to another country doesn't fit.
	if requestCountry == "" {
		requestCountry = SearchCountry(request.Location)
	}
	if requestCountry == "" && (len(metrosIn(requested)) > 0 || locationContainsUSRegion(request.Location)) {
		requestCountry = "US"
	}

	if requestCountry != "" &&
		jobCountry != "" &&
		requestCountry != jobCountry {
		return false
	}

	// Once explicit country compatibility has been established, remote jobs
	// remain eligible without requiring the employer's office city to match
	// the user's requested city.
	if request.Remote &&
		strings.EqualFold(
			strings.TrimSpace(job.WorkArrangement),
			"remote",
		) {
		return true
	}

	location := normalizePreferenceText(job.Location)

	if location == "" {
		// Missing provider metadata should not be interpreted as a confirmed
		// mismatch. The UI can still display the posting's available data.
		return true
	}

	// A search for a whole country or region abroad ("Canada", "Ontario",
	// "UK") takes any job there.
	if country, region := worldCountryIn(request.Location); country != "" {
		if _, _, ok := geoLocate(request.Location, "", true); !ok {
			if jobCountry != "" && jobCountry != country {
				return false
			}
			if region == "" {
				return jobCountry == country
			}
			_, jobKey, ok := geoLocate(job.Location, worldKey(country, region), false)
			return ok && jobKey == worldKey(country, region)
		}
	}

	// Any town, city or ZIP: measure the distance.
	if miles, jobState, ok := jobDistance(request, job); ok {
		radius := request.RadiusMiles
		if radius == 0 {
			radius = defaultRadiusMiles
		}
		if radius == wholeStateRadius {
			_, requestState, _ := geoLocate(request.Location, "", true)
			return miles <= 150 || (jobState != "" && jobState == requestState)
		}
		return miles <= radius
	}
	// A search for a whole state ("Florida") takes any job in it.
	if state, ok := geoStateOnly(request.Location); ok {
		if _, has := usStatesIn(job.Location)[state]; has {
			return true
		}
	}

	// Same city name, different state ("Gainesville, FL" vs "Gainesville,
	// GA") is a different place.
	if statesConflict(request.Location, job.Location) {
		return false
	}

	if strings.Contains(location, requested) ||
		strings.Contains(requested, location) {
		return true
	}

	// Metro areas: a search for "Miami" includes Fort Lauderdale and
	// Bay Harbor Islands, and a posting titled "... - Miami / Ft Lauderdale"
	// is local even when its location field names a suburb or venue.
	if requestedMetros := metrosIn(requested); len(requestedMetros) > 0 {
		for metro := range metrosIn(location + " " + normalizePreferenceText(job.Title)) {
			if _, ok := requestedMetros[metro]; ok {
				return true
			}
		}
	}

	requestTokens := preferenceTokenSet(requested)
	locationTokens := preferenceTokenSet(location)

	if len(requestTokens) == 0 ||
		len(locationTokens) == 0 {
		return true
	}

	// Sharing only a state ("Miami, FL" vs "Orlando, FL") is not local,
	// unless the user asked for a whole state.
	cityTokens := 0
	for token := range requestTokens {
		if _, isState := usStateTokens[token]; !isState {
			cityTokens++
		}
	}

	matched := 0

	for token := range requestTokens {
		if _, isState := usStateTokens[token]; isState && cityTokens > 0 {
			continue
		}
		if _, exists := locationTokens[token]; exists {
			matched++
		}
	}

	// Location strings vary substantially across providers:
	// "Miami, FL", "Miami, Florida", "Miami-Fort Lauderdale Area", etc.
	// A shared meaningful geographic token is sufficient at this layer.
	return matched > 0
}

func matchesWorkArrangementPreference(
	request SearchRequest,
	job Job,
) bool {
	accepted := make(map[string]struct{})

	if request.Remote {
		accepted["remote"] = struct{}{}
	}

	if request.Hybrid {
		accepted["hybrid"] = struct{}{}
	}

	if request.Onsite {
		accepted["onsite"] = struct{}{}
	}

	if len(accepted) == 0 {
		return true
	}

	arrangement := normalizeArrangement(
		job.WorkArrangement,
	)

	if arrangement == "" {
		// Do not invent a work arrangement from missing provider data.
		// Unknown is allowed through rather than being falsely classified.
		return true
	}

	_, ok := accepted[arrangement]

	return ok
}

func matchesEmploymentTypePreference(
	request SearchRequest,
	job Job,
) bool {
	if len(request.EmploymentType) == 0 {
		return true
	}

	jobType := normalizePreferenceText(
		job.EmploymentType,
	)

	if jobType == "" {
		return true
	}

	for _, requested := range request.EmploymentType {
		requested = normalizePreferenceText(requested)

		if requested == "" {
			continue
		}

		if jobType == requested ||
			strings.Contains(jobType, requested) ||
			strings.Contains(requested, jobType) {
			return true
		}
	}

	return false
}

func matchesMinimumSalaryPreference(
	request SearchRequest,
	job Job,
) bool {
	minimum, ok := parseMinimumSalary(
		request.MinimumSalary,
	)

	if !ok || minimum <= 0 {
		return true
	}

	// Unknown salary must remain eligible. Otherwise Jane Careers would silently
	// discard jobs simply because a provider or employer omitted compensation.
	if job.SalaryMin == nil &&
		job.SalaryMax == nil {
		return true
	}

	// Compare yearly pay to the yearly floor: "$45/hour" is about $93,600 a
	// year, not $45.
	annual := func(value float64) float64 {
		return annualizeSalary(value, job.SalaryPeriod)
	}

	// If the maximum known salary is below the user's minimum, the posting
	// cannot satisfy the requested floor.
	if job.SalaryMax != nil &&
		annual(*job.SalaryMax) < minimum {
		return false
	}

	// When only a minimum is available, it must meet the requested floor.
	if job.SalaryMax == nil &&
		job.SalaryMin != nil &&
		annual(*job.SalaryMin) < minimum {
		return false
	}

	return true
}

func annualizeSalary(value float64, period string) float64 {
	switch normalizePreferenceText(period) {
	case "hour", "hourly", "hr", "per hour":
		return value * 2080
	case "day", "daily", "per day":
		return value * 260
	case "week", "weekly", "per week":
		return value * 52
	case "biweekly", "bi weekly":
		return value * 26
	case "month", "monthly", "per month":
		return value * 12
	case "year", "yearly", "annual", "annually", "per year":
		return value
	}
	// No period given: amounts this small are hourly rates.
	if value > 0 && value < 300 {
		return value * 2080
	}
	return value
}

func parseMinimumSalary(
	value string,
) (float64, bool) {
	value = strings.TrimSpace(value)

	if value == "" {
		return 0, false
	}

	value = strings.ToLower(value)

	multiplier := 1.0

	if strings.Contains(value, "k") {
		multiplier = 1000
	}

	var builder strings.Builder

	for _, r := range value {
		if (r >= '0' && r <= '9') ||
			r == '.' {
			builder.WriteRune(r)
		}
	}

	number := builder.String()

	if number == "" {
		return 0, false
	}

	parsed, err := strconv.ParseFloat(
		number,
		64,
	)

	if err != nil {
		return 0, false
	}

	return parsed * multiplier, true
}

func pipelineDeduplicationKey(job Job) string {
	source := normalizePreferenceText(job.Source)
	sourceJobID := normalizePreferenceText(job.SourceJobID)

	// A provider's stable source identifier is the strongest identity signal.
	if source != "" &&
		sourceJobID != "" {
		return "source:" +
			source +
			":" +
			sourceJobID
	}

	company := normalizePreferenceText(job.Company)
	title := normalizePreferenceText(job.Title)
	location := normalizePreferenceText(job.Location)

	return "posting:" +
		company +
		"|" +
		title +
		"|" +
		location
}

func jobSortLess(
	left Job,
	right Job,
) bool {
	leftQuality := EvaluateExtractionQuality(left)
	rightQuality := EvaluateExtractionQuality(right)

	if leftQuality.Scorable != rightQuality.Scorable {
		return leftQuality.Scorable
	}

	if leftQuality.Score != rightQuality.Score {
		return leftQuality.Score > rightQuality.Score
	}

	if left.PostedAt != nil &&
		right.PostedAt != nil &&
		!left.PostedAt.Equal(*right.PostedAt) {
		return left.PostedAt.After(*right.PostedAt)
	}

	if left.PostedAt != nil &&
		right.PostedAt == nil {
		return true
	}

	if left.PostedAt == nil &&
		right.PostedAt != nil {
		return false
	}

	leftCompany := normalizePreferenceText(left.Company)
	rightCompany := normalizePreferenceText(right.Company)

	if leftCompany != rightCompany {
		return leftCompany < rightCompany
	}

	leftTitle := normalizePreferenceText(left.Title)
	rightTitle := normalizePreferenceText(right.Title)

	return leftTitle < rightTitle
}

func normalizeArrangement(
	value string,
) string {
	switch normalizePreferenceText(value) {
	case "remote":
		return "remote"

	case "hybrid":
		return "hybrid"

	case "onsite",
		"on site":
		return "onsite"

	default:
		return ""
	}
}

func jobTitleMatchesTargetRole(
	targetRole string,
	jobTitle string,
) bool {
	for _, form := range roleSearchForms(targetRole) {
		if titleMatchesRole(form, jobTitle) {
			return true
		}
	}
	return false
}

// titleMatchesRole checks one cleaned form of a search against a title.
func titleMatchesRole(
	role string,
	jobTitle string,
) bool {
	title := cleanSearchText(jobTitle)

	if role == "" {
		return true
	}

	if title == "" {
		return false
	}

	// Whole words only: "nurse" is not in "Nursery Worker".
	if strings.Contains(" "+title+" ", " "+role+" ") ||
		strings.Contains(" "+role+" ", " "+title+" ") {
		return true
	}

	// Spacing and hyphens never matter: "full stack" finds "Full-Stack"
	// and "Fullstack".
	if compactRole := compactText(role); strings.Contains(role, " ") && len(compactRole) >= 6 && strings.Contains(compactText(title), compactRole) {
		return true
	}

	roleTokens := roleRelevanceTokens(role)
	titleTokens := roleRelevanceTokens(title)

	if len(roleTokens) == 0 || len(titleTokens) == 0 {
		return false
	}

	matched := 0

	for token := range roleTokens {
		if titleHasRoleWord(titleTokens, token) {
			matched++
		}
	}

	if len(roleTokens) == 1 {
		return matched == 1
	}

	// Multi-word specialties should share more than one meaningful term
	// when the exact requested role is not already contained in the title.
	return matched >= 2
}

func roleRelevanceTokens(
	value string,
) map[string]struct{} {
	tokens := preferenceTokenSet(value)

	generic := map[string]struct{}{
		"engineer":    {},
		"engineering": {},
		"developer":   {},
		"manager":     {},
		"specialist":  {},
		"analyst":     {},
		"associate":   {},
		"senior":      {},
		"junior":      {},
		"staff":       {},
		"principal":   {},
		"lead":        {},
		"ii":          {},
		"iii":         {},
		"iv":          {},
	}

	meaningful := make(map[string]struct{})

	for token := range tokens {
		if _, skip := generic[token]; skip {
			continue
		}

		meaningful[token] = struct{}{}
	}

	if len(meaningful) == 0 {
		return tokens
	}

	return meaningful
}

func normalizePreferenceText(
	value string,
) string {
	value = strings.ToLower(
		strings.TrimSpace(value),
	)

	replacer := strings.NewReplacer(
		",", " ",
		".", " ",
		"-", " ",
		"_", " ",
		"/", " ",
		"(", " ",
		")", " ",
	)

	value = replacer.Replace(value)

	return strings.Join(
		strings.Fields(value),
		" ",
	)
}

func preferenceTokenSet(
	value string,
) map[string]struct{} {
	result := make(map[string]struct{})

	for _, token := range strings.Fields(value) {
		if len(token) < 2 {
			continue
		}

		switch token {
		case "the", "area", "metro", "metropolitan":
			continue
		}

		result[token] = struct{}{}
	}

	return result
}

// roleSynonyms lists the words employers actually put in job titles for a
// role people search by another name: a "lawyer" search finds "Associate
// Attorney" and "Legal Counsel", a "doctor" search finds "Physician".
var roleSynonyms = map[string][]string{
	"lawyer":     {"attorney", "counsel", "attorneys"},
	"lawyers":    {"attorney", "counsel", "attorneys"},
	"attorney":   {"lawyer", "counsel"},
	"doctor":     {"physician", "hospitalist", "md", "surgeon", "doctors"},
	"doctors":    {"physician", "hospitalist", "md", "surgeon", "doctor"},
	"physician":  {"doctor", "hospitalist", "md"},
	"dentist":    {"dds", "dmd"},
	"teacher":    {"instructor", "educator", "teachers"},
	"professor":  {"faculty", "lecturer"},
	"cook":       {"chef"},
	"chef":       {"cook"},
	"driver":     {"chauffeur", "courier"},
	"cashier":    {"clerk"},
	"programmer": {"developer", "engineer"},
}

// synonymExclusions are title words that rule out a synonym match: a
// "Physician Assistant" is not a doctor.
var synonymExclusions = map[string][]string{
	"doctor":  {"assistant", "assistants", "associate"},
	"doctors": {"assistant", "assistants", "associate"},
	"lawyer":  {"assistant", "recruiting", "recruiter", "coordinator", "secretary"},
	"lawyers": {"assistant", "recruiting", "recruiter", "coordinator", "secretary"},
}

// titleHasRoleWord reports whether a job title contains the role word or one
// of its synonyms.
func titleHasRoleWord(titleTokens map[string]struct{}, word string) bool {
	if _, ok := titleTokens[word]; ok {
		return true
	}
	for _, excluded := range synonymExclusions[word] {
		if _, ok := titleTokens[excluded]; ok {
			return false
		}
	}
	for _, alternative := range roleSynonyms[word] {
		if _, ok := titleTokens[alternative]; ok {
			return true
		}
	}
	// Other forms of the word: "accounting" finds "Accountant", "nurs"
	// finds "Nursing".
	for token := range titleTokens {
		if wordsMatch(word, token) {
			return true
		}
	}
	return false
}

// titleAliases expands abbreviations employers use in job titles so a search
// for "Registered Nurse" finds "RN - Med Surg" (and the reverse).
var titleAliases = map[string]string{
	"rn":    "registered nurse",
	"lpn":   "licensed practical nurse",
	"lvn":   "licensed vocational nurse",
	"cna":   "certified nursing assistant",
	"np":    "nurse practitioner",
	"aprn":  "advanced practice registered nurse",
	"crna":  "certified registered nurse anesthetist",
	"pa":    "physician assistant",
	"pt":    "physical therapist",
	"ot":    "occupational therapist",
	"slp":   "speech language pathologist",
	"rt":    "respiratory therapist",
	"swe":   "software engineer",
	"sde":   "software development engineer",
	"sre":   "site reliability engineer",
	"qa":    "quality assurance",
	"ux":    "user experience",
	"hr":    "human resources",
	"sr":    "senior",
	"jr":    "junior",
	"mgr":   "manager",
	"acct":  "accountant",
	"cpa":   "certified public accountant",
	"ml":    "machine learning",
	"csr":   "customer service representative",
	"sdr":   "sales development representative",
	"bdr":   "business development representative",
	"ui":    "user interface",
	"dba":   "database administrator",
	"emt":   "emergency medical technician",
	"cma":   "certified medical assistant",
	"asst":  "assistant",
	"admin": "administrative",
	"dev":   "developer",
	"eng":   "engineer",
	"mgmt":  "management",
}

func expandTitleAliases(normalized string) string {
	if normalized == "" {
		return normalized
	}
	words := strings.Fields(normalized)
	for i, word := range words {
		if expansion, ok := titleAliases[word]; ok {
			words[i] = expansion
		}
	}
	return strings.Join(words, " ")
}
