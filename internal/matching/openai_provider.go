package matching

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultOpenAIEmbeddingBaseURL = "https://api.openai.com/v1"
	defaultOpenAIEmbeddingModel   = "text-embedding-3-small"
)

type OpenAISemanticProvider struct {
	APIKey  string
	BaseURL string
	Model   string
	Client  *http.Client

	mu    sync.RWMutex
	cache map[string][]float64
}

func NewOpenAISemanticProvider(apiKey string) *OpenAISemanticProvider {
	return &OpenAISemanticProvider{
		APIKey:  strings.TrimSpace(apiKey),
		BaseURL: defaultOpenAIEmbeddingBaseURL,
		Model:   defaultOpenAIEmbeddingModel,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
		cache: make(map[string][]float64),
	}
}

type openAIEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openAIEmbeddingItem struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type openAIEmbeddingResponse struct {
	Data []openAIEmbeddingItem `json:"data"`
}

func (p *OpenAISemanticProvider) Similarity(
	requirement string,
	evidence string,
) (float64, error) {
	scores, err := p.Similarities(requirement, []string{evidence})
	if err != nil {
		return 0, err
	}

	if len(scores) != 1 {
		return 0, fmt.Errorf(
			"openai returned %d similarity scores; expected 1",
			len(scores),
		)
	}

	return scores[0], nil
}

func (p *OpenAISemanticProvider) Similarities(
	requirement string,
	evidence []string,
) ([]float64, error) {
	requirement = normalizeSemanticText(requirement)
	if requirement == "" {
		return make([]float64, len(evidence)), nil
	}

	texts := make([]string, 0, len(evidence)+1)
	texts = append(texts, requirement)

	for _, item := range evidence {
		texts = append(texts, normalizeSemanticText(item))
	}

	embeddings, err := p.embeddings(texts)
	if err != nil {
		return nil, err
	}

	if len(embeddings) != len(texts) {
		return nil, fmt.Errorf(
			"openai returned %d embeddings; expected %d",
			len(embeddings),
			len(texts),
		)
	}

	requirementEmbedding := embeddings[0]
	scores := make([]float64, len(evidence))

	for i := range evidence {
		if len(embeddings[i+1]) == 0 {
			continue
		}

		scores[i] = cosineSimilarity(
			requirementEmbedding,
			embeddings[i+1],
		)
	}

	return scores, nil
}

func (p *OpenAISemanticProvider) embeddings(
	texts []string,
) ([][]float64, error) {
	results := make([][]float64, len(texts))

	missingTexts := make([]string, 0, len(texts))
	missingIndexes := make([]int, 0, len(texts))

	for i, text := range texts {
		text = normalizeSemanticText(text)

		if text == "" {
			continue
		}

		if embedding, ok := p.cachedEmbedding(text); ok {
			results[i] = embedding
			continue
		}

		missingTexts = append(missingTexts, text)
		missingIndexes = append(missingIndexes, i)
	}

	if len(missingTexts) == 0 {
		return results, nil
	}

	fetched, err := p.fetchEmbeddings(missingTexts)
	if err != nil {
		return nil, err
	}

	if len(fetched) != len(missingTexts) {
		return nil, fmt.Errorf(
			"openai returned %d embeddings for %d inputs",
			len(fetched),
			len(missingTexts),
		)
	}

	for i, embedding := range fetched {
		if len(embedding) == 0 {
			return nil, errors.New(
				"openai returned an empty embedding",
			)
		}

		index := missingIndexes[i]
		results[index] = embedding

		p.storeEmbedding(missingTexts[i], embedding)
	}

	return results, nil
}

func (p *OpenAISemanticProvider) fetchEmbeddings(
	texts []string,
) ([][]float64, error) {
	apiKey := strings.TrimSpace(p.APIKey)
	if apiKey == "" {
		return nil, errors.New("openai API key is not configured")
	}

	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultOpenAIEmbeddingBaseURL
	}

	model := strings.TrimSpace(p.Model)
	if model == "" {
		model = defaultOpenAIEmbeddingModel
	}

	payload, err := json.Marshal(openAIEmbeddingRequest{
		Model: model,
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf(
			"encode openai embedding request: %w",
			err,
		)
	}

	req, err := http.NewRequest(
		http.MethodPost,
		baseURL+"/embeddings",
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create openai embedding request: %w",
			err,
		)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := p.Client
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf(
			"call openai embeddings: %w",
			err,
		)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf(
			"read openai embedding response: %w",
			err,
		)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"openai embeddings returned HTTP %d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}

	var decoded openAIEmbeddingResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf(
			"decode openai embedding response: %w",
			err,
		)
	}

	if len(decoded.Data) == 0 {
		return nil, errors.New(
			"openai returned no embeddings",
		)
	}

	results := make([][]float64, len(decoded.Data))

	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(results) {
			return nil, fmt.Errorf(
				"openai returned invalid embedding index %d",
				item.Index,
			)
		}

		results[item.Index] = item.Embedding
	}

	return results, nil
}

func (p *OpenAISemanticProvider) cachedEmbedding(
	text string,
) ([]float64, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.cache == nil {
		return nil, false
	}

	embedding, ok := p.cache[text]
	return embedding, ok
}

func (p *OpenAISemanticProvider) storeEmbedding(
	text string,
	embedding []float64,
) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cache == nil {
		p.cache = make(map[string][]float64)
	}

	p.cache[text] = embedding
}
