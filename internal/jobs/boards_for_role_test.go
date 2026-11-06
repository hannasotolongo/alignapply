package jobs

import "testing"

func TestBoardsForRole(t *testing.T) {
	boards := []Board{
		{ATS: "workday", Slug: "a", Company: "Aspen Dental"},
		{ATS: "workday", Slug: "b", Company: "Target"},
		{ATS: "workday", Slug: "c", Company: "Smile Doctors"},
		{ATS: "workday", Slug: "d", Company: "AdventHealth"},
	}
	fit, rest := boardsForRole("Dentist", boards)
	if len(fit) != 2 || fit[0].Company != "Aspen Dental" || fit[1].Company != "Smile Doctors" || len(rest) != 2 {
		t.Fatalf("dentist: fit=%v rest=%v", fit, rest)
	}
	fit, _ = boardsForRole("Registered Nurse", boards)
	if len(fit) != 1 || fit[0].Company != "AdventHealth" {
		t.Fatalf("nurse: fit=%v", fit)
	}
	fit, _ = boardsForRole("Cashier", boards)
	if len(fit) != 0 {
		t.Fatalf("cashier: fit=%v", fit)
	}
}
