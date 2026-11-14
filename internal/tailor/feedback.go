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
	b.WriteString(`You are Jane, an honest interview coach sitting next to the candidate. They just practiced an answer out loud. Tell them what an interviewer for this job would think, and how to make it better.

RULES:
- "strengths": 1 to 3 specific things that work. Quote their words or name the exact part ("Opening with the 40-minute deploy time gets attention"). No general praise like "Great answer" or "Good job".
- "improvements": 2 to 4 changes, each one sentence they can act on in the next try: what to add, cut or move, and why the interviewer cares. Point to this job's requirements where it helps. If the answer doesn't answer the question, say so first.
- "strongerAnswer": their answer, rewritten the way they'd actually say it out loud, 90 to 160 words, first person, contractions are fine. Use ONLY facts from their answer and résumé. Never invent employers, titles, tools, numbers, results or credentials. If a strong answer needs a detail they haven't given, write a bracketed placeholder like [the result you saw] instead of making it up.
`)
	b.WriteString(plainWritingRules)
	b.WriteString("\n")
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
	return c.structuredWith(ctx, c.Model, "low", prompt, name, schema)
}

// structuredWith is structured with a chosen model and reasoning effort.
func (c *Client) structuredWith(ctx context.Context, model, effort, prompt, name, schema string) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"model":     model,
		"reasoning": map[string]string{"effort": effort},
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
	feedback.Strengths = plainAll(trim(feedback.Strengths, 3))
	feedback.Improvements = plainAll(trim(feedback.Improvements, 4))
	feedback.StrongerAnswer = Plain(feedback.StrongerAnswer)
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
      "approach":{"type":"string"},
      "level":{"type":"string","enum":["Easy","Medium","Hard"]},
      "tests":{"type":"string"},
      "fromResume":{"type":"boolean"}
    },"required":["question","approach","level","tests","fromResume"],"additionalProperties":false}}
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
// Every answer is spoken, so no stage asks the candidate to write code.
var InterviewStages = map[string]string{
	"mixed":       `8 questions for the whole interview loop, in the order they usually come: one phone screen question, then behavioral, role-specific and technical or design questions for this job, and a final-round question`,
	"phone":       `8 questions for a first introduction phone screen with a recruiter: tell me about yourself, why this role and company, motivation for a move, availability and pay expectations, and a quick check on the must-have requirements`,
	"first":       `8 questions a hiring manager would ask: walking through the résumé, how the candidate works, how their past work fits the job's main responsibilities, what they would own in the first 90 days, and one about the biggest gap versus the requirements`,
	"behavioral":  `8 behavioral questions ("tell me about a time…") tied to this job, such as teamwork, conflict, failure, leadership, prioritizing and handling pressure`,
	"role":        `8 technical deep dive questions answered out loud, with no code to write: explain a concept the job relies on, debug a realistic scenario step by step, talk through how you would approach a problem and its trade-offs, and go deep on a technical project from the résumé`,
	"design":      `8 system design questions like the ones this employer asks: design a real system close to what this company builds, each with scale numbers to work toward. The approach for each walks through how to structure the answer: clarify requirements, estimate scale, sketch the APIs and data model, design the main components, then scaling, reliability and trade-offs`,
	"panel":       `8 questions a panel of several interviewers (the hiring manager, a peer and someone from a partner team) would ask: collaboration across teams, role skills, a behavioral question, and how the candidate handles different stakeholders`,
	"final":       `8 questions a hiring manager or senior leader would ask in a final round: impact, judgment, how the candidate would approach the first 90 days, culture fit, and why they're the best choice`,
	"exec":        `8 questions a senior leader or "bar raiser" would ask: judgment under ambiguity, the hardest decision the candidate has made, raising the bar for the team, and long-term thinking`,
	"case":        `8 case interview questions answered out loud: business problems such as why a metric dropped, whether to enter a market or how to size an opportunity, each with enough numbers to reason with`,
	"product":     `8 product sense questions: improve an existing product this company makes, design a product for a specific user, pick metrics for success and make prioritization calls`,
	"situational": `8 situational questions ("what would you do if…") about realistic problems in this job`,
	"walkthrough": `8 questions that walk through the candidate's résumé in depth: each role, why they moved, and detailed follow-ups on the biggest projects`,
	"values":      `8 culture and values questions built around what this company says it values, asking for real examples from the candidate's work`,
	"clinical":    `8 clinical scenario questions for healthcare roles: patient situations, safety and priorities, escalation and teamwork on the unit`,
	"teaching":    `8 teaching interview questions: classroom management, lesson planning, differentiation, working with parents and a short sample lesson to describe out loud`,
	"sales":       `8 sales interview questions, including a short role-play to talk through: discovery, handling objections, closing, and the candidate's numbers`,
	"service":     `8 customer service questions, including how the candidate would talk a frustrated customer down, step by step`,
	"finance":     `8 finance and accounting questions answered out loud: concepts like the three financial statements, and judgment calls the role makes`,
	"data":        `8 data and analytics questions answered out loud: metrics, A/B tests, explaining an analysis and diagnosing a change in a number`,
	"marketing":   `8 marketing questions: campaign ideas, channel strategy, budgets and explaining results`,
	"leadership":  `8 leadership questions for managers: managing low performers, hiring, building a team, giving feedback and handling conflict`,
	"change":      `8 questions for a career changer: why the switch, what carries over from past work and how they have prepared for this field`,
	"salary":      `8 lines a hiring manager or recruiter says in a salary negotiation, such as the first offer, "what are your expectations?" and pushback on a counteroffer, for the candidate to answer out loud`,
	"ask":         `8 smart questions the CANDIDATE should ask the interviewer about this role, team and company. For each, the approach explains why it's a good question and what to listen for in the answer`,
}

// InterviewStageNames say what each interview stage is, for Jane's coaching.
var InterviewStageNames = map[string]string{
	"phone":       "first introduction phone screen with a recruiter",
	"first":       "interview with the hiring manager",
	"behavioral":  "behavioral interview",
	"role":        "technical deep dive interview, answered out loud",
	"design":      "system design interview",
	"panel":       "panel interview",
	"final":       "final-round interview",
	"exec":        "interview with a senior leader",
	"case":        "case interview",
	"product":     "product sense interview",
	"situational": "situational interview",
	"walkthrough": "résumé walkthrough",
	"values":      "culture and values interview",
	"clinical":    "clinical scenario interview",
	"teaching":    "teaching interview",
	"sales":       "sales interview",
	"service":     "customer service interview",
	"finance":     "finance interview",
	"data":        "data and analytics interview",
	"marketing":   "marketing interview",
	"leadership":  "leadership interview",
	"change":      "career change interview",
	"salary":      "salary negotiation",
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
	fmt.Fprintf(&b, "You are Jane, an interview coach who has sat on hiring panels for jobs like this one. Predict %s.\n\n", focus)
	b.WriteString(`QUESTIONS:
- Write each question the way a real interviewer at this company would say it out loud, about this job's actual work: name the product area, tools, customers or situations from the posting. Avoid questions that would fit any job ("What are your strengths?") unless this is a recruiter phone screen.
- Mix difficulty: at least one question should be hard for THIS candidate, aimed at the biggest gap between the posting and the résumé.
- Set level to Easy, Medium or Hard for this candidate. Set tests to 2 to 4 words on what the question checks (for example "scale, trade-offs"). Set fromResume to true only for questions built on a specific project or job from the résumé; write 1 or 2 of those.
- These are likely questions for this kind of interview at this company, never claimed to be its real or leaked questions.
APPROACH (1 to 2 sentences, to the candidate as "you"):
- Name the specific example from their résumé to use and the one thing they must make clear. If the résumé has nothing that fits, say how to answer honestly (a related example, or what they'd do), never to claim experience they don't have.
`)
	b.WriteString(plainWritingRules)
	b.WriteString("\n")
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
	if strings.TrimSpace(jobDescription) == "" {
		b.WriteString("JOB POSTING: none given. Base the questions on what this kind of role at this company usually covers, using public knowledge of the company's products, scale and stated values.\n\nCANDIDATE RÉSUMÉ:\n" + resumeText)
	} else {
		b.WriteString("JOB POSTING:\n" + jobDescription + "\n\nCANDIDATE RÉSUMÉ:\n" + resumeText)
	}
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
	for i := range result.Questions {
		result.Questions[i].Question = strings.TrimSpace(strings.NewReplacer("—", ", ", " – ", ", ").Replace(result.Questions[i].Question))
		result.Questions[i].Approach = Plain(result.Questions[i].Approach)
		result.Questions[i].Tests = strings.TrimSpace(result.Questions[i].Tests)
	}
	return result.Questions, nil
}
