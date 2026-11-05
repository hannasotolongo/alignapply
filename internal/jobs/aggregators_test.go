package jobs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJoobleParsesJobs(t *testing.T) {
	var body map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/secret") {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"totalCount":2,"jobs":[
			{"id":-4821,"title":"Teacher","location":"Seattle, WA","snippet":"&nbsp;Teach <b>5th grade</b> math...","salary":"$58,000 - $72,000 per year","type":"Full-time","link":"https://jooble.org/desc/-4821","company":"Seattle Public Schools","updated":"2026-11-02T00:00:00.0000000"},
			{"id":77,"title":"Substitute Teacher","location":"Seattle, WA","snippet":"","salary":"$22 - $28 per hour","type":"Part-time","link":"https://jooble.org/desc/77","company":"Kelly Education","updated":""}
		]}`))
	}))
	defer server.Close()

	p := &JoobleProvider{apiKey: "secret", baseURL: server.URL + "/"}
	jobs, err := p.Search(context.Background(), SearchRequest{TargetRole: "Teacher", Location: "Seattle, WA"})
	if err != nil {
		t.Fatal(err)
	}
	if body["keywords"] != "Teacher" || body["location"] != "Seattle, WA" {
		t.Fatalf("unexpected body %v", body)
	}
	if len(jobs) != 2 || jobs[0].ID != "jooble--4821" || jobs[0].Description != "Teach 5th grade math..." {
		t.Fatalf("unexpected jobs %+v", jobs)
	}
	if *jobs[0].SalaryMin != 58000 || *jobs[0].SalaryMax != 72000 || jobs[0].SalaryPeriod != "year" {
		t.Fatalf("salary %+v", jobs[0])
	}
	if jobs[1].SalaryPeriod != "hour" || *jobs[1].SalaryMin != 22 {
		t.Fatalf("hourly salary %+v", jobs[1])
	}
}

func TestAggregatorsOffWithoutKeys(t *testing.T) {
	t.Setenv("JOOBLE_API_KEY", "")
	if NewJoobleProvider() != nil {
		t.Fatal("providers must be off until keys are set")
	}
}

func TestAggregatorJobsPassPipeline(t *testing.T) {
	low, high := 55000.0, 65000.0
	in := []Job{
		{ID: "jooble-1", Source: SourceJooble, Title: "Store Manager", Company: "Target", Location: "Denver, CO", CountryCode: "US", IsActive: true, ApplyURL: "https://jooble.org/desc/1", Description: "Lead a team and drive sales.", SalaryMin: &low, SalaryMax: &high, SalaryPeriod: "year"},
		{ID: "jooble-2", Source: SourceJooble, Title: "Retail Store Manager", Company: "Ulta", Location: "Aurora, CO", IsActive: true, ApplyURL: "https://jooble.org/desc/2", Description: "Manage store operations."},
		{ID: "jooble-3", Source: SourceJooble, Title: "Store Manager", Company: "Far Away", Location: "Miami, FL", IsActive: true, ApplyURL: "https://jooble.org/desc/3", Description: "x"},
	}
	out := NewPipeline().Process(SearchRequest{TargetRole: "Store Manager", Location: "Denver, CO", Remote: true, Hybrid: true, Onsite: true}, in)
	ids := map[string]bool{}
	for _, job := range out {
		ids[job.ID] = true
	}
	if !ids["jooble-1"] || !ids["jooble-2"] || ids["jooble-3"] {
		t.Fatalf("expected Denver-area jobs only, got %v", ids)
	}
}

func TestJoobleStopsAtBudget(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(`{"jobs":[]}`))
	}))
	defer server.Close()

	p := (&JoobleProvider{apiKey: "k", baseURL: server.URL + "/"}).WithBudget(NewMemoryBudget(1, time.Hour))
	if _, err := p.Search(context.Background(), SearchRequest{TargetRole: "Teacher"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Search(context.Background(), SearchRequest{TargetRole: "Nurse"}); err != ErrBudgetExhausted {
		t.Fatalf("expected budget error, got %v", err)
	}
	if hits != 1 {
		t.Fatalf("expected 1 request to Jooble, got %d", hits)
	}
}

type fixedProvider struct {
	name  string
	count int
	calls *int
}

func (f fixedProvider) Name() string { return f.name }
func (f fixedProvider) Search(context.Context, SearchRequest) ([]Job, error) {
	*f.calls++
	out := make([]Job, f.count)
	for i := range out {
		out[i] = Job{ID: f.name + string(rune('a'+i))}
	}
	return out, nil
}

func TestLastResortCountsOnlyMatchingJobs(t *testing.T) {
	var calls, joobleCalls int
	// 20 jobs come back, but none are the role searched for.
	offTopic := searchableProvider{"remotive", 20, "Backend Engineer", &calls}
	jooble := searchableProvider{"jooble", 2, "Teacher", &joobleCalls}
	NewFallbackProvider(fixedProvider{"main", 0, &calls}, 15, offTopic).WithLastResort(jooble).
		Search(context.Background(), SearchRequest{TargetRole: "Teacher", Location: "Seattle, WA", Remote: true, Hybrid: true, Onsite: true})
	if joobleCalls != 1 {
		t.Fatal("Jooble should be asked when the other jobs don't match the search")
	}
}

type searchableProvider struct {
	name  string
	count int
	title string
	calls *int
}

func (f searchableProvider) Name() string { return f.name }
func (f searchableProvider) Search(context.Context, SearchRequest) ([]Job, error) {
	*f.calls++
	out := make([]Job, f.count)
	for i := range out {
		id := f.name + "-" + string(rune('a'+i))
		out[i] = Job{ID: id, Title: f.title, Company: "Co " + id, Location: "Seattle, WA", CountryCode: "US", IsActive: true, ApplyURL: "https://example.com/" + id}
	}
	return out, nil
}

func TestLastResortOnlyWhenThin(t *testing.T) {
	var primaryCalls, backupCalls, joobleCalls int
	jooble := fixedProvider{"jooble", 5, &joobleCalls}

	plenty := NewFallbackProvider(fixedProvider{"main", 0, &primaryCalls}, 15, searchableProvider{"greenhouse", 20, "Data Analyst", &backupCalls}).WithLastResort(jooble)
	plenty.Search(context.Background(), SearchRequest{TargetRole: "Data Analyst", Location: "Seattle, WA", Remote: true, Hybrid: true, Onsite: true})
	if joobleCalls != 0 {
		t.Fatal("Jooble should not be asked when free sources found enough jobs")
	}

	thin := NewFallbackProvider(fixedProvider{"main", 0, &primaryCalls}, 15, fixedProvider{"greenhouse", 3, &backupCalls}).WithLastResort(jooble, nil)
	jobs, _ := thin.Search(context.Background(), SearchRequest{TargetRole: "Teacher"})
	if joobleCalls != 1 || len(jobs) != 8 {
		t.Fatalf("expected Jooble to fill in: calls=%d jobs=%d", joobleCalls, len(jobs))
	}
}
