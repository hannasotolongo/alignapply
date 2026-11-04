package api

import (
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

func sampleFitMatch() jobs.JobMatch {
	return jobs.JobMatch{
		MatchPercentage: 62,
		MatchLevel:      "Moderate Match",
		Requirements: []jobs.Requirement{
			{Text: "Active RN license", Category: jobs.RequirementLicense, Importance: jobs.RequirementRequired},
			{Text: "BLS certification", Category: jobs.RequirementCertification, Importance: jobs.RequirementRequired},
			{Text: "Epic EHR", Category: jobs.RequirementSkill, Importance: jobs.RequirementPreferred},
			{Text: "Patient education", Category: jobs.RequirementSkill, Importance: jobs.RequirementRequired},
		},
		SupportedRequirements: []string{"Active RN license"},
		PartialRequirements:   []string{"Patient education"},
		MissingRequirements:   []string{"BLS  certification", "Epic EHR"},
		Explanation:           "details",
	}
}

func TestFitCheckLocksDetailsForAnonymousUsers(t *testing.T) {
	response := buildFitCheckResponse(sampleFitMatch(), false)

	if !response.Locked || response.Gaps != nil || response.Supported != nil || response.Explanation != "" {
		t.Fatalf("anonymous response leaked details: %+v", response)
	}
	if response.MatchPercentage != 62 || response.MissingCount != 2 || response.PartialCount != 1 || response.SupportedCount != 1 {
		t.Fatalf("unexpected counts: %+v", response)
	}
	if response.RequiredMissing != 1 {
		t.Fatalf("RequiredMissing = %d, want 1", response.RequiredMissing)
	}
	if response.Verdict == "" {
		t.Fatal("expected a verdict")
	}
}

func TestFitCheckGapCloserForPro(t *testing.T) {
	response := buildFitCheckResponse(sampleFitMatch(), true)
	if response.Locked || len(response.Gaps) != 3 {
		t.Fatalf("expected 3 unlocked gaps, got %+v", response)
	}

	kinds := map[string]string{}
	for _, gap := range response.Gaps {
		kinds[gap.Requirement] = gap.Kind
	}
	want := map[string]string{
		"BLS certification": gapQuickWin,
		"Epic EHR":          gapAddIfTrue,
		"Patient education": gapStrengthen,
	}
	for text, kind := range want {
		if kinds[text] != kind {
			t.Errorf("gap %q kind = %q, want %q (all: %v)", text, kinds[text], kind, kinds)
		}
	}
}

func TestFitCheckUnscorable(t *testing.T) {
	response := buildFitCheckResponse(jobs.JobMatch{MatchLevel: "Insufficient Evidence"}, false)
	if response.Scorable {
		t.Fatal("expected unscorable")
	}
}
