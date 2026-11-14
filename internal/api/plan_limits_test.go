package api

import (
	"testing"
	"time"
)

func TestMeterLimitsByPlan(t *testing.T) {
	free := gradedAnswerMeter.stateFor(entitlement{})
	if !free.free || free.limit != FreeGradedAnswers || !free.since.Equal(freeAllowanceStart) {
		t.Fatalf("free graded answers: %+v", free)
	}
	prep := gradedAnswerMeter.stateFor(entitlement{Interview: true, InterviewOnly: true})
	if prep.free || prep.limit != PrepGradedAnswersPerMonth || prep.since.After(time.Now()) || prep.since.Day() != 1 {
		t.Fatalf("prep graded answers: %+v", prep)
	}
	pro := gradedAnswerMeter.stateFor(entitlement{Pro: true, Interview: true})
	if pro.limit != ProGradedAnswersPerMonth {
		t.Fatalf("pro graded answers: %+v", pro)
	}
	if got := tailoredResumeMeter.stateFor(entitlement{}); got.limit != 0 {
		t.Fatalf("free plan should not tailor résumés: %+v", got)
	}
	if got := tailoredResumeMeter.stateFor(entitlement{Interview: true}); got.limit != PrepResumesPerMonth {
		t.Fatalf("prep résumés: %+v", got)
	}
}
