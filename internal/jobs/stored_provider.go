package jobs

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// StoredJobsProvider searches the jobs Jane's crawler has saved. The
// database narrows by title; the pipeline then applies the exact title,
// location and preference rules.
type StoredJobsProvider struct {
	store func() BoardStore
	limit int

	// Jobs saved from an employer's list alone have no description yet,
	// so Jane can't score them. A search reads the nearest few on the
	// spot (and saves them), so they can be scored and shown.
	client      *crawlClient
	readMax     int
	readPerHost int
	readBudget  time.Duration
	readAtOnce  int
}

// NewStoredJobsProvider takes a function so the store can be connected
// after the provider is built.
func NewStoredJobsProvider(store func() BoardStore) *StoredJobsProvider {
	return &StoredJobsProvider{store: store, limit: 1500, client: newCrawlClient(),
		readMax: 40, readPerHost: 10, readBudget: 3 * time.Second, readAtOnce: 16}
}

func (p *StoredJobsProvider) Name() string { return "jane-crawler" }

func (p *StoredJobsProvider) Search(ctx context.Context, request SearchRequest) ([]Job, error) {
	if p == nil || p.store == nil {
		return nil, nil
	}
	store := p.store()
	if store == nil {
		return nil, nil
	}
	pattern := TitleSearchPattern(request.TargetRole)
	if pattern == "" {
		return nil, nil
	}
	queryStarted := time.Now()
	found, err := store.SearchJobs(ctx, pattern, NearSearchPattern(request.Location), p.limit)
	RecordTiming(ctx, "db-query", time.Since(queryStarted))
	RecordTiming(ctx, "db-rows-"+strconv.Itoa(len(found)), 0)
	if err != nil {
		return nil, err
	}
	var ready, unread []Job
	for _, job := range found {
		if strings.TrimSpace(job.Description) != "" {
			ready = append(ready, job)
		} else {
			unread = append(unread, job)
		}
	}
	readStarted := time.Now()
	read := p.readNearest(ctx, store, request, unread)
	RecordTiming(ctx, "read-details", time.Since(readStarted))
	return append(ready, read...), nil
}

// readNearest reads the details of the unread jobs that fit the search,
// nearest first, within a few seconds, saves them, and returns them.
func (p *StoredJobsProvider) readNearest(ctx context.Context, store BoardStore, request SearchRequest, unread []Job) []Job {
	if len(unread) == 0 || p.client == nil || p.readMax <= 0 {
		return nil
	}
	wide := request
	if wide.RadiusMiles == 0 {
		wide.RadiusMiles = wholeStateRadius
	}
	type candidate struct {
		job   Job
		miles float64
	}
	var candidates []candidate
	for _, job := range unread {
		if !strings.HasPrefix(job.ID, "workday:") || !roleMatches(request, job) {
			continue
		}
		if !matchesLocationPreference(wide, job) {
			continue
		}
		miles := math.MaxFloat64
		if m, _, ok := jobDistance(request, job); ok {
			miles = m
		}
		candidates = append(candidates, candidate{job, miles})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].miles < candidates[j].miles })

	perHost := map[string]int{}
	var picked []Job
	for _, c := range candidates {
		slug, _, ok := splitWorkdayJobID(c.job.ID)
		if !ok {
			continue
		}
		host, _, _, _ := workdayParts(slug)
		if perHost[host] >= p.readPerHost {
			continue
		}
		perHost[host]++
		picked = append(picked, c.job)
		if len(picked) >= p.readMax {
			break
		}
	}
	if len(picked) == 0 {
		return nil
	}

	readCtx, cancel := context.WithTimeout(ctx, p.readBudget)
	defer cancel()

	// Check each site's robots.txt once, before the reads start; otherwise
	// every read would fetch it and the half-second pacing per site would
	// use up the time.
	var robots sync.WaitGroup
	for host := range perHost {
		robots.Add(1)
		go func(host string) {
			defer robots.Done()
			if target, err := url.Parse("https://" + host + "/"); err == nil {
				_, _ = p.client.allowed(readCtx, target)
			}
		}(host)
	}
	robots.Wait()
	var (
		mu       sync.Mutex
		read     []Job
		firstErr error
		wg       sync.WaitGroup
		slot     = make(chan struct{}, max(1, p.readAtOnce))
	)
	for _, job := range picked {
		wg.Add(1)
		go func(job Job) {
			defer wg.Done()
			select {
			case slot <- struct{}{}:
			case <-readCtx.Done():
				return
			}
			defer func() { <-slot }()
			slug, path, _ := splitWorkdayJobID(job.ID)
			host, tenant, site, ok := workdayParts(slug)
			if !ok {
				return
			}
			board := Board{ATS: "workday", Slug: slug, Company: job.Company}
			base := "https://" + host + "/wday/cxs/" + url.PathEscape(tenant) + "/" + url.PathEscape(site)
			full, err := fetchWorkdayJob(readCtx, p.client, board, base, host, site, path, job.Title, job.Location)
			if err != nil || strings.TrimSpace(full.Description) == "" {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %v (description %d chars)", host+path, err, len(full.Description))
				}
				mu.Unlock()
				return
			}
			full.ID = job.ID
			finishCrawledJob(&full, board)
			full.IsActive = true
			mu.Lock()
			read = append(read, full)
			mu.Unlock()
		}(job)
	}
	wg.Wait()

	if len(read) > 0 {
		saved := append([]Job(nil), read...)
		go func() {
			saveCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := store.SaveJobDetails(saveCtx, saved); err != nil {
				log.Printf("[jane-crawler] saving details read during a search failed: %v", err)
			}
		}()
	}
	log.Printf("[jane-crawler] %q in %q: %d unread matches, read %d of %d tried (first problem: %v)", request.TargetRole, request.Location, len(candidates), len(read), len(picked), firstErr)
	return read
}

// splitWorkdayJobID takes "workday:<host/tenant/site>:<path>" apart.
func splitWorkdayJobID(id string) (slug, path string, ok bool) {
	rest, found := strings.CutPrefix(id, "workday:")
	if !found {
		return "", "", false
	}
	i := strings.Index(rest, ":/")
	if i <= 0 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

// NearSearchPattern is a PostgreSQL regular expression for job locations
// in the searched state, so a common role ("cashier") brings back the
// jobs near the user first rather than the newest ones nationwide.
func NearSearchPattern(location string) string {
	state := ""
	if _, code, ok := geoLocate(location, "", true); ok {
		state = code
	} else if code, ok := geoStateOnly(location); ok {
		state = code
	}
	return StatePattern(state)
}

// StatePattern is a PostgreSQL regular expression for job locations in a
// state ("FL"): ", FL" or "Florida".
func StatePattern(state string) string {
	if state == "" {
		return ""
	}
	return `(,\s*` + state + `\y|\y` + usStateFullName(state) + `\y)`
}

// TitleSearchPattern is a PostgreSQL regular expression that finds every
// title jobTitleMatchesTargetRole could accept for the role: titles with
// one of the role's meaningful words, including short forms (a "registered
// nurse" search also finds "ICU RN").
func TitleSearchPattern(role string) string {
	// An industry search reads that industry's jobs ("crypto" is in the
	// searchable title of every job at a crypto employer) and titles that
	// name it; the pipeline then checks the rest of the search.
	if industry, _ := splitIndustry(role); industry == "crypto" {
		return `\m(crypto|blockchain|web3|defi|bitcoin|ethereum|nft|on-?chain|solidity|smart contract|tokeniz|digital asset)`
	}
	words := map[string]struct{}{}
	pieces := map[string]struct{}{}
	for _, form := range roleSearchForms(role) {
		tokens := roleRelevanceTokens(form)
		if len(tokens) == 0 {
			tokens = preferenceTokenSet(form)
		}
		for token := range tokens {
			// A word's stem finds its other forms in the database
			// ("account" finds accountant and accounting); the title
			// filter decides afterward.
			if stem := stemWord(token); len(stem) >= 4 {
				words[stem] = struct{}{}
			} else {
				words[token] = struct{}{}
			}
			for _, alternative := range roleSynonyms[token] {
				words[alternative] = struct{}{}
			}
			if parts, ok := compoundWords[token]; ok {
				pieces[regexp.QuoteMeta(parts[0])+`[\s/-]*`+regexp.QuoteMeta(parts[1])] = struct{}{}
			}
		}
		for short, expansion := range titleAliases {
			for _, word := range strings.Fields(expansion) {
				if _, ok := tokens[word]; ok {
					words[short] = struct{}{}
				}
			}
		}
	}
	if len(words) == 0 && len(pieces) == 0 {
		return ""
	}
	list := make([]string, 0, len(words)+len(pieces))
	for word := range words {
		list = append(list, regexp.QuoteMeta(word))
	}
	for piece := range pieces {
		list = append(list, piece)
	}
	sort.Strings(list)
	return `\m(` + strings.Join(list, "|") + `)`
}
