package matching

import (
	"testing"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/candidate"
)

func TestCareerYearsFromDateRanges(t *testing.T) {
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	profile := candidate.Profile{RawResumeText: `Jane Doe
Registered Nurse, Jackson Memorial Hospital    Jan 2019 – Present
Patient Care Tech, Baptist Health   06/2016 - 12/2018
EDUCATION
BSN, University of Florida 2012 - 2016`}
	// 2016-06..2026-11 continuous = ~10 years; school years excluded.
	if got := careerYears(profile, now); got != 10 {
		t.Fatalf("careerYears = %d, want 10", got)
	}

	overlap := candidate.Profile{Experience: []candidate.Evidence{
		{Text: "Software Engineer, Acme 2020 - 2023"},
		{Text: "Contract Developer 2021 to 2022"},
	}}
	if got := careerYears(overlap, now); got != 4 {
		t.Fatalf("overlapping careerYears = %d, want 4", got)
	}

	if got := careerYears(candidate.Profile{RawResumeText: "No dates here"}, now); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}
