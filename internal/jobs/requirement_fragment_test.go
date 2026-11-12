package jobs

import "testing"

func TestCutOffRequirementsAreFragments(t *testing.T) {
	for _, text := range []string{"Fresenius Medical Care is an", "· Entry level for RNs with less than", "Experience in"} {
		if !isIncompleteRequirementFragment(text) {
			t.Errorf("%q should be rejected as a cut-off fragment", text)
		}
	}
	for _, text := range []string{"Current CPR certification", "2 years of experience or more", "BLS"} {
		if isIncompleteRequirementFragment(text) {
			t.Errorf("%q is a real requirement", text)
		}
	}
	if got := cleanRequirementText("· Chronic/acute hemodialysis experience (preferred)"); got != "Chronic/acute hemodialysis experience (preferred)" {
		t.Errorf("bullet not stripped: %q", got)
	}
}
