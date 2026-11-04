package matching

import (
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

const icuPosting = `ICU Registered Nurse
Requirements:
- Active Florida RN license required
- BLS and ACLS certification required
- 2+ years of critical care experience required
- Bachelor of Science in Nursing required
- Experience with ventilated patients and titrated drips
Preferred:
- CCRN certification preferred
- Experience precepting new nurses`

func TestQualifiedNurseIsNotReach(t *testing.T) {
	resume := `Maria Lopez, RN
EXPERIENCE
Registered Nurse, Medical ICU — Jackson Memorial Hospital    Jan 2020 – Present
- Care for 2-3 critically ill patients per shift including ventilated patients and titrated drips
- Patient and family education on discharge plans and medications
- Charted in Epic EHR; precepted new graduate nurses
Patient Care Technician — Baptist Health    06/2017 - 12/2019
EDUCATION
BSN, Florida International University 2013 - 2017
LICENSES & CERTIFICATIONS
Registered Nurse, Florida License
BLS, ACLS (American Heart Association)`

	m := Match(jobs.SearchRequest{ResumeText: resume}, jobs.EnrichJob(jobs.Job{ID: "icu", Description: icuPosting}))
	if m.MatchLevel == "Reach" {
		t.Fatalf("qualified nurse scored Reach: %+v", m)
	}
	for _, missing := range m.MissingRequirements {
		if missing == "BLS and ACLS certification required" {
			t.Fatalf("BLS/ACLS on résumé but marked missing")
		}
	}
	ccrnMet := false
	for _, met := range m.SupportedRequirements {
		if met == "CCRN certification preferred" {
			ccrnMet = true
		}
	}
	if ccrnMet {
		t.Fatal("CCRN is not on the résumé but was marked met")
	}
}

func TestUnqualifiedCandidateIsStillReach(t *testing.T) {
	resume := `Alex Kim
EXPERIENCE
Retail Sales Associate — Target    Mar 2025 – Present
- Helped customers and managed the register
EDUCATION
High School Diploma 2024`

	m := Match(jobs.SearchRequest{ResumeText: resume}, jobs.EnrichJob(jobs.Job{ID: "icu", Description: icuPosting}))
	if m.MatchLevel != "Reach" {
		t.Fatalf("unqualified candidate should be Reach, got %q (%d%%)", m.MatchLevel, m.MatchPercentage)
	}
}
