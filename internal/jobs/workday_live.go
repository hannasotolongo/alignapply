package jobs

import (
	"context"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"
)

// WorkdayLiveProvider searches every known Workday employer board at the
// moment of a search, using each board's own search box, so a search finds
// matching jobs right away even before the crawler has saved them. Links
// go to the employer's own posting.
type WorkdayLiveProvider struct {
	boards      []Board
	client      *crawlClient
	perQuery    int           // listings asked for per board and query
	maxDetails  int           // postings whose details are read per search
	timeBudget  time.Duration // stop and return what's found by then
	concurrency int
	clusters    *clusterSlots
}

func NewWorkdayLiveProvider(boards []Board) *WorkdayLiveProvider {
	var workday []Board
	for _, board := range boards {
		if board.ATS == "workday" {
			workday = append(workday, board)
		}
	}
	return &WorkdayLiveProvider{
		boards:      workday,
		client:      newCrawlClient(),
		perQuery:    20,
		maxDetails:  80,
		timeBudget:  12 * time.Second,
		concurrency: 200,
		clusters:    &clusterSlots{size: 8},
	}
}

func (p *WorkdayLiveProvider) Name() string { return "workday-live" }

// clusterSlots limits how many requests go to one Workday server cluster
// (for example wd1.myworkdayjobs.com) at once: Workday answers HTTP 429 when
// one caller asks many of its employers at the same moment.
type clusterSlots struct {
	mu    sync.Mutex
	slots map[string]chan struct{}
	size  int
}

func (c *clusterSlots) acquire(ctx context.Context, host string) bool {
	cluster := host
	if i := strings.IndexByte(host, '.'); i >= 0 {
		cluster = host[i+1:]
	}
	c.mu.Lock()
	if c.slots == nil {
		c.slots = map[string]chan struct{}{}
	}
	slot, ok := c.slots[cluster]
	if !ok {
		slot = make(chan struct{}, c.size)
		c.slots[cluster] = slot
	}
	c.mu.Unlock()
	select {
	case slot <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (c *clusterSlots) release(host string) {
	cluster := host
	if i := strings.IndexByte(host, '.'); i >= 0 {
		cluster = host[i+1:]
	}
	c.mu.Lock()
	slot := c.slots[cluster]
	c.mu.Unlock()
	<-slot
}

type workdayHit struct {
	board                 Board
	base, host, site      string
	path, title, location string
}

func (p *WorkdayLiveProvider) Search(ctx context.Context, request SearchRequest) ([]Job, error) {
	role := strings.TrimSpace(request.TargetRole)
	if p == nil || role == "" || len(p.boards) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeBudget)
	defer cancel()

	queries := workdayQueries(role)
	city := strings.TrimSpace(strings.SplitN(request.Location, ",", 2)[0])
	if strings.EqualFold(city, "remote") || strings.EqualFold(city, "anywhere") {
		city = ""
	}
	state := ""
	if city != "" {
		for code := range usStatesIn(request.Location) {
			state = usStateFullName(code)
		}
	}

	// 1. Ask every board's search for the role (near the city first),
	// leaving time to read the details of what's found.
	listCtx, cancelList := context.WithTimeout(ctx, p.timeBudget*60/100)
	defer cancelList()
	answered := 0
	var (
		mu   sync.Mutex
		hits []workdayHit
		seen = map[string]bool{}
		wg   sync.WaitGroup
		slot = make(chan struct{}, p.concurrency)
	)
	// Employers in the searched field (dental groups for a dentist search)
	// go first, so the busy Workday servers answer them before anyone else
	// and slow or rate-limited boards can't crowd them out.
	priority, rest := boardsForRole(role, p.boards)
	priorityDone := make(chan struct{})
	var priorityWG sync.WaitGroup
	launch := func(board Board, first bool) {
		host, tenant, site, ok := workdayParts(board.Slug)
		if !ok {
			return
		}
		base := "https://" + host + "/wday/cxs/" + url.PathEscape(tenant) + "/" + url.PathEscape(site)
		// Read all of a fitting employer's statewide list (Aspen Dental
		// has 185 dentist openings in Florida; the Tampa-area ones can be
		// anywhere in it).
		stateReach := 20
		if first {
			stateReach = 200
			priorityWG.Add(1)
		} else if len(priority) > 0 {
			// The search has its own employers (dental groups for a
			// dentist); the others only get the city search, which keeps
			// Workday from rate-limiting the ones that matter.
			stateReach = 0
		}
		wg.Add(1)
		go func(board Board, base, host, site string) {
			defer wg.Done()
			if first {
				defer priorityWG.Done()
			} else {
				select {
				case <-priorityDone:
				case <-listCtx.Done():
					return
				}
			}
			select {
			case slot <- struct{}{}:
			case <-listCtx.Done():
				return
			}
			defer func() { <-slot }()
			keep := func(listings []workdayListing) int {
				kept := 0
				for _, item := range listings {
					path := strings.TrimSpace(item.ExternalPath)
					if path == "" || !jobTitleMatchesTargetRole(role, item.Title) {
						continue
					}
					if !workdayListingNear(request, item.LocationsText) {
						continue
					}
					kept++
					mu.Lock()
					if key := board.Slug + path; !seen[key] {
						seen[key] = true
						hits = append(hits, workdayHit{board, base, host, site, path, item.Title, item.LocationsText})
					}
					mu.Unlock()
				}
				return kept
			}
			for _, query := range queries {
				// Workday's own search matches the city in a job's location,
				// so "dentist tampa" returns that employer's Tampa jobs.
				listings, _, ok := p.listPage(listCtx, host, base, strings.TrimSpace(query+" "+city), 0)
				if !ok {
					continue
				}
				mu.Lock()
				answered++
				mu.Unlock()
				// Few real matches in the city itself: look across the state
				// and let the location filter keep the nearby towns (Largo
				// and Brandon for Tampa).
				if keep(listings) < 3 && state != "" && stateReach > 0 {
					// Workday returns at most 20 jobs per request; the rest
					// of the pages are asked for at once.
					more, total, ok := p.listPage(listCtx, host, base, query+" "+state, 0)
					if ok {
						keep(more)
						var pages sync.WaitGroup
						for offset := 20; offset < stateReach && offset < total; offset += 20 {
							pages.Add(1)
							go func(offset int) {
								defer pages.Done()
								if more, _, ok := p.listPage(listCtx, host, base, query+" "+state, offset); ok {
									keep(more)
								}
							}(offset)
						}
						pages.Wait()
					}
				}
			}
		}(board, base, host, site)
	}
	for _, board := range priority {
		launch(board, true)
	}
	go func() {
		// The rest start once the fitting employers are done, or after
		// a short head start.
		finished := make(chan struct{})
		go func() { priorityWG.Wait(); close(finished) }()
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
		case <-listCtx.Done():
		}
		close(priorityDone)
	}()
	for _, board := range rest {
		launch(board, false)
	}
	wg.Wait()
	found := len(hits)

	// 2. Read the details (description, apply link) of the best few,
	// spread across employers so one big employer doesn't fill the list.
	hits = spreadAcrossBoards(hits, p.maxDetails)
	var jobs []Job
	for _, hit := range hits {
		wg.Add(1)
		go func(hit workdayHit) {
			defer wg.Done()
			select {
			case slot <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slot }()
			if !p.clusters.acquire(ctx, hit.host) {
				return
			}
			job, err := fetchWorkdayJob(ctx, p.client, hit.board, hit.base, hit.host, hit.site, hit.path, hit.title, hit.location)
			if isRateLimited(err) {
				time.Sleep(time.Second)
				job, err = fetchWorkdayJob(ctx, p.client, hit.board, hit.base, hit.host, hit.site, hit.path, hit.title, hit.location)
			}
			p.clusters.release(hit.host)
			if err != nil {
				if workdayLiveDebug {
					log.Printf("[workday-live] details %s%s: %v", hit.host, hit.path, err)
				}
				return
			}
			finishCrawledJob(&job, hit.board)
			mu.Lock()
			jobs = append(jobs, job)
			mu.Unlock()
		}(hit)
	}
	wg.Wait()
	log.Printf("[workday-live] %q in %q: %d of %d boards answered, %d matching listings, %d read", role, request.Location, answered, len(p.boards), found, len(jobs))
	return jobs, nil
}

type workdayListing struct {
	Title         string `json:"title"`
	ExternalPath  string `json:"externalPath"`
	LocationsText string `json:"locationsText"`
}

func (p *WorkdayLiveProvider) listPage(ctx context.Context, host, base, searchText string, offset int) ([]workdayListing, int, bool) {
	var page struct {
		Total       int              `json:"total"`
		JobPostings []workdayListing `json:"jobPostings"`
	}
	body := map[string]any{"appliedFacets": map[string]any{}, "limit": p.perQuery, "offset": offset, "searchText": searchText}
	if !p.clusters.acquire(ctx, host) {
		return nil, 0, false
	}
	err := p.client.postJSON(ctx, base+"/jobs", body, &page)
	if isRateLimited(err) {
		time.Sleep(time.Second)
		err = p.client.postJSON(ctx, base+"/jobs", body, &page)
	}
	p.clusters.release(host)
	if err != nil {
		if workdayLiveDebug {
			log.Printf("[workday-live] %s: %v", base, err)
		}
		return nil, 0, false
	}
	return page.JobPostings, page.Total, true
}

// workdayLiveDebug logs each board's error (for the dev check tool).
var workdayLiveDebug = false

// SetWorkdayLiveDebug turns per-board error logging on or off.
func SetWorkdayLiveDebug(on bool) { workdayLiveDebug = on }

// workdayQueries is the role plus the names employers use for it (a
// "doctor" search also asks for "physician").
func workdayQueries(role string) []string {
	queries := []string{role}
	tokens := roleRelevanceTokens(expandTitleAliases(normalizePreferenceText(role)))
	if len(tokens) == 1 {
		for token := range tokens {
			for _, alternative := range roleSynonyms[token] {
				if len(queries) >= 2 {
					break
				}
				queries = append(queries, alternative)
			}
		}
	}
	return queries
}

// workdayListingNear drops listings clearly in another place before their
// details are read. "3 Locations" and blank locations are kept.
func workdayListingNear(request SearchRequest, locationsText string) bool {
	text := strings.TrimSpace(locationsText)
	if text == "" || strings.HasSuffix(strings.ToLower(text), "locations") {
		return true
	}
	job := Job{Location: workdayLocation(text), CountryCode: countryFromLocation(text), WorkArrangement: arrangementFromLocation(text)}
	return matchesLocationPreference(request, job)
}

func spreadAcrossBoards(hits []workdayHit, limit int) []workdayHit {
	if len(hits) <= limit {
		return hits
	}
	byBoard := map[string][]workdayHit{}
	var order []string
	for _, hit := range hits {
		if _, ok := byBoard[hit.board.Slug]; !ok {
			order = append(order, hit.board.Slug)
		}
		byBoard[hit.board.Slug] = append(byBoard[hit.board.Slug], hit)
	}
	var out []workdayHit
	for round := 0; len(out) < limit; round++ {
		added := false
		for _, slug := range order {
			if round < len(byBoard[slug]) && len(out) < limit {
				out = append(out, byBoard[slug][round])
				added = true
			}
		}
		if !added {
			break
		}
	}
	return out
}

func isRateLimited(err error) bool {
	return err != nil && strings.Contains(err.Error(), "HTTP 429")
}

// fieldEmployerWords links a kind of job to words in the names of the
// employers that hire for it.
var fieldEmployerWords = []struct{ roleWords, companyWords []string }{
	{[]string{"dentist", "dental", "hygienist", "orthodont", "oral surgeon", "endodont", "periodont", "prosthodont", "dds", "dmd"},
		[]string{"dental", "dentist", "smile", "ortho", "oral", "teeth", "tooth"}},
	{[]string{"physician", "doctor", "nurse", "rn", "lpn", "pharmacist", "pharmacy", "therapist", "medical", "clinical", "surgeon", "radiolog", "sonograph", "phlebotom", "paramedic", "emt", "caregiver", "nurse practitioner", "physician assistant", "dialysis"},
		[]string{"health", "medical", "hospital", "clinic", "care", "medicine", "pharm", "dialysis", "fresenius", "kaiser", "cvs", "walgreens", "mayo", "baptist", "methodist", "mercy", "children"}},
	{[]string{"lawyer", "attorney", "counsel", "paralegal", "legal", "litigat"},
		[]string{"law", "llp", "legal", "cooley", "goodwin", "lovells", "jackson lewis", "holland", "mwe", "mcdermott"}},
}

// boardsForRole splits boards into the ones whose employer fits the role
// (by field, or the role's own words in the company name) and the rest.
func boardsForRole(role string, boards []Board) (fit, rest []Board) {
	roleText := " " + strings.ToLower(role) + " "
	var words []string
	for _, field := range fieldEmployerWords {
		for _, w := range field.roleWords {
			if strings.Contains(roleText, w) && (len(w) > 3 || strings.Contains(roleText, " "+w+" ")) {
				words = append(words, field.companyWords...)
				break
			}
		}
	}
	for _, w := range strings.Fields(strings.ToLower(role)) {
		if len(w) > 3 {
			words = append(words, w)
		}
	}
	for _, board := range boards {
		company := strings.ToLower(board.Company)
		matched := false
		for _, w := range words {
			if strings.Contains(company, w) {
				matched = true
				break
			}
		}
		if matched {
			fit = append(fit, board)
		} else {
			rest = append(rest, board)
		}
	}
	return fit, rest
}
