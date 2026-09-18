package candidate

type EvidenceCategory string

const (
	EvidenceSkill         EvidenceCategory = "skill"
	EvidenceExperience    EvidenceCategory = "experience"
	EvidenceEducation     EvidenceCategory = "education"
	EvidenceCertification EvidenceCategory = "certification"
	EvidenceLicense       EvidenceCategory = "license"
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
	default:
		return nil
	}
}
