package matching_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
	"github.com/hannasotolongo/casemade-backend/internal/matching"
)

// Real-world fit checks: each résumé against each posting (one real home
// health posting, plus typical teacher, office manager and software
// postings). Each person must be a strong match for their own kind of job
// and not for the others.
var fitCases = []struct {
	name, posting, resume, role, title, location string
}{
	{"home health RN", "humana_hh_rn.html", "nurse_resume.txt", "Registered nurse", "Home Health Registered Nurse", "Tampa, FL"},
	{"elementary teacher", "teacher_posting.html", "teacher_resume.txt", "Teacher", "Elementary Teacher", "Austin, TX"},
	{"office manager", "office_posting.html", "office_resume.txt", "Office manager", "Office Manager", "Phoenix, AZ"},
	{"software engineer", "swe_posting.html", "swe_resume.txt", "Software engineer", "Software Engineer", "New York, NY"},
}

func readFit(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "fit", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFitAcrossJobs(t *testing.T) {
	for _, posting := range fitCases {
		job := jobs.EnrichJob(jobs.Job{ID: "fit:" + posting.name, Title: posting.title, Company: "Employer", Location: posting.location,
			CountryCode: "US", Description: readFit(t, posting.posting), ApplyURL: "https://example.com/apply",
			SourceURL: "https://example.com/apply", Source: "workday", IsActive: true})
		for _, person := range fitCases {
			match := matching.MatchFast(jobs.SearchRequest{ResumeText: readFit(t, person.resume), TargetRole: posting.role, Location: posting.location}, job)
			if person.name == posting.name {
				if match.MatchPercentage < 85 || match.MatchLevel != "Strong Applicant" {
					t.Errorf("%s résumé on its own job: %d%% %s, want 85%%+ Strong Applicant (missing %q, partial %q)",
						person.name, match.MatchPercentage, match.MatchLevel, match.MissingRequirements, match.PartialRequirements)
				}
				continue
			}
			if match.MatchPercentage >= 60 || match.MatchLevel == "Strong Applicant" {
				t.Errorf("%s résumé on %s job: %d%% %s, want a weak match", person.name, posting.name, match.MatchPercentage, match.MatchLevel)
			}
		}
	}
}
