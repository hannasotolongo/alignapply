package candidate

import "testing"

func TestCareerProfileNormalizeTrimsAndDeduplicates(t *testing.T) {
	profile := CareerProfile{
		About: "  Backend engineer  ",
		Skills: []string{
			" Go ",
			"Go",
			"go",
			"",
			"  ",
			"Python",
		},
		Experience: []string{
			" Built production APIs ",
			"Built production APIs",
		},
	}

	profile.Normalize()

	if profile.About != "Backend engineer" {
		t.Fatalf(
			"expected trimmed about text; got %q",
			profile.About,
		)
	}

	if len(profile.Skills) != 2 {
		t.Fatalf(
			"expected 2 unique skills; got %d: %v",
			len(profile.Skills),
			profile.Skills,
		)
	}

	if profile.Skills[0] != "Go" ||
		profile.Skills[1] != "Python" {
		t.Fatalf(
			"unexpected normalized skills: %v",
			profile.Skills,
		)
	}

	if len(profile.Experience) != 1 {
		t.Fatalf(
			"expected duplicate experience to be removed; got %v",
			profile.Experience,
		)
	}
}

func TestCareerProfileHasEvidence(t *testing.T) {
	if (CareerProfile{}).HasEvidence() {
		t.Fatal(
			"expected empty CareerProfile to contain no evidence",
		)
	}

	profile := CareerProfile{
		Projects: []string{
			"Built a distributed scheduler",
		},
	}

	if !profile.HasEvidence() {
		t.Fatal(
			"expected project evidence to make CareerProfile non-empty",
		)
	}
}

func TestBuildProfilePreservesStructuredCategories(t *testing.T) {
	career := CareerProfile{
		About: "Backend and distributed systems engineer",
		Skills: []string{
			"Go",
		},
		Experience: []string{
			"Built production APIs for 4 years",
		},
		Education: []string{
			"Bachelor of Science in Biology",
		},
		Projects: []string{
			"Built a distributed workload scheduler",
		},
		Certifications: []string{
			"AWS Certified Developer",
		},
		Licenses: []string{
			"Registered Pharmacist License",
		},
	}

	profile := BuildProfile("", career)

	if len(profile.Summary) != 1 {
		t.Fatalf(
			"expected 1 summary item; got %d",
			len(profile.Summary),
		)
	}

	if len(profile.Skills) != 1 {
		t.Fatalf(
			"expected 1 skill item; got %d",
			len(profile.Skills),
		)
	}

	if len(profile.Experience) != 1 {
		t.Fatalf(
			"expected 1 experience item; got %d",
			len(profile.Experience),
		)
	}

	if len(profile.Education) != 1 {
		t.Fatalf(
			"expected 1 education item; got %d",
			len(profile.Education),
		)
	}

	if len(profile.Projects) != 1 {
		t.Fatalf(
			"expected 1 project item; got %d",
			len(profile.Projects),
		)
	}

	if len(profile.Certifications) != 1 {
		t.Fatalf(
			"expected 1 certification item; got %d",
			len(profile.Certifications),
		)
	}

	if len(profile.Licenses) != 1 {
		t.Fatalf(
			"expected 1 license item; got %d",
			len(profile.Licenses),
		)
	}

	// Most importantly, a project remains a project.
	if profile.Projects[0].Category != EvidenceProject {
		t.Fatalf(
			"expected project category; got %q",
			profile.Projects[0].Category,
		)
	}

	if len(profile.Experience) != 1 {
		t.Fatal(
			"project evidence must not be added to professional experience",
		)
	}
}

func TestBuildProfilePreservesCareerProfileProvenance(t *testing.T) {
	career := CareerProfile{
		Skills: []string{
			"Go",
		},
		Projects: []string{
			"Built an LLM workload scheduler",
		},
	}

	profile := BuildProfile("", career)

	if len(profile.Skills) != 1 {
		t.Fatalf(
			"expected skill evidence; got %v",
			profile.Skills,
		)
	}

	if profile.Skills[0].Source !=
		"career_profile.skills" {
		t.Fatalf(
			"expected career profile skill provenance; got %q",
			profile.Skills[0].Source,
		)
	}

	if len(profile.Projects) != 1 {
		t.Fatalf(
			"expected project evidence; got %v",
			profile.Projects,
		)
	}

	if profile.Projects[0].Source !=
		"career_profile.projects" {
		t.Fatalf(
			"expected career profile project provenance; got %q",
			profile.Projects[0].Source,
		)
	}
}

func TestBuildProfileKeepsResumeAndCareerEvidenceDistinct(t *testing.T) {
	resumeText := "Built production services in Python"

	career := CareerProfile{
		Skills: []string{
			"Go",
		},
	}

	profile := BuildProfile(
		resumeText,
		career,
	)

	foundResumeEvidence := false
	foundCareerEvidence := false

	for _, evidence := range profile.AllEvidence {
		if evidence.Source == resumeText {
			foundResumeEvidence = true
		}

		if evidence.Source ==
			"career_profile.skills" {
			foundCareerEvidence = true
		}
	}

	if !foundResumeEvidence {
		t.Fatal(
			"expected resume evidence to retain resume provenance",
		)
	}

	if !foundCareerEvidence {
		t.Fatal(
			"expected structured evidence to retain Career Profile provenance",
		)
	}
}
