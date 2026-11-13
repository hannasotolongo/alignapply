package tailor

import (
	"strings"
	"testing"
)

func TestFeedbackPromptNamesTheStage(t *testing.T) {
	prompt := buildFeedbackPrompt(FeedbackRequest{Question: "Tell me about yourself", Answer: "I have worked in retail for five years.", Stage: "phone"})
	if !strings.Contains(prompt, "first introduction phone screen") {
		t.Fatal("phone stage should be named in the coaching prompt")
	}
	if strings.Contains(buildFeedbackPrompt(FeedbackRequest{Stage: "mixed"}), "INTERVIEW STAGE") {
		t.Fatal("a mixed practice set has no single stage")
	}
	if !strings.Contains(buildFeedbackPrompt(FeedbackRequest{Stage: "design"}), "system design interview") {
		t.Fatal("design stage should be named in the coaching prompt")
	}
	for _, stage := range []string{"phone", "first", "behavioral", "role", "design", "panel", "final", "ask", "mixed"} {
		if InterviewStages[stage] == "" {
			t.Errorf("no question focus for stage %q", stage)
		}
	}
}
