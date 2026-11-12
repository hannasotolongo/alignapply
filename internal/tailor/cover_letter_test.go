package tailor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testResume = `Jordan Lee
Software Engineer, Acme Corp (2021 - Present)
Built REST APIs in Go and Python serving 2M requests per day
Designed PostgreSQL schemas. Deployed services on AWS with Docker and Kubernetes.
Led code reviews for a team of 5 engineers.
B.S. Computer Science, 2019`

func TestEnforceCoverLetterKeepsItHonestAndPlain(t *testing.T) {
	letter := EnforceCoverLetter(CoverLetter{
		Paragraphs: []string{
			"I am applying for the Software Engineer position at Northwind Labs. Since 2019 I have built backend services.",
			"At Acme Corp, the APIs I build serve 2M requests a day. I cut latency by 40% last year.",
			"I am excited to join your dynamic team! Thank you for your consideration.",
		},
	}, testResume+"\nNorthwind Labs")

	text := strings.Join(letter.Paragraphs, "\n")
	if strings.Contains(text, "40%") {
		t.Error("a number not in the résumé should be removed")
	}
	if strings.Contains(strings.ToLower(text), "excited") || strings.Contains(text, "dynamic") {
		t.Error("stock phrases should be removed")
	}
	if !strings.Contains(text, "2M requests") || !strings.Contains(text, "Since 2019") {
		t.Errorf("true sentences should stay: %q", text)
	}
	if !strings.Contains(text, "Thank you for your consideration.") {
		t.Errorf("a clean sentence in a flagged paragraph should stay: %q", text)
	}
	if letter.Greeting != "Dear Hiring Manager," || letter.Closing != "Sincerely," {
		t.Errorf("defaults = %q / %q", letter.Greeting, letter.Closing)
	}
}

func TestCoverLetterPreviewShowsOnlyTheStart(t *testing.T) {
	letter := CoverLetter{Greeting: "Dear Hiring Manager,", Paragraphs: []string{
		"First sentence here. Second sentence here. Third sentence is private.",
		"Second paragraph is private.",
	}, Closing: "Sincerely,", Name: "Jordan Lee"}
	preview := letter.Preview()
	if !strings.Contains(preview, "Second sentence here.") || strings.Contains(preview, "Third") || strings.Contains(preview, "private") {
		t.Errorf("preview = %q", preview)
	}
	if text := letter.Text(); !strings.HasSuffix(text, "Sincerely,\nJordan Lee") {
		t.Errorf("text = %q", text)
	}
}

func TestWriteCoverLetter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		letter, _ := json.Marshal(CoverLetter{Greeting: "Dear Northwind Labs Hiring Team,", Paragraphs: []string{
			"I am applying for the Software Engineer role at Northwind Labs.",
			"At Acme Corp I build APIs that serve 2M requests a day.",
			"Thank you for your consideration.",
		}, Closing: "Sincerely,", Name: "Jordan Lee"})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": []any{map[string]any{"content": []any{map[string]any{"type": "output_text", "text": string(letter)}}}},
		})
	}))
	defer server.Close()
	client := New("test-key")
	client.BaseURL = server.URL
	letter, err := client.WriteCoverLetter(context.Background(), CoverLetterRequest{ResumeText: testResume, JobDescription: "Build APIs.", JobTitle: "Software Engineer", Company: "Northwind Labs"})
	if err != nil || len(letter.Paragraphs) != 3 || letter.Name != "Jordan Lee" {
		t.Fatalf("letter = %+v, err = %v", letter, err)
	}
}
