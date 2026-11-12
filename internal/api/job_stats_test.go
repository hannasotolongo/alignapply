package api

import (
	"testing"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

func TestJobCountsUsePostingDates(t *testing.T) {
	now := time.Now()
	ago := func(d time.Duration) *time.Time { at := now.Add(-d); return &at }
	var counts jobCounts
	counts.add(ago(2*time.Hour), now)    // today and this week
	counts.add(ago(3*24*time.Hour), now) // this week
	counts.add(ago(36*time.Hour), now)   // last 2 days and this week
	counts.add(ago(30*24*time.Hour), now)
	counts.add(nil, now) // no posting date: open only
	if counts != (jobCounts{Open: 5, NewThisWeek: 3, NewToday: 2}) {
		t.Fatalf("counts = %+v", counts)
	}
}

func TestStateNameAndStatesIn(t *testing.T) {
	if got := jobs.StateName("FL"); got != "Florida" {
		t.Errorf("StateName(FL) = %q", got)
	}
	if got := jobs.StateName("NY"); got != "New York" {
		t.Errorf("StateName(NY) = %q", got)
	}
	if got := jobs.StatesIn("Tampa, FL"); len(got) != 1 || got[0] != "FL" {
		t.Errorf("StatesIn = %v", got)
	}
}
