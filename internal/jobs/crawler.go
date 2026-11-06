package jobs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Jane's own job crawler. It reads employers' own job boards (the hiring
// systems they post to: Greenhouse, Lever, Ashby, Workday), saves the jobs
// in the database, and keeps them current, so job search doesn't depend on
// a paid job feed. It only reads public job listings, follows each site's
// robots.txt, waits between requests to the same site, and identifies
// itself with a user agent that links back to Jane Careers.

// CrawlerUserAgent identifies Jane's crawler to the sites it reads.
const CrawlerUserAgent = "JaneCareersBot/1.0 (+https://janecareers.com/contact)"

// Board is one employer job board.
type Board struct {
	ATS     string `json:"ats"`  // greenhouse, lever, ashby, workday
	Slug    string `json:"slug"` // board id; for Workday "host/tenant/site"
	Company string `json:"company"`
}

func (b Board) key() string { return b.ATS + "/" + b.Slug }

// BoardStore is where boards and crawled jobs live (the database).
type BoardStore interface {
	SeedBoards(ctx context.Context, boards []Board) error
	DueBoards(ctx context.Context, limit int, olderThan time.Duration) ([]Board, error)
	KnownJobIDs(ctx context.Context, board Board) (map[string]bool, error)
	// SaveBoardJobs saves new or changed jobs, marks every job in seenIDs as
	// still open, and when complete is true closes the board's other jobs.
	SaveBoardJobs(ctx context.Context, board Board, jobs []Job, seenIDs []string, complete bool) error
	RecordCrawl(ctx context.Context, board Board, status string, jobCount int, failed bool) error
	SearchJobs(ctx context.Context, titlePattern, nearPattern string, limit int) ([]Job, error)
	PruneClosed(ctx context.Context, olderThan time.Duration) error
}

// crawlResult is what one board read produced.
type crawlResult struct {
	Jobs     []Job    // jobs to save (new or refreshed)
	SeenIDs  []string // every job id currently listed on the board
	Complete bool     // the whole board was listed, so unlisted jobs are closed
}

type boardReader func(ctx context.Context, client *crawlClient, board Board, known map[string]bool) (crawlResult, error)

var boardReaders = map[string]boardReader{
	"greenhouse": readGreenhouseBoard,
	"lever":      readLeverBoard,
	"ashby":      readAshbyBoard,
	"workday":    readWorkdayBoard,
}

// Crawler keeps the saved jobs current, a batch of boards at a time.
type Crawler struct {
	Store BoardStore
	Seed  []Board
	// Boards are re-read when their last read is older than this.
	RefreshAfter time.Duration
	BatchSize    int
	Concurrency  int

	client *crawlClient
}

// NewCrawler returns a crawler with sensible pacing for a small server.
func NewCrawler(store BoardStore, seed []Board) *Crawler {
	return &Crawler{
		Store:        store,
		Seed:         seed,
		RefreshAfter: 20 * time.Hour,
		BatchSize:    12,
		Concurrency:  3,
		client:       newCrawlClient(),
	}
}

// Run crawls until ctx is canceled. It is safe to restart at any time:
// progress lives in the database.
func (c *Crawler) Run(ctx context.Context) {
	if c == nil || c.Store == nil {
		return
	}
	if err := c.Store.SeedBoards(ctx, c.Seed); err != nil {
		log.Printf("[crawler] seeding boards failed: %v", err)
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(20 * time.Second): // let the server finish starting
	}

	lastPrune := time.Time{}
	for {
		if time.Since(lastPrune) > 6*time.Hour {
			if err := c.Store.PruneClosed(ctx, 72*time.Hour); err != nil {
				log.Printf("[crawler] pruning closed jobs failed: %v", err)
			}
			lastPrune = time.Now()
		}

		boards, err := c.Store.DueBoards(ctx, c.BatchSize, c.RefreshAfter)
		if err != nil {
			log.Printf("[crawler] loading boards failed: %v", err)
		}
		pause := 30 * time.Second
		if len(boards) == 0 {
			pause = 15 * time.Minute
		} else {
			c.CrawlBoards(ctx, boards)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(pause):
		}
	}
}

// CrawlBoards reads a set of boards, a few at a time.
func (c *Crawler) CrawlBoards(ctx context.Context, boards []Board) {
	if c.client == nil {
		c.client = newCrawlClient()
	}
	slots := make(chan struct{}, max(1, c.Concurrency))
	var wg sync.WaitGroup
	for _, board := range boards {
		wg.Add(1)
		go func(board Board) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			c.crawlBoard(ctx, board)
		}(board)
	}
	wg.Wait()
}

func (c *Crawler) crawlBoard(ctx context.Context, board Board) {
	reader, ok := boardReaders[board.ATS]
	if !ok {
		_ = c.Store.RecordCrawl(ctx, board, "unknown job board type", 0, true)
		return
	}
	started := time.Now()
	boardCtx, cancel := context.WithTimeout(ctx, 20*time.Minute) // big Workday boards take a while at a polite pace
	defer cancel()

	known, err := c.Store.KnownJobIDs(boardCtx, board)
	if err != nil {
		log.Printf("[crawler] %s: loading saved jobs failed: %v", board.key(), err)
		return
	}
	result, err := reader(boardCtx, c.client, board, known)
	if err != nil {
		log.Printf("[crawler] %s: %v", board.key(), err)
		_ = c.Store.RecordCrawl(ctx, board, truncate(err.Error(), 200), 0, true)
		return
	}
	for i := range result.Jobs {
		finishCrawledJob(&result.Jobs[i], board)
	}
	if err := c.Store.SaveBoardJobs(boardCtx, board, result.Jobs, result.SeenIDs, result.Complete); err != nil {
		log.Printf("[crawler] %s: saving jobs failed: %v", board.key(), err)
		_ = c.Store.RecordCrawl(ctx, board, "saving failed", 0, true)
		return
	}
	status := "ok"
	if !result.Complete {
		status = "partial"
	}
	_ = c.Store.RecordCrawl(ctx, board, status, len(result.SeenIDs), false)
	log.Printf("[crawler] %s: %d listed, %d saved (%s) in %s", board.key(), len(result.SeenIDs), len(result.Jobs), status, time.Since(started).Round(100*time.Millisecond))
}

// maxStoredDescription keeps the database small; requirements sections
// fit comfortably within it.
const maxStoredDescription = 12000

func finishCrawledJob(job *Job, board Board) {
	job.Source = board.ATS
	if job.Company == "" {
		job.Company = board.Company
	}
	job.Description = truncate(normalizeJobDescription(job.Description), maxStoredDescription)
	if job.CountryCode == "" {
		job.CountryCode = countryFromLocation(job.Location)
	}
	if job.WorkArrangement == "" {
		job.WorkArrangement = arrangementFromLocation(job.Location + " " + job.Title)
	}
	// Non-US jobs are kept as a short record (so they aren't downloaded
	// again every day) without their description.
	if job.CountryCode != "" && job.CountryCode != "US" {
		job.Description = ""
	}
	job.IsActive = true
}

func crawledJobID(board Board, id string) string {
	return board.ATS + ":" + board.Slug + ":" + id
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && value[cut]&0xC0 == 0x80 { // don't split a UTF-8 character
		cut--
	}
	return value[:cut]
}

// ---------------------------------------------------------------------------
// HTTP client: robots.txt, pacing, and a clear user agent
// ---------------------------------------------------------------------------

var errDisallowed = errors.New("blocked by the site's robots.txt")

type crawlClient struct {
	http *http.Client
	gap  time.Duration // minimum time between requests to one host

	mu     sync.Mutex
	next   map[string]time.Time
	robots map[string]robotsEntry
}

type robotsEntry struct {
	rules   robotsRules
	fetched time.Time
}

func newCrawlClient() *crawlClient {
	return &crawlClient{
		http:   &http.Client{Timeout: 30 * time.Second},
		gap:    500 * time.Millisecond,
		next:   make(map[string]time.Time),
		robots: make(map[string]robotsEntry),
	}
}

// wait paces requests to one host.
func (c *crawlClient) wait(ctx context.Context, host string) error {
	c.mu.Lock()
	now := time.Now()
	at := c.next[host]
	if at.Before(now) {
		at = now
	}
	c.next[host] = at.Add(c.gap)
	c.mu.Unlock()

	if delay := time.Until(at); delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil
}

func (c *crawlClient) allowed(ctx context.Context, target *url.URL) (bool, error) {
	host := target.Scheme + "://" + target.Host
	c.mu.Lock()
	entry, ok := c.robots[host]
	c.mu.Unlock()
	if !ok || time.Since(entry.fetched) > 24*time.Hour {
		rules, err := c.fetchRobots(ctx, host, target.Host)
		if err != nil {
			return false, err
		}
		entry = robotsEntry{rules: rules, fetched: time.Now()}
		c.mu.Lock()
		c.robots[host] = entry
		c.mu.Unlock()
	}
	return entry.rules.allows(target.EscapedPath()), nil
}

func (c *crawlClient) fetchRobots(ctx context.Context, origin, host string) (robotsRules, error) {
	if err := c.wait(ctx, host); err != nil {
		return robotsRules{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/robots.txt", nil)
	if err != nil {
		return robotsRules{}, err
	}
	request.Header.Set("User-Agent", CrawlerUserAgent)
	response, err := c.http.Do(request)
	if err != nil {
		return robotsRules{}, fmt.Errorf("reading robots.txt: %w", err)
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		return parseRobots(io.LimitReader(response.Body, 512<<10)), nil
	case response.StatusCode >= 400 && response.StatusCode < 500:
		return robotsRules{}, nil // no robots.txt: everything is allowed
	default:
		// The site is having trouble; try again on the next run.
		return robotsRules{}, fmt.Errorf("robots.txt returned HTTP %d", response.StatusCode)
	}
}

func (c *crawlClient) do(ctx context.Context, method, rawURL string, body any, out any) error {
	target, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	ok, err := c.allowed(ctx, target)
	if err != nil {
		return err
	}
	if !ok {
		return errDisallowed
	}
	if err := c.wait(ctx, target.Host); err != nil {
		return err
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", CrawlerUserAgent)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d from %s", response.StatusCode, target.Host)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 64<<20)).Decode(out)
}

func (c *crawlClient) getJSON(ctx context.Context, rawURL string, out any) error {
	return c.do(ctx, http.MethodGet, rawURL, nil, out)
}

func (c *crawlClient) postJSON(ctx context.Context, rawURL string, body, out any) error {
	return c.do(ctx, http.MethodPost, rawURL, body, out)
}

// robotsRules is the part of robots.txt that applies to Jane's crawler:
// the group for "janecareersbot" if there is one, otherwise the "*" group.
type robotsRules struct {
	allow    []string
	disallow []string
}

func parseRobots(r io.Reader) robotsRules {
	type group struct {
		agents   []string
		allow    []string
		disallow []string
	}
	var groups []*group
	var current *group
	lastWasAgent := false

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch key {
		case "user-agent":
			if current == nil || !lastWasAgent {
				current = &group{}
				groups = append(groups, current)
			}
			current.agents = append(current.agents, strings.ToLower(value))
			lastWasAgent = true
		case "allow", "disallow":
			lastWasAgent = false
			if current == nil {
				continue
			}
			if key == "allow" {
				current.allow = append(current.allow, value)
			} else if value != "" {
				current.disallow = append(current.disallow, value)
			}
		default:
			lastWasAgent = false
		}
	}

	pick := func(agent string) *group {
		for _, g := range groups {
			for _, a := range g.agents {
				if a == agent {
					return g
				}
			}
		}
		return nil
	}
	chosen := pick("janecareersbot")
	if chosen == nil {
		chosen = pick("*")
	}
	if chosen == nil {
		return robotsRules{}
	}
	return robotsRules{allow: chosen.allow, disallow: chosen.disallow}
}

// allows applies the longest matching rule; on a tie, Allow wins.
func (r robotsRules) allows(path string) bool {
	if path == "" {
		path = "/"
	}
	best, allowed := -1, true
	for _, rule := range r.disallow {
		if n := robotsMatch(rule, path); n > best {
			best, allowed = n, false
		}
	}
	for _, rule := range r.allow {
		if n := robotsMatch(rule, path); n >= best && n >= 0 {
			best, allowed = n, true
		}
	}
	return allowed
}

// robotsMatch returns the rule's length if it matches the path, or -1. It
// supports "*" wildcards and a trailing "$".
func robotsMatch(rule, path string) int {
	if rule == "" {
		return -1
	}
	anchored := strings.HasSuffix(rule, "$")
	pattern := strings.TrimSuffix(rule, "$")
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(path, parts[0]) {
		return -1
	}
	position := len(parts[0])
	for _, part := range parts[1:] {
		index := strings.Index(path[position:], part)
		if index < 0 {
			return -1
		}
		position += index + len(part)
	}
	if anchored && position != len(path) && !strings.HasSuffix(pattern, "*") {
		return -1
	}
	return len(rule)
}
