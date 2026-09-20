package jobs

import (
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/candidate"
)

type SearchRequest struct {
	// CareerProfile is the user's structured, user-confirmed evidence.
	// It is the preferred evidence source for overall job fit.
	CareerProfile candidate.CareerProfile `json:"careerProfile,omitempty"`

	// ResumeText remains supported during migration and for legacy clients.
	// Individual resume evidence stays conceptually separate from CareerProfile.
	ResumeText string `json:"resumeText,omitempty"`

	TargetRole     string   `json:"targetRole"`
	Location       string   `json:"location"`
	MinimumSalary  string   `json:"minimumSalary"`
	Remote         bool     `json:"remote"`
	Hybrid         bool     `json:"hybrid"`
	Onsite         bool     `json:"onsite"`
	EmploymentType []string `json:"employmentType,omitempty"`
	CountryCode    string   `json:"countryCode,omitempty"`
}

// RequirementCategory describes what kind of evidence is needed.
type RequirementCategory string

const (
	RequirementSkill         RequirementCategory = "skill"
	RequirementExperience    RequirementCategory = "experience"
	RequirementEducation     RequirementCategory = "education"
	RequirementLicense       RequirementCategory = "license"
	RequirementCertification RequirementCategory = "certification"
	RequirementPhysical      RequirementCategory = "physical"
	RequirementTravel        RequirementCategory = "travel"
	RequirementOther         RequirementCategory = "other"
)

// RequirementImportance distinguishes mandatory requirements from
// qualifications that the employer describes as preferred.
type RequirementImportance string

const (
	RequirementRequired  RequirementImportance = "required"
	RequirementPreferred RequirementImportance = "preferred"
)

// Requirement is one atomic qualification from a job posting.
//
// Text should contain only the individual qualification itself.
// It should not contain section headings, company marketing copy,
// benefits, responsibilities, or neighboring requirements.
type Requirement struct {
	Text       string                `json:"text"`
	Category   RequirementCategory   `json:"category"`
	Importance RequirementImportance `json:"importance"`
}

type Job struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	SourceJobID string `json:"sourceJobID,omitempty"`

	Title       string `json:"title"`
	Company     string `json:"company"`
	Description string `json:"description"`

	Location        string `json:"location"`
	CountryCode     string `json:"countryCode,omitempty"`
	WorkArrangement string `json:"workArrangement,omitempty"`
	EmploymentType  string `json:"employmentType,omitempty"`

	SalaryMin      *float64 `json:"salaryMin,omitempty"`
	SalaryMax      *float64 `json:"salaryMax,omitempty"`
	SalaryCurrency string   `json:"salaryCurrency,omitempty"`
	SalaryPeriod   string   `json:"salaryPeriod,omitempty"`

	PostedAt  *time.Time `json:"postedAt,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	IsActive  bool       `json:"isActive"`

	// Canonical structured requirements used by the matcher.
	Requirements []Requirement `json:"requirements,omitempty"`

	// Existing compatibility fields. These remain temporarily while
	// the API and iOS client migrate to Requirements.
	MinimumQualifications   []string `json:"minimumQualifications,omitempty"`
	PreferredQualifications []string `json:"preferredQualifications,omitempty"`
	Skills                  []string `json:"skills,omitempty"`

	ExperienceRequirements    []string `json:"experienceRequirements,omitempty"`
	EducationRequirements     []string `json:"educationRequirements,omitempty"`
	LicenseRequirements       []string `json:"licenseRequirements,omitempty"`
	CertificationRequirements []string `json:"certificationRequirements,omitempty"`
	PhysicalRequirements      []string `json:"physicalRequirements,omitempty"`
	TravelRequirements        []string `json:"travelRequirements,omitempty"`

	ApplyURL  string `json:"applyURL"`
	SourceURL string `json:"sourceURL,omitempty"`

	Metadata map[string]string `json:"metadata,omitempty"`
}

type JobMatch struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Company string `json:"company"`

	Description     string `json:"description"`
	Location        string `json:"location"`
	CountryCode     string `json:"countryCode,omitempty"`
	WorkArrangement string `json:"workArrangement,omitempty"`
	EmploymentType  string `json:"employmentType,omitempty"`

	SalaryMin      *float64 `json:"salaryMin,omitempty"`
	SalaryMax      *float64 `json:"salaryMax,omitempty"`
	SalaryCurrency string   `json:"salaryCurrency,omitempty"`
	SalaryPeriod   string   `json:"salaryPeriod,omitempty"`

	PostedAt  *time.Time `json:"postedAt,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`

	// Canonical requirements that produced this match.
	Requirements []Requirement `json:"requirements,omitempty"`

	// Existing compatibility fields.
	MinimumQualifications   []string `json:"minimumQualifications,omitempty"`
	PreferredQualifications []string `json:"preferredQualifications,omitempty"`
	Skills                  []string `json:"skills,omitempty"`

	ExperienceRequirements    []string `json:"experienceRequirements,omitempty"`
	EducationRequirements     []string `json:"educationRequirements,omitempty"`
	LicenseRequirements       []string `json:"licenseRequirements,omitempty"`
	CertificationRequirements []string `json:"certificationRequirements,omitempty"`
	PhysicalRequirements      []string `json:"physicalRequirements,omitempty"`
	TravelRequirements        []string `json:"travelRequirements,omitempty"`

	MatchPercentage int    `json:"matchPercentage"`
	MatchLevel      string `json:"matchLevel"`

	SupportedRequirements []string `json:"supportedRequirements"`
	PartialRequirements   []string `json:"partialRequirements"`
	MissingRequirements   []string `json:"missingRequirements"`

	Explanation string `json:"explanation"`

	ApplyURL  string `json:"applyURL"`
	SourceURL string `json:"sourceURL,omitempty"`
	Source    string `json:"source,omitempty"`
}
