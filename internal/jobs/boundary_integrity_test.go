package jobs

import (
	"strings"
	"testing"
)

func TestBoundaryIntegrityKeepsDegreeSubjectTogether(t *testing.T) {
	description := `Your Toolbox: Bachelor's degree in Computer Science, Engineering, or a related field (or equivalent experience) 8+ years of relevant engineering experience`
	requirements := ExtractRequirements(description)

	requireBoundaryRequirement(t, requirements, "Bachelor's degree in Computer Science, Engineering, or a related field (or equivalent experience)", RequirementEducation)
	rejectBoundaryExact(t, requirements, "Bachelor's degree in Computer")
	rejectBoundaryExact(t, requirements, "Science, Engineering, or a related field (or equivalent experience)")
	requireBoundaryContains(t, requirements, "8+ years", RequirementExperience)
}

func TestBoundaryIntegrityKeepsModifierNounsAttached(t *testing.T) {
	description := `Your Toolbox: End-to-end lifecycle development and implementation experience including requirements gathering, configuration, design, build, test, and go-live phases Strong functional knowledge around financial systems, processes, and financial reporting practices Ability to advise users on best practices`
	requirements := ExtractRequirements(description)

	requireBoundaryContains(t, requirements, "End-to-end lifecycle development and implementation experience", RequirementExperience)
	requireBoundaryContains(t, requirements, "Strong functional knowledge around financial systems", RequirementSkill)
	rejectBoundaryExact(t, requirements, "End-to-end lifecycle development and implementation")
	rejectBoundaryExact(t, requirements, "experience including requirements gathering, configuration, design, build, test, and go-live phases")
	rejectBoundaryExact(t, requirements, "Strong functional")
	rejectBoundaryExact(t, requirements, "knowledge around financial systems, processes, and financial reporting practices")
}

func requireBoundaryRequirement(t *testing.T, requirements []Requirement, text string, category RequirementCategory) {
	t.Helper()
	for _, requirement := range requirements {
		if strings.EqualFold(strings.TrimSpace(requirement.Text), strings.TrimSpace(text)) && requirement.Category == category {
			return
		}
	}
	t.Fatalf("expected intact requirement %q category=%q, got %#v", text, category, requirements)
}

func requireBoundaryContains(t *testing.T, requirements []Requirement, text string, category RequirementCategory) {
	t.Helper()
	needle := strings.ToLower(strings.TrimSpace(text))
	for _, requirement := range requirements {
		if strings.Contains(strings.ToLower(requirement.Text), needle) && requirement.Category == category {
			return
		}
	}
	t.Fatalf("expected requirement containing %q category=%q, got %#v", text, category, requirements)
}

func rejectBoundaryExact(t *testing.T, requirements []Requirement, text string) {
	t.Helper()
	for _, requirement := range requirements {
		if strings.EqualFold(strings.TrimSpace(requirement.Text), strings.TrimSpace(text)) {
			t.Fatalf("unexpected broken requirement %q in %#v", text, requirements)
		}
	}
}
