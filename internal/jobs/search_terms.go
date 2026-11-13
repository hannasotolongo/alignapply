package jobs

import (
	"regexp"
	"strings"
)

// How Jane reads what someone types in the job search box. People type
// "full stack", "fullstack" or "full-stack"; "accounting" when the title
// says "Accountant"; "ML engineer"; "coding jobs near me"; "part time
// cashier". Every search path (the saved-jobs query, the title filter, the
// live employer reads) goes through roleSearchForms and titleMatchesRole,
// so they all understand the same things.

// searchFiller words describe the search, not the job.
var searchFiller = map[string]struct{}{
	"job": {}, "jobs": {}, "work": {}, "working": {}, "position": {}, "positions": {}, "role": {}, "roles": {},
	"career": {}, "careers": {}, "opening": {}, "openings": {}, "hiring": {}, "near": {}, "me": {}, "in": {},
	"at": {}, "for": {}, "an": {}, "and": {}, "or": {}, "of": {}, "with": {}, "entry": {}, "level": {},
	"remote": {}, "hybrid": {}, "onsite": {}, "fulltime": {}, "parttime": {}, "no": {}, "experience": {},
	"needed": {}, "required": {}, "opportunity": {}, "opportunities": {}, "looking": {}, "want": {},
	"any": {}, "something": {}, "like": {}, "kind": {}, "as": {}, "my": {}, "area": {}, "local": {},
	"paying": {}, "pay": {}, "good": {}, "best": {}, "high": {}, "new": {}, "grad": {}, "today": {},
	"immediate": {}, "start": {}, "weekend": {}, "weekends": {}, "night": {}, "nights": {}, "shift": {},
	"seasonal": {}, "temporary": {}, "temp": {}, "contract": {}, "summer": {}, "gig": {}, "side": {},
}

// compoundWords are written as one word, two words or with a hyphen. Both
// the search and the job title are rewritten to the joined form.
var compoundWords = map[string][2]string{
	"fullstack":  {"full", "stack"},
	"frontend":   {"front", "end"},
	"backend":    {"back", "end"},
	"devops":     {"dev", "ops"},
	"database":   {"data", "base"},
	"healthcare": {"health", "care"},
	"ecommerce":  {"e", "commerce"},
	"fulltime":   {"full", "time"},
	"parttime":   {"part", "time"},
	"onsite":     {"on", "site"},
	"website":    {"web", "site"},
	"childcare":  {"child", "care"},
	"daycare":    {"day", "care"},
	"forklift":   {"fork", "lift"},
	"bookkeeper": {"book", "keeper"},
	"homecare":   {"home", "care"},
	"helpdesk":   {"help", "desk"},
	"workday":    {"work", "day"},
}

var compoundPairs = func() map[string]string {
	pairs := make(map[string]string, len(compoundWords))
	for joined, parts := range compoundWords {
		pairs[parts[0]+" "+parts[1]] = joined
	}
	return pairs
}()

// joinCompounds rewrites "full stack" to "fullstack" in normalized text.
func joinCompounds(normalized string) string {
	words := strings.Fields(normalized)
	out := make([]string, 0, len(words))
	for i := 0; i < len(words); i++ {
		if i+1 < len(words) {
			if joined, ok := compoundPairs[words[i]+" "+words[i+1]]; ok {
				out = append(out, joined)
				i++
				continue
			}
		}
		out = append(out, words[i])
	}
	return strings.Join(out, " ")
}

// cleanSearchText is normalizePreferenceText plus joined compounds and
// expanded abbreviations: "Sr. Full-Stack SWE" -> "senior fullstack
// software engineer".
func cleanSearchText(value string) string {
	return expandTitleAliases(joinCompounds(normalizePreferenceText(value)))
}

// cleanRoleQuery is what the person typed, without the words that describe
// the search instead of the job ("part time", "jobs near me").
func cleanRoleQuery(role string) string {
	words := strings.Fields(cleanSearchText(role))
	kept := words[:0]
	for _, word := range words {
		if _, filler := searchFiller[word]; filler {
			continue
		}
		kept = append(kept, word)
	}
	if len(kept) == 0 {
		// Only filler ("remote jobs"): keep what they typed.
		return cleanSearchText(role)
	}
	return strings.Join(kept, " ")
}

// everydayRoles turn the words people use for a kind of work into the
// titles employers post: "coding" finds Software Developer, "teaching"
// finds Teacher and Tutor, "cleaning" finds Custodian.
var everydayRoles = map[string][]string{
	"coding":         {"software engineer", "software developer", "programmer", "web developer"},
	"code":           {"software engineer", "software developer", "programmer"},
	"coder":          {"software engineer", "software developer", "programmer"},
	"programming":    {"software engineer", "software developer", "programmer"},
	"programmer":     {"software engineer", "software developer"},
	"computer":       {"software engineer", "it support", "computer"},
	"computers":      {"software engineer", "it support", "computer"},
	"tech":           {"software engineer", "technician", "it support"},
	"technology":     {"software engineer", "information technology"},
	"web":            {"web developer", "frontend developer"},
	"ai":             {"machine learning", "artificial intelligence"},
	"data":           {"data analyst", "data scientist", "data engineer"},
	"teaching":       {"teacher", "tutor", "instructor", "educator", "paraprofessional"},
	"teach":          {"teacher", "tutor", "instructor", "educator"},
	"tutoring":       {"tutor"},
	"school":         {"teacher", "paraprofessional", "school"},
	"kids":           {"teacher", "childcare", "nanny", "paraprofessional", "youth"},
	"children":       {"teacher", "childcare", "nanny", "paraprofessional", "youth"},
	"childcare":      {"childcare", "daycare", "nanny", "preschool"},
	"babysitting":    {"nanny", "childcare", "babysitter"},
	"nursing":        {"nurse", "lpn", "cna"},
	"healthcare":     {"nurse", "medical assistant", "patient", "clinical", "healthcare"},
	"medical":        {"medical", "nurse", "patient", "clinical"},
	"hospital":       {"nurse", "patient", "hospital"},
	"caregiver":      {"caregiver", "home health aide", "cna", "personal care"},
	"caregiving":     {"caregiver", "home health aide", "personal care"},
	"therapy":        {"therapist", "therapy"},
	"pharmacy":       {"pharmacist", "pharmacy technician", "pharmacy"},
	"dental":         {"dental", "hygienist", "dentist"},
	"driving":        {"driver", "courier", "delivery"},
	"drive":          {"driver", "courier", "delivery"},
	"delivery":       {"delivery", "driver", "courier"},
	"trucking":       {"truck driver", "cdl"},
	"accounting":     {"accountant", "accounting", "bookkeeper"},
	"bookkeeping":    {"bookkeeper", "accounting clerk"},
	"finance":        {"finance", "financial analyst", "accountant"},
	"bank":           {"teller", "banker", "bank"},
	"banking":        {"teller", "banker", "bank"},
	"insurance":      {"insurance", "claims", "underwriter"},
	"sales":          {"sales", "account executive", "business development"},
	"selling":        {"sales", "account executive"},
	"retail":         {"retail", "sales associate", "cashier", "store"},
	"store":          {"store", "retail", "cashier", "sales associate"},
	"restaurant":     {"server", "cook", "host", "crew member", "dishwasher", "barista", "restaurant"},
	"food":           {"cook", "food service", "server", "crew member", "barista"},
	"kitchen":        {"cook", "dishwasher", "kitchen", "chef", "prep cook"},
	"waitress":       {"server"},
	"waiter":         {"server"},
	"waiting":        {"server"},
	"coffee":         {"barista"},
	"warehouse":      {"warehouse", "picker", "packer", "forklift", "material handler"},
	"construction":   {"construction", "carpenter", "laborer", "electrician", "plumber"},
	"manufacturing":  {"production", "assembler", "machine operator", "manufacturing"},
	"factory":        {"production", "assembler", "machine operator", "manufacturing"},
	"cleaning":       {"janitor", "custodian", "housekeeper", "cleaner", "environmental services"},
	"janitorial":     {"janitor", "custodian"},
	"office":         {"administrative assistant", "receptionist", "office", "clerk"},
	"admin":          {"administrative", "admin", "receptionist"},
	"administrative": {"administrative", "receptionist", "office"},
	"design":         {"designer", "design"},
	"art":            {"designer", "artist"},
	"writing":        {"writer", "editor", "content"},
	"animals":        {"veterinary", "animal", "kennel", "groomer", "vet"},
	"animal":         {"veterinary", "animal", "kennel", "groomer", "vet"},
	"pets":           {"veterinary", "animal", "kennel", "groomer", "pet"},
	"cars":           {"automotive", "mechanic", "technician", "car"},
	"auto":           {"automotive", "mechanic", "technician"},
	"beauty":         {"stylist", "esthetician", "beauty", "cosmetologist"},
	"hair":           {"stylist", "hair"},
	"law":            {"attorney", "paralegal", "legal"},
	"legal":          {"attorney", "paralegal", "legal"},
	"recruiting":     {"recruiter", "talent acquisition"},
	"customer":       {"customer service", "customer support", "customer success"},
	"fitness":        {"trainer", "fitness", "coach"},
	"gym":            {"trainer", "fitness", "front desk"},
	"hotel":          {"front desk", "housekeeper", "hotel", "guest service"},
	"hospitality":    {"front desk", "housekeeper", "hotel", "guest service", "server"},
	"security":       {"security"},
	"engineering":    {"engineer"},
}

// roleSearchForms lists what a search can mean: the cleaned search itself,
// plus the job titles its everyday words stand for.
func roleSearchForms(role string) []string {
	cleaned := cleanRoleQuery(role)
	forms := []string{cleaned}
	seen := map[string]bool{cleaned: true}
	for _, word := range strings.Fields(cleaned) {
		for _, title := range everydayRoles[word] {
			form := cleanSearchText(title)
			if !seen[form] {
				seen[form] = true
				forms = append(forms, form)
			}
		}
	}
	return forms
}

// stemWord trims common endings so "accounting", "accountant" and
// "accounts" compare as "account", and "therapy" and "therapist" as
// "therap".
func stemWord(word string) string {
	for _, suffix := range []string{"ments", "ment", "ings", "ing", "ists", "ist", "ants", "ant", "ians", "ian", "ers", "er", "ors", "or", "ies", "ions", "ion", "ics", "ical", "al", "y", "es", "e", "s"} {
		if strings.HasSuffix(word, suffix) && len(word)-len(suffix) >= 4 {
			return word[:len(word)-len(suffix)]
		}
	}
	return word
}

// wordsMatch compares one search word with one title word: the same word,
// the same stem, or a word the person started typing ("soft" for
// "software", "nurs" for "nursing").
func wordsMatch(search, title string) bool {
	if search == title {
		return true
	}
	if prefixCollision(search, title) {
		return false
	}
	if len(search) >= 3 && strings.HasPrefix(title, search) {
		return true
	}
	a, b := stemWord(search), stemWord(title)
	if a == b {
		return true
	}
	return len(a) >= 5 && len(b) >= 5 && (strings.HasPrefix(a, b) || strings.HasPrefix(b, a))
}

var nonAlphanumericSearch = regexp.MustCompile(`[^a-z0-9]+`)

// compactText drops spaces and punctuation: "full-stack" and "full stack"
// both become "fullstack".
func compactText(value string) string {
	return nonAlphanumericSearch.ReplaceAllString(strings.ToLower(value), "")
}

// titleLooselyMatchesRole is the fallback when nothing matches the whole
// search: a title that shares one meaningful word with any form of it.
func titleLooselyMatchesRole(targetRole, jobTitle string) bool {
	titleTokens := roleRelevanceTokens(cleanSearchText(jobTitle))
	for _, form := range roleSearchForms(targetRole) {
		for token := range roleRelevanceTokens(form) {
			if len(token) < 3 {
				continue
			}
			if titleHasRoleWord(titleTokens, token) {
				return true
			}
		}
	}
	return false
}

// roleMatches is the title check a search uses: the full rules, or the
// loose fallback when the request asks for it.
func roleMatches(request SearchRequest, jobTitle string) bool {
	if jobTitleMatchesTargetRole(request.TargetRole, jobTitle) {
		return true
	}
	return request.LooseTitles && titleLooselyMatchesRole(request.TargetRole, jobTitle)
}

// prefixCollisions are words that start with a search word but mean
// something else: an "intern" search shouldn't find "Internal Auditor".
var prefixCollisions = map[string][]string{
	"intern":  {"internal", "international", "internet"},
	"art":     {"artificial", "article"},
	"car":     {"care", "career", "careers", "cargo", "cardio", "cardiac", "cardiology", "cardiovascular"},
	"care":    {"career", "careers"},
	"sales":   {"salesforce"},
	"tech":    {"techniques"},
	"server":  {"servers"},
	"host":    {"hospital", "hospice", "hostess"},
	"cook":    {"cookie"},
	"nurse":   {"nursery"},
	"pharm":   {},
	"account": {},
}

func prefixCollision(search, title string) bool {
	for _, other := range prefixCollisions[search] {
		if title == other {
			return true
		}
	}
	return false
}
