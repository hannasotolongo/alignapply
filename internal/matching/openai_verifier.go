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

const defaultOpenAIVerifierModel = "gpt-5-mini"

type OpenAIEvidenceVerifier struct {
	APIKey  string
	BaseURL string
	Model   string
	Client  *http.Client
}

func NewOpenAIEvidenceVerifier(apiKey string) *OpenAIEvidenceVerifier {
	return &OpenAIEvidenceVerifier{
		APIKey:  strings.TrimSpace(apiKey),
		BaseURL: "https://api.openai.com/v1",
		Model:   defaultOpenAIVerifierModel,
		Client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type openAIResponseRequest struct {
	Model string               `json:"model"`
	Input []openAIInputMessage `json:"input"`
	Text  openAITextConfig     `json:"text"`
}

type openAIInputMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAITextConfig struct {
	Format openAIJSONSchemaFormat `json:"format"`
}

type openAIJSONSchemaFormat struct {
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

type openAIResponse struct {
	Output []struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
}

func (v *OpenAIEvidenceVerifier) Verify(
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
		// Preserve the existing conservative fallback behavior.
		return verifyEvidenceDeterministically(
			requirement,
			evidence,
		)
	}

	return result
}

func (v *OpenAIEvidenceVerifier) verifyWithModel(
	requirement string,
	evidence string,
) (evidenceResult, error) {
	apiKey := strings.TrimSpace(v.APIKey)
	if apiKey == "" {
		return missingEvidence(), fmt.Errorf(
			"openai API key is not configured",
		)
	}

	baseURL := strings.TrimRight(v.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	model := strings.TrimSpace(v.Model)
	if model == "" {
		model = defaultOpenAIVerifierModel
	}

	schema := json.RawMessage(`{
		"type":"object",
		"properties":{
			"result":{
				"type":"string",
				"enum":["supported","partial","missing"]
			}
		},
		"required":["result"],
		"additionalProperties":false
	}`)

	payload, err := json.Marshal(openAIResponseRequest{
		Model: model,
		Input: []openAIInputMessage{
			{
				Role: "user",
				Content: buildEvidenceVerificationPrompt(
					requirement,
					evidence,
				),
			},
		},
		Text: openAITextConfig{
			Format: openAIJSONSchemaFormat{
				Type:   "json_schema",
				Name:   "evidence_verification",
				Strict: true,
				Schema: schema,
			},
		},
	})
	if err != nil {
		return missingEvidence(), fmt.Errorf(
			"encode openai verification request: %w",
			err,
		)
	}

	req, err := http.NewRequest(
		http.MethodPost,
		baseURL+"/responses",
		bytes.NewReader(payload),
	)
	if err != nil {
		return missingEvidence(), fmt.Errorf(
			"create openai verification request: %w",
			err,
		)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := v.Client
	if client == nil {
		client = &http.Client{
			Timeout: 60 * time.Second,
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return missingEvidence(), fmt.Errorf(
			"call openai verifier: %w",
			err,
		)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(
		io.LimitReader(resp.Body, 2<<20),
	)
	if err != nil {
		return missingEvidence(), fmt.Errorf(
			"read openai verification response: %w",
			err,
		)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return missingEvidence(), fmt.Errorf(
			"openai verifier returned HTTP %d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}

	var decoded openAIResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return missingEvidence(), fmt.Errorf(
			"decode openai verification response: %w",
			err,
		)
	}

	responseText := ""

	for _, output := range decoded.Output {
		for _, content := range output.Content {
			if content.Type == "output_text" &&
				strings.TrimSpace(content.Text) != "" {
				responseText = strings.TrimSpace(content.Text)
				break
			}
		}

		if responseText != "" {
			break
		}
	}

	if responseText == "" {
		return missingEvidence(), fmt.Errorf(
			"openai verifier returned an empty response",
		)
	}

	var decision evidenceVerificationResponse

	if err := json.Unmarshal(
		[]byte(responseText),
		&decision,
	); err != nil {
		return missingEvidence(), fmt.Errorf(
			"decode openai verification decision: %w",
			err,
		)
	}

	switch strings.ToLower(
		strings.TrimSpace(decision.Result),
	) {
	case "supported":
		return supportedEvidence(), nil

	case "partial":
		return partialEvidence(), nil

	case "missing":
		return missingEvidence(), nil

	default:
		return missingEvidence(), fmt.Errorf(
			"openai verifier returned unknown result %q",
			decision.Result,
		)
	}
}
