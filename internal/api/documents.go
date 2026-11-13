package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/hannasotolongo/casemade-backend/internal/docs"
	"github.com/hannasotolongo/casemade-backend/internal/tailor"
)

// Tailored résumé and downloadable files.
//
//	POST /api/v1/tailored-resume          (Pro) rewrites the résumé for one job
//	POST /api/v1/documents/resume         (Pro) lays a tailored résumé out as PDF or Word
//	POST /api/v1/documents/cover-letter   (signed in) lays a cover letter out as PDF or Word
//
// The document endpoints only format what the browser sends back; they never
// call the model, so they are cheap and can't spend anyone's allowance.

// POST /api/v1/tailored-resume (Pro)
func (s *Server) handleTailoredResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	if s.applicationKit == nil {
		writeError(w, http.StatusServiceUnavailable, "Tailored résumés aren't available right now. Please try again later.")
		return
	}
	if !s.kitLimiter.Allow("resume:" + user.ID) {
		writeError(w, http.StatusTooManyRequests, "You've tailored a lot of résumés this hour. Give Jane a few minutes.")
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
		writeError(w, http.StatusBadRequest, "This posting doesn't include enough detail to tailor your résumé.")
		return
	case len(request.JobDescription) > fitCheckMaxJobChars:
		request.JobDescription = request.JobDescription[:fitCheckMaxJobChars]
	}

	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	resume, err := s.applicationKit.WriteTailoredResume(ctx, tailor.CoverLetterRequest{
		ResumeText:     request.ResumeText,
		JobDescription: request.JobDescription,
		JobTitle:       strings.TrimSpace(request.JobTitle),
		Company:        strings.TrimSpace(request.Company),
	})
	if err != nil {
		if errors.Is(err, tailor.ErrUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "Tailored résumés aren't available right now.")
			return
		}
		log.Printf("[tailored-resume] FAILED user=%s after=%s: %v", user.ID, time.Since(started), err)
		writeError(w, http.StatusBadGateway, "Jane couldn't tailor your résumé just now. Try again in a minute.")
		return
	}
	log.Printf("[tailored-resume] DONE user=%s after=%s roles=%d", user.ID, time.Since(started), len(resume.Experience))
	writeJSON(w, http.StatusOK, resume)
}

type resumeDocumentRequest struct {
	Resume  tailor.TailoredResume `json:"resume"`
	Company string                `json:"company,omitempty"`
}

// POST /api/v1/documents/resume?format=pdf|docx (Pro)
func (s *Server) handleResumeDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if _, ok := s.requireSessionUser(w, r); !ok {
		return
	}
	format, ok := documentFormat(w, r)
	if !ok {
		return
	}
	var request resumeDocumentRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	resume := capTailoredResume(request.Resume)
	if resume.Name == "" && len(resume.Experience) == 0 {
		writeError(w, http.StatusBadRequest, "There's no résumé to download yet.")
		return
	}
	name := documentFileName(resume.Name, "Resume", request.Company, format)
	if format == "pdf" {
		writeDocument(w, name, format, docs.ResumePDF(resume))
		return
	}
	data, err := docs.ResumeDOCX(resume)
	if err != nil {
		log.Printf("[documents] resume docx: %v", err)
		writeError(w, http.StatusInternalServerError, "Couldn't build the Word file. Try the PDF.")
		return
	}
	writeDocument(w, name, format, data)
}

type letterDocumentRequest struct {
	Name       string   `json:"name"`
	Contact    []string `json:"contact,omitempty"`
	Greeting   string   `json:"greeting"`
	Paragraphs []string `json:"paragraphs"`
	Closing    string   `json:"closing"`
	Company    string   `json:"company,omitempty"`
	ResumeText string   `json:"resumeText,omitempty"`
}

// POST /api/v1/documents/cover-letter?format=pdf|docx (signed in)
func (s *Server) handleCoverLetterDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if _, ok := s.requireSessionUser(w, r); !ok {
		return
	}
	format, ok := documentFormat(w, r)
	if !ok {
		return
	}
	var request letterDocumentRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	letter := docs.Letter{
		Name:       clip(request.Name, 80),
		Contact:    capList(request.Contact, 4, 120),
		Date:       time.Now(),
		Greeting:   clip(request.Greeting, 160),
		Paragraphs: capList(request.Paragraphs, 8, 2000),
		Closing:    clip(request.Closing, 60),
	}
	if len(letter.Contact) == 0 {
		letter.Contact = contactFromResume(request.ResumeText)
	}
	if len(letter.Paragraphs) == 0 {
		writeError(w, http.StatusBadRequest, "There's no cover letter to download yet.")
		return
	}
	name := documentFileName(letter.Name, "Cover_Letter", request.Company, format)
	if format == "pdf" {
		writeDocument(w, name, format, docs.LetterPDF(letter))
		return
	}
	data, err := docs.LetterDOCX(letter)
	if err != nil {
		log.Printf("[documents] letter docx: %v", err)
		writeError(w, http.StatusInternalServerError, "Couldn't build the Word file. Try the PDF.")
		return
	}
	writeDocument(w, name, format, data)
}

func documentFormat(w http.ResponseWriter, r *http.Request) (string, bool) {
	switch format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format"))); format {
	case "", "pdf":
		return "pdf", true
	case "docx", "word":
		return "docx", true
	default:
		writeError(w, http.StatusBadRequest, "format must be pdf or docx")
		return "", false
	}
}

func writeDocument(w http.ResponseWriter, fileName, format string, data []byte) {
	contentType := "application/pdf"
	if format == "docx" {
		contentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// documentFileName builds names like Maya_Torres_Resume_Microsoft.pdf using
// only ASCII letters, digits and underscores.
func documentFileName(person, kind, company, format string) string {
	parts := []string{}
	if p := fileNamePart(person, 40); p != "" {
		parts = append(parts, p)
	}
	parts = append(parts, kind)
	if c := fileNamePart(company, 40); c != "" {
		parts = append(parts, c)
	}
	return strings.Join(parts, "_") + "." + format
}

var accentFold = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ä", "a", "ã", "a", "å", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "ö", "o", "õ", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ñ", "n", "ç", "c",
	"Á", "A", "É", "E", "Í", "I", "Ó", "O", "Ú", "U", "Ñ", "N", "Ç", "C",
)

func fileNamePart(s string, max int) string {
	s = accentFold.Replace(s)
	var b strings.Builder
	lastUnderscore := true
	for _, r := range s {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
			lastUnderscore = false
		} else if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
		if b.Len() >= max {
			break
		}
	}
	return strings.Trim(b.String(), "_")
}

var (
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	phonePattern = regexp.MustCompile(`(?:\+?1[\s.\-]?)?\(?\d{3}\)?[\s.\-]?\d{3}[\s.\-]?\d{4}`)
)

// contactFromResume finds an email and phone number near the top of a résumé.
func contactFromResume(resumeText string) []string {
	head := resumeText
	if len(head) > 1500 {
		head = head[:1500]
	}
	var contact []string
	if email := emailPattern.FindString(head); email != "" {
		contact = append(contact, email)
	}
	if phone := phonePattern.FindString(head); phone != "" {
		contact = append(contact, strings.TrimSpace(phone))
	}
	return contact
}

func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	for len(cut) > 0 && !utf8Start(cut[len(cut)-1]) {
		cut = cut[:len(cut)-1]
	}
	if len(cut) > 0 {
		cut = cut[:len(cut)-1]
	}
	return strings.TrimSpace(cut)
}

// utf8Start reports whether b begins a UTF-8 sequence (or is ASCII).
func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

func capList(items []string, maxItems, maxLen int) []string {
	out := []string{}
	for _, item := range items {
		if item = clip(item, maxLen); item != "" {
			out = append(out, item)
		}
		if len(out) == maxItems {
			break
		}
	}
	return out
}

// capTailoredResume bounds what the browser sends back before layout.
func capTailoredResume(resume tailor.TailoredResume) tailor.TailoredResume {
	resume.Name = clip(resume.Name, 80)
	resume.Contact = capList(resume.Contact, 5, 120)
	resume.Summary = clip(resume.Summary, 900)
	if len(resume.Experience) > 8 {
		resume.Experience = resume.Experience[:8]
	}
	for i := range resume.Experience {
		role := &resume.Experience[i]
		role.Title, role.Company = clip(role.Title, 120), clip(role.Company, 120)
		role.Location, role.Dates = clip(role.Location, 80), clip(role.Dates, 60)
		role.Bullets = capList(role.Bullets, 8, 400)
	}
	if len(resume.Education) > 5 {
		resume.Education = resume.Education[:5]
	}
	for i := range resume.Education {
		school := &resume.Education[i]
		school.Degree, school.School = clip(school.Degree, 160), clip(school.School, 160)
		school.Dates, school.Details = clip(school.Dates, 60), clip(school.Details, 300)
	}
	resume.Skills = capList(resume.Skills, 40, 80)
	if len(resume.Sections) > 4 {
		resume.Sections = resume.Sections[:4]
	}
	for i := range resume.Sections {
		resume.Sections[i].Title = clip(resume.Sections[i].Title, 60)
		resume.Sections[i].Items = capList(resume.Sections[i].Items, 8, 300)
	}
	return resume
}
