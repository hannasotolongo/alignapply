package tailor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// TailoredResume is the candidate's whole résumé, restructured and rewritten
// for one job, ready to lay out as a PDF or Word file. It is truth-locked like
// the rest of the kit: every employer, title, date, school and number comes
// from the original résumé.
type TailoredResume struct {
	Name       string         `json:"name"`
	Contact    []string       `json:"contact"`
	Summary    string         `json:"summary"`
	Experience []ResumeRole   `json:"experience"`
	Education  []ResumeSchool `json:"education"`
	Skills     []string       `json:"skills"`
	Sections   []ResumeExtra  `json:"sections"`
}

// ResumeRole is one job on the résumé.
type ResumeRole struct {
	Title    string   `json:"title"`
	Company  string   `json:"company"`
	Location string   `json:"location"`
	Dates    string   `json:"dates"`
	Bullets  []string `json:"bullets"`
}

// ResumeSchool is one degree, certificate or program.
type ResumeSchool struct {
	Degree  string `json:"degree"`
	School  string `json:"school"`
	Dates   string `json:"dates"`
	Details string `json:"details"`
}

// ResumeExtra is any other section the résumé has (certifications,
// projects, licenses, volunteering).
type ResumeExtra struct {
	Title string   `json:"title"`
	Items []string `json:"items"`
}

const tailoredResumeSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["name", "contact", "summary", "experience", "education", "skills", "sections"],
  "properties": {
    "name": {"type": "string"},
    "contact": {"type": "array", "items": {"type": "string"}},
    "summary": {"type": "string"},
    "experience": {"type": "array", "items": {
      "type": "object", "additionalProperties": false,
      "required": ["title", "company", "location", "dates", "bullets"],
      "properties": {
        "title": {"type": "string"}, "company": {"type": "string"}, "location": {"type": "string"},
        "dates": {"type": "string"}, "bullets": {"type": "array", "items": {"type": "string"}}
      }
    }},
    "education": {"type": "array", "items": {
      "type": "object", "additionalProperties": false,
      "required": ["degree", "school", "dates", "details"],
      "properties": {
        "degree": {"type": "string"}, "school": {"type": "string"}, "dates": {"type": "string"}, "details": {"type": "string"}
      }
    }},
    "skills": {"type": "array", "items": {"type": "string"}},
    "sections": {"type": "array", "items": {
      "type": "object", "additionalProperties": false,
      "required": ["title", "items"],
      "properties": {"title": {"type": "string"}, "items": {"type": "array", "items": {"type": "string"}}}
    }}
  }
}`

func buildTailoredResumePrompt(request CoverLetterRequest) string {
	var b strings.Builder
	b.WriteString(`Rewrite this candidate's résumé for the job below, as structured data for a clean one-column résumé. Use ONLY facts from the résumé.

Keep exactly as written: the candidate's name, every employer, job title, location, date, school, degree, certification and number. Keep every job and every degree; don't drop any.

Tailor it:
- contact: email, phone, city/state and links from the résumé, each as its own item. Leave out anything the résumé doesn't have.
- summary: 2 sentences. Who the candidate is and the experience that matters most for THIS job, in the posting's language where it truthfully fits.
- experience: most recent first. For each job, put the bullets most relevant to this posting first and rewrite them to lead with the action and the result, using the posting's words where they truthfully describe the same work. 2 to 5 bullets per job. Each bullet one sentence, under 30 words.
- skills: the résumé's skills, the ones this job asks for first. At most 16.
- sections: any other sections the résumé has (certifications, licenses, projects, volunteering, awards), with their items. Empty if none.

Never: invent an employer, title, project, result, number, date, degree, tool, skill or responsibility; add a skill the résumé doesn't list or show; use "I" or "my"; start a bullet with "Responsible for", "Helped" or an -ing word.

Bullets read like a strong human-written résumé: a plain past-tense verb (present tense for the current job), what was done, and the result with its number when the résumé has one. "Cut p95 latency from 900ms to 180ms with Redis caching", not "Leveraged caching solutions to drive performance improvements".
`)
	b.WriteString(plainWritingRules)
	if request.JobTitle != "" {
		fmt.Fprintf(&b, "\nJOB TITLE: %s", request.JobTitle)
	}
	if request.Company != "" {
		fmt.Fprintf(&b, "\nCOMPANY: %s", request.Company)
	}
	b.WriteString("\n\nJOB POSTING:\n")
	b.WriteString(truncate(request.JobDescription, 9000))
	b.WriteString("\n\nCANDIDATE RÉSUMÉ:\n")
	b.WriteString(truncate(request.ResumeText, 14000))
	return b.String()
}

// WriteTailoredResume rewrites the whole résumé for one job.
func (c *Client) WriteTailoredResume(ctx context.Context, request CoverLetterRequest) (TailoredResume, error) {
	if c == nil {
		return TailoredResume{}, ErrUnavailable
	}
	prompt := buildTailoredResumePrompt(request)
	text, err := c.structuredWith(ctx, coverLetterModel, coverLetterEffort, prompt, "tailored_resume", tailoredResumeSchema)
	if err != nil && coverLetterModel != c.Model {
		log.Printf("[tailored-resume] %s failed, using %s: %v", coverLetterModel, c.Model, err)
		text, err = c.structured(ctx, prompt, "tailored_resume", tailoredResumeSchema)
	}
	if err != nil {
		return TailoredResume{}, err
	}
	var resume TailoredResume
	if err := json.Unmarshal([]byte(text), &resume); err != nil {
		return TailoredResume{}, fmt.Errorf("tailored_resume: decode: %w", err)
	}
	resume = EnforceTailoredResume(resume, request.ResumeText)
	if resume.Name == "" && len(resume.Experience) == 0 && len(resume.Education) == 0 {
		return TailoredResume{}, fmt.Errorf("tailored_resume: nothing usable came back")
	}
	return resume, nil
}

// EnforceTailoredResume keeps the rewrite honest: a line with a number the
// original résumé doesn't have is dropped, and lists are trimmed and capped.
func EnforceTailoredResume(resume TailoredResume, originalResume string) TailoredResume {
	allowed := numberSet(normalize(originalResume))
	clean := func(text string) string {
		text = strings.TrimSpace(strings.NewReplacer("—", ", ", "\n", " ").Replace(text))
		return strings.TrimLeft(text, "-•*· ")
	}
	keep := func(items []string, limit int) []string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			item = clean(item)
			if item == "" || hasInventedNumber(item, allowed) {
				continue
			}
			out = append(out, item)
			if len(out) == limit {
				break
			}
		}
		return out
	}

	resume.Name = clean(resume.Name)
	resume.Contact = keep(resume.Contact, 6)
	resume.Summary = Plain(clean(resume.Summary))
	if hasInventedNumber(resume.Summary, allowed) {
		var kept []string
		for _, sentence := range sentencePattern.FindAllString(resume.Summary, -1) {
			if !hasInventedNumber(sentence, allowed) {
				kept = append(kept, strings.TrimSpace(sentence))
			}
		}
		resume.Summary = strings.Join(kept, " ")
	}

	roles := make([]ResumeRole, 0, len(resume.Experience))
	for _, role := range resume.Experience {
		role.Title, role.Company, role.Location, role.Dates = clean(role.Title), clean(role.Company), clean(role.Location), clean(role.Dates)
		role.Bullets = plainAll(keep(role.Bullets, 6))
		if role.Title == "" && role.Company == "" {
			continue
		}
		roles = append(roles, role)
	}
	resume.Experience = roles

	schools := make([]ResumeSchool, 0, len(resume.Education))
	for _, school := range resume.Education {
		school.Degree, school.School, school.Dates, school.Details = clean(school.Degree), clean(school.School), clean(school.Dates), clean(school.Details)
		if school.Degree == "" && school.School == "" {
			continue
		}
		schools = append(schools, school)
	}
	resume.Education = schools

	resume.Skills = keep(resume.Skills, 16)

	sections := make([]ResumeExtra, 0, len(resume.Sections))
	for _, section := range resume.Sections {
		section.Title = clean(section.Title)
		section.Items = keep(section.Items, 8)
		if section.Title != "" && len(section.Items) > 0 {
			sections = append(sections, section)
		}
	}
	resume.Sections = sections
	return resume
}
