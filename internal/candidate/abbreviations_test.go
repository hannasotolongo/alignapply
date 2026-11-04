package candidate

import (
	"strings"
	"testing"
)

func TestExpandCredentialAbbreviations(t *testing.T) {
	got := ExpandCredentialAbbreviations("Education: BSN, University of Florida. BLS and ACLS certified. Florida RN.")
	for _, want := range []string{"Bachelor of Science in Nursing", "Basic Life Support", "Advanced Cardiovascular Life Support", "Registered Nurse"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestExpandCredentialAbbreviationsIgnoresProse(t *testing.T) {
	text := "Worked in sales or marketing for a business."
	if got := ExpandCredentialAbbreviations(text); got != text {
		t.Fatalf("prose should not be expanded, got %q", got)
	}
	if got := ExpandCredentialAbbreviations("B.S.N. 2019"); !strings.Contains(got, "Bachelor of Science in Nursing") {
		t.Fatalf("dotted form not expanded: %q", got)
	}
}
