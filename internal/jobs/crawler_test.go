package jobs

import (
	"regexp"
	"strings"
	"testing"
)

func TestRobotsRules(t *testing.T) {
	rules := parseRobots(strings.NewReader(`
User-agent: Googlebot
Disallow: /

User-agent: *
Allow: /targetcareers/
Disallow: /refreshFacet/
Disallow: /private*.json$
`))
	cases := map[string]bool{
		"/targetcareers/job/123":  true,
		"/wday/cxs/target/jobs":   true,
		"/refreshFacet/x":         false,
		"/private-data.json":      false,
		"/private-data.json?page": true,
	}
	for path, want := range cases {
		if got := rules.allows(path); got != want {
			t.Errorf("allows(%q) = %v, want %v", path, got, want)
		}
	}

	ours := parseRobots(strings.NewReader("User-agent: *\nAllow: /\n\nUser-agent: JaneCareersBot\nDisallow: /jobs\n"))
	if ours.allows("/jobs/1") {
		t.Error("a group naming Jane's crawler must take priority over *")
	}
	if !parseRobots(strings.NewReader("")).allows("/anything") {
		t.Error("an empty robots.txt allows everything")
	}
}

func TestWorkdayLocation(t *testing.T) {
	cases := map[string]string{
		"305 Carolina Green Pkwy, Southern Pines,NC 28387": "Southern Pines, NC",
		"7000 Target Pkwy N, Brooklyn Park, MN 55445":      "Brooklyn Park, MN",
		"Minneapolis, MN": "Minneapolis, MN",
		"2 Locations":     "2 Locations",
		"Remote - US":     "Remote - US",
	}
	for in, want := range cases {
		if got := workdayLocation(in); got != want {
			t.Errorf("workdayLocation(%q) = %q, want %q", in, got, want)
		}
	}
}

// The database narrows saved jobs with TitleSearchPattern before the
// pipeline's title matcher runs, so the pattern must find every title the
// matcher accepts.
func TestTitleSearchPatternNeverHidesAMatch(t *testing.T) {
	roles := []string{"Registered Nurse", "Retail Store Manager", "Teacher", "Software Engineer", "Manager",
		"Data Analyst", "Customer Service Representative", "Marketing Manager", "Physical Therapist", "Accountant",
		"Administrative Assistant", "Sales Representative", "Product Manager", "Barista", "Warehouse Associate"}
	titles := []string{"ICU RN", "Registered Nurse - Med Surg", "Nurse Practitioner", "Store Manager", "Retail Sales Associate",
		"Assistant Store Manager", "5th Grade Teacher", "Senior Software Engineer", "Software Development Engineer II",
		"Engineering Manager", "Data Analyst II", "Senior Data Scientist", "Customer Service Rep", "Customer Success Manager",
		"Marketing Manager, Lifecycle", "PT - Outpatient Physical Therapy", "Staff Accountant", "Executive Assistant",
		"Administrative Assistant III", "Account Executive", "Sales Representative - SMB", "Product Manager, Payments",
		"Barista", "Warehouse Associate - Night Shift", "Food & Beverage Team Leader", "Principal Engineer - Core Platform"}
	for _, role := range roles {
		pattern := TitleSearchPattern(role)
		// PostgreSQL's \m (start of word) is \b in Go's syntax.
		re := regexp.MustCompile(strings.Replace(pattern, `\m`, `\b`, 1))
		for _, title := range titles {
			if jobTitleMatchesTargetRole(role, title) && !re.MatchString(strings.ToLower(title)) {
				t.Errorf("role %q: matcher accepts %q but the search pattern %s hides it", role, title, pattern)
			}
		}
	}
}

func TestCrawlSeedBoardsIncludesKnownEmployers(t *testing.T) {
	boards := CrawlSeedBoards()
	seen := map[string]bool{}
	for _, b := range boards {
		if seen[b.key()] {
			t.Fatalf("duplicate board %s", b.key())
		}
		seen[b.key()] = true
	}
	if !seen["greenhouse/stripe"] || !seen["lever/filevine"] {
		t.Fatalf("existing employers missing from the crawl list (%d boards)", len(boards))
	}
}
