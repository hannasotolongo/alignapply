// Command discoverapplitrack finds school districts (and other employers)
// that post jobs on Frontline Applicant Tracking (applitrack.com), checks
// each one's public job list, and writes the ones with open jobs and a known
// town as crawler boards (the format of internal/jobs/seed_boards.json).
//
// District ids come from the Common Crawl URL index (a public archive of
// web addresses) plus any ids passed in -extra. Only public job lists are
// read, at most a few requests per district, paced by the crawler client.
//
//	go run ./cmd/discoverapplitrack -out jane_found_applitrack.json
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

type found struct {
	jobs.Board
	Name  string `json:"name"`
	City  string `json:"city"`
	State string `json:"state"`
	Jobs  int    `json:"jobs"`
}

var (
	httpClient = &http.Client{Timeout: 90 * time.Second}
	slugRE     = regexp.MustCompile(`(?i)applitrack\.com/([A-Za-z0-9_-]{1,60})/onlineapp`)
)

func main() {
	out := flag.String("out", "jane_found_applitrack.json", "write found boards here")
	extra := flag.String("extra", "", "file with more district ids, one per line")
	crawls := flag.Int("crawls", 4, "how many recent Common Crawl indexes to search")
	workers := flag.Int("workers", 3, "districts checked at once")
	limit := flag.Int("limit", 0, "check at most this many districts (0 = all)")
	jobSpot := flag.Bool("jobspot", true, "also learn district ids from K12JobSpot job pages (ids only; Jane never links there)")
	flag.Parse()

	ids := map[string]bool{}
	for _, id := range commonCrawlIDs(*crawls) {
		ids[id] = true
	}
	log.Printf("Common Crawl: %d district ids", len(ids))
	if *jobSpot {
		for _, id := range jobSpotIDs() {
			ids[id] = true
		}
		log.Printf("with K12JobSpot: %d district ids", len(ids))
	}
	if *extra != "" {
		if file, err := os.Open(*extra); err == nil {
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				if id := strings.ToLower(strings.TrimSpace(scanner.Text())); id != "" && !strings.HasPrefix(id, "#") {
					ids[id] = true
				}
			}
			file.Close()
		}
	}
	var list []string
	for id := range ids {
		list = append(list, id)
	}
	sort.Strings(list)
	if *limit > 0 && len(list) > *limit {
		list = list[:*limit]
	}
	log.Printf("checking %d districts", len(list))

	var (
		mu      sync.Mutex
		results []found
		noPlace int
		noJobs  int
		failed  int
		done    int
	)
	queue := make(chan string)
	var wg sync.WaitGroup
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range queue {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				district, err := jobs.CheckAppliTrackDistrict(ctx, id)
				cancel()
				mu.Lock()
				done++
				switch {
				case err != nil:
					failed++
				case district.Jobs == 0:
					noJobs++
				case district.City == "" || district.State == "":
					noPlace++
					log.Printf("no town found: %s (%s, %d jobs)", id, district.Name, district.Jobs)
				default:
					name := district.Name
					if name == "" {
						name = id
					}
					results = append(results, found{
						Board: jobs.Board{ATS: "applitrack", Slug: jobs.AppliTrackSlug(id, district.City, district.State), Company: name},
						Name:  name, City: district.City, State: district.State, Jobs: district.Jobs,
					})
				}
				if done%50 == 0 {
					log.Printf("%d/%d checked: %d kept, %d no jobs, %d no town, %d failed", done, len(list), len(results), noJobs, noPlace, failed)
					save(*out, results)
				}
				mu.Unlock()
			}
		}()
	}
	for _, id := range list {
		queue <- id
	}
	close(queue)
	wg.Wait()
	save(*out, results)
	total := 0
	states := map[string]int{}
	for _, r := range results {
		total += r.Jobs
		states[r.State]++
	}
	log.Printf("DONE: %d districts kept with %d open jobs across %d states (%d no jobs, %d no town, %d failed)", len(results), total, len(states), noJobs, noPlace, failed)
}

func save(path string, results []found) {
	sort.Slice(results, func(i, j int) bool { return results[i].Slug < results[j].Slug })
	data, _ := json.MarshalIndent(results, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("write %s: %v", path, err)
	}
}

// commonCrawlIDs lists AppliTrack district ids seen in recent crawls.
func commonCrawlIDs(crawls int) []string {
	var info []struct {
		ID     string `json:"id"`
		CDXAPI string `json:"cdx-api"`
	}
	if err := getJSON("https://index.commoncrawl.org/collinfo.json", &info); err != nil {
		log.Printf("Common Crawl list failed: %v", err)
		return nil
	}
	ids := map[string]bool{}
	for i, crawl := range info {
		if i >= crawls {
			break
		}
		for _, pattern := range []string{"www.applitrack.com/*", "applitrack.com/*"} {
			base := crawl.CDXAPI + "?url=" + url.QueryEscape(pattern) + "&output=json&fl=url"
			var pages struct {
				Pages int `json:"pages"`
			}
			if err := getJSON(base+"&showNumPages=true", &pages); err != nil {
				log.Printf("%s %s: %v", crawl.ID, pattern, err)
				continue
			}
			for page := 0; page < pages.Pages; page++ {
				body, err := get(base + fmt.Sprintf("&page=%d", page))
				if err != nil {
					log.Printf("%s page %d: %v", crawl.ID, page, err)
					continue
				}
				for _, match := range slugRE.FindAllStringSubmatch(body, -1) {
					ids[strings.ToLower(match[1])] = true
				}
			}
			log.Printf("%s %s: %d pages, %d ids so far", crawl.ID, pattern, pages.Pages, len(ids))
		}
	}
	var list []string
	for id := range ids {
		list = append(list, id)
	}
	return list
}

// jobSpotIDs reads K12JobSpot's public sitemap of job pages (its
// robots.txt allows this) and collects the AppliTrack district id each job
// page names. It stops when many pages in a row add nothing new.
func jobSpotIDs() []string {
	index, err := get("https://www.k12jobspot.com/sitemap")
	if err != nil {
		log.Printf("K12JobSpot sitemap: %v", err)
		return nil
	}
	locRE := regexp.MustCompile(`<loc>([^<]+)</loc>`)
	var pages []string
	for _, match := range locRE.FindAllStringSubmatch(index, -1) {
		if strings.Contains(match[1], "/sitemap/job/") {
			list, err := get(match[1])
			if err != nil {
				log.Printf("%s: %v", match[1], err)
				continue
			}
			for _, page := range locRE.FindAllStringSubmatch(list, -1) {
				pages = append(pages, page[1])
			}
		}
	}
	log.Printf("K12JobSpot: %d job pages to read", len(pages))
	idRE := regexp.MustCompile(`(?i)applitrack\.com\\?/([A-Za-z0-9_-]{1,60})(?:\\?/|["'\s])`)
	ids := map[string]bool{}
	sinceNew := 0
	for i, page := range pages {
		time.Sleep(700 * time.Millisecond)
		body, err := get(page)
		if err != nil {
			continue
		}
		added := false
		for _, match := range idRE.FindAllStringSubmatch(body, -1) {
			id := strings.ToLower(match[1])
			if strings.HasPrefix(id, "dev") || id == "olauserdocs" {
				continue
			}
			if !ids[id] {
				ids[id] = true
				added = true
			}
		}
		if added {
			sinceNew = 0
		} else {
			sinceNew++
		}
		if (i+1)%200 == 0 {
			log.Printf("K12JobSpot: %d/%d pages read, %d district ids", i+1, len(pages), len(ids))
		}
		if sinceNew >= 1500 {
			log.Printf("K12JobSpot: no new districts in 1500 pages, stopping")
			break
		}
	}
	var list []string
	for id := range ids {
		list = append(list, id)
	}
	return list
}

func get(rawURL string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
		request, _ := http.NewRequest(http.MethodGet, rawURL, nil)
		request.Header.Set("User-Agent", jobs.CrawlerUserAgent)
		response, err := httpClient.Do(request)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, 256<<20))
		response.Body.Close()
		if response.StatusCode == http.StatusNotFound {
			return "", nil // no captures for this pattern in this crawl
		}
		if response.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d", response.StatusCode)
			continue
		}
		return string(body), err
	}
	return "", lastErr
}

func getJSON(rawURL string, out any) error {
	body, err := get(rawURL)
	if err != nil {
		return err
	}
	if body == "" {
		return nil
	}
	return json.Unmarshal([]byte(body), out)
}
