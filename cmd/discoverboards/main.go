// Command discoverboards finds employers' public job boards for Jane's crawler.
//
// For each employer name in candidates.txt it tries likely board ids on
// Workday, Greenhouse, Lever and Ashby, keeps only boards that answer with
// open jobs, and writes them as JSON (the format of
// internal/jobs/seed_boards.json). It reads only robots.txt files and public
// job-board endpoints, a few requests per employer.
//
//	go run ./cmd/discoverboards -in cmd/discoverboards/candidates.txt -out found.json
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
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
	Jobs int `json:"jobs"`
}

var (
	client      = &http.Client{Timeout: 12 * time.Second}
	workdayWDs  = []string{"wd1", "wd5", "wd3", "wd12", "wd10", "wd2", "wd103", "wd108", "wd101", "wd105", "wd501", "wd503"}
	sitemapRE   = regexp.MustCompile(`(?i)Sitemap:\s*https://([^/\s]+)/([^/\s]+)/siteMap\.xml`)
	nonAlnum    = regexp.MustCompile(`[^a-z0-9]+`)
	suffixWords = regexp.MustCompile(`\b(inc|corp|corporation|company|co|group|the|llc|ltd|holdings)\b`)
)

func main() {
	in := flag.String("in", "cmd/discoverboards/candidates.txt", "employer list")
	out := flag.String("out", "", "write found boards here (default stdout)")
	workers := flag.Int("workers", 8, "employers checked at once")
	flag.Parse()

	type candidate struct {
		name  string
		slugs []string
	}
	file, err := os.Open(*in)
	if err != nil {
		log.Fatal(err)
	}
	var candidates []candidate
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, override, hasOverride := strings.Cut(line, "|")
		name = strings.TrimSpace(name)
		var slugs []string
		if hasOverride {
			for _, slug := range strings.Split(override, ",") {
				if slug = strings.TrimSpace(strings.ToLower(slug)); slug != "" {
					slugs = append(slugs, slug)
				}
			}
		}
		slugs = append(slugs, guessSlugs(name)...)
		candidates = append(candidates, candidate{name, unique(slugs)})
	}
	file.Close()

	var (
		mu      sync.Mutex
		results []found
		wg      sync.WaitGroup
		slots   = make(chan struct{}, *workers)
	)
	for i, c := range candidates {
		wg.Add(1)
		go func(i int, c candidate) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			boards := discover(c.name, c.slugs)
			mu.Lock()
			results = append(results, boards...)
			mu.Unlock()
			summary := "-"
			if len(boards) > 0 {
				parts := make([]string, len(boards))
				for j, b := range boards {
					parts[j] = fmt.Sprintf("%s %s (%d)", b.ATS, b.Slug, b.Jobs)
				}
				summary = strings.Join(parts, "; ")
			}
			log.Printf("[%d/%d] %s: %s", i+1, len(candidates), c.name, summary)
		}(i, c)
	}
	wg.Wait()

	// A board with only a few jobs found under a guessed id is usually a
	// different, smaller company with a similar name; leave those out.
	kept := results[:0]
	for _, r := range results {
		if r.Jobs >= 5 {
			kept = append(kept, r)
		}
	}
	results = kept

	sort.Slice(results, func(i, j int) bool {
		if results[i].ATS != results[j].ATS {
			return results[i].ATS < results[j].ATS
		}
		return results[i].Slug < results[j].Slug
	})
	encoded, _ := json.MarshalIndent(results, "", "  ")
	if *out == "" {
		os.Stdout.Write(append(encoded, '\n'))
	} else if err := os.WriteFile(*out, append(encoded, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("found %d boards for %d employers", len(results), len(candidates))
}

func guessSlugs(name string) []string {
	lower := strings.ToLower(strings.ReplaceAll(name, "&", " and "))
	joined := nonAlnum.ReplaceAllString(lower, "")
	trimmed := nonAlnum.ReplaceAllString(suffixWords.ReplaceAllString(lower, " "), "")
	return []string{joined, trimmed}
}

func unique(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// discover tries every slug on every hiring system and returns the boards
// that have open jobs.
func discover(company string, slugs []string) []found {
	var boards []found
	for _, slug := range slugs {
		if n := greenhouseJobs(slug); n > 0 {
			boards = append(boards, found{jobs.Board{ATS: "greenhouse", Slug: slug, Company: company}, n})
			break
		}
	}
	for _, slug := range slugs {
		if n := leverJobs(slug); n > 0 {
			boards = append(boards, found{jobs.Board{ATS: "lever", Slug: slug, Company: company}, n})
			break
		}
	}
	for _, slug := range slugs {
		if n := ashbyJobs(slug); n > 0 {
			boards = append(boards, found{jobs.Board{ATS: "ashby", Slug: slug, Company: company}, n})
			break
		}
	}
	for _, slug := range slugs {
		if board, ok := workdayBoard(company, slug); ok {
			boards = append(boards, board)
			break
		}
	}
	return boards
}

func get(url string) (*http.Response, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", jobs.CrawlerUserAgent)
	// robots.txt is plain text: Workday rejects it (HTTP 422) when the
	// request says it only accepts JSON.
	if !strings.HasSuffix(url, "/robots.txt") {
		request.Header.Set("Accept", "application/json")
	}
	return client.Do(request)
}

func readJSON(url string, out any) bool {
	response, err := get(url)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	return json.NewDecoder(io.LimitReader(response.Body, 64<<20)).Decode(out) == nil
}

func greenhouseJobs(slug string) int {
	var payload struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	if !readJSON("https://boards-api.greenhouse.io/v1/boards/"+slug+"/jobs", &payload) {
		return 0
	}
	return len(payload.Jobs)
}

func leverJobs(slug string) int {
	var postings []json.RawMessage
	if !readJSON("https://api.lever.co/v0/postings/"+slug+"?mode=json&limit=100", &postings) {
		return 0
	}
	return len(postings)
}

func ashbyJobs(slug string) int {
	var payload struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	if !readJSON("https://api.ashbyhq.com/posting-api/job-board/"+slug, &payload) {
		return 0
	}
	return len(payload.Jobs)
}

// workdayBoard finds a tenant's public career site from its robots.txt
// sitemap lines, then confirms it lists jobs.
func workdayBoard(company, tenant string) (found, bool) {
	for _, wd := range workdayWDs {
		host := tenant + "." + wd + ".myworkdayjobs.com"
		response, err := get("https://" + host + "/robots.txt")
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			continue
		}
		if bytes.Contains(bytes.ToLower(body), []byte("disallow: /wday")) {
			continue // the site asks crawlers not to read its job data
		}
		var best found
		for _, match := range sitemapRE.FindAllStringSubmatch(string(body), 5) {
			if !strings.EqualFold(match[1], host) {
				continue
			}
			site := match[2]
			if total := workdayTotal(host, tenant, site); total > best.Jobs {
				best = found{jobs.Board{ATS: "workday", Slug: host + "/" + tenant + "/" + site, Company: company}, total}
			}
		}
		if best.Jobs > 0 {
			return best, true
		}
	}
	return found{}, false
}

func workdayTotal(host, tenant, site string) int {
	body, _ := json.Marshal(map[string]any{"appliedFacets": map[string]any{}, "limit": 1, "offset": 0, "searchText": ""})
	request, err := http.NewRequest(http.MethodPost, "https://"+host+"/wday/cxs/"+tenant+"/"+site+"/jobs", bytes.NewReader(body))
	if err != nil {
		return 0
	}
	request.Header.Set("User-Agent", jobs.CrawlerUserAgent)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return 0
	}
	defer response.Body.Close()
	var payload struct {
		Total int `json:"total"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&payload) != nil {
		return 0
	}
	return payload.Total
}
