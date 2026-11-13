package jobs

import (
	"regexp"
	"strings"
	"testing"
)

// searchFinds reports whether a search would show a title: the saved-jobs
// query (TitleSearchPattern, run here the way PostgreSQL runs it on the
// lowercased title) and then the title filter.
func searchFinds(t *testing.T, search, title string) bool {
	t.Helper()
	pattern := TitleSearchPattern(search)
	if pattern == "" {
		return false
	}
	re, err := regexp.Compile(strings.Replace(pattern, `\m`, `\b`, 1))
	if err != nil {
		t.Fatalf("%q: bad pattern %q: %v", search, pattern, err)
	}
	return re.MatchString(strings.ToLower(title)) && jobTitleMatchesTargetRole(search, title)
}

func TestSearchUnderstandsHowPeopleType(t *testing.T) {
	finds := map[string][]string{
		"full stack":             {"Full-Stack Engineer", "Fullstack Developer", "Software Engineer, Full Stack", "Software Engineer II - Fullstack"},
		"fullstack jobs near me": {"Full Stack Software Engineer"},
		"front end":              {"Frontend Engineer", "Front-End Developer"},
		"backend":                {"Back-End Engineer", "Back End Developer", "Backend Engineer"},
		"accounting":             {"Accountant", "Staff Accountant", "Accounting Clerk"},
		"physical therapy":       {"Physical Therapist"},
		"ml engineer":            {"Machine Learning Engineer"},
		"nurs":                   {"Registered Nurse - ICU"},
		"soft":                   {"Software Developer"},
		"part time cashier":      {"Cashier"},
		"coding":                 {"Software Developer", "Web Developer", "Software Engineering Intern"},
		"teaching":               {"Elementary Teacher", "Math Tutor"},
		"cleaning":               {"Custodian", "Housekeeper - Hotel"},
		"animals":                {"Veterinary Technician", "Kennel Attendant"},
		"rn":                     {"Registered Nurse - ICU", "RN Med Surg"},
		"swe intern":             {"Software Engineering Intern"},
	}
	for search, titles := range finds {
		for _, title := range titles {
			if !searchFinds(t, search, title) {
				t.Errorf("%q should find %q (pattern %s)", search, title, TitleSearchPattern(search))
			}
		}
	}
	misses := map[string][]string{
		"full stack": {"Full Time Cashier"},
		"intern":     {"Internal Auditor", "International Sales Manager"},
		"nurse":      {"Nursery Worker"},
	}
	for search, titles := range misses {
		for _, title := range titles {
			if searchFinds(t, search, title) {
				t.Errorf("%q should not find %q", search, title)
			}
		}
	}
}

func TestNoResultsFallsBackToCloseTitles(t *testing.T) {
	jobs := []Job{{ID: "1", Title: "Pediatric Nurse", Company: "X", Location: "Tampa, FL", CountryCode: "US", IsActive: true, ApplyURL: "https://x.example/1", Description: "Care for kids."}}
	request := SearchRequest{TargetRole: "pediatric oncology nurse practitioner", Location: "Tampa, FL", CountryCode: "US"}
	if got := NewPipeline().Process(request, jobs); len(got) != 1 {
		t.Fatalf("with no exact matches, close titles should still show; got %d", len(got))
	}
}
