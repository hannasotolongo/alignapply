package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Free backup job sources. They're used when the main Job Opportunities API
// feed returns too few jobs or fails (for example, when its allowance runs
// out), so searches keep working.

var backupHTTPClient = &http.Client{Timeout: 12 * time.Second}

func getJSON(ctx context.Context, rawURL string, headers map[string]string, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := backupHTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(out)
}

func parseTimeLoose(value string) *time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04:05.0000000", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed
		}
	}
	return nil
}

func arrangementFromLocation(location string) string {
	lower := strings.ToLower(location)
	switch {
	case strings.Contains(lower, "hybrid"):
		return "hybrid"
	case strings.Contains(lower, "remote"):
		return "remote"
	}
	return ""
}

var foreignCountries = map[string]string{
	"canada": "CA", "united kingdom": "GB", "london": "GB", "england": "GB", "ireland": "IE", "dublin": "IE",
	"germany": "DE", "berlin": "DE", "france": "FR", "paris": "FR", "spain": "ES", "portugal": "PT", "lisbon": "PT",
	"netherlands": "NL", "amsterdam": "NL", "poland": "PL", "india": "IN", "bangalore": "IN", "bengaluru": "IN",
	"singapore": "SG", "australia": "AU", "sydney": "AU", "japan": "JP", "tokyo": "JP", "brazil": "BR", "mexico": "MX",
	"toronto": "CA", "vancouver, bc": "CA", "israel": "IL", "tel aviv": "IL", "philippines": "PH", "argentina": "AR",
	"colombia": "CO", "czech": "CZ", "prague": "CZ", "romania": "RO", "sweden": "SE", "switzerland": "CH", "italy": "IT",
	"emea": "EU", "europe": "EU", "apac": "APAC", "latam": "LATAM",
}

func countryFromLocation(location string) string {
	lower := strings.ToLower(location)
	explicitUS := strings.Contains(lower, "united states") || strings.Contains(lower, "usa") || strings.Contains(lower, "u.s.") || strings.Contains(lower, " us ") || strings.HasSuffix(lower, " us") || strings.Contains(lower, "us-")
	if explicitUS {
		// "Remote - US or Canada" is open to US candidates.
		return "US"
	}
	for name, code := range foreignCountries {
		if strings.Contains(lower, name) {
			return code
		}
	}
	if locationContainsUSRegion(location) {
		return "US"
	}
	return ""
}

// ---------------------------------------------------------------------------
// Greenhouse public job boards (free, no key, full descriptions)
// ---------------------------------------------------------------------------

// GreenhouseBoards are employers with verified public Greenhouse boards.
func GreenhouseBoards() []ATSEmployer {
	tokens := map[string]string{
		"onemedical": "One Medical", "oscar": "Oscar Health", "cloverhealth": "Clover Health", "zocdoc": "Zocdoc",
		"swordhealth": "Sword Health", "omadahealth": "Omada Health", "mavenclinic": "Maven Clinic", "flatironhealth": "Flatiron Health",
		"komodohealth": "Komodo Health", "talkspace": "Talkspace", "carrotfertility": "Carrot Fertility", "doximity": "Doximity",
		"sweetgreen": "sweetgreen", "airbnb": "Airbnb", "stripe": "Stripe", "figma": "Figma", "robinhood": "Robinhood",
		"coinbase": "Coinbase", "lyft": "Lyft", "pinterest": "Pinterest", "reddit": "Reddit", "dropbox": "Dropbox",
		"instacart": "Instacart", "gusto": "Gusto", "brex": "Brex", "twilio": "Twilio", "datadog": "Datadog",
		"mongodb": "MongoDB", "elastic": "Elastic", "squarespace": "Squarespace", "peloton": "Peloton", "mercury": "Mercury",
		"chime": "Chime", "sofi": "SoFi", "affirm": "Affirm", "toast": "Toast", "faire": "Faire", "duolingo": "Duolingo",
		"asana": "Asana", "discord": "Discord", "twitch": "Twitch", "roblox": "Roblox", "pagerduty": "PagerDuty",
		"okta": "Okta", "zscaler": "Zscaler", "cloudflare": "Cloudflare", "databricks": "Databricks", "gitlab": "GitLab",
		"samsara": "Samsara",
	}
	boards := make([]ATSEmployer, 0, len(tokens))
	for token, name := range tokens {
		boards = append(boards, ATSEmployer{Name: name, Provider: "greenhouse", Site: token})
	}
	return boards
}

type GreenhouseProvider struct {
	boards []ATSEmployer
	ttl    time.Duration

	mu    sync.Mutex
	cache map[string]greenhouseBoardCache
}

type greenhouseListing struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	AbsoluteURL string `json:"absolute_url"`
	UpdatedAt   string `json:"updated_at"`
	Published   string `json:"first_published"`
	Location    struct {
		Name string `json:"name"`
	} `json:"location"`
	Content string `json:"content"`
}

type greenhouseBoardCache struct {
	jobs    []greenhouseListing
	expires time.Time
}

func NewGreenhouseProvider(boards []ATSEmployer) *GreenhouseProvider {
	return &GreenhouseProvider{boards: boards, ttl: 6 * time.Hour, cache: make(map[string]greenhouseBoardCache)}
}

func (p *GreenhouseProvider) Name() string { return "greenhouse" }

// listing returns a board's job list (titles and locations only), cached so
// each board is fetched at most every few hours.
func (p *GreenhouseProvider) listing(ctx context.Context, token string) ([]greenhouseListing, error) {
	p.mu.Lock()
	if entry, ok := p.cache[token]; ok && time.Now().Before(entry.expires) {
		p.mu.Unlock()
		return entry.jobs, nil
	}
	p.mu.Unlock()

	var payload struct {
		Jobs []greenhouseListing `json:"jobs"`
	}
	if err := getJSON(ctx, "https://boards-api.greenhouse.io/v1/boards/"+url.PathEscape(token)+"/jobs", nil, &payload); err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.cache[token] = greenhouseBoardCache{jobs: payload.Jobs, expires: time.Now().Add(p.ttl)}
	p.mu.Unlock()
	return payload.Jobs, nil
}

func (p *GreenhouseProvider) Search(ctx context.Context, request SearchRequest) ([]Job, error) {
	type candidate struct {
		board   ATSEmployer
		listing greenhouseListing
	}
	var (
		mu         sync.Mutex
		candidates []candidate
		failures   int
		wg         sync.WaitGroup
		slots      = make(chan struct{}, 8)
	)
	for _, board := range p.boards {
		wg.Add(1)
		go func(board ATSEmployer) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			listings, err := p.listing(ctx, board.Site)
			if err != nil {
				mu.Lock()
				failures++
				mu.Unlock()
				return
			}
			for _, listing := range listings {
				if !jobTitleMatchesTargetRole(request.TargetRole, listing.Title) {
					continue
				}
				probe := Job{Location: listing.Location.Name, CountryCode: countryFromLocation(listing.Location.Name), WorkArrangement: arrangementFromLocation(listing.Location.Name)}
				if !matchesLocationPreference(request, probe) {
					continue
				}
				mu.Lock()
				candidates = append(candidates, candidate{board, listing})
				mu.Unlock()
			}
		}(board)
	}
	wg.Wait()

	if len(candidates) == 0 {
		if failures == len(p.boards) && failures > 0 {
			return nil, fmt.Errorf("all %d boards failed", failures)
		}
		return nil, nil
	}
	if len(candidates) > 25 {
		candidates = candidates[:25]
	}

	// Fetch full descriptions only for jobs that match the search.
	jobs := make([]Job, len(candidates))
	for i, item := range candidates {
		wg.Add(1)
		go func(i int, item candidate) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			var detail greenhouseListing
			endpoint := fmt.Sprintf("https://boards-api.greenhouse.io/v1/boards/%s/jobs/%d", url.PathEscape(item.board.Site), item.listing.ID)
			if err := getJSON(ctx, endpoint, nil, &detail); err != nil {
				return
			}
			posted := parseTimeLoose(item.listing.Published)
			if posted == nil {
				posted = parseTimeLoose(item.listing.UpdatedAt)
			}
			location := item.listing.Location.Name
			jobs[i] = Job{
				ID:              fmt.Sprintf("greenhouse-%s-%d", item.board.Site, item.listing.ID),
				Source:          "greenhouse",
				SourceJobID:     strconv.FormatInt(item.listing.ID, 10),
				Title:           strings.TrimSpace(item.listing.Title),
				Company:         item.board.Name,
				Description:     html.UnescapeString(detail.Content),
				Location:        location,
				CountryCode:     countryFromLocation(location),
				WorkArrangement: arrangementFromLocation(location),
				PostedAt:        posted,
				IsActive:        true,
				ApplyURL:        item.listing.AbsoluteURL,
				SourceURL:       item.listing.AbsoluteURL,
			}
		}(i, item)
	}
	wg.Wait()

	out := jobs[:0]
	for _, job := range jobs {
		if job.ID != "" && strings.TrimSpace(job.Description) != "" {
			out = append(out, job)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Remotive (free, remote jobs only)
// ---------------------------------------------------------------------------

type RemotiveProvider struct{}

func NewRemotiveProvider() *RemotiveProvider { return &RemotiveProvider{} }

func (p *RemotiveProvider) Name() string { return "remotive" }

func (p *RemotiveProvider) Search(ctx context.Context, request SearchRequest) ([]Job, error) {
	if !request.Remote || strings.TrimSpace(request.TargetRole) == "" {
		return nil, nil
	}
	var payload struct {
		Jobs []struct {
			ID          int64  `json:"id"`
			URL         string `json:"url"`
			Title       string `json:"title"`
			Company     string `json:"company_name"`
			JobType     string `json:"job_type"`
			Published   string `json:"publication_date"`
			Location    string `json:"candidate_required_location"`
			Description string `json:"description"`
		} `json:"jobs"`
	}
	endpoint := "https://remotive.com/api/remote-jobs?limit=50&search=" + url.QueryEscape(strings.TrimSpace(request.TargetRole))
	if err := getJSON(ctx, endpoint, nil, &payload); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(payload.Jobs))
	for _, item := range payload.Jobs {
		if item.URL == "" || item.Title == "" || item.Company == "" {
			continue
		}
		country := ""
		lower := strings.ToLower(item.Location)
		if strings.Contains(lower, "usa") || strings.Contains(lower, "united states") || strings.Contains(lower, "us only") {
			country = "US"
		}
		jobs = append(jobs, Job{
			ID:              fmt.Sprintf("remotive-%d", item.ID),
			Source:          "remotive",
			SourceJobID:     strconv.FormatInt(item.ID, 10),
			Title:           strings.TrimSpace(item.Title),
			Company:         strings.TrimSpace(item.Company),
			Description:     item.Description,
			Location:        "Remote · " + strings.TrimSpace(item.Location),
			CountryCode:     country,
			WorkArrangement: "remote",
			EmploymentType:  strings.ReplaceAll(item.JobType, "_", "-"),
			PostedAt:        parseTimeLoose(item.Published),
			IsActive:        true,
			ApplyURL:        item.URL,
			SourceURL:       item.URL,
		})
	}
	return jobs, nil
}

// ---------------------------------------------------------------------------
// USAJobs (free key: developer.usajobs.gov; strong for VA nursing roles)
// ---------------------------------------------------------------------------

type USAJobsProvider struct {
	apiKey string
	email  string
}

// NewUSAJobsProvider returns nil unless USAJOBS_API_KEY and USAJOBS_EMAIL
// are set.
func NewUSAJobsProvider() *USAJobsProvider {
	key := strings.TrimSpace(os.Getenv("USAJOBS_API_KEY"))
	email := strings.TrimSpace(os.Getenv("USAJOBS_EMAIL"))
	if key == "" || email == "" {
		return nil
	}
	return &USAJobsProvider{apiKey: key, email: email}
}

func (p *USAJobsProvider) Name() string { return "usajobs" }

func (p *USAJobsProvider) Search(ctx context.Context, request SearchRequest) ([]Job, error) {
	if p == nil || strings.TrimSpace(request.TargetRole) == "" {
		return nil, nil
	}
	query := url.Values{}
	query.Set("Keyword", strings.TrimSpace(request.TargetRole))
	query.Set("ResultsPerPage", "50")
	if location := strings.TrimSpace(request.Location); location != "" {
		query.Set("LocationName", location)
		query.Set("Radius", "50")
	}
	var payload struct {
		SearchResult struct {
			Items []struct {
				Descriptor struct {
					PositionID   string   `json:"PositionID"`
					Title        string   `json:"PositionTitle"`
					Organization string   `json:"OrganizationName"`
					Location     string   `json:"PositionLocationDisplay"`
					URI          string   `json:"PositionURI"`
					ApplyURI     []string `json:"ApplyURI"`
					Qualify      string   `json:"QualificationSummary"`
					Start        string   `json:"PublicationStartDate"`
					Close        string   `json:"ApplicationCloseDate"`
					Pay          []struct {
						Min      string `json:"MinimumRange"`
						Max      string `json:"MaximumRange"`
						Interval string `json:"RateIntervalCode"`
					} `json:"PositionRemuneration"`
					UserArea struct {
						Details struct {
							JobSummary      string   `json:"JobSummary"`
							MajorDuties     []string `json:"MajorDuties"`
							Education       string   `json:"Education"`
							Requirements    string   `json:"Requirements"`
							KeyRequirements []string `json:"KeyRequirements"`
						} `json:"Details"`
					} `json:"UserArea"`
				} `json:"MatchedObjectDescriptor"`
			} `json:"SearchResultItems"`
		} `json:"SearchResult"`
	}
	headers := map[string]string{"Host": "data.usajobs.gov", "User-Agent": p.email, "Authorization-Key": p.apiKey}
	if err := getJSON(ctx, "https://data.usajobs.gov/api/search?"+query.Encode(), headers, &payload); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(payload.SearchResult.Items))
	for _, item := range payload.SearchResult.Items {
		d := item.Descriptor
		apply := d.URI
		if len(d.ApplyURI) > 0 && d.ApplyURI[0] != "" {
			apply = d.ApplyURI[0]
		}
		var parts []string
		for _, part := range []string{d.UserArea.Details.JobSummary, strings.Join(d.UserArea.Details.MajorDuties, "\n"), "Qualifications:\n" + d.Qualify, d.UserArea.Details.Requirements, d.UserArea.Details.Education} {
			if strings.TrimSpace(part) != "" && strings.TrimSpace(part) != "Qualifications:" {
				parts = append(parts, part)
			}
		}
		if len(d.UserArea.Details.KeyRequirements) > 0 {
			parts = append(parts, "Requirements:\n- "+strings.Join(d.UserArea.Details.KeyRequirements, "\n- "))
		}
		job := Job{
			ID:          "usajobs-" + d.PositionID,
			Source:      "usajobs",
			SourceJobID: d.PositionID,
			Title:       strings.TrimSpace(d.Title),
			Company:     strings.TrimSpace(d.Organization),
			Description: strings.Join(parts, "\n\n"),
			Location:    d.Location,
			CountryCode: "US",
			PostedAt:    parseTimeLoose(d.Start),
			ExpiresAt:   parseTimeLoose(d.Close),
			IsActive:    true,
			ApplyURL:    apply,
			SourceURL:   d.URI,
		}
		if len(d.Pay) > 0 {
			if min, err := strconv.ParseFloat(d.Pay[0].Min, 64); err == nil {
				job.SalaryMin = &min
			}
			if max, err := strconv.ParseFloat(d.Pay[0].Max, 64); err == nil {
				job.SalaryMax = &max
			}
			job.SalaryCurrency = "USD"
			switch strings.ToUpper(d.Pay[0].Interval) {
			case "PH":
				job.SalaryPeriod = "hour"
			case "PA":
				job.SalaryPeriod = "year"
			}
		}
		if job.Title != "" && job.ApplyURL != "" {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

// ---------------------------------------------------------------------------
// Fallback: use backups only when the main feed comes up short
// ---------------------------------------------------------------------------

type FallbackProvider struct {
	primary Provider
	backups []Provider
	// lastResort sources have tight request allowances, so they're asked
	// only when the main feed and free backups together come up short.
	lastResort []Provider
	minimum    int
	timeout    time.Duration
}

// WithLastResort adds sources asked only when everything else returned
// fewer than the minimum number of jobs.
func (f *FallbackProvider) WithLastResort(providers ...Provider) *FallbackProvider {
	for _, provider := range providers {
		if provider != nil {
			f.lastResort = append(f.lastResort, provider)
		}
	}
	return f
}

func NewFallbackProvider(primary Provider, minimum int, backups ...Provider) *FallbackProvider {
	kept := make([]Provider, 0, len(backups))
	for _, backup := range backups {
		if backup != nil {
			kept = append(kept, backup)
		}
	}
	return &FallbackProvider{primary: primary, backups: kept, minimum: minimum, timeout: 14 * time.Second}
}

func (f *FallbackProvider) Name() string { return f.primary.Name() }

// NoFeed stands in for a main job feed when there isn't one: every search
// goes straight to the free sources.
type NoFeed struct{}

func (NoFeed) Name() string                                         { return "none" }
func (NoFeed) Search(context.Context, SearchRequest) ([]Job, error) { return nil, nil }

func (f *FallbackProvider) Search(ctx context.Context, request SearchRequest) ([]Job, error) {
	primaryJobs, primaryErr := f.primary.Search(ctx, request)
	if primaryErr == nil && len(primaryJobs) >= f.minimum {
		return primaryJobs, nil
	}
	if _, none := f.primary.(NoFeed); !none {
		log.Printf("[job-backups] main feed returned %d jobs (err=%v); querying %d backup sources", len(primaryJobs), primaryErr, len(f.backups))
	}

	backupCtx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	var (
		mu  sync.Mutex
		all = append([]Job(nil), primaryJobs...)
	)
	searchAll := func(providers []Provider) {
		var wg sync.WaitGroup
		for _, backup := range providers {
			wg.Add(1)
			go func(backup Provider) {
				defer wg.Done()
				jobs, err := backup.Search(backupCtx, request)
				log.Printf("[job-backups] %s returned %d jobs (err=%v)", backup.Name(), len(jobs), err)
				if err != nil {
					return
				}
				mu.Lock()
				all = append(all, jobs...)
				mu.Unlock()
			}(backup)
		}
		wg.Wait()
	}
	searchAll(f.backups)
	if len(f.lastResort) > 0 {
		// Count only jobs that survive the location, title and other
		// filters: remote and federal backups return plenty of jobs that
		// don't match the search and would otherwise hide a thin result.
		if usable := usableJobCount(request, all); usable < f.minimum {
			log.Printf("[job-backups] %d matching jobs so far (of %d); asking %d last-resort sources", usable, len(all), len(f.lastResort))
			searchAll(f.lastResort)
		}
	}

	if len(all) == 0 && primaryErr != nil {
		return nil, primaryErr
	}
	return all, nil
}

// usableJobCount is how many of these jobs would be shown for the request.
// Requirement reading is cached by posting, so the real pipeline run that
// follows doesn't repeat this work.
func usableJobCount(request SearchRequest, jobs []Job) int {
	return len(NewPipeline().Process(request, jobs))
}
