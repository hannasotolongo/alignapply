package matching

import (
	"reflect"
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/candidate"
)

func TestRequirementAlternatives(t *testing.T) {
	got := requirementAlternatives("Med surg, ICU, ER, or acute experience")
	want := []string{"med surg", "icu", "er", "acute experience"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("alternatives = %q, want %q", got, want)
	}
	if got := requirementAlternatives("Ability to communicate clearly with patients or their families about care plans and medication changes"); got != nil {
		t.Errorf("a sentence with 'or' should not split, got %q", got)
	}
	if verifyEvidenceDeterministically("Med surg, ICU, ER, or acute experience", "registered nurse on a medical-surgical unit").Level != evidenceSupported {
		t.Error("one alternative (med-surg) should meet the requirement")
	}
}

func TestSpelledOutYears(t *testing.T) {
	for text, want := range map[string]int{"One year nursing experience": 1, "Two (2) years of classroom teaching": 2, "3+ years of experience": 3, "5-7 yrs": 5} {
		if got, ok := extractRequiredYears(text); !ok || got != want {
			t.Errorf("extractRequiredYears(%q) = %d, %v; want %d", text, got, ok, want)
		}
	}
}

func TestCPRFromBLS(t *testing.T) {
	evidence := []candidate.Evidence{{Text: "Basic Life Support (BLS), American Heart Association"}}
	if evaluateExplicitCredential(evidence, "Current CPR certification").Level != evidenceSupported {
		t.Error("BLS should meet a CPR requirement")
	}
	if evaluateExplicitCredential([]candidate.Evidence{{Text: "Certified Nursing Assistant"}}, "Current CPR certification").Level == evidenceSupported {
		t.Error("a CNA alone should not meet a CPR requirement")
	}
}

func TestDegreeLadder(t *testing.T) {
	associate := []candidate.Evidence{{Text: "Associate of Arts in Business Administration, Phoenix College"}}
	if result, handled := evaluateDegreeLadder(associate, "High school diploma or GED required"); !handled || result.Level != evidenceSupported {
		t.Error("an associate degree should meet a high school requirement")
	}
	master := []candidate.Evidence{{Text: "M.S. Data Science, University of Florida"}}
	if result, handled := evaluateDegreeLadder(master, "Bachelor's degree required"); !handled || result.Level != evidenceSupported {
		t.Error("a master's should meet a bachelor's requirement")
	}
	if _, handled := evaluateDegreeLadder(master, "Bachelor's degree in Nursing"); handled {
		t.Error("a field-specific degree must still be checked for the field")
	}
	if _, handled := evaluateDegreeLadder(associate, "Bachelor's degree required"); handled {
		t.Error("an associate degree does not meet a bachelor's requirement")
	}
}

func TestWorkAtSchoolsIsWork(t *testing.T) {
	for line, want := range map[string]bool{
		"3rd Grade Teacher — Austin Independent School District (Aug 2019 – Present)": false,
		"University of South Florida, 2016 – 2020":                                    true,
		"Bachelor of Science in Nursing, 2020":                                        true,
		"Staff Nurse, University Hospital (2018 – 2022)":                              false,
	} {
		if got := isEducationLine(line); got != want {
			t.Errorf("isEducationLine(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestLogisticsDontCount(t *testing.T) {
	for _, text := range []string{"Valid driver's license, auto insurance and reliable transportation", "Must pass a background check", "Able to lift up to 50 pounds"} {
		if !isLogisticsRequirement(text) {
			t.Errorf("%q should not count toward fit", text)
		}
	}
	if isLogisticsRequirement("CDL Class A license") {
		t.Error("a CDL is a real qualification")
	}
}
