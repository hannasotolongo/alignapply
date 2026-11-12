package tailor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

// Cover letters use a stronger model than the rest of the kit: the letter is
// the one thing a hiring manager reads word for word. If that model isn't
// available on the account, the client's default model is used instead.
const (
	coverLetterModel  = "gpt-5"
	coverLetterEffort = "low"
)

// The model plans before it writes: what this job needs most, and which
// résumé facts prove the candidate has it. The plan is not shown to anyone;
// it keeps the letter about this job instead of a summary of the résumé.
const coverLetterSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["whatTheyNeed", "proof", "greeting", "paragraphs", "closing", "name"],
  "properties": {
    "whatTheyNeed": {"type": "array", "items": {"type": "string"}},
    "proof": {"type": "array", "items": {"type": "string"}},
    "greeting": {"type": "string"},
    "paragraphs": {"type": "array", "items": {"type": "string"}},
    "closing": {"type": "string"},
    "name": {"type": "string"}
  }
}`

const coverLetterExample = `Dear Riverside Health Hiring Team,

Your posting for a Patient Access Coordinator puts accuracy at the front desk first, and that is the part of the job I know best. For the last three years I have handled check-in, insurance verification and scheduling for a 12-provider family practice in Orlando.

The clinic sees about 90 patients a day, and I cut insurance denials from front-desk errors by building a simple pre-visit checklist the whole team now uses. When we moved to Epic last spring, I trained the other four front-desk staff on registration and scheduling, which is the same system your team uses.

I'd like to bring that same steadiness to Riverside's new outpatient center. Thank you for your time.

Sincerely,
Dana Brooks`

func buildCoverLetterPrompt(request CoverLetterRequest) string {
	var b strings.Builder
	b.WriteString(`You are an experienced recruiter writing a cover letter for a candidate. A busy hiring manager will read it in 30 seconds. Use ONLY facts from the candidate's résumé.

First, plan (these two fields are never shown to anyone):
- whatTheyNeed: the 3 things this posting cares about most, in plain words. Read the duties and requirements, not the company boilerplate.
- proof: for each need the résumé can actually back up, one line pairing the need with the specific résumé fact that proves it (with any number the résumé gives). Skip needs the résumé doesn't show.

Then write the letter from that plan:
- Paragraph 1 (2 sentences): name the role and company, and connect the candidate's single strongest matching experience to the main work this role does. Make the first sentence specific to this job; don't open with "I am applying for", "I am writing to" or "Your team needs".
- Paragraph 2 (2 to 3 sentences): the two strongest proofs. Each one says what the candidate did and what came of it, and makes clear why it matters for THIS job. Use the résumé's numbers when it has them.
- Paragraph 3 (1 to 2 sentences): one specific, plain-English reason this role fits the candidate, based on the work the posting describes (not internal team names, codenames or acronyms), then a short thank-you.
- 110 to 160 words in the three paragraphs. Every sentence under 28 words.
- Say each fact once. Never repeat a résumé line or copy it word for word; rephrase it as a sentence a person would say.
- Plain, confident, professional English, like a strong candidate writing their own letter. Vary how sentences start. Contractions are fine.
- Greeting: "Dear <Company> Hiring Team," when the company is known, otherwise "Dear Hiring Manager,". Closing: "Sincerely,". Name: the candidate's name from the top of the résumé, or "" if none.

Never:
- Invent anything: no employer, title, project, result, number, date, degree, tool or skill the résumé doesn't state. If the résumé doesn't show something the job wants, leave it out.
- Claim to know the company beyond what the posting says.
- List tools or skills in a row. Name at most four tools in the whole letter, written exactly as the résumé writes them (never swap a named tool for a vague one like "a relational database").
- Tack on a second "which" clause, or end a sentence with a claim about what the job "demands".
- Use these words or phrases, or anything like them: "I am excited", "thrilled", "passionate", "from day one", "I look forward to hearing from you", "dynamic", "fast-paced", "leverage", "synergy", "proven track record", "team player", "perfect fit", "great asset", "hands-on", "I am confident", "align with", "contribute", "immediately", "exactly what", "robust", "end-to-end", "drive impact".
- Use bullet points, exclamation marks, em dashes or headings.

Before you answer, check the letter against every rule above and fix it.

Here is an example of the tone and shape to aim for (a different job and person; don't reuse its facts or sentences):

`)
	b.WriteString(coverLetterExample)
	if request.JobTitle != "" {
		fmt.Fprintf(&b, "\n\nJOB TITLE: %s", request.JobTitle)
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
	prompt := buildCoverLetterPrompt(request)
	text, err := c.structuredWith(ctx, coverLetterModel, coverLetterEffort, prompt, "cover_letter", coverLetterSchema)
	if err != nil && coverLetterModel != c.Model {
		log.Printf("[cover-letter] %s failed, using %s: %v", coverLetterModel, c.Model, err)
		text, err = c.structured(ctx, prompt, "cover_letter", coverLetterSchema)
	}
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
	stockPhrasePattern = regexp.MustCompile(`(?i)\b(i am excited|i'm excited|i’m excited|thrilled|passionate|from day one|look forward to hearing|dynamic|fast-paced|leverag|synergy|proven track record|team player|perfect fit|great asset|i am confident|i'm confident)\b`)
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
