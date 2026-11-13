package jobs

import (
	"strings"
	"testing"
)

// Postings sampled from real Greenhouse boards (Nov 2026).

func requirementTexts(requirements []Requirement) string {
	var b strings.Builder
	for _, r := range requirements {
		b.WriteString(string(r.Importance) + "/" + string(r.Category) + ": " + r.Text + "\n")
	}
	return b.String()
}

func TestLabelledRequirementsAreKept(t *testing.T) {
	description := `<p>We're building premium experiences.</p>
<p><strong>What you should have</strong></p>
<ul>
<li><strong>5+ years of software engineering experience.</strong> You’re a key technical contributor to your team and have led sizable projects which have had a big impact on your team’s roadmap.</li>
<li><strong>Full-stack proficiency.</strong> You are an expert in React/React Native with a solid grasp of Python backend development. We work primarily in the web (React, TypeScript), mobile and backend (Python).</li>
<li><strong>Product sense.</strong> You maintain a high degree of empathy for your internal and external users, and seek feedback from them about your work.</li>
</ul>
<p><strong>Bonus Points</strong></p>
<ul><li>Experience with performance optimization at scale</li></ul>`
	job := EnrichJob(Job{Title: "Senior Software Engineer", Description: description})
	got := requirementTexts(job.Requirements)
	for _, want := range []string{"required/experience: 5+ years of software engineering experience", "React/React Native with a solid grasp of Python backend development", "preferred/experience: Experience with performance optimization at scale"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "education") {
		t.Fatalf("'a high degree of empathy' is not an education requirement:\n%s", got)
	}
	if strings.Contains(got, "We work primarily") {
		t.Fatalf("company sentences shouldn't become requirements:\n%s", got)
	}
}

func TestRequirementsStopBeforePerks(t *testing.T) {
	description := `<p><strong>What we are looking for in you (qualifications)</strong></p>
<ul><li>Fluent in any one of the following programming languages: Java, Python, Go, C++</li>
<li>Must be authorized to work in the United States.</li></ul>
<p><strong>What you'll find at MongoDB</strong></p>
<ul><li>Relocation Support: Financial assistance, housing, and a commuting stipend</li>
<li>Work-Life Harmony: paid time off, free lunch, and more!</li></ul>`
	got := requirementTexts(EnrichJob(Job{Title: "Software Engineering Intern", Description: description}).Requirements)
	if !strings.Contains(got, "Java, Python, Go") {
		t.Fatalf("the language requirement is missing:\n%s", got)
	}
	if strings.Contains(got, "Relocation") || strings.Contains(got, "free lunch") {
		t.Fatalf("perks became requirements:\n%s", got)
	}
}

func TestDegreeAbbreviationsAreEducation(t *testing.T) {
	for _, text := range []string{"BS/MS or equivalent in Computer Engineering, Computer Science, or related field or equivalent practical experience", "BS in Computer Science"} {
		if category := classifyRequirement(text, RequirementOther); category != RequirementEducation {
			t.Fatalf("%q: got %s", text, category)
		}
	}
	if category := classifyRequirement("Advanced MS Office skills", RequirementOther); category == RequirementEducation {
		t.Fatal("MS Office is not a degree")
	}
}
