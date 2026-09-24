package email

import "testing"

func TestMatchApplicationCompanyInBody(t *testing.T) {
	message := Message{
		From:    "recruiting@icims.com",
		Subject: "Thank you for applying",
		Body:    "Thank you for applying to Allvue Systems.",
	}

	applications := []TrackedApplication{
		{
			ApplicationID: "app-1",
			JobID:         "job-1",
			Company:       "Allvue",
			JobTitle:      "Software Engineer",
			CurrentStatus: StatusApplied,
		},
	}

	match, ok := MatchApplication(message, applications)
	if !ok {
		t.Fatal("expected application match")
	}

	if match.Application.ApplicationID != "app-1" {
		t.Fatalf("unexpected application %q", match.Application.ApplicationID)
	}
}

func TestMatchApplicationRejectsUnrelatedEmail(t *testing.T) {
	message := Message{
		From:    "notifications@example.com",
		Subject: "Your weekly update",
		Body:    "Nothing about this message relates to the employer.",
	}

	applications := []TrackedApplication{
		{
			ApplicationID: "app-1",
			Company:       "Allvue",
			JobTitle:      "Software Engineer",
			CurrentStatus: StatusApplied,
		},
	}

	if _, ok := MatchApplication(message, applications); ok {
		t.Fatal("expected unrelated message not to match")
	}
}

func TestMatchApplicationRejectsAmbiguousCompanyApplications(t *testing.T) {
	message := Message{
		From:    "recruiting@allvue.com",
		Subject: "Allvue application update",
		Body:    "Thank you for your interest in Allvue.",
	}

	applications := []TrackedApplication{
		{
			ApplicationID: "app-1",
			Company:       "Allvue",
			JobTitle:      "Software Engineer",
			CurrentStatus: StatusApplied,
		},
		{
			ApplicationID: "app-2",
			Company:       "Allvue",
			JobTitle:      "Backend Engineer",
			CurrentStatus: StatusApplied,
		},
	}

	if _, ok := MatchApplication(message, applications); ok {
		t.Fatal("expected ambiguous applications not to match")
	}
}
