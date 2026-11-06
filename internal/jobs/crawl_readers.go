package jobs

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Readers for each hiring system. Each lists every open job on a board and
// returns them as Jane jobs with full descriptions.

// ---------------------------------------------------------------------------
// Greenhouse: one request returns every job with its description.
// ---------------------------------------------------------------------------

func readGreenhouseBoard(ctx context.Context, client *crawlClient, board Board, _ map[string]bool) (crawlResult, error) {
	var payload struct {
		Jobs []struct {
			ID          int64  `json:"id"`
			Title       string `json:"title"`
			AbsoluteURL string `json:"absolute_url"`
			UpdatedAt   string `json:"updated_at"`
			Published   string `json:"first_published"`
			Location    struct {
				Name string `json:"name"`
			} `json:"location"`
			Content string `json:"content"`
		} `json:"jobs"`
	}
	endpoint := "https://boards-api.greenhouse.io/v1/boards/" + url.PathEscape(board.Slug) + "/jobs?content=true"
	if err := client.getJSON(ctx, endpoint, &payload); err != nil {
		return crawlResult{}, err
	}

	result := crawlResult{Complete: true}
	for _, item := range payload.Jobs {
		if item.ID == 0 || strings.TrimSpace(item.Title) == "" || item.AbsoluteURL == "" {
			continue
		}
		id := crawledJobID(board, strconv.FormatInt(item.ID, 10))
		posted := parseTimeLoose(item.Published)
		if posted == nil {
			posted = parseTimeLoose(item.UpdatedAt)
		}
		result.SeenIDs = append(result.SeenIDs, id)
		result.Jobs = append(result.Jobs, Job{
			ID:          id,
			SourceJobID: strconv.FormatInt(item.ID, 10),
			Title:       strings.TrimSpace(item.Title),
			Company:     board.Company,
			Description: html.UnescapeString(item.Content),
			Location:    strings.TrimSpace(item.Location.Name),
			PostedAt:    posted,
			ApplyURL:    item.AbsoluteURL,
			SourceURL:   item.AbsoluteURL,
		})
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Lever: pages of 100 postings with descriptions.
// ---------------------------------------------------------------------------

func readLeverBoard(ctx context.Context, client *crawlClient, board Board, _ map[string]bool) (crawlResult, error) {
	result := crawlResult{Complete: true}
	employer := ATSEmployer{Name: board.Company, Provider: "lever", Site: board.Slug}
	const pageSize, maxPages = 100, 20
	for page := 0; page < maxPages; page++ {
		var postings []leverPosting
		endpoint := fmt.Sprintf("%s/%s?mode=json&limit=%d&skip=%d", leverBaseURL, url.PathEscape(board.Slug), pageSize, page*pageSize)
		if err := client.getJSON(ctx, endpoint, &postings); err != nil {
			if page == 0 {
				return crawlResult{}, err
			}
			result.Complete = false
			break
		}
		for _, posting := range postings {
			job := normalizeLeverPosting(employer, posting)
			if !validJob(job) {
				continue
			}
			job.ID = crawledJobID(board, job.SourceJobID)
			result.SeenIDs = append(result.SeenIDs, job.ID)
			result.Jobs = append(result.Jobs, job)
		}
		if len(postings) < pageSize {
			return result, nil
		}
	}
	result.Complete = false // stopped at the page cap
	return result, nil
}

// ---------------------------------------------------------------------------
// Ashby: one request returns every listed job.
// ---------------------------------------------------------------------------

func readAshbyBoard(ctx context.Context, client *crawlClient, board Board, _ map[string]bool) (crawlResult, error) {
	var payload struct {
		Jobs []struct {
			ID               string `json:"id"`
			Title            string `json:"title"`
			Location         string `json:"location"`
			EmploymentType   string `json:"employmentType"`
			IsRemote         bool   `json:"isRemote"`
			WorkplaceType    string `json:"workplaceType"`
			IsListed         *bool  `json:"isListed"`
			PublishedAt      string `json:"publishedAt"`
			DescriptionHTML  string `json:"descriptionHtml"`
			DescriptionPlain string `json:"descriptionPlain"`
			JobURL           string `json:"jobUrl"`
			ApplyURL         string `json:"applyUrl"`
			Address          struct {
				PostalAddress struct {
					Country  string `json:"addressCountry"`
					Region   string `json:"addressRegion"`
					Locality string `json:"addressLocality"`
				} `json:"postalAddress"`
			} `json:"address"`
		} `json:"jobs"`
	}
	endpoint := "https://api.ashbyhq.com/posting-api/job-board/" + url.PathEscape(board.Slug) + "?includeCompensation=true"
	if err := client.getJSON(ctx, endpoint, &payload); err != nil {
		return crawlResult{}, err
	}

	result := crawlResult{Complete: true}
	for _, item := range payload.Jobs {
		if item.IsListed != nil && !*item.IsListed {
			continue
		}
		link := firstNonEmpty(item.ApplyURL, item.JobURL)
		if item.ID == "" || strings.TrimSpace(item.Title) == "" || link == "" {
			continue
		}
		description := item.DescriptionHTML
		if strings.TrimSpace(description) == "" {
			description = item.DescriptionPlain
		}
		location := strings.TrimSpace(item.Location)
		if address := item.Address.PostalAddress; address.Locality != "" && address.Region != "" {
			location = address.Locality + ", " + address.Region
		}
		arrangement := ""
		switch strings.ToLower(item.WorkplaceType) {
		case "remote":
			arrangement = "remote"
		case "hybrid":
			arrangement = "hybrid"
		case "onsite", "on-site":
			arrangement = "onsite"
		}
		if item.IsRemote && arrangement == "" {
			arrangement = "remote"
		}
		country := ""
		switch strings.ToLower(strings.TrimSpace(item.Address.PostalAddress.Country)) {
		case "united states", "usa", "us", "united states of america":
			country = "US"
		case "":
		default:
			country = countryFromLocation(item.Address.PostalAddress.Country)
			if country == "" {
				country = "XX" // somewhere outside the US
			}
		}
		id := crawledJobID(board, item.ID)
		result.SeenIDs = append(result.SeenIDs, id)
		result.Jobs = append(result.Jobs, Job{
			ID:              id,
			SourceJobID:     item.ID,
			Title:           strings.TrimSpace(item.Title),
			Company:         board.Company,
			Description:     description,
			Location:        location,
			CountryCode:     country,
			WorkArrangement: arrangement,
			EmploymentType:  strings.ToLower(item.EmploymentType),
			PostedAt:        parseTimeLoose(item.PublishedAt),
			ApplyURL:        link,
			SourceURL:       firstNonEmpty(item.JobURL, link),
		})
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Workday: the career site's own job list (20 per page), then each job's
// details. Big employers list thousands of jobs, so only jobs Jane hasn't
// saved yet are downloaded, a capped number per run; the rest follow on
// later runs.
// ---------------------------------------------------------------------------

const (
	workdayPageSize            = 20
	workdayMaxListed           = 3000
	workdayMaxNewPerRunDefault = 200 // ~2 minutes per board; unfinished boards come back soon
)

// workdayParts splits "host/tenant/site".
func workdayParts(slug string) (host, tenant, site string, ok bool) {
	parts := strings.Split(strings.Trim(slug, "/"), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func readWorkdayBoard(ctx context.Context, client *crawlClient, board Board, known map[string]bool) (crawlResult, error) {
	host, tenant, site, ok := workdayParts(board.Slug)
	if !ok {
		return crawlResult{}, fmt.Errorf("bad Workday board %q", board.Slug)
	}
	base := "https://" + host + "/wday/cxs/" + url.PathEscape(tenant) + "/" + url.PathEscape(site)

	type listing struct {
		Title         string `json:"title"`
		ExternalPath  string `json:"externalPath"`
		LocationsText string `json:"locationsText"`
		PostedOn      string `json:"postedOn"`
	}
	var listings []listing
	total := -1
	complete := true
	for offset := 0; offset < workdayMaxListed; offset += workdayPageSize {
		var page struct {
			Total       int       `json:"total"`
			JobPostings []listing `json:"jobPostings"`
		}
		body := map[string]any{"appliedFacets": map[string]any{}, "limit": workdayPageSize, "offset": offset, "searchText": ""}
		if err := client.postJSON(ctx, base+"/jobs", body, &page); err != nil {
			if offset == 0 {
				return crawlResult{}, err
			}
			complete = false
			break
		}
		if total < 0 {
			total = page.Total // later pages sometimes report 0
		}
		listings = append(listings, page.JobPostings...)
		if len(page.JobPostings) < workdayPageSize || (total > 0 && offset+workdayPageSize >= total) {
			break
		}
		if offset+workdayPageSize >= workdayMaxListed {
			complete = false
		}
	}

	result := crawlResult{Complete: complete}
	newFetched := 0
	for _, item := range listings {
		path := strings.TrimSpace(item.ExternalPath)
		if path == "" || strings.TrimSpace(item.Title) == "" {
			continue
		}
		id := crawledJobID(board, path)
		result.SeenIDs = append(result.SeenIDs, id)
		if known[id] {
			continue // already saved; it stays open because it's listed
		}
		if newFetched >= workdayMaxNewPerRun {
			result.Complete = false // the rest are saved on later runs
			continue
		}
		newFetched++

		job, err := fetchWorkdayJob(ctx, client, board, base, host, site, path, item.Title, item.LocationsText)
		if err != nil {
			result.Complete = false
			continue
		}
		job.ID = id
		result.Jobs = append(result.Jobs, job)
	}
	return result, nil
}

// fetchWorkdayJob reads one Workday posting's details (description, exact
// location, apply link) and returns it as a Jane job.
func fetchWorkdayJob(ctx context.Context, client *crawlClient, board Board, base, host, site, path, listTitle, listLocation string) (Job, error) {
	var detail struct {
		JobPostingInfo struct {
			Title               string   `json:"title"`
			JobDescription      string   `json:"jobDescription"`
			Location            string   `json:"location"`
			AdditionalLocations []string `json:"additionalLocations"`
			TimeType            string   `json:"timeType"`
			StartDate           string   `json:"startDate"`
			PostedOn            string   `json:"postedOn"`
			ExternalURL         string   `json:"externalUrl"`
			JobReqID            string   `json:"jobReqId"`
			Country             struct {
				Descriptor string `json:"descriptor"`
			} `json:"country"`
		} `json:"jobPostingInfo"`
	}
	if err := client.getJSON(ctx, base+path, &detail); err != nil {
		return Job{}, err
	}
	info := detail.JobPostingInfo
	link := firstNonEmpty(info.ExternalURL, "https://"+host+"/"+site+path)
	location := workdayLocation(firstNonEmpty(info.Location, listLocation))
	country := ""
	switch strings.ToLower(info.Country.Descriptor) {
	case "united states of america", "united states", "usa":
		country = "US"
	case "":
	default:
		country = countryFromLocation(info.Country.Descriptor)
		if country == "" {
			country = "XX"
		}
	}
	return Job{
		ID:             crawledJobID(board, path),
		SourceJobID:    firstNonEmpty(info.JobReqID, path),
		Title:          strings.TrimSpace(firstNonEmpty(info.Title, listTitle)),
		Company:        board.Company,
		Description:    info.JobDescription,
		Location:       strings.TrimSpace(location),
		CountryCode:    country,
		EmploymentType: strings.ToLower(info.TimeType),
		PostedAt:       parseTimeLoose(info.StartDate),
		ApplyURL:       link,
		SourceURL:      link,
	}, nil
}

// workdayMaxNewPerRun caps how many new Workday jobs one run downloads.
var workdayMaxNewPerRun = workdayMaxNewPerRunDefault

var workdayCityStateRE = regexp.MustCompile(`([A-Za-z][A-Za-z .'-]*?)\s*,\s*([A-Z]{2})(?:\s+\d{5}(?:-\d{4})?)?\s*$`)

// workdayLocation turns a store address such as
// "305 Carolina Green Pkwy, Southern Pines,NC 28387" into "Southern Pines, NC"
// so location matching works. Other formats are kept as they are.
func workdayLocation(value string) string {
	value = strings.TrimSpace(value)
	match := workdayCityStateRE.FindStringSubmatch(value)
	if match == nil || !strings.ContainsAny(value, "0123456789") {
		return value
	}
	city := strings.TrimSpace(match[1])
	if i := strings.LastIndex(city, ","); i >= 0 {
		city = strings.TrimSpace(city[i+1:])
	}
	return city + ", " + match[2]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
