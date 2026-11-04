package candidate

import "testing"

func TestDraftCareerProfileFollowsSections(t *testing.T) {
	summary, items := DraftCareerProfile("Jordan Rivera\njordan@example.com\nSUMMARY\nRegistered nurse with 6 years of acute care.\n" +
		"EXPERIENCE\nRegistered Nurse, Jackson Memorial 2019-2025\nEDUCATION\nBachelor of Science in Nursing\n" +
		"LICENSES\nActive RN license, Florida\nSKILLS\nPatient assessment, Epic EHR; IV therapy")

	if summary != "Registered nurse with 6 years of acute care." {
		t.Fatalf("summary = %q", summary)
	}

	got := map[EvidenceCategory][]string{}
	for _, item := range items {
		got[item.Category] = append(got[item.Category], item.Text)
	}

	if len(got[EvidenceSkill]) != 3 || got[EvidenceSkill][1] != "Epic EHR" {
		t.Fatalf("skills = %q, want 3 split skills", got[EvidenceSkill])
	}
	if len(got[EvidenceExperience]) != 1 || len(got[EvidenceEducation]) != 1 || len(got[EvidenceLicense]) != 1 {
		t.Fatalf("sections = %#v", got)
	}
	for _, item := range items {
		if item.Text == "Jordan Rivera" || item.Text == "jordan@example.com" {
			t.Fatalf("contact header leaked into profile: %q", item.Text)
		}
	}
}
