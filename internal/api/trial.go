package api

import (
	"context"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

// The free trial (janecareers.com/try): one search without an account.
// A visitor gives the job they want, a city and their résumé text; Jane
// searches, scores every job, and answers with
//
//   - feedback: the requirements the best-matching jobs ask for that the
//     résumé doesn't show (what to fix), and what it already shows well;
//   - the 3 best matches, each with its apply link on the employer's site;
//   - how many matches there are in all, to unlock with Pro.
//
// Nothing is stored. Each network gets a few trial searches a day, and at
// most two trial searches run at once, so the trial can't slow the site.

const (
	trialSearchesPerDay = 3
	trialMatchesShown   = 3
	trialFixesShown     = 3
	trialStrengthsShown = 4
	trialJobsConsidered = 15
	trialMaxRequirement = 140
)

var trialSlots = make(chan struct{}, 2)

type trialRequest struct {
	TargetRole string `json:"targetRole"`
	Location   string `json:"location"`
	ResumeText string `json:"resumeText"`
}

type trialFix struct {
	Text     string `json:"text"`
	Category string `json:"category,omitempty"`
	Jobs     int    `json:"jobs"`
}

type trialMatch struct {
	Title           string   `json:"title"`
	Company         string   `json:"company"`
	Location        string   `json:"location"`
	DistanceMiles   *float64 `json:"distanceMiles,omitempty"`
	MatchPercentage int      `json:"matchPercentage"`
	MatchLevel      string   `json:"matchLevel"`
	Missing         []string `json:"missing,omitempty"`
	ApplyURL        string   `json:"applyURL"`
	PostedAt        any      `json:"postedAt,omitempty"`
}

type trialResponse struct {
	Total        int          `json:"total"`
	AverageMatch int          `json:"averageMatch"`
	Fixes        []trialFix   `json:"fixes"`
	Strengths    []string     `json:"strengths"`
	Matches      []trialMatch `json:"matches"`
}

func (s *Server) handleTrialSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}

	var request trialRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	request.TargetRole = strings.TrimSpace(request.TargetRole)
	request.Location = strings.TrimSpace(request.Location)
	request.ResumeText = strings.TrimSpace(request.ResumeText)

	switch {
	case request.TargetRole == "" || utf8.RuneCountInString(request.TargetRole) > 100:
		writeError(w, http.StatusBadRequest, "Tell Jane the job you want.")
		return
	case request.Location == "" || utf8.RuneCountInString(request.Location) > 100:
		writeError(w, http.StatusBadRequest, "Tell Jane your city or ZIP code.")
		return
	case utf8.RuneCountInString(request.ResumeText) < minResumeTextChars:
		writeError(w, http.StatusBadRequest, "Jane couldn't read your résumé. Upload it as a PDF with selectable text.")
		return
	case utf8.RuneCountInString(request.ResumeText) > maxResumeTextChars:
		writeError(w, http.StatusBadRequest, "That résumé is too long. Upload a shorter version.")
		return
	}

	if !s.trialLimiter.Allow("trial:" + clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "You've used your free searches for today. Create an account to keep going.",
			"code":  "trial_used",
		})
		return
	}

	select {
	case trialSlots <- struct{}{}:
		defer func() { <-trialSlots }()
	case <-time.After(15 * time.Second):
		writeError(w, http.StatusServiceUnavailable, "Jane is busy right now. Try again in a minute.")
		return
	case <-r.Context().Done():
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	search := jobs.SearchRequest{
		ResumeText: request.ResumeText,
		TargetRole: request.TargetRole,
		Location:   request.Location,
	}
	normalizeSearchRequest(&search)

	started := time.Now()
	result := s.jobProvider.Search(ctx, search)
	processed := s.pipeline.Process(search, result.Jobs)

	matches := make([]jobs.JobMatch, len(processed))
	work := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < 6 && worker < len(processed); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range work {
				matches[index] = previewOrScore(search, processed[index])
				matches[index].DistanceMiles = processed[index].DistanceMiles
			}
		}()
	}
	for index := range processed {
		work <- index
	}
	close(work)
	wg.Wait()

	scored := matches[:0]
	for _, match := range matches {
		if match.MatchLevel != "Insufficient Evidence" && match.MatchLevel != previewMatchLevel && strings.TrimSpace(match.ApplyURL) != "" {
			scored = append(scored, match)
		}
	}
	sortJobMatches(scored)
	scored = nearbyTrialMatches(scored)

	log.Printf("[trial] role=%q location=%q jobs=%d scored=%d in %s", request.TargetRole, request.Location, len(result.Jobs), len(scored), time.Since(started))

	writeJSON(w, http.StatusOK, buildTrialResponse(scored))
}

// buildTrialResponse turns ranked matches into the trial's feedback and
// its first three jobs.
func buildTrialResponse(ranked []jobs.JobMatch) trialResponse {
	response := trialResponse{Total: len(ranked), Fixes: []trialFix{}, Strengths: []string{}, Matches: []trialMatch{}}
	if len(ranked) == 0 {
		return response
	}

	considered := ranked
	if len(considered) > trialJobsConsidered {
		considered = considered[:trialJobsConsidered]
	}

	sum := 0
	type tally struct {
		text     string
		category string
		jobs     int
		required bool
		order    int
	}
	missing := map[string]*tally{}
	strong := map[string]*tally{}
	order := 0
	for _, match := range considered {
		sum += match.MatchPercentage
		categories := map[string]jobs.Requirement{}
		for _, requirement := range match.Requirements {
			categories[trialKey(requirement.Text)] = requirement
		}
		seen := map[string]bool{}
		for _, text := range append(append([]string{}, match.MissingRequirements...), match.PartialRequirements...) {
			key := trialKey(text)
			if key == "" || seen[key] || len(text) > trialMaxRequirement || !trialUseful(text) {
				continue
			}
			seen[key] = true
			entry := missing[key]
			if entry == nil {
				requirement := categories[key]
				entry = &tally{text: strings.TrimSpace(text), category: string(requirement.Category), required: requirement.Importance == "required", order: order}
				order++
				missing[key] = entry
			}
			entry.jobs++
		}
		for _, text := range match.SupportedRequirements {
			key := trialKey(text)
			if key == "" || seen[key] || len(text) > trialMaxRequirement || !trialUseful(text) {
				continue
			}
			seen[key] = true
			entry := strong[key]
			if entry == nil {
				entry = &tally{text: strings.TrimSpace(text), order: order}
				order++
				strong[key] = entry
			}
			entry.jobs++
		}
	}
	response.AverageMatch = (sum + len(considered)/2) / len(considered)

	rank := func(entries map[string]*tally) []*tally {
		list := make([]*tally, 0, len(entries))
		for _, entry := range entries {
			list = append(list, entry)
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].jobs != list[j].jobs {
				return list[i].jobs > list[j].jobs
			}
			if list[i].required != list[j].required {
				return list[i].required
			}
			return list[i].order < list[j].order
		})
		return list
	}
	for _, entry := range rank(missing) {
		if len(response.Fixes) == trialFixesShown {
			break
		}
		response.Fixes = append(response.Fixes, trialFix{Text: entry.text, Category: entry.category, Jobs: entry.jobs})
	}
	for _, entry := range rank(strong) {
		if len(response.Strengths) == trialStrengthsShown {
			break
		}
		response.Strengths = append(response.Strengths, entry.text)
	}

	for _, match := range ranked {
		if len(response.Matches) == trialMatchesShown {
			break
		}
		missingTexts := []string{}
		for _, text := range match.MissingRequirements {
			if len(text) <= trialMaxRequirement && len(missingTexts) < 2 && trialUseful(text) {
				missingTexts = append(missingTexts, strings.TrimSpace(text))
			}
		}
		var posted any
		if match.PostedAt != nil {
			posted = match.PostedAt
		}
		response.Matches = append(response.Matches, trialMatch{
			Title:           match.Title,
			Company:         match.Company,
			Location:        match.Location,
			DistanceMiles:   match.DistanceMiles,
			MatchPercentage: match.MatchPercentage,
			MatchLevel:      match.MatchLevel,
			Missing:         missingTexts,
			ApplyURL:        match.ApplyURL,
			PostedAt:        posted,
		})
	}
	return response
}

func trialKey(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(text)), " ")
}

// Trial results stay near the searched place: a job counts when Jane knows
// how far it is and it is within 60 miles, widening to 150 miles when fewer
// than three are that close. Jobs whose distance is unknown are left out, so
// a Gainesville search never shows a job in another state.
func nearbyTrialMatches(ranked []jobs.JobMatch) []jobs.JobMatch {
	within := func(limit float64) []jobs.JobMatch {
		kept := []jobs.JobMatch{}
		for _, match := range ranked {
			if match.DistanceMiles != nil && *match.DistanceMiles <= limit {
				kept = append(kept, match)
			}
		}
		return kept
	}
	if near := within(60); len(near) >= trialMatchesShown {
		return near
	}
	return within(150)
}

// trialUseful leaves out posting boilerplate (background checks, legal
// notices, section headings) so the feedback is about the résumé.
func trialUseful(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.HasSuffix(trimmed, ":") {
		return false
	}
	letters, upper := 0, 0
	for _, r := range trimmed {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsUpper(r) {
				upper++
			}
		}
	}
	if letters > 0 && upper*10 >= letters*8 {
		return false // a heading in capitals, like "MINIMUM QUALIFICATIONS"
	}
	lower := strings.ToLower(trimmed)
	for _, phrase := range []string{
		"background check", "background screen", "clearance", "drug", "equal opportunity",
		"eoe", "years of age", "18 years", "authorized to work", "sponsorship", "vaccin",
		"in accordance with", "policies", "policy", "pre-employment", "e-verify",
	} {
		if strings.Contains(lower, phrase) {
			return false
		}
	}
	return true
}
