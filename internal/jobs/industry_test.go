package jobs

import (
	"regexp"
	"strings"
	"testing"
)

func TestCryptoSearch(t *testing.T) {
	cryptoCompanies["examplecryptoexchange"] = struct{}{}
	defer delete(cryptoCompanies, "examplecryptoexchange")

	job := func(id, title, company string) Job {
		return Job{ID: id, Title: title, Company: company, Location: "New York, NY", CountryCode: "US", IsActive: true,
			ApplyURL: "https://x.example/" + id, Description: "Work."}
	}
	all := []Job{
		job("swe", "Software Engineer", "Example Crypto Exchange"),
		job("ae", "Account Executive", "Example Crypto Exchange"),
		job("bank", "Blockchain Engineer", "Big Bank"),
		job("other", "Software Engineer", "Plain Software Co"),
	}
	run := func(search string) map[string]bool {
		got := map[string]bool{}
		for _, j := range NewPipeline().Process(SearchRequest{TargetRole: search, Location: "New York, NY"}, all) {
			got[j.ID] = true
		}
		return got
	}
	for search, want := range map[string][]string{
		"crypto":                 {"swe", "ae", "bank"},
		"crypto jobs":            {"swe", "ae", "bank"},
		"web3 software engineer": {"swe"},
		"blockchain":             {"swe", "ae", "bank"},
		"software engineer":      {"swe", "other"},
	} {
		got := run(search)
		if len(got) != len(want) {
			t.Errorf("%q: got %v, want %v", search, got, want)
			continue
		}
		for _, id := range want {
			if !got[id] {
				t.Errorf("%q: missing %s (got %v)", search, id, got)
			}
		}
	}

	// The saved-jobs query finds crypto employers' jobs by their tag.
	pattern := regexp.MustCompile(strings.Replace(TitleSearchPattern("crypto jobs"), `\m`, `\b`, 1))
	if !pattern.MatchString(TitleSearchText("Software Engineer", "Example Crypto Exchange")) {
		t.Fatal("crypto employer jobs should match the crypto search")
	}
	if pattern.MatchString(TitleSearchText("Software Engineer", "Plain Software Co")) {
		t.Fatal("other employers should not")
	}
}
