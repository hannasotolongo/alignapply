package jobs

import "testing"

func TestWorldCountries(t *testing.T) {
	cases := map[string]string{
		"Toronto, ON":                     "CA",
		"London, ON, Canada":              "CA",
		"London, UK":                      "GB",
		"London, England, United Kingdom": "GB",
		"Sydney NSW":                      "AU",
		"Bengaluru, Karnataka, India":     "IN",
		"Remote - Canada":                 "CA",
		"Dublin, Ireland":                 "IE",
		"Auckland, New Zealand":           "NZ",
		"Singapore":                       "SG",
		"Ontario, CA":                     "",
		"Canterbury, CT":                  "",
		"Remote - New England":            "",
		"Austin, TX":                      "",
		"Vancouver, WA":                   "",
		"Melbourne, FL":                   "",
	}
	for location, want := range cases {
		if got, _ := worldCountryIn(location); got != want {
			t.Errorf("worldCountryIn(%q) = %q, want %q", location, got, want)
		}
	}
	for location, want := range map[string]string{"London": "GB", "Toronto": "CA", "Bengaluru": "IN", "Birmingham": "", "Austin, TX": "", "Ontario, CA": ""} {
		if got := SearchCountry(location); got != want {
			t.Errorf("SearchCountry(%q) = %q, want %q", location, got, want)
		}
	}
	if got := countryFromLocation("Remote - US or Canada"); got != "US" {
		t.Errorf("Remote - US or Canada is open to US candidates: got %q", got)
	}
	if got := countryFromLocation("Mississauga, Ontario, Canada"); got != "CA" {
		t.Errorf("got %q", got)
	}
}

func TestSearchAbroad(t *testing.T) {
	job := func(id, location string) Job {
		return Job{ID: id, Title: "Software Engineer", Company: "X", Location: location, CountryCode: countryFromLocation(location),
			IsActive: true, ApplyURL: "https://x.example/" + id, Description: "Build things."}
	}
	all := []Job{
		job("toronto", "Toronto, Ontario, Canada"),
		job("mississauga", "Mississauga, ON"),
		job("vancouver", "Vancouver, BC, Canada"),
		job("london", "London, England, United Kingdom"),
		job("manchester", "Manchester, UK"),
		job("austin", "Austin, TX"),
		job("londonky", "London, KY"),
	}
	ids := func(search string) map[string]bool {
		got := map[string]bool{}
		for _, j := range NewPipeline().Process(SearchRequest{TargetRole: "software engineer", Location: search}, all) {
			got[j.ID] = true
		}
		return got
	}
	check := func(search string, want, not []string) {
		t.Helper()
		got := ids(search)
		for _, id := range want {
			if !got[id] {
				t.Errorf("%q should show %s; got %v", search, id, got)
			}
		}
		for _, id := range not {
			if got[id] {
				t.Errorf("%q should not show %s; got %v", search, id, got)
			}
		}
	}
	check("Toronto, ON", []string{"toronto", "mississauga"}, []string{"austin", "london", "londonky"})
	check("London, UK", []string{"london"}, []string{"toronto", "londonky", "austin"})
	check("Canada", []string{"toronto", "mississauga", "vancouver"}, []string{"london", "austin"})
	check("Austin, TX", []string{"austin"}, []string{"toronto", "london"})
	check("London, KY", []string{"londonky"}, []string{"london", "toronto"})
}
