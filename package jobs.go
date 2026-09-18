package jobs

type SearchRequest struct {
	ResumeText    string `json:"resumeText"`
	TargetRole    string `json:"targetRole"`
	Location      string `json:"location"`
	MinimumSalary string `json:"minimumSalary"`
	Remote        bool   `json:"remote"`
	Hybrid        bool   `json:"hybrid"`
	Onsite        bool   `json:"onsite"`
}

type Job struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Company     string `json:"company"`
	Location    string `json:"location"`
	Description string `json:"description"`
	ApplyURL    string `json:"applyURL"`
}

type JobMatch struct {
	ID                    string   `json:"id"`
	Title                 string   `json:"title"`
	Company               string   `json:"company"`
	Location              string   `json:"location"`
	MatchLevel            string   `json:"matchLevel"`
	SupportedRequirements []string `json:"supportedRequirements"`
	PartialRequirements   []string `json:"partialRequirements"`
	MissingRequirements   []string `json:"missingRequirements"`
	Explanation           string   `json:"explanation"`
	ApplyURL              string   `json:"applyURL"`
}
