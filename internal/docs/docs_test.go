package docs

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/tailor"
)

var sampleResume = tailor.TailoredResume{
	Name:    "Maya Torres",
	Contact: []string{"Tampa, FL", "maya.torres@email.com", "(813) 555-0142"},
	Summary: "Full-stack software engineer with 3 years building React front ends and Node.js and PostgreSQL APIs for a payments product used by 4,000 small businesses.",
	Experience: []tailor.ResumeRole{
		{Title: "Software Engineer", Company: "Bright Ledger", Location: "Tampa, FL", Dates: "2023 – Present", Bullets: []string{
			"Designed REST APIs in Node.js and PostgreSQL for invoices and payments; cut page load time from 3.2s to 1.1s.",
			"Built and maintain the customer billing dashboard in React and TypeScript, used by 4,000 small businesses.",
			"Raised integration test coverage from 40% to 78% and set up continuous integration in GitHub Actions (café résumé test).",
		}},
		{Title: "Software Engineering Intern", Company: "Tampa General Hospital IT", Dates: "Summer 2022", Bullets: []string{
			"Built an internal Python tool that matched nurse shift requests, saving schedulers about 5 hours a week.",
		}},
	},
	Education: []tailor.ResumeSchool{{Degree: "B.S. Computer Science", School: "University of South Florida", Dates: "2023"}},
	Skills:    []string{"React", "TypeScript", "Node.js", "PostgreSQL", "Python", "AWS (S3, Lambda)"},
	Sections:  []tailor.ResumeExtra{{Title: "Certifications", Items: []string{"AWS Certified Cloud Practitioner"}}},
}

func TestResumePDF(t *testing.T) {
	pdf := ResumePDF(sampleResume)
	if !bytes.HasPrefix(pdf, []byte("%PDF-1.4")) || !bytes.Contains(pdf, []byte("%%EOF")) {
		t.Fatal("not a PDF")
	}
	if out := os.Getenv("DOCS_OUT"); out != "" {
		_ = os.WriteFile(out+"/resume.pdf", pdf, 0o644)
	}
}

func TestLetterFiles(t *testing.T) {
	letter := Letter{Name: "Maya Torres", Contact: []string{"Tampa, FL", "maya.torres@email.com"}, Date: time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC),
		Greeting: "Dear Microsoft Hiring Team,", Paragraphs: []string{
			"Microsoft is hiring a Software Engineer to build the services behind its web products, and that's the work I do at Bright Ledger (yes, with parentheses).",
			"That dashboard serves 4,000 small businesses, and I cut its slowest page from 3.2 seconds to 1.1 seconds.",
		}, Closing: "Sincerely,"}
	pdf := LetterPDF(letter)
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatal("not a PDF")
	}
	word, err := LetterDOCX(letter)
	if err != nil {
		t.Fatal(err)
	}
	checkDocx(t, word, "Dear Microsoft Hiring Team,")
	if out := os.Getenv("DOCS_OUT"); out != "" {
		_ = os.WriteFile(out+"/letter.pdf", pdf, 0o644)
		_ = os.WriteFile(out+"/letter.docx", word, 0o644)
	}
}

func TestResumeDOCX(t *testing.T) {
	word, err := ResumeDOCX(sampleResume)
	if err != nil {
		t.Fatal(err)
	}
	checkDocx(t, word, "Bright Ledger")
	if out := os.Getenv("DOCS_OUT"); out != "" {
		_ = os.WriteFile(out+"/resume.docx", word, 0o644)
	}
}

func checkDocx(t *testing.T, data []byte, want string) {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		rc, _ := file.Open()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(rc)
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("document.xml is missing %q", want)
		}
		return
	}
	t.Fatal("no word/document.xml")
}

func TestWrapAndEscape(t *testing.T) {
	lines := wrap("one two three four five six seven eight nine ten", regular, 10, 60)
	if len(lines) < 3 {
		t.Fatalf("wrap = %v", lines)
	}
	if got := pdfString(`a(b)c\é•`); got != `a\(b\)c\\\351\225` {
		t.Fatalf("pdfString = %q", got)
	}
}
