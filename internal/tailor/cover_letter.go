package tailor

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// CoverLetter is a short cover letter for one job, written only from the
// candidate's own résumé.
type CoverLetter struct {
	Greeting   string   `json:"greeting"`
	Paragraphs []string `json:"paragraphs"`
	Closing    string   `json:"closing"`
	Name       string   `json:"name"`
}

// CoverLetterRequest is the résumé and job a letter is written for.
type CoverLetterRequest struct {
	ResumeText     string
	JobDescription string
	JobTitle       string
	Company        string
}

const coverLetterSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["greeting", "paragraphs", "closing", "name"],
  "properties": {
    "greeting": {"type": "string"},
    "paragraphs": {"type": "array", "items": {"type": "string"}},
    "closing": {"type": "string"},
    "name": {"type": "string"}
  }
}`

func buildCoverLetterPrompt(request CoverLetterRequest) string {
	var b strings.Builder
	b.WriteString(`Write a cover letter for this job, using ONLY facts from the candidate's résumé.

How it must read:
- Professional, plain and to the point. It should sound like a capable person wrote it, not an AI.
- Exactly 3 short paragraphs, 120 to 170 words in total.
  1. The role and company by name, and in one sentence who the candidate is and their strongest relevant experience.
  2. The best proof: one or two concrete things from their résumé that match what this job asks for most. Write them as sentences about the work, not a list of tools or skills. Mention at most two tools by name.
  3. One short sentence on why they'd do well in this role, then a brief thank-you.
- Greeting: "Dear Hiring Manager," (or "Dear <Company> Hiring Team," if the company is known).
- Closing: "Sincerely,". Name: the candidate's name from the top of the résumé, or "" if none.

Never:
- Invent anything: no employer, title, project, result, number, date, degree or skill the résumé doesn't state. If the résumé doesn't show something the job wants, leave it out; don't claim it.
- Use these phrases or anything like them: "I am excited", "I'm thrilled", "passionate", "I am confident I can contribute from day one", "I look forward to hearing from you", "dynamic", "fast-paced", "leverage", "synergy", "proven track record", "team player", "perfect fit", "I believe I would be a great asset".
- Use bullet points, lists, exclamation marks, em dashes or headings.
`)
	if request.JobTitle != "" {
		fmt.Fprintf(&b, "\nJOB TITLE: %s", request.JobTitle)
	}
	if request.Company != "" {
		fmt.Fprintf(&b, "\nCOMPANY: %s", request.Company)
	}
	b.WriteString("\n\nJOB POSTING:\n")
	b.WriteString(truncate(request.JobDescription, 9000))
	b.WriteString("\n\nCANDIDATE RÉSUMÉ:\n")
	b.WriteString(truncate(request.ResumeText, 12000))
	return b.String()
}

func truncate(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max]
}

// WriteCoverLetter writes a truth-locked cover letter for one job.
func (c *Client) WriteCoverLetter(ctx context.Context, request CoverLetterRequest) (CoverLetter, error) {
	if c == nil {
		return CoverLetter{}, ErrUnavailable
	}
	text, err := c.structured(ctx, buildCoverLetterPrompt(request), "cover_letter", coverLetterSchema)
	if err != nil {
		return CoverLetter{}, err
	}
	var letter CoverLetter
	if err := json.Unmarshal([]byte(text), &letter); err != nil {
		return CoverLetter{}, fmt.Errorf("cover_letter: decode: %w", err)
	}
	letter = EnforceCoverLetter(letter, request.ResumeText+"\n"+request.JobDescription+"\n"+request.Company+"\n"+request.JobTitle)
	if len(letter.Paragraphs) == 0 {
		return CoverLetter{}, fmt.Errorf("cover_letter: nothing usable came back")
	}
	return letter, nil
}

var (
	sentencePattern = regexp.MustCompile(`[^.!?]+[.!?]+["”’]?\s*|[^.!?]+$`)
	// Stock phrases that make a letter sound machine-written; a sentence
	// that still uses one is dropped.
	stockPhrasePattern = regexp.MustCompile(`(?i)\b(i am excited|i'm excited|i’m excited|thrilled|passionate|from day one|look forward to hearing|dynamic|fast-paced|leverag|synergy|proven track record|team player|perfect fit|great asset)\b`)
)

// EnforceCoverLetter keeps the letter honest and plain: a sentence with a
// number the source texts don't have, or a stock phrase, is removed, and
// list formatting is flattened.
func EnforceCoverLetter(letter CoverLetter, sourceText string) CoverLetter {
	allowed := numberSet(normalize(sourceText))
	paragraphs := make([]string, 0, len(letter.Paragraphs))
	for _, paragraph := range letter.Paragraphs {
		paragraph = strings.TrimSpace(strings.NewReplacer("—", ", ", "–", "-", "!", ".", "\n", " ").Replace(paragraph))
		paragraph = strings.TrimLeft(paragraph, "-•* ")
		// Most paragraphs pass whole; only a flagged one is split, and its
		// pieces are rejoined exactly as written.
		if hasInventedNumber(paragraph, allowed) || stockPhrasePattern.MatchString(paragraph) {
			var kept strings.Builder
			for _, sentence := range sentencePattern.FindAllString(paragraph, -1) {
				if strings.TrimSpace(sentence) == "" || hasInventedNumber(sentence, allowed) || stockPhrasePattern.MatchString(sentence) {
					continue
				}
				kept.WriteString(sentence)
			}
			paragraph = strings.TrimSpace(kept.String())
		}
		if paragraph != "" {
			paragraphs = append(paragraphs, paragraph)
		}
		if len(paragraphs) == 4 {
			break
		}
	}
	letter.Paragraphs = paragraphs
	letter.Greeting = strings.TrimSpace(letter.Greeting)
	if letter.Greeting == "" {
		letter.Greeting = "Dear Hiring Manager,"
	}
	letter.Closing = strings.TrimSpace(letter.Closing)
	if letter.Closing == "" {
		letter.Closing = "Sincerely,"
	}
	letter.Name = strings.TrimSpace(letter.Name)
	return letter
}

// Text is the whole letter as plain text.
func (letter CoverLetter) Text() string {
	parts := []string{letter.Greeting}
	parts = append(parts, letter.Paragraphs...)
	closing := letter.Closing
	if letter.Name != "" {
		closing += "\n" + letter.Name
	}
	parts = append(parts, closing)
	return strings.Join(parts, "\n\n")
}

// Preview is the greeting and the first two sentences: what free users see
// before they unlock the full letter.
func (letter CoverLetter) Preview() string {
	if len(letter.Paragraphs) == 0 {
		return letter.Greeting
	}
	sentences := sentencePattern.FindAllString(letter.Paragraphs[0], -1)
	if len(sentences) > 2 {
		sentences = sentences[:2]
	}
	return letter.Greeting + "\n\n" + strings.TrimSpace(strings.Join(sentences, ""))
}
