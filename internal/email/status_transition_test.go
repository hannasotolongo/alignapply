package email

import "testing"

func TestCanAdvanceApplicationStatus(t *testing.T) {
	tests := []struct {
		name    string
		current string
		next    string
		want    bool
	}{
		{"applied to received", StatusApplied, StatusReceived, true},
		{"received to recruiter", StatusReceived, StatusRecruiterContact, true},
		{"recruiter to interview", StatusRecruiterContact, StatusInterviewing, true},
		{"interview to offer", StatusInterviewing, StatusOffer, true},
		{"offer to hired", StatusOffer, StatusHired, true},
		{"applied to rejected", StatusApplied, StatusRejected, true},
		{"interview to rejected", StatusInterviewing, StatusRejected, true},
		{"interview does not regress", StatusInterviewing, StatusReceived, false},
		{"same status ignored", StatusReceived, StatusReceived, false},
		{"rejected terminal", StatusRejected, StatusOffer, false},
		{"hired terminal", StatusHired, StatusRejected, false},
		{"withdrawn terminal", "withdrawn", StatusInterviewing, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CanAdvanceApplicationStatus(test.current, test.next)
			if got != test.want {
				t.Fatalf(
					"CanAdvanceApplicationStatus(%q, %q) = %v, want %v",
					test.current,
					test.next,
					got,
					test.want,
				)
			}
		})
	}
}
