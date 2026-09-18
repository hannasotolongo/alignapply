package jobs

// ATSEmployer identifies an employer whose public ATS postings
// AlignApply can query.
type ATSEmployer struct {
	Name     string
	Provider string
	Site     string
}

// DefaultATSEmployers returns verified public ATS employers enabled
// for AlignApply's beta discovery layer.
//
// Provider implementations remain generic. Employer configuration
// belongs here rather than inside retrieval, extraction, or matching.
func DefaultATSEmployers() []ATSEmployer {
	return []ATSEmployer{
		{
			Name:     "Filevine",
			Provider: "lever",
			Site:     "filevine",
		},
		{
			Name:     "Flex",
			Provider: "lever",
			Site:     "Flex",
		},
		{
			Name:     "Ro",
			Provider: "lever",
			Site:     "ro",
		},
		{
			Name:     "Integrate",
			Provider: "lever",
			Site:     "integrate",
		},
		{
			Name:     "Instrumentl",
			Provider: "lever",
			Site:     "Instrumentl",
		},
		{
			Name:     "Supermove",
			Provider: "lever",
			Site:     "supermove",
		},
	}
}
