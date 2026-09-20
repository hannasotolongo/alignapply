package candidate

type EvidenceCategory string

const (
	EvidenceSkill         EvidenceCategory = "skill"
	EvidenceExperience    EvidenceCategory = "experience"
	EvidenceEducation     EvidenceCategory = "education"
	EvidenceCertification EvidenceCategory = "certification"
	EvidenceLicense       EvidenceCategory = "license"
	EvidenceProject       EvidenceCategory = "project"
	EvidenceSummary       EvidenceCategory = "summary"
)

type Evidence struct {
	Text     string           `json:"text"`
	Category EvidenceCategory `json:"category"`
	Source   string           `json:"source,omitempty"`
}

type Profile struct {
	RawResumeText string `json:"rawResumeText"`

	Skills         []Evidence `json:"skills"`
	Experience     []Evidence `json:"experience"`
	Education      []Evidence `json:"education"`
	Certifications []Evidence `json:"certifications"`
	Licenses       []Evidence `json:"licenses"`
	Projects       []Evidence `json:"projects"`
	Summary        []Evidence `json:"summary"`

	AllEvidence []Evidence `json:"allEvidence"`
}

func (p Profile) EvidenceByCategory(
	category EvidenceCategory,
) []Evidence {
	switch category {
	case EvidenceSkill:
		return p.Skills

	case EvidenceExperience:
		return p.Experience

	case EvidenceEducation:
		return p.Education

	case EvidenceCertification:
		return p.Certifications

	case EvidenceLicense:
		return p.Licenses

	case EvidenceProject:
		return p.Projects

	case EvidenceSummary:
		return p.Summary

	default:
		return nil
	}
}
