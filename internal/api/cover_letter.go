package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"html"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/tailor"
)

// Cover letters. Pro users get the whole letter for any job
// (POST /api/v1/cover-letter). Free-trial visitors see the start of the
// letter Jane wrote for one of their three free matches
// (POST /api/v1/trial/cover-letter), with the rest unlocked by Pro.
//
// Letters are truth-locked like the application kit (see
// tailor.EnforceCoverLetter): nothing the résumé doesn't state.

type coverLetterRequest struct {
	ResumeText     string `json:"resumeText"`
	JobDescription string `json:"jobDescription"`
	JobTitle       string `json:"jobTitle,omitempty"`
	Company        string `json:"company,omitempty"`
}

type coverLetterResponse struct {
	Greeting   string   `json:"greeting"`
	Paragraphs []string `json:"paragraphs"`
	Closing    string   `json:"closing"`
	Name       string   `json:"name"`
	Text       string   `json:"text"`
}

func newCoverLetterResponse(letter tailor.CoverLetter) coverLetterResponse {
	return coverLetterResponse{Greeting: letter.Greeting, Paragraphs: letter.Paragraphs, Closing: letter.Closing, Name: letter.Name, Text: letter.Text()}
}

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

// plainJobText turns a posting (often HTML) into plain text for the writer.
func plainJobText(description string) string {
	text := htmlTagPattern.ReplaceAllString(description, " ")
	return strings.Join(strings.Fields(html.UnescapeString(text)), " ")
}

// POST /api/v1/cover-letter (Pro)
func (s *Server) handleCoverLetter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	if s.applicationKit == nil {
		writeError(w, http.StatusServiceUnavailable, "Cover letters aren't available right now. Please try again later.")
		return
	}
	if !s.kitLimiter.Allow("letter:" + user.ID) {
		writeError(w, http.StatusTooManyRequests, "You've written a lot of cover letters this hour. Give Jane a few minutes.")
		return
	}
	var request coverLetterRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	request.ResumeText = strings.TrimSpace(request.ResumeText)
	request.JobDescription = plainJobText(request.JobDescription)
	switch {
	case request.ResumeText == "":
		writeError(w, http.StatusBadRequest, "Add your résumé first.")
		return
	case len(request.ResumeText) > fitCheckMaxResumeChars:
		writeError(w, http.StatusBadRequest, "That résumé is too long.")
		return
	case len(request.JobDescription) < 80:
		writeError(w, http.StatusBadRequest, "This posting doesn't include enough detail for a cover letter.")
		return
	case len(request.JobDescription) > fitCheckMaxJobChars:
		request.JobDescription = request.JobDescription[:fitCheckMaxJobChars]
	}

	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	letter, err := s.applicationKit.WriteCoverLetter(ctx, tailor.CoverLetterRequest{
		ResumeText:     request.ResumeText,
		JobDescription: request.JobDescription,
		JobTitle:       strings.TrimSpace(request.JobTitle),
		Company:        strings.TrimSpace(request.Company),
	})
	if err != nil {
		if errors.Is(err, tailor.ErrUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "Cover letters aren't available right now.")
			return
		}
		log.Printf("[cover-letter] FAILED user=%s after=%s: %v", user.ID, time.Since(started), err)
		writeError(w, http.StatusBadGateway, "Jane couldn't write this letter just now. Try again in a minute.")
		return
	}
	log.Printf("[cover-letter] DONE user=%s after=%s paragraphs=%d", user.ID, time.Since(started), len(letter.Paragraphs))
	writeJSON(w, http.StatusOK, newCoverLetterResponse(letter))
}

// Free trial: the three free matches are kept for 30 minutes under a random
// token, so the visitor can ask for a cover letter preview without sending
// the job back. Only the preview (greeting and first two sentences) is ever
// returned to them.

const trialSessionTTL = 30 * time.Minute

type trialJob struct {
	Title       string
	Company     string
	Description string
}

type trialSession struct {
	resumeText string
	jobs       []trialJob
	letters    map[int]tailor.CoverLetter
	expires    time.Time
}

var trialSessions = struct {
	mu       sync.Mutex
	sessions map[string]*trialSession
}{sessions: map[string]*trialSession{}}

// saveTrialSession stores the free matches and returns their token.
func saveTrialSession(resumeText string, jobs []trialJob) string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	token := hex.EncodeToString(buf)
	now := time.Now()
	trialSessions.mu.Lock()
	defer trialSessions.mu.Unlock()
	for key, session := range trialSessions.sessions {
		if now.After(session.expires) {
			delete(trialSessions.sessions, key)
		}
	}
	if len(trialSessions.sessions) > 2000 {
		return ""
	}
	trialSessions.sessions[token] = &trialSession{resumeText: resumeText, jobs: jobs, letters: map[int]tailor.CoverLetter{}, expires: now.Add(trialSessionTTL)}
	return token
}

type trialLetterRequest struct {
	Token string `json:"token"`
	Index int    `json:"index"`
}

type trialLetterResponse struct {
	Preview string `json:"preview"`
	Company string `json:"company"`
	Title   string `json:"title"`
}

// POST /api/v1/trial/cover-letter
func (s *Server) handleTrialCoverLetter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if s.applicationKit == nil {
		writeError(w, http.StatusServiceUnavailable, "Cover letters aren't available right now.")
		return
	}
	var request trialLetterRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	trialSessions.mu.Lock()
	session, ok := trialSessions.sessions[strings.TrimSpace(request.Token)]
	if ok && time.Now().After(session.expires) {
		ok = false
	}
	var job trialJob
	var cached tailor.CoverLetter
	var haveCached bool
	var resumeText string
	if ok && request.Index >= 0 && request.Index < len(session.jobs) {
		job = session.jobs[request.Index]
		cached, haveCached = session.letters[request.Index]
		resumeText = session.resumeText
	} else {
		ok = false
	}
	trialSessions.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "Run your free search again to see your cover letter.")
		return
	}
	if haveCached {
		writeJSON(w, http.StatusOK, trialLetterResponse{Preview: cached.Preview(), Company: job.Company, Title: job.Title})
		return
	}
	if !s.trialLetterLimiter.Allow("trial-letter:" + clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "You've seen your free cover letter previews for today.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	started := time.Now()
	letter, err := s.applicationKit.WriteCoverLetter(ctx, tailor.CoverLetterRequest{
		ResumeText:     resumeText,
		JobDescription: job.Description,
		JobTitle:       job.Title,
		Company:        job.Company,
	})
	if err != nil {
		log.Printf("[trial-letter] FAILED after=%s: %v", time.Since(started), err)
		writeError(w, http.StatusBadGateway, "Jane couldn't write this letter just now. Try again in a minute.")
		return
	}
	log.Printf("[trial-letter] DONE after=%s", time.Since(started))
	trialSessions.mu.Lock()
	if current, ok := trialSessions.sessions[request.Token]; ok {
		current.letters[request.Index] = letter
	}
	trialSessions.mu.Unlock()
	writeJSON(w, http.StatusOK, trialLetterResponse{Preview: letter.Preview(), Company: job.Company, Title: job.Title})
}
