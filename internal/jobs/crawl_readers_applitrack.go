package jobs

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Frontline Applicant Tracking (AppliTrack): the hiring system thousands of
// US school districts use. Each district has a public list of every opening,
// with full descriptions, at
//
//	https://www.applitrack.com/<district>/onlineapp/jobpostings/Output.asp?all=1
//
// and each opening has its own page on the district's site, which is where
// the apply link goes. robots.txt only blocks the admin pages.
//
// The board slug is "<district>|<City>|<ST>", for example
// "aasd|Appleton|WI": the district's AppliTrack id plus where it is, found
// once by the discovery tool, because the job list itself rarely names the
// town in a way a search can use.
// ---------------------------------------------------------------------------

func init() {
	boardReaders["applitrack"] = readAppliTrackBoard
}

var (
	appliTrackSlugRE    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,60}$`)
	appliTrackWriteRE   = regexp.MustCompile(`'\);\s*document\.write\('`)
	appliTrackPostingRE = regexp.MustCompile(`(?i)<ul class=["']?postingsList["']? id=["']?p(\d+)_["']?>`)
	appliTrackTitleRE   = regexp.MustCompile(`(?is)id=["']?wrapword["']?[^>]*>(.*?)</td>`)
	appliTrackFieldRE   = regexp.MustCompile(`(?is)<span class=["']?label["']?>\s*([^<]{1,60}?)\s*:?\s*(?:&nbsp;)?\s*</span>(.*?)</li>`)
	appliTrackDescRE    = regexp.MustCompile(`(?is)id=["']?DescriptionText\d+_["']?[^>]*>`)
	appliTrackDescEndRE = regexp.MustCompile(`(?is)<a [^>]*>\s*<span[^>]*>\s*Email To A Friend|<a [^>]*>\s*<span[^>]*>\s*Print Version`)
	appliTrackTagRE     = regexp.MustCompile(`(?s)<[^>]*>`)
	appliTrackAddressRE = regexp.MustCompile(`([A-Z][A-Za-z.' ]{1,28}[a-z]),\s*([A-Z]{2})\.?\s+(\d{5})`)
	appliTrackK12RE     = regexp.MustCompile(`(?i)\.k12\.([a-z]{2})\.us\b`)
	appliTrackTitleTag  = regexp.MustCompile(`(?is)<title>\s*(.*?)\s*</title>`)
)

// appliTrackBoardParts splits "aasd|Appleton|WI".
func appliTrackBoardParts(slug string) (district, city, state string) {
	parts := strings.Split(slug, "|")
	district = strings.TrimSpace(parts[0])
	if len(parts) >= 3 {
		city, state = strings.TrimSpace(parts[1]), strings.ToUpper(strings.TrimSpace(parts[2]))
	}
	return district, city, state
}

// AppliTrackSlug builds the board slug the reader expects.
func AppliTrackSlug(district, city, state string) string {
	return district + "|" + city + "|" + strings.ToUpper(state)
}

func appliTrackOrigin(district string) string {
	return "https://www.applitrack.com/" + url.PathEscape(district) + "/onlineapp"
}

// appliTrackPostingURL is the opening's own page on the district's site.
func appliTrackPostingURL(district, jobID string) string {
	return appliTrackOrigin(district) + "/default.aspx?AppliTrackJobId=" + url.QueryEscape(jobID) +
		"&AppliTrackLayoutMode=detail&AppliTrackViewPosting=1"
}

func readAppliTrackBoard(ctx context.Context, client *crawlClient, board Board, known map[string]bool) (crawlResult, error) {
	district, city, state := appliTrackBoardParts(board.Slug)
	if !appliTrackSlugRE.MatchString(district) {
		return crawlResult{}, fmt.Errorf("bad AppliTrack district id %q", district)
	}
	if city == "" || state == "" {
		return crawlResult{}, errors.New("AppliTrack board has no city and state")
	}
	page, err := client.getText(ctx, appliTrackOrigin(district)+"/jobpostings/Output.asp?all=1")
	if err != nil {
		return crawlResult{}, err
	}
	location, _ := usLocation(city, state, "US")
	result := crawlResult{Complete: true}
	for _, posting := range parseAppliTrackPostings(page) {
		id := crawledJobID(board, posting.ID)
		result.SeenIDs = append(result.SeenIDs, id)
		if known[id] {
			continue
		}
		description := posting.Description
		if posting.Building != "" {
			description = "Location: " + posting.Building + "\n\n" + description
		}
		link := appliTrackPostingURL(district, posting.ID)
		result.Jobs = append(result.Jobs, Job{
			ID:          id,
			SourceJobID: posting.ID,
			Title:       posting.Title,
			Company:     firstNonEmpty(posting.District, board.Company),
			Description: description,
			Location:    location,
			CountryCode: "US",
			PostedAt:    posting.Posted,
			ExpiresAt:   posting.Closes,
			ApplyURL:    link,
			SourceURL:   link,
		})
	}
	return result, nil
}

type appliTrackPosting struct {
	ID          string
	Title       string
	Building    string
	District    string
	Description string
	Posted      *time.Time
	Closes      *time.Time
}

// parseAppliTrackPostings reads the openings out of Output.asp, which is
// the job list written as JavaScript document.write calls.
func parseAppliTrackPostings(page string) []appliTrackPosting {
	page = appliTrackUnscript(page)
	starts := appliTrackPostingRE.FindAllStringSubmatchIndex(page, -1)
	seen := map[string]bool{}
	var postings []appliTrackPosting
	for i, start := range starts {
		end := len(page)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		chunk := page[start[1]:end]
		if cut := strings.Index(strings.ToLower(chunk), "</ul>"); cut >= 0 {
			chunk = chunk[:cut]
		}
		id := page[start[2]:start[3]]
		if seen[id] {
			continue
		}
		posting := appliTrackPosting{ID: id}
		if match := appliTrackTitleRE.FindStringSubmatch(chunk); match != nil {
			posting.Title = appliTrackText(match[1])
		}
		if posting.Title == "" {
			continue
		}
		head := chunk
		descStart := appliTrackDescRE.FindStringIndex(chunk)
		if descStart != nil {
			head = chunk[:descStart[0]]
			desc := chunk[descStart[1]:]
			if stop := appliTrackDescEndRE.FindStringIndex(desc); stop != nil {
				desc = desc[:stop[0]]
			}
			posting.Description = desc
		}
		for _, field := range appliTrackFieldRE.FindAllStringSubmatch(head, -1) {
			label := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(appliTrackText(field[1]), ":")))
			value := appliTrackText(field[2])
			switch label {
			case "location":
				posting.Building = value
			case "district":
				posting.District = strings.TrimSpace(strings.TrimSuffix(value, "- website"))
			case "date posted":
				posting.Posted = parseUSDate(value)
			case "closing date":
				posting.Closes = parseUSDate(value)
			}
		}
		seen[id] = true
		postings = append(postings, posting)
	}
	return postings
}

// appliTrackUnscript turns the document.write script back into HTML.
func appliTrackUnscript(page string) string {
	page = appliTrackWriteRE.ReplaceAllString(page, "")
	return strings.NewReplacer(`\'`, `'`, `\"`, `"`, `\/`, `/`, `\\`, `\`).Replace(page)
}

func appliTrackText(fragment string) string {
	text := appliTrackTagRE.ReplaceAllString(fragment, " ")
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, " ", " ")
	return strings.Join(strings.Fields(text), " ")
}

func parseUSDate(value string) *time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"1/2/2006", "01/02/2006", "1/2/06", "January 2, 2006"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed
		}
	}
	return nil
}

// AppliTrackDistrict describes one district's job list, for discovery.
type AppliTrackDistrict struct {
	ID    string
	Name  string
	City  string
	State string
	Jobs  int
}

// CheckAppliTrackDistrict reads a district's AppliTrack pages and works out
// its name, town and state, and how many openings it lists. City and State
// are empty when the pages don't say where the district is.
func CheckAppliTrackDistrict(ctx context.Context, district string) (AppliTrackDistrict, error) {
	found := AppliTrackDistrict{ID: district}
	if !appliTrackSlugRE.MatchString(district) {
		return found, fmt.Errorf("bad district id %q", district)
	}
	client := newCrawlClient()
	home, err := client.getText(ctx, appliTrackOrigin(district)+"/default.aspx")
	if err != nil {
		return found, err
	}
	if match := appliTrackTitleTag.FindStringSubmatch(home); match != nil {
		name := appliTrackText(match[1])
		name = strings.TrimSpace(strings.TrimSuffix(name, "- Frontline Recruitment"))
		name = strings.TrimSpace(strings.TrimSuffix(name, "- Frontline Education"))
		name = strings.TrimSpace(strings.TrimPrefix(name, "Current Openings at"))
		if name != "" && !strings.EqualFold(name, "Frontline Recruitment") {
			found.Name = name
		}
	}
	list, err := client.getText(ctx, appliTrackOrigin(district)+"/jobpostings/Output.asp?all=1")
	if err != nil {
		return found, err
	}
	postings := parseAppliTrackPostings(list)
	found.Jobs = len(postings)
	if found.Name == "" {
		for _, posting := range postings {
			if posting.District != "" {
				found.Name = posting.District
				break
			}
		}
	}
	found.City, found.State = appliTrackPlace(home+"\n"+appliTrackUnscript(list), found.Name)
	return found, nil
}

// appliTrackPlace finds the district's town from addresses on its pages
// ("Appleton, WI 54911"), or from its name and its k12.XX.us email domain.
func appliTrackPlace(page, name string) (city, state string) {
	text := appliTrackText(page)
	counts := map[string]int{}
	for _, match := range appliTrackAddressRE.FindAllStringSubmatch(text, -1) {
		place := strings.TrimSpace(match[1])
		// Drop words before the town ("Street Appleton" -> try shorter
		// tails until the town is a known place in that state).
		words := strings.Fields(place)
		for start := 0; start < len(words) && start < 4; start++ {
			candidate := strings.Join(words[start:], " ")
			if isUSPlace(candidate, match[2]) {
				counts[candidate+"|"+match[2]]++
				break
			}
		}
	}
	if len(counts) > 0 {
		type pair struct {
			key   string
			count int
		}
		var pairs []pair
		for key, count := range counts {
			pairs = append(pairs, pair{key, count})
		}
		sort.Slice(pairs, func(i, j int) bool {
			if pairs[i].count != pairs[j].count {
				return pairs[i].count > pairs[j].count
			}
			return pairs[i].key < pairs[j].key
		})
		city, state, _ = strings.Cut(pairs[0].key, "|")
		return city, state
	}
	// No address: a k12.XX.us email gives the state, and the district's
	// name usually names its town.
	if match := appliTrackK12RE.FindStringSubmatch(page); match != nil && name != "" {
		hint := strings.ToUpper(match[1])
		words := strings.Fields(name)
		for size := min(3, len(words)); size >= 1; size-- {
			for start := 0; start+size <= len(words); start++ {
				candidate := strings.Join(words[start:start+size], " ")
				if len(candidate) < 3 {
					continue
				}
				if isUSPlace(candidate, hint) {
					return candidate, hint
				}
			}
		}
	}
	return "", ""
}

// isUSPlace reports whether name is exactly a town or city in state.
func isUSPlace(name, state string) bool {
	loadGeo()
	_, ok := geoByState[geoNormalize(name)+"|"+strings.ToUpper(state)]
	return ok
}

// getText fetches an HTML page (robots.txt and pacing apply, like getJSON).
func (c *crawlClient) getText(ctx context.Context, rawURL string) (string, error) {
	target, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	ok, err := c.allowed(ctx, target)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errDisallowed
	}
	if err := c.wait(ctx, target.Host); err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", CrawlerUserAgent)
	request.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	response, err := c.http.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d from %s", response.StatusCode, target.Host)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	return string(body), err
}
