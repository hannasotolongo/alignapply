package jobs

import "testing"

func TestNormalizeJobOpportunitiesJobCorrectsCountryFromExplicitUSLocation(t *testing.T) {
	input := jobOpportunitiesJob{
		ID:          "diocesan-sales-miami",
		Title:       "Sales Representative",
		Company:     "Diocesan",
		Description: "Sales role",
		Location:    "Miami, FL",
		Country:     "GB",
		Remote:      "on_site",
		ApplyURL:    "https://example.com/apply",
		Status:      "active",
	}

	job := normalizeJobOpportunitiesJob(input)

	if job.Location != "Miami, FL" {
		t.Fatalf(
			"expected location %q, got %q",
			"Miami, FL",
			job.Location,
		)
	}

	if job.CountryCode != "US" {
		t.Fatalf(
			"expected country code %q for explicit US location, got %q",
			"US",
			job.CountryCode,
		)
	}
}

func TestNormalizeJobOpportunitiesJobPreservesValidCountryCode(t *testing.T) {
	input := jobOpportunitiesJob{
		ID:          "london-job",
		Title:       "Sales Representative",
		Company:     "Example Company",
		Description: "Sales role",
		Location:    "London, England",
		Country:     "GB",
		Remote:      "on_site",
		ApplyURL:    "https://example.com/apply",
		Status:      "active",
	}

	job := normalizeJobOpportunitiesJob(input)

	if job.CountryCode != "GB" {
		t.Fatalf(
			"expected valid country code %q to be preserved, got %q",
			"GB",
			job.CountryCode,
		)
	}
}

func TestNormalizeJobOpportunitiesJobDoesNotTreatRemoteAsLocation(t *testing.T) {
	input := jobOpportunitiesJob{
		ID:          "remote-test",
		Title:       "Software Engineer",
		Company:     "Example Company",
		Description: "Engineering role",
		City:        "Miami",
		Region:      "FL",
		Country:     "US",
		Remote:      "remote",
		ApplyURL:    "https://example.com/apply",
		Status:      "active",
	}

	job := normalizeJobOpportunitiesJob(input)

	if job.Location != "Miami, FL, US" {
		t.Fatalf(
			"expected geographic location %q, got %q",
			"Miami, FL, US",
			job.Location,
		)
	}

	if job.WorkArrangement != "remote" {
		t.Fatalf(
			"expected work arrangement %q, got %q",
			"remote",
			job.WorkArrangement,
		)
	}
}
