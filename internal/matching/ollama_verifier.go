package matching

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultOllamaVerifierModel = "qwen3:4b"

// OllamaEvidenceVerifier uses a generation-capable Ollama model to determine
// whether retrieved candidate evidence actually demonstrates a requirement.
//
// Embedding similarity is intentionally not provided to this verifier.
// Retrieval answers "what evidence should we inspect?"; this verifier answers
// "what does that evidence actually demonstrate?"
type OllamaEvidenceVerifier struct {
	BaseURL string
	Model   string
	Client  *http.Client
}

// NewOllamaEvidenceVerifier creates the default local evidence verifier.
func NewOllamaEvidenceVerifier() *OllamaEvidenceVerifier {
	return &OllamaEvidenceVerifier{
		BaseURL: defaultOllamaBaseURL,
		Model:   defaultOllamaVerifierModel,
		Client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type ollamaGenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
	Think  bool   `json:"think"`
	Format string `json:"format"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
}

type evidenceVerificationResponse struct {
	Result string `json:"result"`
}

func (v *OllamaEvidenceVerifier) Verify(
	requirement string,
	evidence string,
) evidenceResult {
	requirement = strings.TrimSpace(requirement)
	evidence = strings.TrimSpace(evidence)

	if requirement == "" || evidence == "" {
		return missingEvidence()
	}

	result, err := v.verifyWithModel(requirement, evidence)
	if err != nil {
		// If the model is unavailable or returns an invalid response, fall back
		// conservatively to deterministic verification. Retrieval similarity is
		// never converted directly into evidence support.
		return verifyEvidenceDeterministically(requirement, evidence)
	}

	return result
}

func (v *OllamaEvidenceVerifier) verifyWithModel(
	requirement string,
	evidence string,
) (evidenceResult, error) {
	baseURL := strings.TrimRight(v.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}

	model := strings.TrimSpace(v.Model)
	if model == "" {
		model = defaultOllamaVerifierModel
	}

	prompt := buildEvidenceVerificationPrompt(requirement, evidence)

	payload, err := json.Marshal(ollamaGenerateRequest{
		Model:  model,
		Prompt: prompt,
		Stream: false,
		Think:  false,
		Format: "json",
	})
	if err != nil {
		return missingEvidence(), fmt.Errorf(
			"encode ollama verification request: %w",
			err,
		)
	}

	req, err := http.NewRequest(
		http.MethodPost,
		baseURL+"/api/generate",
		bytes.NewReader(payload),
	)
	if err != nil {
		return missingEvidence(), fmt.Errorf(
			"create ollama verification request: %w",
			err,
		)
	}

	req.Header.Set("Content-Type", "application/json")

	client := v.Client
	if client == nil {
		client = &http.Client{
			Timeout: 60 * time.Second,
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return missingEvidence(), fmt.Errorf(
			"call ollama verifier: %w",
			err,
		)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return missingEvidence(), fmt.Errorf(
			"read ollama verification response: %w",
			err,
		)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return missingEvidence(), fmt.Errorf(
			"ollama verifier returned HTTP %d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}

	var generated ollamaGenerateResponse
	if err := json.Unmarshal(body, &generated); err != nil {
		return missingEvidence(), fmt.Errorf(
			"decode ollama generate response: %w",
			err,
		)
	}

	responseText := strings.TrimSpace(generated.Response)
	if responseText == "" {
		return missingEvidence(), fmt.Errorf(
			"ollama verifier returned an empty response",
		)
	}

	var decision evidenceVerificationResponse
	if err := json.Unmarshal(
		[]byte(responseText),
		&decision,
	); err != nil {
		return missingEvidence(), fmt.Errorf(
			"decode ollama verification decision: %w",
			err,
		)
	}

	switch strings.ToLower(strings.TrimSpace(decision.Result)) {
	case "supported":
		return supportedEvidence(), nil

	case "partial":
		return partialEvidence(), nil

	case "missing":
		return missingEvidence(), nil

	default:
		return missingEvidence(), fmt.Errorf(
			"ollama verifier returned unknown result %q",
			decision.Result,
		)
	}
}

func buildEvidenceVerificationPrompt(
	requirement string,
	evidence string,
) string {
	return fmt.Sprintf(`You are an evidence verifier for a job-matching system.

Determine whether the candidate evidence demonstrates the job requirement.

Use meaning, not exact keyword overlap. Equivalent professional language can
demonstrate the same capability.

Rules:
- SUPPORTED: the evidence clearly demonstrates the capability or experience.
- PARTIAL: the evidence demonstrates a meaningful part of the requirement but
  does not establish the complete requirement.
- MISSING: the evidence does not demonstrate the requirement.
- Do not infer credentials, licenses, degrees, certifications, years of
  experience, or facts that are not stated in the evidence.
- Related experience is not automatically sufficient.
- Judge only the supplied requirement and evidence.
- Be conservative when the evidence is ambiguous.

Return JSON only in exactly this shape:
{"result":"supported"}

The value must be exactly one of:
supported
partial
missing

Requirement:
%s

Candidate evidence:
%s
`, requirement, evidence)
}
