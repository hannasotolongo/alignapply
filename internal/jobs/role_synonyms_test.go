package jobs

import (
	"regexp"
	"strings"
	"testing"
)

func TestRoleSynonymsMatchRealTitles(t *testing.T) {
	cases := []struct {
		role, title string
		want        bool
	}{
		{"Lawyer", "Associate Attorney - Litigation", true},
		{"lawyer", "Senior Legal Counsel, Employment", true},
		{"Lawyer", "Paralegal", false},
		{"Lawyer", "Attorney Assistant", false},
		{"Attorney", "Corporate Lawyer", true},
		{"Doctor", "Physician - Family Medicine", true},
		{"doctor", "Hospitalist (Nocturnist)", true},
		{"Doctor", "Physician Assistant - Urgent Care", false},
		{"Doctor", "Medical Assistant", false},
		{"Physician", "Emergency Medicine Doctor", true},
		{"Teacher", "Math Instructor", true},
		{"Registered Nurse", "RN - Med Surg Nights", true},
		{"Registered Nurse", "Software Engineer", false},
	}
	for _, c := range cases {
		if got := jobTitleMatchesTargetRole(c.role, c.title); got != c.want {
			t.Errorf("jobTitleMatchesTargetRole(%q, %q) = %v; want %v", c.role, c.title, got, c.want)
		}
		// The database search must find every title the filter accepts.
		if c.want {
			pattern := regexp.MustCompile(strings.ReplaceAll(TitleSearchPattern(c.role), `\m`, `\b`))
			if !pattern.MatchString(strings.ToLower(c.title)) {
				t.Errorf("TitleSearchPattern(%q) = %q misses %q", c.role, TitleSearchPattern(c.role), c.title)
			}
		}
	}
}
