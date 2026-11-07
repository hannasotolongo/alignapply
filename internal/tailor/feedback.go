package tailor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type FeedbackRequest struct {
	Question       string
	Answer         string
	ResumeText     string
	JobDescription string
	JobTitle       string
	// Stage is the interview stage practiced (see InterviewStages).
	Stage string
}

// Feedback is Jane's coaching on one practice interview answer.
type Feedback struct {
	Strengths      []string `json:"strengths"`
	Improvements   []string `json:"improvements"`
	StrongerAnswer string   `json:"strongerAnswer"`
}

const feedbackSchema = `{
  "type":"object",
  "properties":{
    "strengths":{"type":"array","items":{"type":"string"}},
    "improvements":{"type":"array","items":{"type":"string"}},
    "strongerAnswer":{"type":"string"}
  },
  "required":["strengths","improvements","strongerAnswer"],
  "additionalProperties":false
}`

func buildFeedbackPrompt(request FeedbackRequest) string {
	var b strings.Builder
	b.WriteString(`You are Jane, an honest, encouraging interview coach. The candidate practiced an answer to an interview question. Coach them.

RULES:
- "strengths": 1 to 3 specific things that work in their answer. Quote or name the part.
- "improvements": 2 to 4 concrete, actionable changes (structure such as situation, action, result; missing specifics; tie back to the job's requirements; trim filler). Be direct, kind, and specific.
- "strongerAnswer": a rewritten answer of 90 to 160 words in first person, spoken style. Use ONLY facts from the candidate's answer and résumé. Never invent employers, titles, tools, numbers, results, or credentials. If a strong answer needs a detail they haven't given, write it as a bracketed placeholder like [the result you saw] instead of making it up.

`)
	if request.JobTitle != "" {
		fmt.Fprintf(&b, "JOB: %s\n\n", request.JobTitle)
	}
	if stage, ok := InterviewStageNames[request.Stage]; ok {
		fmt.Fprintf(&b, "INTERVIEW STAGE: %s. Coach for what interviewers listen for at this stage, and keep the stronger answer at the length and depth this stage calls for.\n\n", stage)
	}
	b.WriteString("JOB POSTING (for context):\n")
	b.WriteString(request.JobDescription)
	b.WriteString("\n\nCANDIDATE RÉSUMÉ:\n")
	b.WriteString(request.ResumeText)
	b.WriteString("\n\nINTERVIEW QUESTION:\n")
	b.WriteString(request.Question)
	b.WriteString("\n\nCANDIDATE'S PRACTICE ANSWER:\n")
	b.WriteString(request.Answer)
	return b.String()
}

// structured calls the Responses API with a strict JSON schema and returns
// the model's JSON text.
func (c *Client) structured(ctx context.Context, prompt, name, schema string) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"model":     c.Model,
		"reasoning": map[string]string{"effort": "low"},
		"input":     []map[string]string{{"role": "user", "content": prompt}},
		"text": map[string]any{"format": map[string]any{
			"type": "json_schema", "name": name, "strict": true,
			"schema": json.RawMessage(schema),
		}},
	})
	if err != nil {
		return "", err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/responses", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := c.HTTP.Do(httpRequest)
	if err != nil {
		return "", fmt.Errorf("%s request: %w", name, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		snippet := strings.Join(strings.Fields(string(body)), " ")
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}
		return "", fmt.Errorf("%s: openai status %d: %s", name, response.StatusCode, snippet)
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
		return "", fmt.Errorf("%s: decode response: %w", name, err)
	}
	for _, output := range decoded.Output {
		for _, content := range output.Content {
			if content.Type == "output_text" && content.Text != "" {
				return content.Text, nil
			}
		}
	}
	return "", errors.New(name + ": empty response")
}

func (c *Client) Feedback(ctx context.Context, request FeedbackRequest) (Feedback, error) {
	if c == nil {
		return Feedback{}, ErrUnavailable
	}
	text, err := c.structured(ctx, buildFeedbackPrompt(request), "answer_feedback", feedbackSchema)
	if err != nil {
		return Feedback{}, err
	}
	var feedback Feedback
	if err := json.Unmarshal([]byte(text), &feedback); err != nil {
		return Feedback{}, fmt.Errorf("answer_feedback: decode: %w", err)
	}
	return EnforceFeedback(feedback, request.ResumeText+"\n"+request.Answer), nil
}

// EnforceFeedback keeps the rewritten answer honest: any number that isn't
// in the candidate's own résumé or answer becomes a placeholder.
func EnforceFeedback(feedback Feedback, candidateText string) Feedback {
	allowed := numberSet(normalize(candidateText))
	feedback.StrongerAnswer = numberPattern.ReplaceAllStringFunc(feedback.StrongerAnswer, func(match string) string {
		if _, ok := allowed[strings.ReplaceAll(match, ",", "")]; ok {
			return match
		}
		return "[your number]"
	})
	trim := func(items []string, max int) []string {
		kept := items[:0]
		for _, item := range items {
			if item = strings.TrimSpace(item); item != "" {
				kept = append(kept, item)
			}
			if len(kept) == max {
				break
			}
		}
		return kept
	}
	feedback.Strengths = trim(feedback.Strengths, 3)
	feedback.Improvements = trim(feedback.Improvements, 4)
	feedback.StrongerAnswer = strings.TrimSpace(feedback.StrongerAnswer)
	return feedback
}

// PracticeQuestions is the model output for a practice set.
type PracticeQuestions struct {
	Questions []InterviewQuestion `json:"questions"`
}

const questionsSchema = `{
  "type":"object",
  "properties":{
    "questions":{"type":"array","items":{"type":"object","properties":{
      "question":{"type":"string"},
      "approach":{"type":"string"}
    },"required":["question","approach"],"additionalProperties":false}}
  },
  "required":["questions"],
  "additionalProperties":false
}`

// QuestionOptions shapes a practice set.
type QuestionOptions struct {
	// Stage is the kind of interview to practice for (see InterviewStages).
	Stage string
	// Exclude lists questions already asked, so a "More questions" set
	// brings new ones.
	Exclude []string
}

// InterviewStages are the interview types a candidate can practice for.
var InterviewStages = map[string]string{
	"mixed":      `the 6 questions this employer is most likely to ask: a mix of behavioral ("tell me about a time…"), role-specific, and one about the candidate's biggest gap versus the requirements`,
	"phone":      `6 questions for a first introduction phone screen with a recruiter: tell me about yourself, why this role and company, motivation for a move, availability and pay expectations, and a quick check on the must-have requirements`,
	"first":      `6 questions for a first-round interview with the hiring manager: walking through the résumé, why this role, how the candidate's past work fits the job's main responsibilities, one behavioral question, and one about the biggest gap versus the requirements`,
	"behavioral": `6 behavioral questions ("tell me about a time…") tied to this job's requirements, such as teamwork, conflict, failure, leadership, prioritizing and handling pressure`,
	"role":       `6 technical or skills questions that test the specific skills, tools, methods and knowledge this job requires, the way a technical interviewer or skills assessment would`,
	"panel":      `6 questions a panel of several interviewers (the hiring manager, a peer and someone from a partner team) would ask: collaboration across teams, role skills, a behavioral question, and how the candidate handles different stakeholders`,
	"final":      `6 questions a hiring manager or senior leader would ask in a final round: impact, judgment, how the candidate would approach the first 90 days, culture fit, and why they're the best choice`,
	"ask":        `6 smart questions the CANDIDATE should ask the interviewer about this role, team and company. For each, the approach explains why it's a good question and what to listen for in the answer`,
}

// InterviewStageNames say what each interview stage is, for Jane's coaching.
var InterviewStageNames = map[string]string{
	"phone":      "first introduction phone screen with a recruiter",
	"first":      "first-round interview with the hiring manager",
	"behavioral": "behavioral interview",
	"role":       "technical or skills interview",
	"panel":      "panel interview",
	"final":      "final-round interview",
}

// Questions predicts likely interview questions for a job, with an honest
// approach for each based on the candidate's real experience.
func (c *Client) Questions(ctx context.Context, resumeText, jobDescription, jobTitle string, options QuestionOptions) ([]InterviewQuestion, error) {
	if c == nil {
		return nil, ErrUnavailable
	}
	focus, ok := InterviewStages[options.Stage]
	if !ok {
		focus = InterviewStages["mixed"]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You are Jane, an interview coach. Predict %s. For each, give a 1 to 2 sentence approach that draws on the candidate's REAL experience from the résumé. Never suggest claiming experience they don't have.\n\n", focus)
	if len(options.Exclude) > 0 {
		b.WriteString("The candidate has already practiced these questions. Write different ones; don't repeat or reword them:\n")
		for _, question := range options.Exclude {
			b.WriteString("- " + question + "\n")
		}
		b.WriteString("\n")
	}
	if jobTitle != "" {
		fmt.Fprintf(&b, "JOB: %s\n\n", jobTitle)
	}
	b.WriteString("JOB POSTING:\n" + jobDescription + "\n\nCANDIDATE RÉSUMÉ:\n" + resumeText)
	text, err := c.structured(ctx, b.String(), "interview_questions", questionsSchema)
	if err != nil {
		return nil, err
	}
	var result PracticeQuestions
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("interview_questions: decode: %w", err)
	}
	if len(result.Questions) > 8 {
		result.Questions = result.Questions[:8]
	}
	return result.Questions, nil
}
