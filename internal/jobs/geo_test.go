package jobs

import "testing"

func TestGeoLocate(t *testing.T) {
	cases := []struct {
		text, hint string
		any        bool
		state      string
		ok         bool
	}{
		{"Largo, Florida", "", false, "FL", true},
		{"St. Petersburg, FL", "", false, "FL", true},
		{"Keystone Heights, FL", "", false, "FL", true},
		{"32656", "", true, "FL", true},
		{"ADVENTHEALTH ORLANDO", "FL", false, "FL", true},
		{"FL032 Gainesville - Clinic", "", false, "FL", true},
		{"OH - Cleveland", "", false, "OH", true},
		{"US - NY - New York City", "", false, "NY", true},
		{"1045 5th St Unit 201, Miami Beach,FL 33139", "", false, "", true},
		{"Gainesville", "", true, "FL", true},
		{"Remote", "", true, "", false},
		{"3 Locations", "FL", false, "", false},
		{"Cleveland Clinic Main Campus", "FL", false, "", false},
		{"United Hospital - Hastings Regina Campus", "FL", false, "", false},
		{"Boston", "NY", false, "", false},
		{"HM Clear Lake - Medical Office Building 1", "FL", false, "", false},
		{"05066 - Lake Hallie Dialysis", "FL", false, "", false},
	}
	for _, c := range cases {
		_, state, ok := geoLocate(c.text, c.hint, c.any)
		if ok != c.ok || (c.state != "" && state != c.state) {
			t.Errorf("geoLocate(%q) = %s %v, want %s %v", c.text, state, ok, c.state, c.ok)
		}
	}
}

func TestJobDistance(t *testing.T) {
	near := []struct{ search, job string }{
		{"Tampa, FL", "Largo, Florida"},
		{"Keystone Heights, FL", "Gainesville, FL"},
		{"Miami, FL", "Coral Gables, FL"},
		{"33101", "Fort Lauderdale, FL"},
		{"Naperville, IL", "Chicago, Illinois"},
	}
	for _, c := range near {
		miles, _, ok := jobDistance(SearchRequest{Location: c.search}, Job{Location: c.job})
		if !ok || miles > defaultRadiusMiles {
			t.Errorf("%s -> %s: %.0f miles ok=%v, want within %d", c.search, c.job, miles, ok, defaultRadiusMiles)
		}
	}
	miles, _, ok := jobDistance(SearchRequest{Location: "Gainesville, FL"}, Job{Location: "Gainesville, GA"})
	if !ok || miles < 200 {
		t.Errorf("Gainesville FL -> GA: %.0f ok=%v", miles, ok)
	}
}

func TestPipelineFindsNearbyTownsAndWidens(t *testing.T) {
	pipeline := NewPipeline()
	// A small town: the dentist job 25 miles away in Gainesville counts.
	result := pipeline.Process(
		SearchRequest{TargetRole: "Dentist", Location: "Keystone Heights, FL"},
		[]Job{
			testPipelineJob("gnv", "Dentist", "Aspen Dental", "Gainesville, FL"),
			testPipelineJob("ga", "Dentist", "Aspen Dental", "Gainesville, GA"),
		},
	)
	if len(result) != 1 || result[0].ID != "gnv" || result[0].DistanceMiles == nil {
		t.Fatalf("Keystone Heights: %#v", result)
	}
	// Nothing near Miami: the Orlando job in the same state is shown, the
	// Chicago one is not.
	result = pipeline.Process(
		SearchRequest{TargetRole: "Dentist", Location: "Miami, FL"},
		[]Job{
			testPipelineJob("orl", "Dentist", "Aspen Dental", "Orlando, FL"),
			testPipelineJob("chi", "Dentist", "Aspen Dental", "Chicago, IL"),
		},
	)
	if len(result) != 1 || result[0].ID != "orl" {
		t.Fatalf("Miami widening: %#v", result)
	}
	// A whole-state search.
	result = pipeline.Process(
		SearchRequest{TargetRole: "Dentist", Location: "Florida"},
		[]Job{
			testPipelineJob("orl", "Dentist", "Aspen Dental", "Orlando, FL"),
			testPipelineJob("chi", "Dentist", "Aspen Dental", "Chicago, IL"),
		},
	)
	if len(result) != 1 || result[0].ID != "orl" {
		t.Fatalf("Florida: %#v", result)
	}
}
