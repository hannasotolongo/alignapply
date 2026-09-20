package candidate

import "strings"

// CareerProfile contains user-confirmed candidate information.
// It is intentionally separate from any individual resume.
type CareerProfile struct {
	About          string   `json:"about,omitempty"`
	Experience     []string `json:"experience,omitempty"`
	Education      []string `json:"education,omitempty"`
	Skills         []string `json:"skills,omitempty"`
	Projects       []string `json:"projects,omitempty"`
	Certifications []string `json:"certifications,omitempty"`
	Licenses       []string `json:"licenses,omitempty"`
}

// Normalize cleans user-entered profile data without inventing,
// rewording, or reclassifying the user's evidence.
func (p *CareerProfile) Normalize() {
	p.About = strings.TrimSpace(p.About)
	p.Experience = normalizeProfileValues(p.Experience)
	p.Education = normalizeProfileValues(p.Education)
	p.Skills = normalizeProfileValues(p.Skills)
	p.Projects = normalizeProfileValues(p.Projects)
	p.Certifications = normalizeProfileValues(p.Certifications)
	p.Licenses = normalizeProfileValues(p.Licenses)
}

// HasEvidence reports whether the Career Profile contains any
// user-confirmed evidence.
func (p CareerProfile) HasEvidence() bool {
	return strings.TrimSpace(p.About) != "" ||
		len(p.Experience) > 0 ||
		len(p.Education) > 0 ||
		len(p.Skills) > 0 ||
		len(p.Projects) > 0 ||
		len(p.Certifications) > 0 ||
		len(p.Licenses) > 0
}

// BuildProfile creates the evidence representation used by matching.
//
// Resume evidence continues through the existing resume extractor.
// Career Profile evidence is then added separately with explicit
// provenance so it is never represented as if it came from a resume.
func BuildProfile(
	resumeText string,
	career CareerProfile,
) Profile {
	profile := ExtractProfile(resumeText)

	career.Normalize()

	addStructuredEvidence(
		&profile,
		career.About,
		EvidenceSummary,
		"career_profile.about",
	)

	for _, value := range career.Experience {
		addStructuredEvidence(
			&profile,
			value,
			EvidenceExperience,
			"career_profile.experience",
		)
	}

	for _, value := range career.Education {
		addStructuredEvidence(
			&profile,
			value,
			EvidenceEducation,
			"career_profile.education",
		)
	}

	for _, value := range career.Skills {
		addStructuredEvidence(
			&profile,
			value,
			EvidenceSkill,
			"career_profile.skills",
		)
	}

	for _, value := range career.Projects {
		addStructuredEvidence(
			&profile,
			value,
			EvidenceProject,
			"career_profile.projects",
		)
	}

	for _, value := range career.Certifications {
		addStructuredEvidence(
			&profile,
			value,
			EvidenceCertification,
			"career_profile.certifications",
		)
	}

	for _, value := range career.Licenses {
		addStructuredEvidence(
			&profile,
			value,
			EvidenceLicense,
			"career_profile.licenses",
		)
	}

	return profile
}

func addStructuredEvidence(
	profile *Profile,
	text string,
	category EvidenceCategory,
	source string,
) {
	text = cleanEvidenceText(text)

	if text == "" {
		return
	}

	addEvidence(
		profile,
		Evidence{
			Text:     text,
			Category: category,
			Source:   source,
		},
	)
}

func normalizeProfileValues(
	values []string,
) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{})

	for _, value := range values {
		value = cleanEvidenceText(value)

		if value == "" {
			continue
		}

		key := strings.ToLower(value)

		if _, exists := seen[key]; exists {
			continue
		}

		seen[key] = struct{}{}
		result = append(result, value)
	}

	return result
}
