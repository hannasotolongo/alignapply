package matching

import "strings"

// Résumés and job postings describe the same work in different words
// ("managed a team" vs "people leadership", "clients" vs "customers").
// conceptGroups maps those words to one shared concept, so the word-matching
// fallback recognizes experience that's worded differently. Groups are kept
// to close synonyms to avoid crediting unrelated work.
var conceptGroups = [][]string{
	{"lead", "led", "leading", "leader", "leaders", "leadership", "manage", "managed", "manages", "managing", "manager", "managers", "management", "supervise", "supervised", "supervising", "supervisor", "supervision", "oversee", "oversaw", "overseeing", "oversight", "direct", "directed", "directing", "head", "headed", "heading", "run", "ran", "running"},
	{"team", "teams", "staff", "employees", "reports", "crew", "group", "people", "personnel", "workforce"},
	{"mentor", "mentored", "mentoring", "mentorship", "coach", "coached", "coaching", "train", "trained", "training", "trainer", "onboard", "onboarded", "onboarding", "teach", "taught", "teaching", "develop-people", "precept", "precepted", "precepting"},
	{"client", "clients", "customer", "customers", "account", "accounts", "consumer", "consumers", "buyer", "buyers", "guest", "guests", "stakeholder", "stakeholders"},
	{"relationship", "relationships", "rapport", "partnership", "partnerships", "retention", "engagement"},
	{"sell", "selling", "sold", "sales", "sale", "closing", "closed", "close", "deals", "deal", "quota", "quotas", "prospecting", "prospect", "pipeline"},
	{"revenue", "revenues", "income", "bookings", "arr", "mrr", "sales-growth"},
	{"grow", "grew", "growing", "growth", "increase", "increased", "increasing", "expand", "expanded", "expanding", "expansion", "boost", "boosted", "scale", "scaled", "scaling", "drive", "drove", "driving", "exceed", "exceeded", "exceeding"},
	{"reduce", "reduced", "reducing", "reduction", "cut", "decrease", "decreased", "lower", "lowered", "save", "saved", "savings"},
	{"improve", "improved", "improving", "improvement", "optimize", "optimized", "optimizing", "optimization", "enhance", "enhanced", "streamline", "streamlined"},
	{"analyze", "analyzed", "analyzing", "analysis", "analyses", "analytics", "analytical", "analyst", "data", "metrics", "insights", "reporting", "reports-data"},
	{"forecast", "forecasts", "forecasting", "forecasted", "projection", "projections", "planning", "plan", "plans", "budgeting"},
	{"budget", "budgets", "financial", "finance", "finances", "cost", "costs", "p&l", "pnl", "spend"},
	{"communicate", "communicated", "communicating", "communication", "communications", "present", "presented", "presenting", "presentation", "presentations", "write", "wrote", "writing", "written", "verbal"},
	{"project", "projects", "program", "programs", "initiative", "initiatives", "campaign", "campaigns"},
	{"create", "created", "creating", "design", "designed", "designing", "build-thing", "launch", "launched", "launching", "establish", "established", "implement", "implemented", "implementing", "implementation", "deploy", "deployed", "deploying", "deployment"},
	{"software", "application", "applications", "app", "apps", "platform", "platforms", "tool", "tools", "system", "systems", "saas"},
	{"crm", "salesforce", "hubspot", "zoho", "pipedrive", "dynamics"},
	{"spreadsheet", "spreadsheets", "excel", "sheets"},
	{"schedule", "scheduled", "scheduling", "calendar", "calendars", "appointments", "appointment", "booking"},
	{"marketing", "marketed", "advertising", "advertise", "promotion", "promotions", "brand", "branding"},
	{"social", "instagram", "tiktok", "facebook", "linkedin", "twitter", "youtube"},
	{"collaborate", "collaborated", "collaborating", "collaboration", "cross-functional", "partnered", "partner", "partners", "coordinate", "coordinated", "coordinating", "coordination", "liaison"},
	{"resolve", "resolved", "resolving", "resolution", "troubleshoot", "troubleshooting", "troubleshot", "solve", "solved", "solving", "fix", "fixed", "fixing", "support", "supported", "supporting", "assist", "assisted", "assisting", "help", "helped"},
	{"b2b", "enterprise", "business-to-business", "mid-market", "smb"},
	{"recruit", "recruited", "recruiting", "recruitment", "hire", "hired", "hiring", "staffing", "talent"},
	{"inventory", "stock", "stocking", "warehouse", "logistics", "supply", "shipping", "fulfillment"},
	{"compliance", "compliant", "regulatory", "regulations", "regulation", "audit", "audits", "auditing"},
	{"process", "processes", "procedure", "procedures", "workflow", "workflows", "operations", "operational"},
	{"quality", "qa", "testing", "tested", "test", "tests"},
	{"code", "coded", "coding", "program-code", "programming", "programmed", "engineer", "engineered", "engineering", "developer", "development"},
	{"database", "databases", "sql", "postgres", "postgresql", "mysql"},
	{"cloud", "aws", "azure", "gcp"},
	{"bachelor", "bachelors", "ba", "bs", "bsc", "undergraduate"},
	{"master", "masters", "ms", "ma", "msc", "graduate"},
}

var conceptIndex = func() map[string]string {
	index := make(map[string]string)
	for _, group := range conceptGroups {
		concept := group[0]
		for _, word := range group {
			if _, exists := index[word]; !exists {
				index[word] = concept
			}
		}
	}
	return index
}()

// wordStem strips common English endings so word forms compare equal
// ("analyzing" and "analyzed" both become "analyz").
func wordStem(token string) string {
	if len(token) < 5 || !alphabeticToken(token) {
		return token
	}
	for _, suffix := range []string{"ational", "ization", "ations", "ation", "ments", "ment", "ities", "ity", "ings", "ing", "ies", "ied", "ers", "er", "ed", "es", "ly", "s"} {
		if strings.HasSuffix(token, suffix) && len(token)-len(suffix) >= 4 {
			stem := strings.TrimSuffix(token, suffix)
			if suffix == "ies" || suffix == "ied" || suffix == "ities" {
				stem += "y"
			}
			return stem
		}
	}
	return token
}

// conceptOf returns the shared concept for a word, or its stem.
func conceptOf(token string) string {
	if concept, ok := conceptIndex[token]; ok {
		return concept
	}
	return wordStem(token)
}
