// Package tailor builds a job-specific application kit from a résumé.
//
// It is "truth-locked": the model may reorder and reword the candidate's own
// résumé lines to match a job's language, but every tailored bullet must cite
// the exact résumé line it came from, and the server discards any bullet whose
// source isn't in the résumé or that introduces numbers the candidate never
// wrote. Nothing is invented.
package tailor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const defaultModel = "gpt-5-mini"

type Client struct {
	APIKey  string
	BaseURL string
	Model   string
	HTTP    *http.Client
}

func New(apiKey string) *Client {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil
	}
	return &Client{
		APIKey:  apiKey,
		BaseURL: "https://api.openai.com/v1",
		Model:   defaultModel,
		HTTP:    &http.Client{Timeout: 90 * time.Second},
	}
}

type Request struct {
	ResumeText     string
	JobDescription string
	JobTitle       string
	Company        string
	// Requirements Jane couldn't confirm, used for interview prep.
	Gaps []string
}

type Bullet struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

type InterviewQuestion struct {
	Question string `json:"question"`
	Approach string `json:"approach"`
}

type Kit struct {
	Headline     string              `json:"headline"`
	Summary      string              `json:"summary"`
	Bullets      []Bullet            `json:"bullets"`
	Keywords     []string            `json:"keywords"`
	Interview    []InterviewQuestion `json:"interview"`
	DroppedLines int                 `json:"droppedLines"`
}

var ErrUnavailable = errors.New("application kits are not available right now")

const schema = `{
  "type":"object",
  "properties":{
    "headline":{"type":"string"},
    "summary":{"type":"string"},
    "bullets":{"type":"array","items":{"type":"object","properties":{
      "text":{"type":"string"},
      "source":{"type":"string"}
    },"required":["text","source"],"additionalProperties":false}},
    "keywords":{"type":"array","items":{"type":"string"}},
    "interview":{"type":"array","items":{"type":"object","properties":{
      "question":{"type":"string"},
      "approach":{"type":"string"}
    },"required":["question","approach"],"additionalProperties":false}}
  },
  "required":["headline","summary","bullets","keywords","interview"],
  "additionalProperties":false
}`

func buildPrompt(request Request) string {
	var b strings.Builder
	b.WriteString(`You are Jane, an honest career coach. Tailor this candidate's résumé to the job below.

STRICT RULES (truth lock):
- Use ONLY facts stated in the résumé. Never add skills, tools, employers, titles, degrees, licenses, certifications, years, numbers, percentages, or results that are not in the résumé.
- Each bullet must be a rewrite of ONE résumé line. Put that original line, copied EXACTLY character for character, in "source".
- You may reorder, tighten, and use the job's vocabulary when the résumé line genuinely describes the same thing.
- Never put a number in a bullet unless the same number appears in its source line.
- Pick the 5 to 8 résumé lines most relevant to this job, most relevant first.
- "headline": one line, the candidate's real title/specialty aimed at this role.
- "summary": 2 to 3 sentences, plain and specific, only from the résumé. No buzzwords like "results-driven" or "passionate".
- "keywords": up to 10 terms from the job posting that the résumé truly supports.
- "interview": 3 likely interview questions for this job, prioritizing the gaps listed, each with a short honest approach that draws on the candidate's real experience. Never suggest claiming experience they don't have.

`)
	if request.JobTitle != "" || request.Company != "" {
		fmt.Fprintf(&b, "JOB: %s at %s\n\n", request.JobTitle, request.Company)
	}
	b.WriteString("JOB POSTING:\n")
	b.WriteString(request.JobDescription)
	b.WriteString("\n\nRÉSUMÉ:\n")
	b.WriteString(request.ResumeText)
	if len(request.Gaps) > 0 {
		b.WriteString("\n\nGAPS JANE FOUND (requirements not shown on the résumé):\n- ")
		b.WriteString(strings.Join(request.Gaps, "\n- "))
	}
	return b.String()
}

func (c *Client) Build(ctx context.Context, request Request) (Kit, error) {
	if c == nil {
		return Kit{}, ErrUnavailable
	}

	payload, err := json.Marshal(map[string]any{
		"model": c.Model,
		"input": []map[string]string{{"role": "user", "content": buildPrompt(request)}},
		"text": map[string]any{"format": map[string]any{
			"type": "json_schema", "name": "application_kit", "strict": true,
			"schema": json.RawMessage(schema),
		}},
	})
	if err != nil {
		return Kit{}, err
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/responses", bytes.NewReader(payload))
	if err != nil {
		return Kit{}, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := c.HTTP.Do(httpRequest)
	if err != nil {
		return Kit{}, fmt.Errorf("tailor request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return Kit{}, err
	}
	if response.StatusCode != http.StatusOK {
		return Kit{}, fmt.Errorf("tailor: openai status %d", response.StatusCode)
	}

	var decoded struct {
		Output []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return Kit{}, fmt.Errorf("tailor: decode response: %w", err)
	}
	var text string
	for _, output := range decoded.Output {
		for _, content := range output.Content {
			if content.Type == "output_text" && content.Text != "" {
				text = content.Text
			}
		}
	}
	if text == "" {
		return Kit{}, errors.New("tailor: empty response")
	}

	var kit Kit
	if err := json.Unmarshal([]byte(text), &kit); err != nil {
		return Kit{}, fmt.Errorf("tailor: decode kit: %w", err)
	}
	return Enforce(kit, request.ResumeText), nil
}

var numberPattern = regexp.MustCompile(`\d+(?:[.,]\d+)*`)

func normalize(text string) string {
	text = strings.ToLower(text)
	replacer := strings.NewReplacer("•", " ", "–", "-", "—", "-", "’", "'", "‘", "'", "“", `"`, "”", `"`, " ", " ")
	return strings.Join(strings.Fields(replacer.Replace(text)), " ")
}

// Enforce applies the truth lock: bullets must cite a real résumé line and may
// not contain numbers absent from that line. The summary and headline may not
// contain numbers absent from the résumé.
func Enforce(kit Kit, resumeText string) Kit {
	resume := normalize(resumeText)
	resumeNumbers := numberSet(resume)

	kept := make([]Bullet, 0, len(kit.Bullets))
	for _, bullet := range kit.Bullets {
		source := normalize(bullet.Source)
		text := strings.TrimSpace(bullet.Text)
		if len(source) < 12 || text == "" || !strings.Contains(resume, source) {
			kit.DroppedLines++
			continue
		}
		sourceNumbers := numberSet(source)
		invented := false
		for number := range numberSet(normalize(text)) {
			if _, ok := sourceNumbers[number]; !ok {
				invented = true
				break
			}
		}
		if invented {
			kit.DroppedLines++
			continue
		}
		kept = append(kept, Bullet{Text: text, Source: strings.TrimSpace(bullet.Source)})
	}
	kit.Bullets = kept

	if hasInventedNumber(kit.Summary, resumeNumbers) {
		kit.Summary = ""
	}
	if hasInventedNumber(kit.Headline, resumeNumbers) {
		kit.Headline = ""
	}

	keywords := kit.Keywords[:0]
	for _, keyword := range kit.Keywords {
		if keyword = strings.TrimSpace(keyword); keyword != "" {
			keywords = append(keywords, keyword)
		}
		if len(keywords) == 10 {
			break
		}
	}
	kit.Keywords = keywords
	if len(kit.Interview) > 3 {
		kit.Interview = kit.Interview[:3]
	}
	return kit
}

func numberSet(text string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, match := range numberPattern.FindAllString(text, -1) {
		set[strings.ReplaceAll(match, ",", "")] = struct{}{}
	}
	return set
}

func hasInventedNumber(text string, allowed map[string]struct{}) bool {
	for number := range numberSet(normalize(text)) {
		if _, ok := allowed[number]; !ok {
			return true
		}
	}
	return false
}
