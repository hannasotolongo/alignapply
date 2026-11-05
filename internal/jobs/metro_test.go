package jobs

import "testing"

func TestMetroLocationMatching(t *testing.T) {
	request := SearchRequest{Location: "miami", Onsite: true}
	accept := []Job{
		{Title: "Engineer", Location: "Bay Harbor Islands, FL"},
		{Title: "Senior Sales Engineer", Location: "Fort Lauderdale"},
		{Title: "Senior Sales Engineer - Miami / FT Lauderdale", Location: "Nu Stadium"},
		{Title: "RN", Location: "Miami, FL"},
	}
	for _, job := range accept {
		if !matchesLocationPreference(request, job) {
			t.Errorf("expected %q (%q) to match miami", job.Title, job.Location)
		}
	}

	stateRequest := SearchRequest{Location: "Miami, FL", Onsite: true}
	reject := []Job{
		{Title: "Engineer", Location: "Orlando, FL"},
		{Title: "Engineer", Location: "New York, NY"},
	}
	for _, job := range reject {
		if matchesLocationPreference(stateRequest, job) {
			t.Errorf("expected %q to be rejected for Miami, FL", job.Location)
		}
	}

	if !matchesLocationPreference(SearchRequest{Location: "Florida"}, Job{Location: "Orlando, FL"}) &&
		!matchesLocationPreference(SearchRequest{Location: "Florida"}, Job{Location: "Orlando, Florida"}) {
		t.Error("state-only searches should still match cities in that state")
	}
}

func TestSalaryAnnualized(t *testing.T) {
	hourly := 50.0
	job := Job{SalaryMin: &hourly, SalaryMax: &hourly, SalaryPeriod: "hour"}
	if !matchesMinimumSalaryPreference(SearchRequest{MinimumSalary: "100,000"}, job) {
		t.Error("$50/hour (~$104k/yr) should meet a $100k floor")
	}
	low := 30.0
	job = Job{SalaryMax: &low}
	if matchesMinimumSalaryPreference(SearchRequest{MinimumSalary: "100000"}, job) {
		t.Error("$30/hour should not meet a $100k floor")
	}
}

func TestTitleAliases(t *testing.T) {
	if !jobTitleMatchesTargetRole("Registered Nurse", "RN - Med Surg Nights") {
		t.Error("RN title should match Registered Nurse")
	}
	if !jobTitleMatchesTargetRole("RN", "Registered Nurse, ICU") {
		t.Error("Registered Nurse title should match RN")
	}
	if jobTitleMatchesTargetRole("Registered Nurse", "Software Engineer") {
		t.Error("unrelated title matched")
	}
}

func TestCountryFromLocation(t *testing.T) {
	cases := map[string]string{
		"Canada - Remote (ON, AB, BC, or NS Only)": "CA",
		"Remote - Portugal":                        "PT",
		"Remote · USA, Canada":                     "US",
		"New York, NY - Hybrid":                    "US",
		"Remote - United States":                   "US",
		"London, England":                          "GB",
	}
	for location, want := range cases {
		if got := countryFromLocation(location); got != want {
			t.Errorf("countryFromLocation(%q) = %q, want %q", location, got, want)
		}
	}
	us := SearchRequest{Location: "San Francisco, CA", Remote: true}
	if matchesLocationPreference(us, Job{Location: "Remote - Portugal", CountryCode: "PT", WorkArrangement: "remote"}) {
		t.Error("Portugal-only remote job should not match a San Francisco search")
	}
	if !matchesLocationPreference(us, Job{Location: "Remote - United States", CountryCode: "US", WorkArrangement: "remote"}) {
		t.Error("US remote job should match")
	}
}
