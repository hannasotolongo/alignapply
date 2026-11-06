package jobs

import "testing"

func TestStatesConflict(t *testing.T) {
	cases := []struct {
		requested, job string
		conflict       bool
	}{
		{"Gainesville, FL", "Gainesville, GA", true},
		{"Gainesville, FL", "Gainesville, FL", false},
		{"gainesville, fl", "Gainesville, Florida", false},
		{"Gainesville, FL", "FL032 Gainesville - Clinic", false},
		{"Cleveland, OH", "OH - Cleveland", false},
		{"Cleveland, OH", "Cleveland, Tennessee", true},
		{"New York, NY", "NY - New York City", false},
		{"New York, NY", "Brooklyn, New York", false},
		{"Washington, DC", "Washington, District of Columbia, US", false},
		{"Seattle, WA", "Washington D.C., District of Columbia", true},
		{"Charleston, SC", "Charleston, West Virginia", true},
		{"Miami, FL", "Miami", false},
		{"Remote", "Remote - Arizona", false},
		{"Portland, OR", "Remote or onsite in Portland, OR", false},
	}
	for _, c := range cases {
		if got := statesConflict(c.requested, c.job); got != c.conflict {
			t.Errorf("statesConflict(%q, %q) = %v; want %v (states %v vs %v)", c.requested, c.job, got, c.conflict, usStatesIn(c.requested), usStatesIn(c.job))
		}
	}
}
