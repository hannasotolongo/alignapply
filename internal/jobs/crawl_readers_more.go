package jobs

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
)

// More hiring systems with public job lists. Many hospitals, dental
// groups, restaurants, retailers and small employers use these instead of
// Greenhouse, Lever, Ashby or Workday. Every apply link goes to the
// employer's own posting.

func init() {
	boardReaders["smartrecruiters"] = readSmartRecruitersBoard
	boardReaders["workable"] = readWorkableBoard
	boardReaders["recruitee"] = readRecruiteeBoard
	boardReaders["bamboohr"] = readBambooHRBoard
}

// usLocation joins a city and state ("Ocala, FL") and reports the country
// code when it's known.
func usLocation(city, region, country string) (string, string) {
	city, region = strings.TrimSpace(city), strings.TrimSpace(region)
	location := city
	if region != "" {
		if location != "" {
			location += ", "
		}
		location += region
	}
	code := ""
	switch strings.ToLower(strings.TrimSpace(country)) {
	case "us", "usa", "united states", "united states of america":
		code = "US"
	case "":
	default:
		if len(country) == 2 {
			code = strings.ToUpper(country)
		} else if code = countryFromLocation(country); code == "" {
			code = "XX"
		}
	}
	return location, code
}

// ---------------------------------------------------------------------------
// SmartRecruiters: pages of 100 postings, then each new posting's details.
// ---------------------------------------------------------------------------

const smartRecruitersMaxNewPerRun = 200

func readSmartRecruitersBoard(ctx context.Context, client *crawlClient, board Board, known map[string]bool) (crawlResult, error) {
	type listing struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Released string `json:"releasedDate"`
		Location struct {
			City    string `json:"city"`
			Region  string `json:"region"`
			Country string `json:"country"`
			Remote  bool   `json:"remote"`
			Hybrid  bool   `json:"hybrid"`
		} `json:"location"`
		TypeOfEmployment struct {
			Label string `json:"label"`
		} `json:"typeOfEmployment"`
	}
	base := "https://api.smartrecruiters.com/v1/companies/" + url.PathEscape(board.Slug) + "/postings"
	result := crawlResult{Complete: true}
	newFetched := 0
	for offset := 0; offset < 5000; offset += 100 {
		var page struct {
			TotalFound int       `json:"totalFound"`
			Content    []listing `json:"content"`
		}
		if err := client.getJSON(ctx, fmt.Sprintf("%s?limit=100&offset=%d", base, offset), &page); err != nil {
			if offset == 0 {
				return crawlResult{}, err
			}
			result.Complete = false
			break
		}
		for _, item := range page.Content {
			if item.ID == "" || strings.TrimSpace(item.Name) == "" {
				continue
			}
			id := crawledJobID(board, item.ID)
			result.SeenIDs = append(result.SeenIDs, id)
			if known[id] {
				continue
			}
			if newFetched >= smartRecruitersMaxNewPerRun {
				result.Complete = false
				continue
			}
			newFetched++
			var detail struct {
				PostingURL string `json:"postingUrl"`
				ApplyURL   string `json:"applyUrl"`
				JobAd      struct {
					Sections map[string]struct {
						Title string `json:"title"`
						Text  string `json:"text"`
					} `json:"sections"`
				} `json:"jobAd"`
			}
			if err := client.getJSON(ctx, base+"/"+url.PathEscape(item.ID), &detail); err != nil {
				result.Complete = false
				continue
			}
			var description strings.Builder
			for _, key := range []string{"jobDescription", "qualifications", "additionalInformation", "companyDescription"} {
				if section, ok := detail.JobAd.Sections[key]; ok && strings.TrimSpace(section.Text) != "" {
					if section.Title != "" {
						description.WriteString("<h3>" + html.EscapeString(section.Title) + "</h3>")
					}
					description.WriteString(section.Text)
				}
			}
			location, country := usLocation(item.Location.City, item.Location.Region, item.Location.Country)
			arrangement := ""
			if item.Location.Remote {
				arrangement = "remote"
			} else if item.Location.Hybrid {
				arrangement = "hybrid"
			}
			link := firstNonEmpty(detail.PostingURL, "https://jobs.smartrecruiters.com/"+url.PathEscape(board.Slug)+"/"+url.PathEscape(item.ID))
			result.Jobs = append(result.Jobs, Job{
				ID:              id,
				SourceJobID:     item.ID,
				Title:           strings.TrimSpace(item.Name),
				Company:         board.Company,
				Description:     description.String(),
				Location:        location,
				CountryCode:     country,
				WorkArrangement: arrangement,
				EmploymentType:  strings.ToLower(item.TypeOfEmployment.Label),
				PostedAt:        parseTimeLoose(item.Released),
				ApplyURL:        firstNonEmpty(detail.ApplyURL, link),
				SourceURL:       link,
			})
		}
		if offset+100 >= page.TotalFound || len(page.Content) == 0 {
			return result, nil
		}
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Workable: one request returns every job with its description.
// ---------------------------------------------------------------------------

func readWorkableBoard(ctx context.Context, client *crawlClient, board Board, _ map[string]bool) (crawlResult, error) {
	var payload struct {
		Jobs []struct {
			Title          string `json:"title"`
			Shortcode      string `json:"shortcode"`
			EmploymentType string `json:"employment_type"`
			Telecommuting  bool   `json:"telecommuting"`
			URL            string `json:"url"`
			ApplicationURL string `json:"application_url"`
			PublishedOn    string `json:"published_on"`
			CreatedAt      string `json:"created_at"`
			Country        string `json:"country"`
			City           string `json:"city"`
			State          string `json:"state"`
			Description    string `json:"description"`
			Locations      []struct {
				Country     string `json:"country"`
				CountryCode string `json:"countryCode"`
				City        string `json:"city"`
				Region      string `json:"region"`
			} `json:"locations"`
		} `json:"jobs"`
	}
	endpoint := "https://apply.workable.com/api/v1/widget/accounts/" + url.PathEscape(board.Slug) + "?details=true"
	if err := client.getJSON(ctx, endpoint, &payload); err != nil {
		return crawlResult{}, err
	}
	result := crawlResult{Complete: true}
	for _, item := range payload.Jobs {
		link := firstNonEmpty(item.URL, item.ApplicationURL)
		if item.Shortcode == "" || strings.TrimSpace(item.Title) == "" || link == "" {
			continue
		}
		city, region, country := item.City, item.State, item.Country
		if len(item.Locations) > 0 {
			first := item.Locations[0]
			city, region, country = first.City, first.Region, firstNonEmpty(first.CountryCode, first.Country)
		}
		location, code := usLocation(city, region, country)
		arrangement := ""
		if item.Telecommuting {
			arrangement = "remote"
		}
		posted := parseTimeLoose(item.PublishedOn)
		if posted == nil {
			posted = parseTimeLoose(item.CreatedAt)
		}
		id := crawledJobID(board, item.Shortcode)
		result.SeenIDs = append(result.SeenIDs, id)
		result.Jobs = append(result.Jobs, Job{
			ID:              id,
			SourceJobID:     item.Shortcode,
			Title:           strings.TrimSpace(item.Title),
			Company:         board.Company,
			Description:     item.Description,
			Location:        location,
			CountryCode:     code,
			WorkArrangement: arrangement,
			EmploymentType:  strings.ToLower(item.EmploymentType),
			PostedAt:        posted,
			ApplyURL:        firstNonEmpty(item.ApplicationURL, link),
			SourceURL:       link,
		})
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Recruitee: one request returns every offer with its description.
// ---------------------------------------------------------------------------

func readRecruiteeBoard(ctx context.Context, client *crawlClient, board Board, _ map[string]bool) (crawlResult, error) {
	var payload struct {
		Offers []struct {
			ID              int64  `json:"id"`
			Title           string `json:"title"`
			Description     string `json:"description"`
			Requirements    string `json:"requirements"`
			City            string `json:"city"`
			StateCode       string `json:"state_code"`
			StateName       string `json:"state_name"`
			CountryCode     string `json:"country_code"`
			Remote          bool   `json:"remote"`
			Hybrid          bool   `json:"hybrid"`
			CareersURL      string `json:"careers_url"`
			CareersApplyURL string `json:"careers_apply_url"`
			PublishedAt     string `json:"published_at"`
			EmploymentType  string `json:"employment_type_code"`
			Status          string `json:"status"`
		} `json:"offers"`
	}
	endpoint := "https://" + url.PathEscape(board.Slug) + ".recruitee.com/api/offers/"
	if err := client.getJSON(ctx, endpoint, &payload); err != nil {
		return crawlResult{}, err
	}
	result := crawlResult{Complete: true}
	for _, item := range payload.Offers {
		if item.ID == 0 || strings.TrimSpace(item.Title) == "" || item.CareersURL == "" {
			continue
		}
		if item.Status != "" && item.Status != "published" {
			continue
		}
		location, code := usLocation(item.City, firstNonEmpty(item.StateCode, item.StateName), item.CountryCode)
		arrangement := ""
		if item.Remote {
			arrangement = "remote"
		} else if item.Hybrid {
			arrangement = "hybrid"
		}
		description := item.Description
		if strings.TrimSpace(item.Requirements) != "" {
			description += "<h3>Requirements</h3>" + item.Requirements
		}
		sourceID := strconv.FormatInt(item.ID, 10)
		id := crawledJobID(board, sourceID)
		result.SeenIDs = append(result.SeenIDs, id)
		result.Jobs = append(result.Jobs, Job{
			ID:              id,
			SourceJobID:     sourceID,
			Title:           strings.TrimSpace(item.Title),
			Company:         board.Company,
			Description:     description,
			Location:        location,
			CountryCode:     code,
			WorkArrangement: arrangement,
			EmploymentType:  strings.ReplaceAll(strings.ToLower(item.EmploymentType), "_", " "),
			PostedAt:        parseTimeLoose(item.PublishedAt),
			ApplyURL:        firstNonEmpty(item.CareersApplyURL, item.CareersURL),
			SourceURL:       item.CareersURL,
		})
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// BambooHR: the career page's job list, then each new job's details.
// ---------------------------------------------------------------------------

const bambooMaxNewPerRun = 150

func readBambooHRBoard(ctx context.Context, client *crawlClient, board Board, known map[string]bool) (crawlResult, error) {
	origin := "https://" + url.PathEscape(board.Slug) + ".bamboohr.com"
	var list struct {
		Result []struct {
			ID                    any    `json:"id"`
			JobOpeningName        string `json:"jobOpeningName"`
			EmploymentStatusLabel string `json:"employmentStatusLabel"`
			IsRemote              any    `json:"isRemote"`
			Location              struct {
				City  string `json:"city"`
				State string `json:"state"`
			} `json:"location"`
			ATSLocation struct {
				Country string `json:"country"`
				State   string `json:"state"`
				City    string `json:"city"`
			} `json:"atsLocation"`
		} `json:"result"`
	}
	if err := client.getJSON(ctx, origin+"/careers/list", &list); err != nil {
		return crawlResult{}, err
	}
	result := crawlResult{Complete: true}
	newFetched := 0
	for _, item := range list.Result {
		jobID := strings.TrimSpace(fmt.Sprint(item.ID))
		if jobID == "" || jobID == "<nil>" || strings.TrimSpace(item.JobOpeningName) == "" {
			continue
		}
		id := crawledJobID(board, jobID)
		result.SeenIDs = append(result.SeenIDs, id)
		if known[id] {
			continue
		}
		if newFetched >= bambooMaxNewPerRun {
			result.Complete = false
			continue
		}
		newFetched++
		var detail struct {
			Result struct {
				JobOpening struct {
					Description        string `json:"description"`
					DatePosted         string `json:"datePosted"`
					JobOpeningShareURL string `json:"jobOpeningShareUrl"`
					LocationType       any    `json:"locationType"`
				} `json:"jobOpening"`
			} `json:"result"`
		}
		if err := client.getJSON(ctx, origin+"/careers/"+url.PathEscape(jobID)+"/detail", &detail); err != nil {
			result.Complete = false
			continue
		}
		city := firstNonEmpty(item.Location.City, item.ATSLocation.City)
		state := firstNonEmpty(item.Location.State, item.ATSLocation.State)
		country := item.ATSLocation.Country
		if country == "" && len(usStatesIn(state)) > 0 {
			country = "US"
		}
		location, code := usLocation(city, state, country)
		arrangement := ""
		if remote := strings.ToLower(fmt.Sprint(item.IsRemote)); remote == "true" || remote == "1" {
			arrangement = "remote"
		}
		link := firstNonEmpty(detail.Result.JobOpening.JobOpeningShareURL, origin+"/careers/"+url.PathEscape(jobID))
		result.Jobs = append(result.Jobs, Job{
			ID:              id,
			SourceJobID:     jobID,
			Title:           strings.TrimSpace(item.JobOpeningName),
			Company:         board.Company,
			Description:     detail.Result.JobOpening.Description,
			Location:        location,
			CountryCode:     code,
			WorkArrangement: arrangement,
			EmploymentType:  strings.ToLower(item.EmploymentStatusLabel),
			PostedAt:        parseTimeLoose(detail.Result.JobOpening.DatePosted),
			ApplyURL:        link,
			SourceURL:       link,
		})
	}
	return result, nil
}

// CheckBoard reads one board the way the crawler does and returns what it
// found, for the dev check tools.
func CheckBoard(ctx context.Context, board Board) ([]Job, bool, error) {
	reader, ok := boardReaders[board.ATS]
	if !ok {
		return nil, false, fmt.Errorf("no reader for %q", board.ATS)
	}
	result, err := reader(ctx, newCrawlClient(), board, map[string]bool{})
	for i := range result.Jobs {
		finishCrawledJob(&result.Jobs[i], board)
	}
	return result.Jobs, result.Complete, err
}

// CheckBoardListOnly reads a board's list without downloading any job's
// details, for the dev check tools.
func CheckBoardListOnly(ctx context.Context, board Board) ([]Job, error) {
	saved := workdayMaxNewPerRun
	workdayMaxNewPerRun = 0
	defer func() { workdayMaxNewPerRun = saved }()
	jobs, _, err := CheckBoard(ctx, board)
	return jobs, err
}
