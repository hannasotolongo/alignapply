package api

import (
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

func TestBuildTrialResponse(t *testing.T) {
	ranked := []jobs.JobMatch{
		{Title: "Office Manager", MatchPercentage: 88, ApplyURL: "https://a.example/1",
			MissingRequirements:   []string{"Microsoft Excel", "Bachelor's degree"},
			SupportedRequirements: []string{"Scheduling", "Customer service"}},
		{Title: "Admin Coordinator", MatchPercentage: 80, ApplyURL: "https://b.example/2",
			MissingRequirements:   []string{"microsoft  excel"},
			SupportedRequirements: []string{"Scheduling"}},
		{Title: "Front Office Lead", MatchPercentage: 70, ApplyURL: "https://c.example/3"},
		{Title: "Receptionist", MatchPercentage: 60, ApplyURL: "https://d.example/4"},
	}
	got := buildTrialResponse(ranked)
	if got.Total != 4 || len(got.Matches) != 3 || got.Matches[0].Title != "Office Manager" {
		t.Fatalf("total/matches = %d %+v", got.Total, got.Matches)
	}
	if got.AverageMatch != 75 {
		t.Errorf("average = %d, want 75", got.AverageMatch)
	}
	if len(got.Fixes) == 0 || got.Fixes[0].Text != "Microsoft Excel" || got.Fixes[0].Jobs != 2 {
		t.Errorf("fixes = %+v", got.Fixes)
	}
	if len(got.Strengths) == 0 || got.Strengths[0] != "Scheduling" {
		t.Errorf("strengths = %+v", got.Strengths)
	}
	empty := buildTrialResponse(nil)
	if empty.Total != 0 || empty.Matches == nil || empty.Fixes == nil {
		t.Errorf("empty = %+v", empty)
	}
}

func TestTrialUseful(t *testing.T) {
	for _, text := range []string{"BACKGROUND CHECKS/CLEARANCES", "Employment will require successful completion of background check(s) in accordance with University policies", "MINIMUM EDUCATION, WORK EXPERIENCE & REQUIRED", "Requirements:"} {
		if trialUseful(text) {
			t.Errorf("%q should be left out", text)
		}
	}
	for _, text := range []string{"Proficiency in Microsoft Excel", "3+ years of office management experience", "Associate Degree"} {
		if !trialUseful(text) {
			t.Errorf("%q should be kept", text)
		}
	}
}
