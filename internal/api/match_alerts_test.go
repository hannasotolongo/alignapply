package api

import (
	"strings"
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
)

func TestAlertDigestMessage(t *testing.T) {
	t.Setenv("COMPANY_POSTAL_ADDRESS", "PO Box 1, Gainesville, FL 32601")
	server := &Server{web: WebConfig{BaseURL: "https://janecareers.com"}}
	miles := 3.2
	alert := repository.MatchAlert{ID: 7, Email: "a@example.com", TargetRole: "Nurse", Location: "Tampa, FL", Token: "tok"}
	message := server.alertDigestMessage(alert, []jobs.JobMatch{
		{ID: "j1", Title: "RN <Night>", Company: "General", Location: "Tampa, FL", MatchPercentage: 91, ApplyURL: "https://careers.example.com/1", DistanceMiles: &miles},
	})

	if message.Subject != "1 new Nurse match near Tampa, FL" {
		t.Errorf("subject = %q", message.Subject)
	}
	want := "https://www.janecareers.com/alerts/unsubscribe?token=tok"
	if message.Headers["List-Unsubscribe"] != "<"+want+">" || message.Headers["List-Unsubscribe-Post"] == "" {
		t.Errorf("headers = %v", message.Headers)
	}
	for _, part := range []string{want, "https://careers.example.com/1", "RN &lt;Night&gt;", "91%", "3 miles away", "PO Box 1"} {
		if !strings.Contains(message.HTML, part) {
			t.Errorf("HTML is missing %q", part)
		}
	}
	if strings.Contains(message.HTML, "<Night>") {
		t.Error("job title is not escaped")
	}
	if !strings.Contains(message.Text, want) {
		t.Error("text version is missing the unsubscribe link")
	}
}

func TestAlertDailyCap(t *testing.T) {
	t.Setenv("MATCH_ALERTS_DAILY_CAP", "2")
	alertDay.mu.Lock()
	alertDay.date, alertDay.sent = "", 0
	alertDay.mu.Unlock()
	if !takeAlertSlot() || !takeAlertSlot() {
		t.Fatal("first two emails should fit the cap")
	}
	if takeAlertSlot() || !dailyCapReached() {
		t.Fatal("third email should be over the cap")
	}
	giveBackAlertSlot()
	if !takeAlertSlot() {
		t.Fatal("a given-back slot should be usable")
	}
}
