package matching

import (
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/candidate"
	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

func TestHardConstraintDoesNotInferLicenseFromHealthcareWork(t *testing.T) {
	profile := candidate.Profile{
		Experience: []candidate.Evidence{
			{Text: "Worked alongside registered nurses in a hospital."},
		},
	}

	result, handled := evaluateHardConstraint(profile, jobs.Requirement{
		Text:       "Active Registered Nurse license",
		Category:   jobs.RequirementLicense,
		Importance: jobs.RequirementRequired,
	})

	if !handled || result.Level != evidenceMissing {
		t.Fatalf("healthcare work must not prove RN licensure: handled=%v result=%+v", handled, result)
	}
}

func TestHardConstraintDoesNotInferDegreeFromRelatedExperience(t *testing.T) {
	profile := candidate.Profile{
		Experience: []candidate.Evidence{
			{Text: "Five years working in hospital operations."},
		},
	}

	result, handled := evaluateHardConstraint(profile, jobs.Requirement{
		Text:       "Bachelor's degree in Nursing",
		Category:   jobs.RequirementEducation,
		Importance: jobs.RequirementRequired,
	})

	if !handled || result.Level != evidenceMissing {
		t.Fatalf("related work must not prove a degree: handled=%v result=%+v", handled, result)
	}
}

func TestHardConstraintYearsCannotUseProjects(t *testing.T) {
	profile := candidate.Profile{
		Projects: []candidate.Evidence{
			{Text: "Built account management tooling for 5 years."},
		},
	}

	result, handled := evaluateHardConstraint(profile, jobs.Requirement{
		Text:       "5 years of account management experience",
		Category:   jobs.RequirementExperience,
		Importance: jobs.RequirementRequired,
	})

	if !handled || result.Level == evidenceSupported {
		t.Fatalf("project duration must not satisfy professional years: handled=%v result=%+v", handled, result)
	}
}
