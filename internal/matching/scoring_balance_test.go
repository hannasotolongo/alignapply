package matching

import (
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

const zzPosting = `Registered Nurse - Medical ICU
Requirements:
- Active Florida RN license required
- BLS and ACLS certification required
- 2+ years of critical care experience required
- Bachelor of Science in Nursing required
- Experience with ventilated patients and titrated drips
- Excellent communication and interpersonal skills
- Strong organizational and time management abilities
- Ability to work well in a fast-paced team environment
- Detail-oriented with strong critical thinking skills
Preferred:
- CCRN certification preferred
- Experience with Cerner EHR preferred
- Experience precepting new nurses preferred
- Bilingual English/Spanish preferred`

func TestSoftSkillsAndPreferredDontSinkQualifiedCandidates(t *testing.T) {
	strong := `Maria Lopez, RN
EXPERIENCE
Registered Nurse, Medical ICU — Jackson Memorial Hospital    Jan 2020 – Present
- Care for 2-3 critically ill patients per shift including ventilated patients and titrated drips
- Patient and family education on discharge plans
- Charted in Epic EHR; precepted new graduate nurses
EDUCATION
BSN, Florida International University 2013 - 2017
LICENSES & CERTIFICATIONS
Registered Nurse, Florida License
BLS, ACLS (American Heart Association)`
	medium := `Sam Rivera, RN
EXPERIENCE
Registered Nurse, Med-Surg — Baptist Hospital    Jun 2023 – Present
- Care for 5 patients per shift on a med-surg unit
EDUCATION
ADN, Miami Dade College 2021 - 2023
LICENSES
Registered Nurse, Florida License
BLS`
	weak := `Alex Kim
EXPERIENCE
Retail Sales Associate — Target    Mar 2025 – Present
- Helped customers and managed the register`
	job := jobs.EnrichJob(jobs.Job{ID: "icu", Description: zzPosting})
	score := func(resume string) jobs.JobMatch { return Match(jobs.SearchRequest{ResumeText: resume}, job) }
	s, m, w := score(strong), score(medium), score(weak)
	if s.MatchLevel != "Strong Applicant" || s.MatchPercentage < 80 {
		t.Fatalf("qualified nurse should be a strong applicant, got %d%% %s (missing %q)", s.MatchPercentage, s.MatchLevel, s.MissingRequirements)
	}
	for _, missing := range s.MissingRequirements {
		if softSkillPattern.MatchString(missing) && !hardSignal.MatchString(missing) {
			t.Fatalf("soft skill %q should not be scored", missing)
		}
	}
	if !(s.MatchPercentage > m.MatchPercentage && m.MatchPercentage > w.MatchPercentage) {
		t.Fatalf("scores should rank strong > medium > weak: %d, %d, %d", s.MatchPercentage, m.MatchPercentage, w.MatchPercentage)
	}
	licenseMissing := false
	for _, missing := range w.MissingRequirements {
		if missing == "Active Florida RN license required" {
			licenseMissing = true
		}
	}
	if !licenseMissing || w.MatchLevel != "Reach" {
		t.Fatalf("non-nurse must be a reach with the license missing: %+v", w)
	}
}
