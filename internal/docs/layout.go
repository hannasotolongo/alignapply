package docs

import (
	"strings"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/tailor"
)

// Letter is a cover letter ready to lay out.
type Letter struct {
	Name       string
	Contact    []string
	Date       time.Time
	Greeting   string
	Paragraphs []string
	Closing    string
}

func joinNonEmpty(parts []string, sep string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, sep)
}

func roleHeading(role tailor.ResumeRole) string {
	if role.Company == "" {
		return role.Title
	}
	if role.Title == "" {
		return role.Company
	}
	return role.Title + ", " + role.Company
}

func schoolHeading(school tailor.ResumeSchool) string {
	return joinNonEmpty([]string{school.Degree, school.School}, ", ")
}

// ResumePDF lays out a tailored résumé as a one-column PDF.
func ResumePDF(resume tailor.TailoredResume) []byte {
	doc := newPDF(50)
	width := doc.width()
	const body = 10.0
	const leading = 13.6

	doc.y -= 22
	doc.text(doc.margin, bold, 22, 0.07, resume.Name)
	if contact := joinNonEmpty(resume.Contact, "  ·  "); contact != "" {
		doc.paragraph(doc.margin, width, regular, 9.5, 14, 0.33, contact)
	}

	heading := func(title string) {
		doc.need(40)
		doc.gap(18)
		doc.text(doc.margin, bold, 9.5, 0.07, strings.ToUpper(title))
		doc.rule(4)
		doc.gap(4)
	}
	bullets := func(items []string) {
		for _, item := range items {
			lines := wrap(item, regular, body, width-14)
			doc.need(leading * float64(len(lines)))
			for i, line := range lines {
				doc.y -= leading
				if i == 0 {
					doc.text(doc.margin+3, regular, body, 0.1, "•")
				}
				doc.text(doc.margin+14, regular, body, 0.1, line)
			}
		}
	}
	titleLine := func(left, right, below string) {
		doc.need(leading * 2)
		doc.gap(leading + 3)
		rightWidth := 0.0
		if right != "" {
			rightWidth = regular.measure(right, body) + 12
			doc.text(pageWidth-doc.margin-regular.measure(right, body), regular, body, 0.33, right)
		}
		lines := wrap(left, bold, body+0.5, width-rightWidth)
		for i, line := range lines {
			if i > 0 {
				doc.y -= leading
			}
			doc.text(doc.margin, bold, body+0.5, 0.07, line)
		}
		if below != "" {
			doc.paragraph(doc.margin, width, regular, 9.5, 13, 0.33, below)
		}
	}

	if resume.Summary != "" {
		heading("Summary")
		doc.paragraph(doc.margin, width, regular, body, leading, 0.1, resume.Summary)
	}
	if len(resume.Experience) > 0 {
		heading("Experience")
		for _, role := range resume.Experience {
			titleLine(roleHeading(role), role.Dates, role.Location)
			doc.gap(2)
			bullets(role.Bullets)
			doc.gap(4)
		}
	}
	if len(resume.Education) > 0 {
		heading("Education")
		for _, school := range resume.Education {
			titleLine(schoolHeading(school), school.Dates, school.Details)
		}
	}
	if len(resume.Skills) > 0 {
		heading("Skills")
		doc.paragraph(doc.margin, width, regular, body, leading, 0.1, strings.Join(resume.Skills, ", "))
	}
	for _, section := range resume.Sections {
		heading(section.Title)
		bullets(section.Items)
	}
	return doc.bytes()
}

// ResumeDOCX lays out a tailored résumé as a Word document.
func ResumeDOCX(resume tailor.TailoredResume) ([]byte, error) {
	var doc docxDoc
	doc.add(para{runs: []run{{text: resume.Name, bold: true, size: 40, color: "121214"}}, after: 40})
	if contact := joinNonEmpty(resume.Contact, "  ·  "); contact != "" {
		doc.add(para{runs: []run{{text: contact, size: 19, color: "55555C"}}, after: 120})
	}
	heading := func(title string) {
		doc.add(para{runs: []run{{text: strings.ToUpper(title), bold: true, size: 19, color: "121214"}}, before: 220, after: 80, bottomRule: true, keepNext: true})
	}
	titleLine := func(left, right, below string) {
		text := left
		if right != "" {
			text += "\t" + right
		}
		doc.add(para{runs: []run{{text: left, bold: true, size: 21}, {text: strings.TrimPrefix(text, left), size: 20, color: "55555C"}}, before: 120, after: 0, rightTab: right != "", keepNext: true})
		if below != "" {
			doc.add(para{runs: []run{{text: below, size: 19, color: "55555C"}}, after: 40, keepNext: true})
		}
	}
	if resume.Summary != "" {
		heading("Summary")
		doc.add(para{runs: []run{{text: resume.Summary}}, after: 40})
	}
	if len(resume.Experience) > 0 {
		heading("Experience")
		for _, role := range resume.Experience {
			titleLine(roleHeading(role), role.Dates, role.Location)
			for _, bullet := range role.Bullets {
				doc.add(para{runs: []run{{text: bullet}}, bullet: true, after: 30})
			}
		}
	}
	if len(resume.Education) > 0 {
		heading("Education")
		for _, school := range resume.Education {
			titleLine(schoolHeading(school), school.Dates, school.Details)
		}
	}
	if len(resume.Skills) > 0 {
		heading("Skills")
		doc.add(para{runs: []run{{text: strings.Join(resume.Skills, ", ")}}})
	}
	for _, section := range resume.Sections {
		heading(section.Title)
		for _, item := range section.Items {
			doc.add(para{runs: []run{{text: item}}, bullet: true, after: 30})
		}
	}
	return doc.bytes()
}

// LetterPDF lays out a cover letter as a PDF.
func LetterPDF(letter Letter) []byte {
	doc := newPDF(64)
	width := doc.width()
	const size = 11.0
	const leading = 16.5

	doc.y -= 22
	if letter.Name != "" {
		doc.text(doc.margin, bold, 22, 0.07, letter.Name)
	}
	if contact := joinNonEmpty(letter.Contact, "  ·  "); contact != "" {
		doc.paragraph(doc.margin, width, regular, 9.5, 14, 0.33, contact)
	}
	if letter.Name != "" || len(letter.Contact) > 0 {
		doc.gap(10)
		doc.rule(0)
		doc.gap(14)
	}
	date := letter.Date
	if date.IsZero() {
		date = time.Now()
	}
	doc.paragraph(doc.margin, width, regular, size, leading, 0.1, date.Format("January 2, 2006"))
	doc.gap(leading)
	doc.paragraph(doc.margin, width, regular, size, leading, 0.1, letter.Greeting)
	for _, paragraph := range letter.Paragraphs {
		doc.gap(leading * 0.7)
		doc.paragraph(doc.margin, width, regular, size, leading, 0.1, paragraph)
	}
	doc.gap(leading)
	doc.paragraph(doc.margin, width, regular, size, leading, 0.1, letter.Closing)
	if letter.Name != "" {
		doc.paragraph(doc.margin, width, regular, size, leading, 0.1, letter.Name)
	}
	return doc.bytes()
}

// LetterDOCX lays out a cover letter as a Word document.
func LetterDOCX(letter Letter) ([]byte, error) {
	var doc docxDoc
	if letter.Name != "" {
		doc.add(para{runs: []run{{text: letter.Name, bold: true, size: 40, color: "121214"}}, after: 40})
	}
	if contact := joinNonEmpty(letter.Contact, "  ·  "); contact != "" {
		doc.add(para{runs: []run{{text: contact, size: 19, color: "55555C"}}, after: 120, bottomRule: true})
	}
	date := letter.Date
	if date.IsZero() {
		date = time.Now()
	}
	text := func(s string, after int) { doc.add(para{runs: []run{{text: s, size: 22}}, before: 0, after: after}) }
	text(date.Format("January 2, 2006"), 240)
	text(letter.Greeting, 200)
	for _, paragraph := range letter.Paragraphs {
		text(paragraph, 200)
	}
	text(letter.Closing, 0)
	if letter.Name != "" {
		text(letter.Name, 0)
	}
	return doc.bytes()
}
