package tailor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const resume = `Jane Doe, RN
• Provided care for 6 patients per shift on a 32-bed med-surg unit
• Educated patients and families on discharge plans
• Charted in Epic EHR`

func TestEnforceDropsUntraceableAndInventedNumbers(t *testing.T) {
	kit := Enforce(Kit{
		Summary: "Med-surg RN with 10 years of experience.",
		Bullets: []Bullet{
			{Text: "Managed care for 6 patients per shift on a 32-bed unit", Source: "Provided care for 6 patients per shift on a 32-bed med-surg unit"},
			{Text: "Cut readmissions 20% through discharge teaching", Source: "Educated patients and families on discharge plans"},
			{Text: "Led ICU rapid response team", Source: "Led ICU rapid response team"},
			{Text: "Documented care in Epic", Source: "Charted in Epic EHR"},
		},
	}, resume)

	if len(kit.Bullets) != 2 || kit.DroppedLines != 2 {
		t.Fatalf("expected 2 kept and 2 dropped, got %+v", kit)
	}
	if kit.Summary != "" {
		t.Fatalf("summary with invented '10 years' should be removed, got %q", kit.Summary)
	}
}

func TestBuildCallsOpenAIAndEnforces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/responses") || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		kit, _ := json.Marshal(Kit{
			Headline: "Med-Surg Registered Nurse",
			Bullets:  []Bullet{{Text: "Charted all care in Epic EHR", Source: "Charted in Epic EHR"}},
		})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": []any{map[string]any{"content": []any{map[string]any{"type": "output_text", "text": string(kit)}}}},
		})
	}))
	defer server.Close()

	client := New("test-key")
	client.BaseURL = server.URL
	kit, err := client.Build(context.Background(), Request{ResumeText: resume, JobDescription: "RN job"})
	if err != nil {
		t.Fatal(err)
	}
	if kit.Headline != "Med-Surg Registered Nurse" || len(kit.Bullets) != 1 {
		t.Fatalf("unexpected kit %+v", kit)
	}
}

func TestNilClientUnavailable(t *testing.T) {
	var client *Client
	if _, err := client.Build(context.Background(), Request{}); err != ErrUnavailable {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestEnforceFeedbackReplacesInventedNumbers(t *testing.T) {
	feedback := EnforceFeedback(Feedback{
		StrongerAnswer: "I cared for 6 patients a shift and cut falls by 40%.",
		Strengths:      []string{"Clear example", " "},
	}, "Care for 6 patients per shift")
	if !strings.Contains(feedback.StrongerAnswer, "6 patients") || strings.Contains(feedback.StrongerAnswer, "40") {
		t.Fatalf("unexpected answer %q", feedback.StrongerAnswer)
	}
	if len(feedback.Strengths) != 1 {
		t.Fatalf("blank strengths should be dropped: %v", feedback.Strengths)
	}
}

func TestQuestionsCallsOpenAI(t *testing.T) {
	var prompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		prompt = string(raw)
		payload, _ := json.Marshal(PracticeQuestions{Questions: []InterviewQuestion{{Question: "Tell me about a launch you led.", Approach: "Use the Q3 campaign."}}})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": []any{map[string]any{"content": []any{map[string]any{"type": "output_text", "text": string(payload)}}}},
		})
	}))
	defer server.Close()
	client := New("test-key")
	client.BaseURL = server.URL
	questions, err := client.Questions(context.Background(), "résumé", "job", "Marketing Manager", QuestionOptions{Stage: "behavioral", Exclude: []string{"Why this company?"}})
	if err != nil || len(questions) != 1 || questions[0].Question == "" {
		t.Fatalf("unexpected %v %+v", err, questions)
	}
	if !strings.Contains(prompt, "behavioral questions") || !strings.Contains(prompt, "Why this company?") {
		t.Fatalf("prompt is missing the interview type or the questions to avoid: %s", prompt)
	}
}
